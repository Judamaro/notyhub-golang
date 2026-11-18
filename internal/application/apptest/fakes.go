// Package apptest provides test doubles for the application ports.
package apptest

import (
	"context"
	"sync"
	"time"

	"github.com/Judamaro/notyhub-golang/internal/application"
	"github.com/Judamaro/notyhub-golang/internal/domain"
)

// Registry is an in-memory ConnectionRegistry with programmable results.
type Registry struct {
	ByClient map[domain.ClientID][]domain.ClientConnection
	ByUser   map[domain.ClientID]map[domain.UserID]domain.ClientConnection
	// AddResult is returned by Add as the replaced connection.
	AddResult *domain.ClientConnection
	// RemoveResult is returned by Remove.
	RemoveResult *domain.ClientConnection
	Added        []domain.ClientConnection
	Removed      []domain.ConnectionID
	Err          error
}

var _ application.ConnectionRegistry = (*Registry)(nil)

// Add records the connection and returns AddResult.
func (r *Registry) Add(_ context.Context, c domain.ClientConnection) (*domain.ClientConnection, error) {
	r.Added = append(r.Added, c)
	return r.AddResult, r.Err
}

// Remove records the id and returns RemoveResult.
func (r *Registry) Remove(_ context.Context, id domain.ConnectionID) (*domain.ClientConnection, error) {
	r.Removed = append(r.Removed, id)
	return r.RemoveResult, r.Err
}

// GetByUser returns the connection in ByUser.
func (r *Registry) GetByUser(_ context.Context, clientID domain.ClientID, userID domain.UserID) (*domain.ClientConnection, error) {
	if c, ok := r.ByUser[clientID][userID]; ok {
		return &c, r.Err
	}
	return nil, r.Err
}

// GetByClient returns the connections in ByClient.
func (r *Registry) GetByClient(_ context.Context, clientID domain.ClientID) ([]domain.ClientConnection, error) {
	return append([]domain.ClientConnection(nil), r.ByClient[clientID]...), r.Err
}

// Stats counts ByClient.
func (r *Registry) Stats() application.RegistryStats {
	s := application.RegistryStats{Clients: len(r.ByClient)}
	for _, c := range r.ByClient {
		s.Connections += len(c)
	}
	return s
}

// Dispatcher records the notifications sent.
type Dispatcher struct {
	mu    sync.Mutex
	Calls []DispatchCall
	Err   error
}

// DispatchCall is one call to Dispatcher.Send.
type DispatchCall struct {
	ConnectionIDs []domain.ConnectionID
	Notification  domain.Notification
}

// Send records the call.
func (d *Dispatcher) Send(_ context.Context, ids []domain.ConnectionID, n domain.Notification) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Calls = append(d.Calls, DispatchCall{ConnectionIDs: ids, Notification: n})
	return d.Err
}

// Metrics counts the recorded metrics.
type Metrics struct {
	Opened, Closed, Replaced int
	Delivered                []Delivery
	NotFound                 []domain.Target
}

// Delivery is one call to Metrics.NotificationDelivered.
type Delivery struct {
	Target     domain.Target
	Recipients int
}

var _ application.Metrics = (*Metrics)(nil)

func (m *Metrics) ConnectionOpened()   { m.Opened++ }
func (m *Metrics) ConnectionClosed()   { m.Closed++ }
func (m *Metrics) ConnectionReplaced() { m.Replaced++ }
func (m *Metrics) NotificationDelivered(t domain.Target, recipients int, _ time.Duration) {
	m.Delivered = append(m.Delivered, Delivery{Target: t, Recipients: recipients})
}
func (m *Metrics) NotificationNotFound(t domain.Target) { m.NotFound = append(m.NotFound, t) }

// FixedClock returns a clock that always returns t.
func FixedClock(t time.Time) application.Clock { return func() time.Time { return t } }
