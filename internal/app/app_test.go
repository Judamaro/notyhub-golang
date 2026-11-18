package app_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Judamaro/notyhub-golang/internal/app"
	"github.com/Judamaro/notyhub-golang/internal/config"
	"github.com/Judamaro/notyhub-golang/internal/infrastructure/realtime"
)

const apiKey = "dev-api-key-change-me"

type petition struct {
	Success    bool            `json:"success"`
	Message    string          `json:"message"`
	Module     string          `json:"module"`
	Result     json.RawMessage `json:"result"`
	StatusCode int             `json:"statusCode"`
}

type frame struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type harness struct {
	t   *testing.T
	srv *httptest.Server
	app *app.App
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	cfg, err := config.LoadFrom(func(k string) (string, bool) {
		if k == "APP_ENV" {
			return "development", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}

	hubOpts := realtime.DefaultHubOptions()
	a := app.New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), app.WithHubOptions(hubOpts))
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Hub.Shutdown(ctx)
		srv.Close()
	})
	return &harness{t: t, srv: srv, app: a}
}

func (h *harness) do(method, path, body string, headers map[string]string) petition {
	h.t.Helper()
	req, err := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()

	var p petition
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		h.t.Fatalf("%s %s: body is not a PetitionResponse: %v", method, path, err)
	}
	if p.StatusCode != resp.StatusCode {
		h.t.Fatalf("%s %s: statusCode %d differs from HTTP status %d", method, path, p.StatusCode, resp.StatusCode)
	}
	return p
}

func (h *harness) post(path, body string) petition {
	return h.do(http.MethodPost, path, body, map[string]string{"X-Api-Key": apiKey, "Content-Type": "application/json"})
}

func (h *harness) token(clientID string) string {
	h.t.Helper()
	p := h.do(http.MethodPost, "/dev/token", `{"clientId":"`+clientID+`"}`, map[string]string{"Content-Type": "application/json"})
	if p.StatusCode != http.StatusOK || p.Module != "TokenService" {
		h.t.Fatalf("token: %+v", p)
	}
	var result struct {
		AccessToken string `json:"accessToken"`
	}
	_ = json.Unmarshal(p.Result, &result)
	return result.AccessToken
}

func (h *harness) wsURL(query url.Values) string {
	return "ws" + strings.TrimPrefix(h.srv.URL, "http") + "/ws/notifications?" + query.Encode()
}

func (h *harness) connect(clientID, userID string) (*websocket.Conn, string) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	q := url.Values{"clientId": {clientID}, "userId": {userID}, "access_token": {h.token(clientID)}}
	c, _, err := websocket.Dial(ctx, h.wsURL(q), nil)
	if err != nil {
		h.t.Fatalf("dial: %v", err)
	}
	h.t.Cleanup(func() { _ = c.CloseNow() })

	f := readFrame(h.t, c)
	if f.Event != realtime.EventConnected {
		h.t.Fatalf("first frame = %s, want Connected", f.Event)
	}
	var info realtime.ConnectionInfo
	_ = json.Unmarshal(f.Data, &info)

	h.waitForConnections(strings.ToLower(clientID), func(n int) bool { return n >= 1 })
	return c, info.ConnectionID
}

// waitForConnections polls the registry: registration happens right after the Connected frame is queued.
func (h *harness) waitForConnections(clientID string, ok func(int) bool) {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := h.do(http.MethodGet, "/api/clients/"+clientID+"/connections", "", map[string]string{"X-Api-Key": apiKey})
		var list []map[string]any
		_ = json.Unmarshal(got.Result, &list)
		if ok(len(list)) {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("connections of %s never matched, last %d", clientID, len(list))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readFrame(t *testing.T, c *websocket.Conn) frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	var f frame
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("frame is not JSON: %s", data)
	}
	return f
}

func TestHealth(t *testing.T) {
	h := newHarness(t)
	p := h.do(http.MethodGet, "/health", "", nil)
	if !p.Success || p.Module != "HealthCheck" || p.StatusCode != 200 || !strings.Contains(string(p.Result), `"Healthy"`) {
		t.Fatalf("health = %+v", p)
	}
}

func TestPipelineErrorsArePetitionResponses(t *testing.T) {
	h := newHarness(t)

	tests := []struct {
		name    string
		method  string
		path    string
		body    string
		headers map[string]string
		status  int
		module  string
		message string
	}{
		{"unknown route", "GET", "/nope", "", nil, 404, "Api", "Not Found."},
		{"method not allowed", "DELETE", "/health", "", nil, 405, "Api", "Method Not Allowed."},
		{"missing api key", "POST", "/api/notifications/clients/acme", `{"type":"t"}`, map[string]string{"Content-Type": "application/json"}, 401, "Api", "Unauthorized."},
		{"invalid api key", "GET", "/api/clients/acme/connections", "", map[string]string{"X-Api-Key": "wrong"}, 401, "Api", "Unauthorized."},
		{"not json", "POST", "/api/notifications/clients/acme", "type=t", map[string]string{"X-Api-Key": apiKey, "Content-Type": "text/plain"}, 415, "Api", "Unsupported Media Type."},
		{"malformed json", "POST", "/api/notifications/clients/acme", `{"type":`, map[string]string{"X-Api-Key": apiKey, "Content-Type": "application/json"}, 400, "RequestValidation", "One or more validation errors occurred."},
		{"missing body", "POST", "/api/notifications/clients/acme", "", map[string]string{"X-Api-Key": apiKey, "Content-Type": "application/json"}, 400, "RequestValidation", "One or more validation errors occurred."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := h.do(tt.method, tt.path, tt.body, tt.headers)
			if p.Success || p.StatusCode != tt.status || p.Module != tt.module || p.Message != tt.message {
				t.Fatalf("got %+v", p)
			}
		})
	}
}

func TestSendValidation(t *testing.T) {
	h := newHarness(t)

	p := h.post("/api/notifications/clients/acme", `{"payload":{"a":1}}`)
	if p.StatusCode != 400 || p.Module != "NotificationService" || !strings.Contains(string(p.Result), `"Type":["'Type' must not be empty."]`) {
		t.Fatalf("got %+v %s", p, p.Result)
	}

	p = h.post("/api/notifications/clients/ac%20me", `{"type":"t"}`)
	if p.StatusCode != 400 || p.Message != "The client id cannot contain whitespace." {
		t.Fatalf("got %+v", p)
	}

	p = h.post("/api/notifications/clients/acme", `{"type":"t"}`)
	if p.StatusCode != 404 || p.Message != "The client 'acme' has no active connections." {
		t.Fatalf("got %+v", p)
	}

	p = h.post("/api/notifications/clients/acme/users/u9", `{"type":"t"}`)
	if p.StatusCode != 404 || p.Message != "The user 'u9' of the client 'acme' has no active connection." {
		t.Fatalf("got %+v", p)
	}
}

func TestHubRejectsInvalidConnections(t *testing.T) {
	h := newHarness(t)
	token := h.token("acme")

	tests := []struct {
		name    string
		query   url.Values
		status  int
		message string
	}{
		{"no token", url.Values{"clientId": {"acme"}, "userId": {"u1"}}, 401, "Unauthorized."},
		{"invalid token", url.Values{"clientId": {"acme"}, "userId": {"u1"}, "access_token": {"x.y.z"}}, 401, "Unauthorized."},
		{"missing user", url.Values{"clientId": {"acme"}, "access_token": {token}}, 400, "The user id is required."},
		{"user with whitespace", url.Values{"clientId": {"acme"}, "userId": {"u 1"}, "access_token": {token}}, 400, "The user id cannot contain whitespace."},
		{"other client", url.Values{"clientId": {"globex"}, "userId": {"u1"}, "access_token": {token}}, 400, "The client id does not match the client id of the access token."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := h.do(http.MethodGet, "/ws/notifications?"+tt.query.Encode(), "", nil)
			if p.StatusCode != tt.status || p.Message != tt.message {
				t.Fatalf("got %+v", p)
			}
		})
	}
}

func TestNotificationFlow(t *testing.T) {
	h := newHarness(t)
	c1, id1 := h.connect("ACME", "u1")
	c2, _ := h.connect("acme", "u2")

	// Client-wide notification reaches both users.
	p := h.post("/api/notifications/clients/Acme", `{"type":"order.updated","payload":{"orderId":7}}`)
	if !p.Success || p.Message != "Notification sent successfully." || !strings.Contains(string(p.Result), `"deliveredConnections":2`) {
		t.Fatalf("send = %+v %s", p, p.Result)
	}
	for _, c := range []*websocket.Conn{c1, c2} {
		f := readFrame(t, c)
		var msg realtime.NotificationMessage
		_ = json.Unmarshal(f.Data, &msg)
		if f.Event != realtime.EventReceiveNotification || msg.Type != "order.updated" || string(msg.Payload) != `{"orderId":7}` {
			t.Fatalf("frame = %s %s", f.Event, f.Data)
		}
	}

	// User notification reaches only that user.
	p = h.post("/api/notifications/clients/acme/users/U1", `{"type":"ping"}`)
	if !strings.Contains(string(p.Result), `"deliveredConnections":1`) {
		t.Fatalf("send to user = %+v", p)
	}
	f := readFrame(t, c1)
	if f.Event != realtime.EventReceiveNotification || !strings.Contains(string(f.Data), `"payload":null`) {
		t.Fatalf("frame = %s %s", f.Event, f.Data)
	}

	// Clients can ask for their connection id.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c1.Write(ctx, websocket.MessageText, []byte(`{"event":"GetConnectionId"}`)); err != nil {
		t.Fatal(err)
	}
	f = readFrame(t, c1)
	if f.Event != realtime.EventConnectionID || !strings.Contains(string(f.Data), id1) {
		t.Fatalf("frame = %s %s", f.Event, f.Data)
	}

	// Connections list, normalized ids.
	p = h.do(http.MethodGet, "/api/clients/ACME/connections", "", map[string]string{"X-Api-Key": apiKey})
	var list []map[string]any
	_ = json.Unmarshal(p.Result, &list)
	if p.Message != "Connections retrieved successfully." || len(list) != 2 || list[0]["clientId"] != "acme" || list[0]["userId"] != "u1" {
		t.Fatalf("connections = %+v %s", p, p.Result)
	}

	// Closing a connection unregisters it.
	_ = c2.Close(websocket.StatusNormalClosure, "bye")
	h.waitForConnections("acme", func(n int) bool { return n == 1 })
}

func TestNewConnectionReplacesPreviousOfUser(t *testing.T) {
	h := newHarness(t)
	old, _ := h.connect("acme", "u1")
	_, newID := h.connect("acme", "u1")

	if f := readFrame(t, old); f.Event != realtime.EventConnectionReplaced {
		t.Fatalf("frame = %s", f.Event)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := old.Read(ctx)
	if status := websocket.CloseStatus(err); status != realtime.StatusConnectionReplaced {
		t.Fatalf("close status = %d (%v), want 4000", status, err)
	}

	h.waitForConnections("acme", func(n int) bool { return n == 1 })
	p := h.do(http.MethodGet, "/api/clients/acme/connections", "", map[string]string{"X-Api-Key": apiKey})
	if !strings.Contains(string(p.Result), newID) {
		t.Fatalf("remaining connection is not the new one: %s", p.Result)
	}
}

func TestMetricsHandler(t *testing.T) {
	h := newHarness(t)
	srv := httptest.NewServer(h.app.MetricsHandler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "notyhub_connections_active 0") {
		t.Fatalf("metrics = %d\n%s", resp.StatusCode, body)
	}

	resp, err = http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("other path on the metrics address = %d, want 404", resp.StatusCode)
	}

	// The public API never serves the metrics.
	if p := h.do(http.MethodGet, "/metrics", "", nil); p.StatusCode != 404 {
		t.Fatalf("public /metrics = %d", p.StatusCode)
	}
}
