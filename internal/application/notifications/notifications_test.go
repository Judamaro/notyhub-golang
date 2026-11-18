package notifications_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Judamaro/notyhub-golang/internal/application/apptest"
	"github.com/Judamaro/notyhub-golang/internal/application/notifications"
	"github.com/Judamaro/notyhub-golang/internal/domain"
)

var now = time.Date(2026, 11, 2, 19, 0, 0, 0, time.UTC)

// otherTarget is a target no default strategy handles. It embeds a domain target to satisfy the sealed interface.
type otherTarget struct{ domain.ClientTarget }

func registryWith() *apptest.Registry {
	return &apptest.Registry{
		ByClient: map[domain.ClientID][]domain.ClientConnection{
			"acme": {{ConnectionID: "c1", ClientID: "acme", UserID: "u1"}, {ConnectionID: "c2", ClientID: "acme", UserID: "u2"}},
		},
		ByUser: map[domain.ClientID]map[domain.UserID]domain.ClientConnection{
			"acme": {"u1": {ConnectionID: "c1", ClientID: "acme", UserID: "u1"}},
		},
	}
}

func TestStrategyResolver(t *testing.T) {
	resolver := notifications.NewStrategyResolver(notifications.DefaultStrategies(registryWith())...)

	for _, target := range []domain.Target{domain.ClientTarget{ClientID: "acme"}, domain.UserTarget{ClientID: "acme", UserID: "u1"}} {
		s, err := resolver.Resolve(target)
		if err != nil || !s.CanHandle(target) {
			t.Fatalf("Resolve(%T) = %v, %v", target, s, err)
		}
	}

	_, err := resolver.Resolve(otherTarget{})
	if e, ok := domain.AsError(err); !ok || e.Code != "Notification.UnsupportedTarget" {
		t.Fatalf("error = %v", err)
	}
}

func TestStrategies(t *testing.T) {
	ctx := context.Background()
	reg := registryWith()

	ids, err := notifications.NewClientDeliveryStrategy(reg).ResolveRecipients(ctx, domain.ClientTarget{ClientID: "acme"})
	if err != nil || len(ids) != 2 {
		t.Fatalf("client recipients = %v, %v", ids, err)
	}

	_, err = notifications.NewClientDeliveryStrategy(reg).ResolveRecipients(ctx, domain.ClientTarget{ClientID: "none"})
	if e, ok := domain.AsError(err); !ok || e.Code != "Client.NoConnections" {
		t.Fatalf("error = %v", err)
	}

	ids, err = notifications.NewUserDeliveryStrategy(reg).ResolveRecipients(ctx, domain.UserTarget{ClientID: "acme", UserID: "u1"})
	if err != nil || len(ids) != 1 || ids[0] != "c1" {
		t.Fatalf("user recipients = %v, %v", ids, err)
	}

	_, err = notifications.NewUserDeliveryStrategy(reg).ResolveRecipients(ctx, domain.UserTarget{ClientID: "acme", UserID: "u9"})
	if e, ok := domain.AsError(err); !ok || e.Code != "User.NotConnected" {
		t.Fatalf("error = %v", err)
	}

	_, err = notifications.NewUserDeliveryStrategy(reg).ResolveRecipients(ctx, domain.ClientTarget{ClientID: "acme"})
	if e, ok := domain.AsError(err); !ok || e.Code != "Notification.UnsupportedTarget" {
		t.Fatalf("error = %v", err)
	}
}

func newSendHandler(dispatcher *apptest.Dispatcher, metrics *apptest.Metrics) *notifications.SendHandler {
	reg := registryWith()
	return notifications.NewSendHandler(
		notifications.NewStrategyResolver(notifications.DefaultStrategies(reg)...),
		dispatcher, apptest.FixedClock(now), metrics)
}

func TestSendToClient(t *testing.T) {
	dispatcher, metrics := &apptest.Dispatcher{}, &apptest.Metrics{}
	resp, err := newSendHandler(dispatcher, metrics).Handle(context.Background(), notifications.SendCommand{
		Target: domain.ClientTarget{ClientID: "acme"}, Type: "order.updated", Payload: json.RawMessage(`{"orderId":7}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.DeliveredConnections != 2 || !resp.SentAt.Equal(now) {
		t.Fatalf("unexpected response %+v", resp)
	}
	if len(dispatcher.Calls) != 1 || dispatcher.Calls[0].Notification.ID != resp.NotificationID || len(dispatcher.Calls[0].ConnectionIDs) != 2 {
		t.Fatalf("dispatch calls %+v", dispatcher.Calls)
	}
	if len(metrics.Delivered) != 1 || metrics.Delivered[0].Recipients != 2 {
		t.Fatalf("delivered metrics %+v", metrics.Delivered)
	}
}

func TestSendToDisconnectedUser(t *testing.T) {
	dispatcher, metrics := &apptest.Dispatcher{}, &apptest.Metrics{}
	_, err := newSendHandler(dispatcher, metrics).Handle(context.Background(), notifications.SendCommand{
		Target: domain.UserTarget{ClientID: "acme", UserID: "u9"}, Type: "t",
	})

	if e, ok := domain.AsError(err); !ok || e.Type != domain.ErrorTypeNotFound {
		t.Fatalf("error = %v", err)
	}
	if len(dispatcher.Calls) != 0 || len(metrics.NotFound) != 1 || len(metrics.Delivered) != 0 {
		t.Fatalf("calls %d, not found %d, delivered %d", len(dispatcher.Calls), len(metrics.NotFound), len(metrics.Delivered))
	}
}

func TestSendDispatchFailureIsUnexpected(t *testing.T) {
	dispatcher := &apptest.Dispatcher{Err: errors.New("boom")}
	_, err := newSendHandler(dispatcher, &apptest.Metrics{}).Handle(context.Background(), notifications.SendCommand{
		Target: domain.ClientTarget{ClientID: "acme"}, Type: "t",
	})
	if _, ok := domain.AsError(err); ok || err == nil {
		t.Fatalf("error = %v, want a non business error", err)
	}
}

func TestSendValidator(t *testing.T) {
	v := notifications.SendValidator{}

	if errs := v.Validate(notifications.SendCommand{Target: domain.ClientTarget{ClientID: "a"}, Type: "ok"}); len(errs) != 0 {
		t.Fatalf("valid command has errors %v", errs)
	}

	errs := v.Validate(notifications.SendCommand{})
	if errs["Type"][0] != "'Type' must not be empty." || errs["Target"][0] != "'Target' must not be empty." {
		t.Fatalf("errors = %v", errs)
	}

	errs = v.Validate(notifications.SendCommand{Target: domain.ClientTarget{}, Type: strings.Repeat("t", 101)})
	if want := "The length of 'Type' must be 100 characters or fewer. You entered 101 characters."; errs["Type"][0] != want {
		t.Fatalf("errors = %v", errs)
	}
}
