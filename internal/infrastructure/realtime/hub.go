package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/Judamaro/notyhub-golang/internal/application"
	"github.com/Judamaro/notyhub-golang/internal/application/connections"
	"github.com/Judamaro/notyhub-golang/internal/domain"
)

// HubOptions configures the WebSocket hub.
type HubOptions struct {
	// OriginPatterns are the hosts (host[:port], with optional wildcards) of the browser origins allowed to connect.
	// Requests without an Origin header (non-browser clients) and same-origin requests are always accepted.
	OriginPatterns []string
	PingInterval   time.Duration
	WriteTimeout   time.Duration
	SendQueueSize  int
	ReadLimit      int64
}

// DefaultHubOptions returns the default hub options.
func DefaultHubOptions() HubOptions {
	return HubOptions{PingInterval: 15 * time.Second, WriteTimeout: 10 * time.Second, SendQueueSize: 64, ReadLimit: 4096}
}

// Hub accepts WebSocket connections, registers them through the connection use cases and pushes notifications
// to them. It also implements application.NotificationDispatcher.
type Hub struct {
	opts       HubOptions
	register   application.CommandHandler[connections.RegisterCommand, connections.RegisterResponse]
	unregister application.CommandHandler[connections.UnregisterCommand, connections.ConnectionResponse]
	logger     *slog.Logger

	mu     sync.RWMutex
	conns  map[domain.ConnectionID]*conn
	closed bool
	wg     sync.WaitGroup
}

var _ application.NotificationDispatcher = (*Hub)(nil)

// NewHub creates a hub.
func NewHub(
	opts HubOptions,
	register application.CommandHandler[connections.RegisterCommand, connections.RegisterResponse],
	unregister application.CommandHandler[connections.UnregisterCommand, connections.ConnectionResponse],
	logger *slog.Logger,
) *Hub {
	return &Hub{opts: opts, register: register, unregister: unregister, logger: logger, conns: map[domain.ConnectionID]*conn{}}
}

// Serve upgrades an already authenticated and validated request to a WebSocket connection of the identity and
// serves it until it closes. A previous connection of the same user receives ConnectionReplaced and is closed.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, identity Identity) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		http.Error(w, "server shutting down", http.StatusServiceUnavailable)
		return
	}
	h.wg.Add(1)
	h.mu.Unlock()
	defer h.wg.Done()

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.opts.OriginPatterns})
	if err != nil {
		h.logger.WarnContext(r.Context(), "WebSocket upgrade failed", "error", err)
		return
	}
	ws.SetReadLimit(h.opts.ReadLimit)

	c := newConn(domain.ConnectionID(uuid.NewString()), ws, h.opts.SendQueueSize)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	h.mu.Lock()
	h.conns[c.id] = c
	h.mu.Unlock()

	c.enqueue(encodeFrame(EventConnected, ConnectionInfo{ConnectionID: c.id.String()}))

	registration, err := h.register.Handle(ctx, connections.RegisterCommand{
		ConnectionID: c.id,
		ClientID:     identity.ClientID,
		UserID:       identity.UserID,
		UserAgent:    r.UserAgent(),
	})
	if err != nil {
		h.logger.ErrorContext(ctx, "Connection registration failed", "connection_id", c.id, "error", err)
		c.close(websocket.StatusInternalError, "registration failed")
	} else if replaced := registration.ReplacedConnectionID; replaced != "" {
		h.logger.InfoContext(ctx, "Connection replaced", "replaced_connection_id", replaced, "connection_id", c.id,
			"client_id", identity.ClientID, "user_id", identity.UserID)
		h.closeReplaced(replaced)
	}

	go func() {
		h.readLoop(ctx, c)
		cancel()
	}()
	c.writeLoop(ctx, h.opts.PingInterval, h.opts.WriteTimeout)
	cancel()
	_ = ws.CloseNow()

	h.mu.Lock()
	if h.conns[c.id] == c {
		delete(h.conns, c.id)
	}
	h.mu.Unlock()

	// A replaced connection was already removed from the registry: Connection.NotFound is expected then.
	_, _ = h.unregister.Handle(context.WithoutCancel(ctx), connections.UnregisterCommand{ConnectionID: c.id})
}

// Send queues the notification on each connection open on this server.
func (h *Hub) Send(_ context.Context, ids []domain.ConnectionID, n domain.Notification) error {
	if len(ids) == 0 {
		return nil
	}

	msg := encodeFrame(EventReceiveNotification, NewNotificationMessage(n))

	h.mu.RLock()
	targets := make([]*conn, 0, len(ids))
	for _, id := range ids {
		if c, ok := h.conns[id]; ok {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range targets {
		c.enqueue(msg)
	}
	return nil
}

// Shutdown closes every connection with "going away" and waits until they are released or ctx ends.
func (h *Hub) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	h.closed = true
	for _, c := range h.conns {
		c.close(websocket.StatusGoingAway, "server shutting down")
	}
	h.mu.Unlock()

	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// closeReplaced notifies the replaced connection and closes it, if it is open on this server.
func (h *Hub) closeReplaced(id domain.ConnectionID) {
	h.mu.Lock()
	old, ok := h.conns[id]
	if ok {
		delete(h.conns, id)
	}
	h.mu.Unlock()

	if ok {
		old.enqueue(encodeFrame(EventConnectionReplaced, nil))
		old.close(StatusConnectionReplaced, "connection replaced")
	}
}

// readLoop reads client frames until the connection fails or closes. Clients may ask for their connection id.
func (h *Hub) readLoop(ctx context.Context, c *conn) {
	for {
		typ, data, err := c.ws.Read(ctx)
		if err != nil {
			if status := websocket.CloseStatus(err); status == -1 && !errors.Is(err, context.Canceled) {
				h.logger.DebugContext(ctx, "WebSocket read failed", "connection_id", c.id, "error", err)
			}
			return
		}
		if typ != websocket.MessageText {
			continue
		}

		var frame Frame
		if json.Unmarshal(data, &frame) != nil {
			continue
		}
		if frame.Event == MethodGetConnectionID {
			c.enqueue(encodeFrame(EventConnectionID, ConnectionInfo{ConnectionID: c.id.String()}))
		}
	}
}
