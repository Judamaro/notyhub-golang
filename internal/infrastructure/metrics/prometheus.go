// Package metrics exports the hub and HTTP metrics in the Prometheus format.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Judamaro/notyhub-golang/internal/application"
	"github.com/Judamaro/notyhub-golang/internal/domain"
)

// ServiceName is reported as service_name in target_info.
const ServiceName = "notyhub"

const (
	labelTarget  = "target"
	labelOutcome = "outcome"
)

// Prometheus records the hub metrics (application.Metrics) and the HTTP server metrics in its own registry.
// Labels are low cardinality on purpose: client ids, user ids, connection ids and notification types are not labels.
type Prometheus struct {
	registry *prometheus.Registry

	connectionsOpened   prometheus.Counter
	connectionsClosed   prometheus.Counter
	connectionsReplaced prometheus.Counter
	notificationsSent   *prometheus.CounterVec
	recipients          *prometheus.CounterVec
	dispatchDuration    *prometheus.HistogramVec
	httpDuration        *prometheus.HistogramVec
	httpActive          prometheus.Gauge
}

var _ application.Metrics = (*Prometheus)(nil)

// New creates the metrics. stats is read on every scrape for the active gauges, so they cannot drift.
func New(stats func() application.RegistryStats) *Prometheus {
	reg := prometheus.NewRegistry()
	m := &Prometheus{
		registry: reg,
		connectionsOpened: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "notyhub_connections_opened_total", Help: "Connections registered in the hub.",
		}),
		connectionsClosed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "notyhub_connections_closed_total", Help: "Connections removed from the hub.",
		}),
		connectionsReplaced: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "notyhub_connections_replaced_total", Help: "Connections closed because the same user opened a new one.",
		}),
		notificationsSent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "notyhub_notifications_sent_total", Help: "Notification requests, by target and outcome.",
		}, []string{labelTarget, labelOutcome}),
		recipients: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "notyhub_notifications_recipients_total", Help: "Connections notifications were delivered to.",
		}, []string{labelTarget}),
		dispatchDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "notyhub_notifications_dispatch_duration_seconds",
			Help:    "Time the hub took to dispatch a notification to its connections.",
			Buckets: []float64{0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		}, []string{labelTarget}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_server_request_duration_seconds",
			Help:    "Duration of HTTP server requests.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10},
		}, []string{"http_route", "http_request_method", "http_response_status_code"}),
		httpActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_server_active_requests", Help: "Number of active HTTP server requests.",
		}),
	}

	targetInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "target_info", Help: "Target metadata.", ConstLabels: prometheus.Labels{"service_name": ServiceName},
	})
	targetInfo.Set(1)

	reg.MustRegister(
		m.connectionsOpened, m.connectionsClosed, m.connectionsReplaced,
		m.notificationsSent, m.recipients, m.dispatchDuration,
		m.httpDuration, m.httpActive, targetInfo,
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "notyhub_connections_active", Help: "Connections currently registered in the hub.",
		}, func() float64 { return float64(stats().Connections) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "notyhub_clients_active", Help: "Clients with at least one connection registered in the hub.",
		}, func() float64 { return float64(stats().Clients) }),
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	// Report zero for every known series before the first notification instead of leaving them absent.
	for _, target := range []string{"client", "user"} {
		m.notificationsSent.WithLabelValues(target, "delivered")
		m.notificationsSent.WithLabelValues(target, "not_found")
		m.recipients.WithLabelValues(target)
	}

	return m
}

// Handler serves the Prometheus scraping endpoint.
func (m *Prometheus) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{Registry: m.registry})
}

// Registry returns the underlying registry.
func (m *Prometheus) Registry() *prometheus.Registry { return m.registry }

func (m *Prometheus) ConnectionOpened()   { m.connectionsOpened.Inc() }
func (m *Prometheus) ConnectionClosed()   { m.connectionsClosed.Inc() }
func (m *Prometheus) ConnectionReplaced() { m.connectionsReplaced.Inc() }

// NotificationDelivered records a delivered notification, its recipients and dispatch time.
func (m *Prometheus) NotificationDelivered(target domain.Target, recipients int, duration time.Duration) {
	name := targetName(target)
	m.notificationsSent.WithLabelValues(name, "delivered").Inc()
	m.recipients.WithLabelValues(name).Add(float64(recipients))
	m.dispatchDuration.WithLabelValues(name).Observe(duration.Seconds())
}

// NotificationNotFound records a notification whose target had no connections.
func (m *Prometheus) NotificationNotFound(target domain.Target) {
	m.notificationsSent.WithLabelValues(targetName(target), "not_found").Inc()
}

// RequestStarted increments the active HTTP requests and returns the function that records the request.
func (m *Prometheus) RequestStarted() func(route, method string, status int, duration time.Duration) {
	m.httpActive.Inc()
	return func(route, method string, status int, duration time.Duration) {
		m.httpActive.Dec()
		m.httpDuration.WithLabelValues(route, method, strconv.Itoa(status)).Observe(duration.Seconds())
	}
}

func targetName(target domain.Target) string {
	switch target.(type) {
	case domain.ClientTarget:
		return "client"
	case domain.UserTarget:
		return "user"
	default:
		return "other"
	}
}
