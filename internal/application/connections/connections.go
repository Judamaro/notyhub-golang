// Package connections holds the use cases that register, unregister and list connections.
package connections

import (
	"context"
	"slices"
	"time"

	"github.com/Judamaro/notyhub-golang/internal/application"
	"github.com/Judamaro/notyhub-golang/internal/domain"
)

// ConnectionResponse is the public view of a connection.
type ConnectionResponse struct {
	ConnectionID string    `json:"connectionId"`
	ClientID     string    `json:"clientId"`
	UserID       string    `json:"userId"`
	ConnectedAt  time.Time `json:"connectedAt"`
	UserAgent    *string   `json:"userAgent"`
}

// NewConnectionResponse maps a connection to its response.
func NewConnectionResponse(c domain.ClientConnection) ConnectionResponse {
	var userAgent *string
	if c.UserAgent != "" {
		ua := c.UserAgent
		userAgent = &ua
	}

	return ConnectionResponse{
		ConnectionID: c.ConnectionID.String(),
		ClientID:     c.ClientID.String(),
		UserID:       c.UserID.String(),
		ConnectedAt:  c.ConnectedAt,
		UserAgent:    userAgent,
	}
}

// RegisterCommand registers a new connection of a user.
type RegisterCommand struct {
	ConnectionID domain.ConnectionID
	ClientID     domain.ClientID
	UserID       domain.UserID
	UserAgent    string
}

// RegisterResponse is the registered connection and the id of the connection it replaced
// (empty when the user had no previous connection).
type RegisterResponse struct {
	Connection           ConnectionResponse
	ReplacedConnectionID domain.ConnectionID
}

// RegisterHandler registers connections. A new connection of a user replaces the previous one.
type RegisterHandler struct {
	registry application.ConnectionRegistry
	clock    application.Clock
	metrics  application.Metrics
}

// NewRegisterHandler creates a RegisterHandler.
func NewRegisterHandler(registry application.ConnectionRegistry, clock application.Clock, metrics application.Metrics) *RegisterHandler {
	return &RegisterHandler{registry: registry, clock: clock, metrics: metrics}
}

// Handle registers the connection.
func (h *RegisterHandler) Handle(ctx context.Context, cmd RegisterCommand) (RegisterResponse, error) {
	connection := domain.ClientConnection{
		ConnectionID: cmd.ConnectionID,
		ClientID:     cmd.ClientID,
		UserID:       cmd.UserID,
		ConnectedAt:  h.clock(),
		UserAgent:    cmd.UserAgent,
	}

	replaced, err := h.registry.Add(ctx, connection)
	if err != nil {
		return RegisterResponse{}, err
	}

	h.metrics.ConnectionOpened()
	response := RegisterResponse{Connection: NewConnectionResponse(connection)}
	if replaced != nil {
		h.metrics.ConnectionReplaced()
		response.ReplacedConnectionID = replaced.ConnectionID
	}

	return response, nil
}

// UnregisterCommand removes a closed connection.
type UnregisterCommand struct {
	ConnectionID domain.ConnectionID
}

// UnregisterHandler removes connections when they close.
type UnregisterHandler struct {
	registry application.ConnectionRegistry
	metrics  application.Metrics
}

// NewUnregisterHandler creates an UnregisterHandler.
func NewUnregisterHandler(registry application.ConnectionRegistry, metrics application.Metrics) *UnregisterHandler {
	return &UnregisterHandler{registry: registry, metrics: metrics}
}

// Handle removes the connection; it fails with Connection.NotFound when it was not registered
// (for example, a connection already replaced by a newer one of the same user).
func (h *UnregisterHandler) Handle(ctx context.Context, cmd UnregisterCommand) (ConnectionResponse, error) {
	removed, err := h.registry.Remove(ctx, cmd.ConnectionID)
	if err != nil {
		return ConnectionResponse{}, err
	}
	if removed == nil {
		return ConnectionResponse{}, domain.ErrConnectionNotFound(cmd.ConnectionID)
	}

	h.metrics.ConnectionClosed()
	return NewConnectionResponse(*removed), nil
}

// GetClientConnectionsQuery lists the active connections of a client.
type GetClientConnectionsQuery struct {
	ClientID domain.ClientID
}

// GetClientConnectionsHandler lists connections, oldest first.
type GetClientConnectionsHandler struct {
	registry application.ConnectionRegistry
}

// NewGetClientConnectionsHandler creates a GetClientConnectionsHandler.
func NewGetClientConnectionsHandler(registry application.ConnectionRegistry) *GetClientConnectionsHandler {
	return &GetClientConnectionsHandler{registry: registry}
}

// Handle returns the connections of the client ordered by connection time; empty when it has none.
func (h *GetClientConnectionsHandler) Handle(ctx context.Context, q GetClientConnectionsQuery) ([]ConnectionResponse, error) {
	conns, err := h.registry.GetByClient(ctx, q.ClientID)
	if err != nil {
		return nil, err
	}

	slices.SortStableFunc(conns, func(a, b domain.ClientConnection) int { return a.ConnectedAt.Compare(b.ConnectedAt) })

	responses := make([]ConnectionResponse, 0, len(conns))
	for _, c := range conns {
		responses = append(responses, NewConnectionResponse(c))
	}

	return responses, nil
}
