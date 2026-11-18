package domain_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Judamaro/notyhub-golang/internal/domain"
)

func TestNewClientID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  domain.ClientID
		err   error
	}{
		{"valid", "acme", "acme", nil},
		{"lowercased", "ACME", "acme", nil},
		{"max length", strings.Repeat("a", 128), domain.ClientID(strings.Repeat("a", 128)), nil},
		{"empty", "", "", domain.ErrClientIDEmpty},
		{"leading space", " acme", "", domain.ErrClientIDContainsSpace},
		{"trailing space", "acme ", "", domain.ErrClientIDContainsSpace},
		{"inner space", "ac me", "", domain.ErrClientIDContainsSpace},
		{"tab", "ac\tme", "", domain.ErrClientIDContainsSpace},
		{"only spaces", "   ", "", domain.ErrClientIDContainsSpace},
		{"too long", strings.Repeat("a", 129), "", domain.ErrClientIDTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.NewClientID(tt.input)
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("error = %v, want %v", err, tt.err)
			}
			if got != tt.want {
				t.Fatalf("id = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewUserID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  domain.UserID
		err   error
	}{
		{"valid", "u1", "u1", nil},
		{"lowercased", "U1", "u1", nil},
		{"empty", "", "", domain.ErrUserIDEmpty},
		{"space", "u 1", "", domain.ErrUserIDContainsSpace},
		{"too long", strings.Repeat("u", 129), "", domain.ErrUserIDTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.NewUserID(tt.input)
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("error = %v, want %v", err, tt.err)
			}
			if got != tt.want {
				t.Fatalf("id = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewConnectionID(t *testing.T) {
	if id, err := domain.NewConnectionID("  abc  "); err != nil || id != "abc" {
		t.Fatalf("got %q, %v; want trimmed id", id, err)
	}
	if _, err := domain.NewConnectionID("   "); !errors.Is(err, domain.ErrConnectionIDEmpty) {
		t.Fatalf("error = %v, want ConnectionId.Empty", err)
	}
	if _, err := domain.NewConnectionID(strings.Repeat("c", 129)); !errors.Is(err, domain.ErrConnectionIDTooLong) {
		t.Fatalf("error = %v, want ConnectionId.TooLong", err)
	}
}

func TestNewNotification(t *testing.T) {
	createdAt := time.Date(2026, 11, 2, 19, 14, 37, 0, time.UTC)

	t.Run("valid with payload", func(t *testing.T) {
		payload := json.RawMessage(`{"orderId":7}`)
		n, err := domain.NewNotification("  order.updated  ", payload, createdAt)
		if err != nil {
			t.Fatal(err)
		}
		if n.Type != "order.updated" {
			t.Errorf("type = %q, want trimmed", n.Type)
		}
		if n.ID.Version() != 7 {
			t.Errorf("id version = %d, want 7", n.ID.Version())
		}
		if !n.CreatedAt.Equal(createdAt) {
			t.Errorf("createdAt = %v", n.CreatedAt)
		}
		payload[2] = 'X'
		if string(n.Payload) != `{"orderId":7}` {
			t.Errorf("payload not copied: %s", n.Payload)
		}
	})

	t.Run("null payload is no payload", func(t *testing.T) {
		n, err := domain.NewNotification("t", json.RawMessage("null"), createdAt)
		if err != nil || n.Payload != nil {
			t.Fatalf("payload = %s, err = %v", n.Payload, err)
		}
	})

	t.Run("empty type", func(t *testing.T) {
		if _, err := domain.NewNotification("   ", nil, createdAt); !errors.Is(err, domain.ErrNotificationTypeEmpty) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("type too long", func(t *testing.T) {
		if _, err := domain.NewNotification(strings.Repeat("t", 101), nil, createdAt); !errors.Is(err, domain.ErrNotificationTypeTooLong) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestErrors(t *testing.T) {
	err := domain.ErrUserNotConnected("acme", "u1")
	if err.Type != domain.ErrorTypeNotFound || err.Code != "User.NotConnected" {
		t.Fatalf("unexpected error %+v", err)
	}
	if err.Description != "The user 'u1' of the client 'acme' has no active connection." {
		t.Fatalf("description = %q", err.Description)
	}

	wrapped := errors.Join(errors.New("context"), err)
	got, ok := domain.AsError(wrapped)
	if !ok || got.Code != "User.NotConnected" {
		t.Fatalf("AsError = %v, %v", got, ok)
	}
	if !errors.Is(wrapped, domain.ErrUserNotConnected("other", "user")) {
		t.Fatal("errors.Is must match by code")
	}

	v := domain.NewValidationErrors(map[string][]string{"Type": {"bad"}})
	if v.Code != domain.ValidationErrorCode || v.Type != domain.ErrorTypeValidation || v.Fields["Type"][0] != "bad" {
		t.Fatalf("unexpected validation error %+v", v)
	}

	if name := domain.ErrUnsupportedTarget(domain.UserTarget{}).Description; !strings.Contains(name, "'UserTarget'") {
		t.Fatalf("description = %q", name)
	}
}
