// Package config loads and validates the configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"
)

// Environments.
const (
	EnvProduction  = "production"
	EnvDevelopment = "development"
)

// Config is the configuration of the hub.
type Config struct {
	Env      string
	LogLevel slog.Level
	HTTPAddr string
	Metrics  Metrics
	JWT      JWT
	APIKeys  APIKeys
	CORS     CORS
	Realtime Realtime
}

// Metrics configures the Prometheus scraping endpoint, served on its own internal address.
type Metrics struct {
	Addr string
	Path string
}

// JWT configures the validation of client tokens (HMAC-SHA256).
type JWT struct {
	Issuer     string
	Audience   string
	SigningKey string
}

// APIKeys configures the keys of the backends allowed to send notifications.
type APIKeys struct {
	HeaderName string
	Keys       []string
}

// CORS lists the browser origins allowed to call the API and connect to the hub.
type CORS struct {
	AllowedOrigins []string
}

// Realtime configures the WebSocket hub.
type Realtime struct {
	HubPath            string
	ClientIDClaim      string
	ClientIDQueryParam string
	UserIDQueryParam   string
}

// IsDevelopment reports whether the hub runs in the development environment.
func (c Config) IsDevelopment() bool { return c.Env == EnvDevelopment }

// Development defaults: local values only, never meant for shared environments.
const (
	devSigningKey = "development-only-signing-key-change-me-0123456789"
	devAPIKey     = "dev-api-key-change-me"
	devOrigins    = "http://localhost:3000,http://localhost:5173"
)

// Load reads the configuration from the environment. In development, the signing key, API key, CORS origins and
// metrics address have local defaults. It returns every validation failure at once.
func Load() (Config, error) { return LoadFrom(os.LookupEnv) }

// LoadFrom reads the configuration through lookup (os.LookupEnv in production code).
func LoadFrom(lookup func(string) (string, bool)) (Config, error) {
	get := func(key, fallback string) string {
		if v, ok := lookup(key); ok {
			return strings.TrimSpace(v)
		}
		return fallback
	}

	env := strings.ToLower(get("APP_ENV", EnvProduction))
	dev := env == EnvDevelopment
	pick := func(devValue, prodValue string) string {
		if dev {
			return devValue
		}
		return prodValue
	}

	cfg := Config{
		Env:      env,
		HTTPAddr: get("HTTP_ADDR", ":8080"),
		Metrics: Metrics{
			Addr: get("METRICS_ADDR", pick(":9465", ":9464")),
			Path: get("METRICS_PATH", "/metrics"),
		},
		JWT: JWT{
			Issuer:     get("JWT_ISSUER", "notyhub"),
			Audience:   get("JWT_AUDIENCE", "notyhub-clients"),
			SigningKey: get("JWT_SIGNING_KEY", pick(devSigningKey, "")),
		},
		APIKeys: APIKeys{
			HeaderName: get("API_KEY_HEADER", "X-Api-Key"),
			Keys:       splitList(get("API_KEYS", pick(devAPIKey, ""))),
		},
		CORS: CORS{AllowedOrigins: splitList(get("CORS_ALLOWED_ORIGINS", pick(devOrigins, "")))},
		Realtime: Realtime{
			HubPath:            get("REALTIME_HUB_PATH", "/ws/notifications"),
			ClientIDClaim:      get("REALTIME_CLIENT_ID_CLAIM", "sub"),
			ClientIDQueryParam: get("REALTIME_CLIENT_ID_QUERY", "clientId"),
			UserIDQueryParam:   get("REALTIME_USER_ID_QUERY", "userId"),
		},
	}

	var errs []error
	if err := cfg.LogLevel.UnmarshalText([]byte(get("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}
	errs = append(errs, cfg.validate()...)

	return cfg, errors.Join(errs...)
}

func (c Config) validate() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if c.Env != EnvProduction && c.Env != EnvDevelopment {
		fail("APP_ENV must be %q or %q", EnvProduction, EnvDevelopment)
	}
	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		fail("HTTP_ADDR is not a valid address: %v", err)
	}
	if _, _, err := net.SplitHostPort(c.Metrics.Addr); err != nil {
		fail("METRICS_ADDR is not a valid address: %v", err)
	}
	if c.Metrics.Addr == c.HTTPAddr {
		fail("METRICS_ADDR must be different from HTTP_ADDR so the metrics are never public")
	}
	if !strings.HasPrefix(c.Metrics.Path, "/") {
		fail("METRICS_PATH must start with '/'")
	}
	if c.JWT.Issuer == "" {
		fail("JWT_ISSUER is required")
	}
	if c.JWT.Audience == "" {
		fail("JWT_AUDIENCE is required")
	}
	if utf8.RuneCountInString(c.JWT.SigningKey) < 32 {
		fail("JWT_SIGNING_KEY is required and must have at least 32 characters")
	}
	if c.APIKeys.HeaderName == "" {
		fail("API_KEY_HEADER is required")
	}
	if len(c.APIKeys.Keys) == 0 {
		fail("API_KEYS must hold at least one key")
	}
	for _, origin := range c.CORS.AllowedOrigins {
		if u, err := url.Parse(origin); err != nil || u.Scheme == "" || u.Host == "" {
			fail("CORS_ALLOWED_ORIGINS has an invalid origin %q", origin)
		}
	}
	if !strings.HasPrefix(c.Realtime.HubPath, "/") {
		fail("REALTIME_HUB_PATH must start with '/'")
	}
	if c.Realtime.ClientIDClaim == "" || c.Realtime.ClientIDQueryParam == "" || c.Realtime.UserIDQueryParam == "" {
		fail("REALTIME_CLIENT_ID_CLAIM, REALTIME_CLIENT_ID_QUERY and REALTIME_USER_ID_QUERY are required")
	}
	return errs
}

func splitList(value string) []string {
	var items []string
	for item := range strings.SplitSeq(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}
