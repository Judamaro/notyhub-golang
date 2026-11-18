package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Judamaro/notyhub-golang/internal/config"
)

// withAPIKey lets through only requests that carry one of the configured keys in the API key header.
// Every key is compared in constant time.
func withAPIKey(cfg config.APIKeys) Middleware {
	keys := make([][]byte, len(cfg.Keys))
	for i, k := range cfg.Keys {
		keys[i] = []byte(k)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provided := []byte(r.Header.Get(cfg.HeaderName))
			match := 0
			for _, k := range keys {
				match |= subtle.ConstantTimeCompare(provided, k)
			}

			if len(provided) == 0 || match != 1 {
				writeStatus(w, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clockSkew is the tolerance applied to the time claims of tokens.
const clockSkew = 30 * time.Second

// tokenService validates and issues client tokens (HMAC-SHA256).
type tokenService struct {
	cfg    config.JWT
	key    []byte
	parser *jwt.Parser
	now    func() time.Time
}

func newTokenService(cfg config.JWT, now func() time.Time) *tokenService {
	return &tokenService{
		cfg: cfg,
		key: []byte(cfg.SigningKey),
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer(cfg.Issuer),
			jwt.WithAudience(cfg.Audience),
			jwt.WithExpirationRequired(),
			jwt.WithIssuedAt(),
			jwt.WithLeeway(clockSkew),
			jwt.WithTimeFunc(now),
		),
		now: now,
	}
}

var errMissingToken = errors.New("missing access token")

// validate returns the claims of a valid token.
func (s *tokenService) validate(token string) (jwt.MapClaims, error) {
	if token == "" {
		return nil, errMissingToken
	}

	claims := jwt.MapClaims{}
	if _, err := s.parser.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return s.key, nil }); err != nil {
		return nil, err
	}
	return claims, nil
}

// issue creates a token for a client id, valid for ttl.
func (s *tokenService) issue(clientIDClaim, clientID string, ttl time.Duration) (string, time.Time, error) {
	now := s.now()
	expiresAt := now.Add(ttl)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":         s.cfg.Issuer,
		"aud":         s.cfg.Audience,
		"iat":         now.Unix(),
		"nbf":         now.Unix(),
		"exp":         expiresAt.Unix(),
		clientIDClaim: clientID,
	})

	signed, err := token.SignedString(s.key)
	return signed, expiresAt, err
}

// accessToken returns the token of a hub request: the access_token query parameter (browsers cannot set headers
// on WebSocket requests) or the bearer token of the Authorization header.
func accessToken(r *http.Request) string {
	if token := r.URL.Query().Get("access_token"); token != "" {
		return token
	}
	if scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(token)
	}
	return ""
}
