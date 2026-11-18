// Package realtime implements the WebSocket hub: it accepts connections, keeps them registered and pushes
// notifications to them.
package realtime

import (
	"net/url"

	"github.com/Judamaro/notyhub-golang/internal/domain"
)

// IdentityOptions names the query parameters and the token claim that identify a connection.
type IdentityOptions struct {
	ClientIDClaim      string
	ClientIDQueryParam string
	UserIDQueryParam   string
}

// Identity is the client and user a connection belongs to.
type Identity struct {
	ClientID domain.ClientID
	UserID   domain.UserID
}

// ReadIdentity reads the client id and user id sent in the query string and checks that the client id is the
// one of the access token (claims). A missing or repeated parameter counts as absent.
func ReadIdentity(query url.Values, claims map[string]any, opts IdentityOptions) (Identity, error) {
	clientID, err := domain.NewClientID(singleValue(query, opts.ClientIDQueryParam))
	if err != nil {
		return Identity{}, err
	}

	userID, err := domain.NewUserID(singleValue(query, opts.UserIDQueryParam))
	if err != nil {
		return Identity{}, err
	}

	claim, _ := claims[opts.ClientIDClaim].(string)
	tokenClientID, err := domain.NewClientID(claim)
	if err != nil || tokenClientID != clientID {
		return Identity{}, domain.ErrClientIDMismatch
	}

	return Identity{ClientID: clientID, UserID: userID}, nil
}

func singleValue(query url.Values, name string) string {
	if values := query[name]; len(values) == 1 {
		return values[0]
	}
	return ""
}
