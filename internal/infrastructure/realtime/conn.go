package realtime

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Judamaro/notyhub-golang/internal/domain"
)

// conn is an open WebSocket connection. A single goroutine writes to the socket (writeLoop), fed by a
// bounded queue, so slow clients never block senders: when the queue is full the connection is closed.
type conn struct {
	id      domain.ConnectionID
	ws      *websocket.Conn
	send    chan []byte
	closing chan struct{}

	closeOnce   sync.Once
	closeCode   websocket.StatusCode
	closeReason string
}

func newConn(id domain.ConnectionID, ws *websocket.Conn, queueSize int) *conn {
	return &conn{id: id, ws: ws, send: make(chan []byte, queueSize), closing: make(chan struct{})}
}

// enqueue queues a message; it returns false when the connection is closing or its queue is full.
func (c *conn) enqueue(msg []byte) bool {
	select {
	case <-c.closing:
		return false
	default:
	}

	select {
	case c.send <- msg:
		return true
	default:
		c.close(websocket.StatusPolicyViolation, "send queue full")
		return false
	}
}

// close asks the writer to flush the queued messages and close the socket with code and reason.
func (c *conn) close(code websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		c.closeCode = code
		c.closeReason = reason
		close(c.closing)
	})
}

// writeLoop writes queued messages and pings until the context ends, a write fails or the connection is closed.
func (c *conn) writeLoop(ctx context.Context, pingInterval, writeTimeout time.Duration) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case msg := <-c.send:
			if c.write(ctx, msg, writeTimeout) != nil {
				return
			}
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		case <-c.closing:
			for {
				select {
				case msg := <-c.send:
					if c.write(ctx, msg, writeTimeout) != nil {
						return
					}
				default:
					_ = c.ws.Close(c.closeCode, c.closeReason)
					return
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (c *conn) write(ctx context.Context, msg []byte, timeout time.Duration) error {
	writeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.ws.Write(writeCtx, websocket.MessageText, msg)
}
