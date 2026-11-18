package metrics_test

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Judamaro/notyhub-golang/internal/application"
	"github.com/Judamaro/notyhub-golang/internal/domain"
	"github.com/Judamaro/notyhub-golang/internal/infrastructure/metrics"
)

func TestHubMetrics(t *testing.T) {
	stats := application.RegistryStats{Clients: 2, Connections: 5}
	m := metrics.New(func() application.RegistryStats { return stats })

	m.ConnectionOpened()
	m.ConnectionOpened()
	m.ConnectionReplaced()
	m.ConnectionClosed()
	m.NotificationDelivered(domain.ClientTarget{ClientID: "acme"}, 3, 2*time.Millisecond)
	m.NotificationDelivered(domain.UserTarget{ClientID: "acme", UserID: "u1"}, 1, time.Millisecond)
	m.NotificationNotFound(domain.UserTarget{ClientID: "acme", UserID: "u2"})
	m.RequestStarted()("/health", "GET", 200, time.Millisecond)

	expected := `
# HELP notyhub_clients_active Clients with at least one connection registered in the hub.
# TYPE notyhub_clients_active gauge
notyhub_clients_active 2
# HELP notyhub_connections_active Connections currently registered in the hub.
# TYPE notyhub_connections_active gauge
notyhub_connections_active 5
# HELP notyhub_connections_closed_total Connections removed from the hub.
# TYPE notyhub_connections_closed_total counter
notyhub_connections_closed_total 1
# HELP notyhub_connections_opened_total Connections registered in the hub.
# TYPE notyhub_connections_opened_total counter
notyhub_connections_opened_total 2
# HELP notyhub_connections_replaced_total Connections closed because the same user opened a new one.
# TYPE notyhub_connections_replaced_total counter
notyhub_connections_replaced_total 1
# HELP notyhub_notifications_recipients_total Connections notifications were delivered to.
# TYPE notyhub_notifications_recipients_total counter
notyhub_notifications_recipients_total{target="client"} 3
notyhub_notifications_recipients_total{target="user"} 1
# HELP notyhub_notifications_sent_total Notification requests, by target and outcome.
# TYPE notyhub_notifications_sent_total counter
notyhub_notifications_sent_total{outcome="delivered",target="client"} 1
notyhub_notifications_sent_total{outcome="delivered",target="user"} 1
notyhub_notifications_sent_total{outcome="not_found",target="client"} 0
notyhub_notifications_sent_total{outcome="not_found",target="user"} 1
# HELP target_info Target metadata.
# TYPE target_info gauge
target_info{service_name="notyhub"} 1
`
	names := []string{
		"notyhub_clients_active", "notyhub_connections_active",
		"notyhub_connections_closed_total", "notyhub_connections_opened_total", "notyhub_connections_replaced_total",
		"notyhub_notifications_recipients_total", "notyhub_notifications_sent_total", "target_info",
	}
	if err := testutil.GatherAndCompare(m.Registry(), strings.NewReader(expected), names...); err != nil {
		t.Fatal(err)
	}

	if n := testutil.CollectAndCount(m.Registry(), "notyhub_notifications_dispatch_duration_seconds"); n != 2 {
		t.Fatalf("dispatch duration series = %d, want 2", n)
	}
	if n := testutil.CollectAndCount(m.Registry(), "http_server_request_duration_seconds"); n != 1 {
		t.Fatalf("http duration series = %d, want 1", n)
	}
}
