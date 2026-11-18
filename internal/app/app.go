// Package app is the composition root: it wires the domain, application, infrastructure and API layers.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/Judamaro/notyhub-golang/internal/api"
	"github.com/Judamaro/notyhub-golang/internal/application"
	"github.com/Judamaro/notyhub-golang/internal/application/connections"
	"github.com/Judamaro/notyhub-golang/internal/application/notifications"
	"github.com/Judamaro/notyhub-golang/internal/config"
	"github.com/Judamaro/notyhub-golang/internal/infrastructure/metrics"
	"github.com/Judamaro/notyhub-golang/internal/infrastructure/realtime"
	"github.com/Judamaro/notyhub-golang/internal/infrastructure/registry"
)

// App holds the wired hub.
type App struct {
	cfg     config.Config
	logger  *slog.Logger
	Handler http.Handler
	// MetricsHandler serves only the metrics path; it must be exposed on an internal address.
	MetricsHandler http.Handler
	Hub            *realtime.Hub
	Registry       *registry.Memory
	Metrics        *metrics.Prometheus
}

// Option customizes the wiring (tests).
type Option func(*options)

type options struct {
	clock   application.Clock
	hubOpts realtime.HubOptions
}

// WithClock replaces the system clock.
func WithClock(clock application.Clock) Option { return func(o *options) { o.clock = clock } }

// WithHubOptions replaces the default hub options.
func WithHubOptions(opts realtime.HubOptions) Option { return func(o *options) { o.hubOpts = opts } }

// New wires the hub from its configuration.
func New(cfg config.Config, logger *slog.Logger, opts ...Option) *App {
	o := options{clock: application.SystemClock, hubOpts: realtime.DefaultHubOptions()}
	for _, opt := range opts {
		opt(&o)
	}
	if o.hubOpts.OriginPatterns == nil {
		o.hubOpts.OriginPatterns = originPatterns(cfg.CORS.AllowedOrigins)
	}

	reg := registry.NewMemory()
	m := metrics.New(reg.Stats)

	register := application.Decorate[connections.RegisterCommand, connections.RegisterResponse](
		connections.NewRegisterHandler(reg, o.clock, m), logger)
	unregister := application.Decorate[connections.UnregisterCommand, connections.ConnectionResponse](
		connections.NewUnregisterHandler(reg, m), logger)
	hub := realtime.NewHub(o.hubOpts, register, unregister, logger)

	resolver := notifications.NewStrategyResolver(notifications.DefaultStrategies(reg)...)
	send := application.Decorate[notifications.SendCommand, notifications.SendResponse](
		notifications.NewSendHandler(resolver, hub, o.clock, m), logger, notifications.SendValidator{})

	handler := api.NewHandler(api.Deps{
		Config:               cfg,
		Logger:               logger,
		Clock:                o.clock,
		SendNotification:     send,
		GetClientConnections: connections.NewGetClientConnectionsHandler(reg),
		Hub:                  hub,
		Metrics:              m,
	})

	return &App{
		cfg:            cfg,
		logger:         logger,
		Handler:        handler,
		MetricsHandler: api.NewMetricsHandler(cfg.Metrics.Path, m.Handler()),
		Hub:            hub,
		Registry:       reg,
		Metrics:        m,
	}
}

// Run serves the API and the metrics on their addresses until ctx ends, then shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	apiServer := &http.Server{Addr: a.cfg.HTTPAddr, Handler: a.Handler, ReadHeaderTimeout: 10 * time.Second}
	metricsServer := &http.Server{Addr: a.cfg.Metrics.Addr, Handler: a.MetricsHandler, ReadHeaderTimeout: 10 * time.Second}

	errs := make(chan error, 2)
	for _, srv := range []*http.Server{apiServer, metricsServer} {
		ln, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			_ = apiServer.Close()
			_ = metricsServer.Close()
			return err
		}
		a.logger.Info("Listening", "addr", ln.Addr().String())
		go func() {
			if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
				errs <- err
			}
		}()
	}

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errs:
	}

	a.logger.Info("Shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return errors.Join(runErr,
		a.Hub.Shutdown(shutdownCtx),
		apiServer.Shutdown(shutdownCtx),
		metricsServer.Shutdown(shutdownCtx),
	)
}

// originPatterns turns allowed origins (scheme://host[:port]) into the host patterns the WebSocket upgrade checks.
func originPatterns(origins []string) []string {
	patterns := make([]string, 0, len(origins))
	for _, origin := range origins {
		if u, err := url.Parse(origin); err == nil && u.Host != "" {
			patterns = append(patterns, u.Host)
		}
	}
	return patterns
}
