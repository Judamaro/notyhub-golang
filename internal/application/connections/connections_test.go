package connections_test

import (
	"context"
	"testing"
	"time"

	"github.com/Judamaro/notyhub-golang/internal/application/apptest"
	"github.com/Judamaro/notyhub-golang/internal/application/connections"
	"github.com/Judamaro/notyhub-golang/internal/domain"
)

var now = time.Date(2026, 11, 2, 19, 0, 0, 0, time.UTC)

func TestRegister(t *testing.T) {
	t.Run("new user", func(t *testing.T) {
		reg, metrics := &apptest.Registry{}, &apptest.Metrics{}
		h := connections.NewRegisterHandler(reg, apptest.FixedClock(now), metrics)

		resp, err := h.Handle(context.Background(), connections.RegisterCommand{ConnectionID: "c1", ClientID: "acme", UserID: "u1", UserAgent: "test"})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ReplacedConnectionID != "" || resp.Connection.ConnectionID != "c1" || !resp.Connection.ConnectedAt.Equal(now) {
			t.Fatalf("unexpected response %+v", resp)
		}
		if *resp.Connection.UserAgent != "test" {
			t.Fatalf("user agent = %v", resp.Connection.UserAgent)
		}
		if len(reg.Added) != 1 || metrics.Opened != 1 || metrics.Replaced != 0 {
			t.Fatalf("added %d, opened %d, replaced %d", len(reg.Added), metrics.Opened, metrics.Replaced)
		}
	})

	t.Run("replaces the previous connection of the user", func(t *testing.T) {
		reg := &apptest.Registry{AddResult: &domain.ClientConnection{ConnectionID: "old", ClientID: "acme", UserID: "u1"}}
		metrics := &apptest.Metrics{}
		h := connections.NewRegisterHandler(reg, apptest.FixedClock(now), metrics)

		resp, err := h.Handle(context.Background(), connections.RegisterCommand{ConnectionID: "new", ClientID: "acme", UserID: "u1"})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ReplacedConnectionID != "old" || resp.Connection.UserAgent != nil {
			t.Fatalf("unexpected response %+v", resp)
		}
		if metrics.Opened != 1 || metrics.Replaced != 1 {
			t.Fatalf("opened %d, replaced %d", metrics.Opened, metrics.Replaced)
		}
	})
}

func TestUnregister(t *testing.T) {
	t.Run("registered connection", func(t *testing.T) {
		reg := &apptest.Registry{RemoveResult: &domain.ClientConnection{ConnectionID: "c1", ClientID: "acme", UserID: "u1"}}
		metrics := &apptest.Metrics{}

		resp, err := connections.NewUnregisterHandler(reg, metrics).Handle(context.Background(), connections.UnregisterCommand{ConnectionID: "c1"})
		if err != nil || resp.ConnectionID != "c1" || metrics.Closed != 1 {
			t.Fatalf("resp %+v, err %v, closed %d", resp, err, metrics.Closed)
		}
	})

	t.Run("unknown connection", func(t *testing.T) {
		metrics := &apptest.Metrics{}
		_, err := connections.NewUnregisterHandler(&apptest.Registry{}, metrics).Handle(context.Background(), connections.UnregisterCommand{ConnectionID: "c1"})
		if e, ok := domain.AsError(err); !ok || e.Code != "Connection.NotFound" {
			t.Fatalf("error = %v", err)
		}
		if metrics.Closed != 0 {
			t.Fatal("closed metric must not be recorded")
		}
	})
}

func TestGetClientConnections(t *testing.T) {
	reg := &apptest.Registry{ByClient: map[domain.ClientID][]domain.ClientConnection{
		"acme": {
			{ConnectionID: "late", ClientID: "acme", UserID: "u2", ConnectedAt: now.Add(time.Minute)},
			{ConnectionID: "early", ClientID: "acme", UserID: "u1", ConnectedAt: now},
		},
	}}
	h := connections.NewGetClientConnectionsHandler(reg)

	got, err := h.Handle(context.Background(), connections.GetClientConnectionsQuery{ClientID: "acme"})
	if err != nil || len(got) != 2 || got[0].ConnectionID != "early" || got[1].ConnectionID != "late" {
		t.Fatalf("got %+v, %v; want ordered by connection time", got, err)
	}

	empty, err := h.Handle(context.Background(), connections.GetClientConnectionsQuery{ClientID: "none"})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("got %#v, %v; want empty non-nil slice", empty, err)
	}
}
