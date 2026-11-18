package realtime_test

import (
	"errors"
	"net/url"
	"testing"

	"github.com/Judamaro/notyhub-golang/internal/domain"
	"github.com/Judamaro/notyhub-golang/internal/infrastructure/realtime"
)

var opts = realtime.IdentityOptions{ClientIDClaim: "sub", ClientIDQueryParam: "clientId", UserIDQueryParam: "userId"}

func TestReadIdentity(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		claims map[string]any
		want   realtime.Identity
		err    error
	}{
		{"valid", "clientId=acme&userId=u1", map[string]any{"sub": "acme"}, realtime.Identity{ClientID: "acme", UserID: "u1"}, nil},
		{"normalized", "clientId=ACME&userId=U1", map[string]any{"sub": "Acme"}, realtime.Identity{ClientID: "acme", UserID: "u1"}, nil},
		{"missing client", "userId=u1", map[string]any{"sub": "acme"}, realtime.Identity{}, domain.ErrClientIDEmpty},
		{"repeated client", "clientId=acme&clientId=acme&userId=u1", map[string]any{"sub": "acme"}, realtime.Identity{}, domain.ErrClientIDEmpty},
		{"client with space", "clientId=ac%20me&userId=u1", map[string]any{"sub": "acme"}, realtime.Identity{}, domain.ErrClientIDContainsSpace},
		{"missing user", "clientId=acme", map[string]any{"sub": "acme"}, realtime.Identity{}, domain.ErrUserIDEmpty},
		{"user with space", "clientId=acme&userId=u%201", map[string]any{"sub": "acme"}, realtime.Identity{}, domain.ErrUserIDContainsSpace},
		{"other client in token", "clientId=acme&userId=u1", map[string]any{"sub": "globex"}, realtime.Identity{}, domain.ErrClientIDMismatch},
		{"no claim", "clientId=acme&userId=u1", map[string]any{}, realtime.Identity{}, domain.ErrClientIDMismatch},
		{"non string claim", "clientId=acme&userId=u1", map[string]any{"sub": 7}, realtime.Identity{}, domain.ErrClientIDMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			got, err := realtime.ReadIdentity(query, tt.claims, opts)
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("error = %v, want %v", err, tt.err)
			}
			if got != tt.want {
				t.Fatalf("identity = %+v, want %+v", got, tt.want)
			}
		})
	}
}
