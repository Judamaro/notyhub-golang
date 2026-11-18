package registry_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Judamaro/notyhub-golang/internal/application"
	"github.com/Judamaro/notyhub-golang/internal/domain"
	"github.com/Judamaro/notyhub-golang/internal/infrastructure/registry"
)

var ctx = context.Background()

func conn(id domain.ConnectionID, client domain.ClientID, user domain.UserID) domain.ClientConnection {
	return domain.ClientConnection{ConnectionID: id, ClientID: client, UserID: user}
}

func TestAddAndQuery(t *testing.T) {
	m := registry.NewMemory()
	for _, c := range []domain.ClientConnection{conn("c1", "acme", "u1"), conn("c2", "acme", "u2"), conn("c3", "other", "u1")} {
		if replaced, _ := m.Add(ctx, c); replaced != nil {
			t.Fatalf("unexpected replacement %+v", replaced)
		}
	}

	if got, _ := m.GetByClient(ctx, "acme"); len(got) != 2 {
		t.Fatalf("acme connections = %d", len(got))
	}
	if got, _ := m.GetByUser(ctx, "other", "u1"); got == nil || got.ConnectionID != "c3" {
		t.Fatalf("user connection = %+v (same user id in two clients is two users)", got)
	}
	if got, _ := m.GetByClient(ctx, "none"); got == nil || len(got) != 0 {
		t.Fatalf("missing client = %#v", got)
	}
	if s := m.Stats(); s != (application.RegistryStats{Clients: 2, Connections: 3}) {
		t.Fatalf("stats = %+v", s)
	}
}

func TestNewConnectionReplacesPreviousOfUser(t *testing.T) {
	m := registry.NewMemory()
	_, _ = m.Add(ctx, conn("old", "acme", "u1"))

	replaced, _ := m.Add(ctx, conn("new", "acme", "u1"))
	if replaced == nil || replaced.ConnectionID != "old" {
		t.Fatalf("replaced = %+v", replaced)
	}
	if got, _ := m.GetByUser(ctx, "acme", "u1"); got.ConnectionID != "new" {
		t.Fatalf("user connection = %s", got.ConnectionID)
	}
	if got, _ := m.GetByClient(ctx, "acme"); len(got) != 1 {
		t.Fatalf("client connections = %d", len(got))
	}

	// The late disconnect of the replaced connection must not affect its replacement.
	if removed, _ := m.Remove(ctx, "old"); removed != nil {
		t.Fatalf("old connection already removed, got %+v", removed)
	}
	if got, _ := m.GetByUser(ctx, "acme", "u1"); got == nil || got.ConnectionID != "new" {
		t.Fatalf("user connection after late disconnect = %+v", got)
	}
	if s := m.Stats(); s != (application.RegistryStats{Clients: 1, Connections: 1}) {
		t.Fatalf("stats = %+v", s)
	}
}

func TestRemove(t *testing.T) {
	m := registry.NewMemory()
	_, _ = m.Add(ctx, conn("c1", "acme", "u1"))

	removed, _ := m.Remove(ctx, "c1")
	if removed == nil || removed.ConnectionID != "c1" {
		t.Fatalf("removed = %+v", removed)
	}
	if got, _ := m.GetByUser(ctx, "acme", "u1"); got != nil {
		t.Fatalf("user still connected: %+v", got)
	}
	if s := m.Stats(); s != (application.RegistryStats{}) {
		t.Fatalf("stats = %+v, want empty (client dropped)", s)
	}
	if removed, _ := m.Remove(ctx, "c1"); removed != nil {
		t.Fatal("second remove must return nil")
	}
}

func TestReAddingSameConnectionIDIsIdempotent(t *testing.T) {
	m := registry.NewMemory()
	_, _ = m.Add(ctx, conn("c1", "acme", "u1"))
	_, _ = m.Add(ctx, conn("c1", "acme", "u1"))
	if s := m.Stats(); s.Connections != 1 {
		t.Fatalf("stats = %+v", s)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := registry.NewMemory()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			id := domain.ConnectionID(fmt.Sprintf("c%d", i))
			_, _ = m.Add(ctx, conn(id, "acme", domain.UserID(fmt.Sprintf("u%d", i%10))))
			_, _ = m.GetByClient(ctx, "acme")
			_, _ = m.Remove(ctx, id)
		})
	}
	wg.Wait()
	if s := m.Stats(); s != (application.RegistryStats{}) {
		t.Fatalf("stats = %+v", s)
	}
}
