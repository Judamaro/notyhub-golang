package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/Judamaro/notyhub-golang/internal/application"
	"github.com/Judamaro/notyhub-golang/internal/application/connections"
	"github.com/Judamaro/notyhub-golang/internal/application/notifications"
	"github.com/Judamaro/notyhub-golang/internal/config"
	"github.com/Judamaro/notyhub-golang/internal/domain"
	"github.com/Judamaro/notyhub-golang/internal/infrastructure/realtime"
)

// HealthPath is the path of the liveness endpoint, used by the Docker healthcheck.
const HealthPath = "/health"

// devTokenTTL is how long development tokens are valid.
const devTokenTTL = 8 * time.Hour

// HubServer serves an authenticated and validated WebSocket request.
type HubServer interface {
	Serve(w http.ResponseWriter, r *http.Request, identity realtime.Identity)
}

// Deps are the dependencies of the API.
type Deps struct {
	Config               config.Config
	Logger               *slog.Logger
	Clock                application.Clock
	SendNotification     application.CommandHandler[notifications.SendCommand, notifications.SendResponse]
	GetClientConnections application.QueryHandler[connections.GetClientConnectionsQuery, []connections.ConnectionResponse]
	Hub                  HubServer
	// Metrics records the HTTP server metrics; nil disables them.
	Metrics RequestRecorder
}

type server struct {
	Deps
	tokens   *tokenService
	identity realtime.IdentityOptions
}

// NewHandler builds the public HTTP handler: REST API, WebSocket hub, health check and, in development,
// Swagger UI and the token issuing endpoint.
func NewHandler(d Deps) http.Handler {
	s := &server{
		Deps:   d,
		tokens: newTokenService(d.Config.JWT, func() time.Time { return d.Clock() }),
		identity: realtime.IdentityOptions{
			ClientIDClaim:      d.Config.Realtime.ClientIDClaim,
			ClientIDQueryParam: d.Config.Realtime.ClientIDQueryParam,
			UserIDQueryParam:   d.Config.Realtime.UserIDQueryParam,
		},
	}

	publisher := withAPIKey(d.Config.APIKeys)

	mux := http.NewServeMux()
	mux.Handle("POST /api/notifications/clients/{clientId}", publisher(http.HandlerFunc(s.sendToClient)))
	mux.Handle("POST /api/notifications/clients/{clientId}/users/{userId}", publisher(http.HandlerFunc(s.sendToUser)))
	mux.Handle("GET /api/clients/{clientId}/connections", publisher(http.HandlerFunc(s.clientConnections)))
	mux.HandleFunc("GET "+d.Config.Realtime.HubPath, s.hub)
	mux.HandleFunc("GET "+HealthPath, s.health)

	if d.Config.IsDevelopment() {
		mux.HandleFunc("POST /dev/token", s.devToken)
		mapOpenAPI(mux, d.Config)
	}

	return chain(mux,
		withPetitionStatusPages,
		withRecover(d.Logger),
		withCORS(d.Config.CORS.AllowedOrigins),
		withHTTPMetrics(d.Metrics, HealthPath),
	)
}

// NewMetricsHandler builds the handler of the internal metrics address: it only serves path; anything else is 404.
func NewMetricsHandler(path string, metrics http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET "+path, metrics)
	return withPetitionStatusPages(mux)
}

// sendNotificationRequest is the body of the notification endpoints.
type sendNotificationRequest struct {
	// Type is the event name clients use to route the message, e.g. "order.updated".
	Type string `json:"type"`
	// Payload is arbitrary JSON delivered as is to the clients.
	Payload json.RawMessage `json:"payload"`
}

const notificationSent = "Notification sent successfully."

func (s *server) sendToClient(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[sendNotificationRequest](w, r)
	if !ok {
		return
	}

	clientID, err := domain.NewClientID(r.PathValue("clientId"))
	if err != nil {
		writeError(w, r, s.Logger, ModuleNotificationService, err)
		return
	}

	s.send(w, r, domain.ClientTarget{ClientID: clientID}, req)
}

func (s *server) sendToUser(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[sendNotificationRequest](w, r)
	if !ok {
		return
	}

	clientID, err := domain.NewClientID(r.PathValue("clientId"))
	if err != nil {
		writeError(w, r, s.Logger, ModuleNotificationService, err)
		return
	}

	userID, err := domain.NewUserID(r.PathValue("userId"))
	if err != nil {
		writeError(w, r, s.Logger, ModuleNotificationService, err)
		return
	}

	s.send(w, r, domain.UserTarget{ClientID: clientID, UserID: userID}, req)
}

func (s *server) send(w http.ResponseWriter, r *http.Request, target domain.Target, req sendNotificationRequest) {
	result, err := s.SendNotification.Handle(r.Context(), notifications.SendCommand{Target: target, Type: req.Type, Payload: req.Payload})
	if err != nil {
		writeError(w, r, s.Logger, ModuleNotificationService, err)
		return
	}
	Ok(ModuleNotificationService, notificationSent, result, http.StatusOK).write(w)
}

func (s *server) clientConnections(w http.ResponseWriter, r *http.Request) {
	clientID, err := domain.NewClientID(r.PathValue("clientId"))
	if err != nil {
		writeError(w, r, s.Logger, ModuleConnectionService, err)
		return
	}

	result, err := s.GetClientConnections.Handle(r.Context(), connections.GetClientConnectionsQuery{ClientID: clientID})
	if err != nil {
		writeError(w, r, s.Logger, ModuleConnectionService, err)
		return
	}
	Ok(ModuleConnectionService, "Connections retrieved successfully.", result, http.StatusOK).write(w)
}

// hub authenticates the request (401), validates its client id and user id against the token (400) and only
// then upgrades it to a WebSocket connection, so clients learn why they cannot connect.
func (s *server) hub(w http.ResponseWriter, r *http.Request) {
	claims, err := s.tokens.validate(accessToken(r))
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		writeStatus(w, http.StatusUnauthorized)
		return
	}

	identity, err := realtime.ReadIdentity(r.URL.Query(), claims, s.identity)
	if err != nil {
		writeError(w, r, s.Logger, ModuleConnectionService, err)
		return
	}

	s.Hub.Serve(w, r, identity)
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	result := map[string]any{"status": "Healthy", "checks": map[string]any{}}
	Ok(ModuleHealthCheck, "The service is healthy.", result, http.StatusOK).write(w)
}

type devTokenRequest struct {
	ClientID string `json:"clientId"`
}

type devTokenResponse struct {
	AccessToken string    `json:"accessToken"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// devToken issues a client token so the hub can be tried without an identity provider (development only).
func (s *server) devToken(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[devTokenRequest](w, r)
	if !ok {
		return
	}

	clientID, err := domain.NewClientID(req.ClientID)
	if err != nil {
		writeError(w, r, s.Logger, ModuleTokenService, err)
		return
	}

	token, expiresAt, err := s.tokens.issue(s.Config.Realtime.ClientIDClaim, clientID.String(), devTokenTTL)
	if err != nil {
		writeError(w, r, s.Logger, ModuleTokenService, err)
		return
	}
	Ok(ModuleTokenService, "Token issued successfully.", devTokenResponse{AccessToken: token, ExpiresAt: expiresAt}, http.StatusOK).write(w)
}
