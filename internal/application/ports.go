// Package application holds the use cases of the hub and the ports (interfaces) the infrastructure implements.
package application

import (
	"context"
	"time"

	"github.com/Judamaro/notyhub-golang/internal/domain"
)

// RegistryStats is a snapshot of the connection registry.
type RegistryStats struct {
	Clients     int
	Connections int
}

// ConnectionRegistry stores the open connections and their relations (connection, user → connection and
// client → connections). Each user of a client has a single connection.
type ConnectionRegistry interface {
	// Add registers a connection and returns the previous connection of the same user, which it replaces, or nil.
	Add(ctx context.Context, connection domain.ClientConnection) (*domain.ClientConnection, error)
	// Remove unregisters a connection and returns it, or nil when it was not registered.
	Remove(ctx context.Context, connectionID domain.ConnectionID) (*domain.ClientConnection, error)
	// GetByUser returns the connection of a user, or nil when the user is not connected.
	GetByUser(ctx context.Context, clientID domain.ClientID, userID domain.UserID) (*domain.ClientConnection, error)
	// GetByClient returns the connections of a client (empty when it has none).
	GetByClient(ctx context.Context, clientID domain.ClientID) ([]domain.ClientConnection, error)
	// Stats returns how many clients and connections are registered.
	Stats() RegistryStats
}

// NotificationDispatcher pushes a notification to connections.
type NotificationDispatcher interface {
	Send(ctx context.Context, connectionIDs []domain.ConnectionID, notification domain.Notification) error
}

// Metrics records the business metrics of the hub.
type Metrics interface {
	ConnectionOpened()
	ConnectionClosed()
	ConnectionReplaced()
	NotificationDelivered(target domain.Target, recipients int, duration time.Duration)
	NotificationNotFound(target domain.Target)
}

// Clock returns the current time; tests replace it.
type Clock func() time.Time

// SystemClock returns the current UTC time.
func SystemClock() time.Time { return time.Now().UTC() }

// NopMetrics discards every metric.
type NopMetrics struct{}

func (NopMetrics) ConnectionOpened()                                       {}
func (NopMetrics) ConnectionClosed()                                       {}
func (NopMetrics) ConnectionReplaced()                                     {}
func (NopMetrics) NotificationDelivered(domain.Target, int, time.Duration) {}
func (NopMetrics) NotificationNotFound(domain.Target)                      {}
