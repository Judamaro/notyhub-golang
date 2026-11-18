// Package registry implements the connection registry.
package registry

import (
	"context"
	"sync"

	"github.com/Judamaro/notyhub-golang/internal/application"
	"github.com/Judamaro/notyhub-golang/internal/domain"
)

type userKey struct {
	clientID domain.ClientID
	userID   domain.UserID
}

// Memory is a single-node registry. It keeps three related indexes per connection: the connection itself,
// the user → connection relation and the set of connections of the client. Every write runs under one lock
// so the indexes never drift apart. Swap it for a distributed implementation (e.g. Redis) to scale out.
type Memory struct {
	mu          sync.RWMutex
	connections map[domain.ConnectionID]domain.ClientConnection
	users       map[userKey]domain.ConnectionID
	clients     map[domain.ClientID]map[domain.ConnectionID]struct{}
}

var _ application.ConnectionRegistry = (*Memory)(nil)

// NewMemory creates an empty registry.
func NewMemory() *Memory {
	return &Memory{
		connections: map[domain.ConnectionID]domain.ClientConnection{},
		users:       map[userKey]domain.ConnectionID{},
		clients:     map[domain.ClientID]map[domain.ConnectionID]struct{}{},
	}
}

// Add registers the connection and returns the previous connection of the same user, which is removed.
func (m *Memory) Add(_ context.Context, c domain.ClientConnection) (*domain.ClientConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.removeLocked(c.ConnectionID)

	key := userKey{clientID: c.ClientID, userID: c.UserID}
	var replaced *domain.ClientConnection
	if previous, ok := m.users[key]; ok {
		replaced = m.removeLocked(previous)
	}

	set, ok := m.clients[c.ClientID]
	if !ok {
		set = map[domain.ConnectionID]struct{}{}
		m.clients[c.ClientID] = set
	}
	set[c.ConnectionID] = struct{}{}
	m.connections[c.ConnectionID] = c
	m.users[key] = c.ConnectionID

	return replaced, nil
}

// Remove unregisters the connection and returns it, or nil when it was not registered.
func (m *Memory) Remove(_ context.Context, id domain.ConnectionID) (*domain.ClientConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.removeLocked(id), nil
}

// GetByUser returns the connection of the user, or nil.
func (m *Memory) GetByUser(_ context.Context, clientID domain.ClientID, userID domain.UserID) (*domain.ClientConnection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	id, ok := m.users[userKey{clientID: clientID, userID: userID}]
	if !ok {
		return nil, nil
	}
	c, ok := m.connections[id]
	if !ok {
		return nil, nil
	}
	return &c, nil
}

// GetByClient returns a snapshot of the connections of the client.
func (m *Memory) GetByClient(_ context.Context, clientID domain.ClientID) ([]domain.ClientConnection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	set := m.clients[clientID]
	snapshot := make([]domain.ClientConnection, 0, len(set))
	for id := range set {
		if c, ok := m.connections[id]; ok {
			snapshot = append(snapshot, c)
		}
	}
	return snapshot, nil
}

// Stats returns how many clients and connections are registered.
func (m *Memory) Stats() application.RegistryStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return application.RegistryStats{Clients: len(m.clients), Connections: len(m.connections)}
}

// removeLocked removes the connection, the relation of its user (only while it still points to this
// connection, so the late disconnect of a replaced connection does not affect its replacement) and the
// connection from the set of its client, dropping the client when it has no connections left.
func (m *Memory) removeLocked(id domain.ConnectionID) *domain.ClientConnection {
	removed, ok := m.connections[id]
	if !ok {
		return nil
	}
	delete(m.connections, id)

	key := userKey{clientID: removed.ClientID, userID: removed.UserID}
	if current, ok := m.users[key]; ok && current == id {
		delete(m.users, key)
	}

	if set, ok := m.clients[removed.ClientID]; ok {
		delete(set, id)
		if len(set) == 0 {
			delete(m.clients, removed.ClientID)
		}
	}

	return &removed
}
