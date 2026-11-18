package config_test

import (
	"strings"
	"testing"

	"github.com/Judamaro/notyhub-golang/internal/config"
)

func lookup(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func TestDevelopmentDefaults(t *testing.T) {
	cfg, err := config.LoadFrom(lookup(map[string]string{"APP_ENV": "development"}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsDevelopment() || cfg.Metrics.Addr != ":9465" || len(cfg.APIKeys.Keys) != 1 || len(cfg.CORS.AllowedOrigins) != 2 {
		t.Fatalf("unexpected development config %+v", cfg)
	}
	if cfg.Realtime.HubPath != "/ws/notifications" || cfg.Realtime.ClientIDClaim != "sub" {
		t.Fatalf("unexpected realtime config %+v", cfg.Realtime)
	}
}

func TestProductionRequiresSecrets(t *testing.T) {
	_, err := config.LoadFrom(lookup(map[string]string{}))
	if err == nil {
		t.Fatal("production without signing key and API keys must fail")
	}
	for _, want := range []string{"JWT_SIGNING_KEY", "API_KEYS"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestProductionConfig(t *testing.T) {
	cfg, err := config.LoadFrom(lookup(map[string]string{
		"JWT_SIGNING_KEY":      "production-signing-key-0123456789abcdef",
		"API_KEYS":             " k1 , k2 ,",
		"CORS_ALLOWED_ORIGINS": "https://app.example.com",
		"LOG_LEVEL":            "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsDevelopment() || cfg.Metrics.Addr != ":9464" || len(cfg.APIKeys.Keys) != 2 || cfg.APIKeys.Keys[1] != "k2" {
		t.Fatalf("unexpected config %+v", cfg)
	}
}

func TestInvalidValues(t *testing.T) {
	_, err := config.LoadFrom(lookup(map[string]string{
		"APP_ENV":              "staging",
		"JWT_SIGNING_KEY":      "short",
		"API_KEYS":             "k",
		"METRICS_PATH":         "metrics",
		"METRICS_ADDR":         ":8080",
		"CORS_ALLOWED_ORIGINS": "not-an-origin",
		"LOG_LEVEL":            "loud",
	}))
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"APP_ENV", "JWT_SIGNING_KEY", "METRICS_PATH", "METRICS_ADDR", "CORS_ALLOWED_ORIGINS", "LOG_LEVEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s:\n%v", want, err)
		}
	}
}
