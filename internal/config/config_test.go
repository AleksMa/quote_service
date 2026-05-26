package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadFromYAML(t *testing.T) {
	cfg := loadConfig(t, `
http:
  addr: ":9090"
  cors_allowed_origins:
    - http://localhost:*
    - http://127.0.0.1:*
database:
  driver: postgres
  url: "postgres://example"
worker:
  interval: 3s
  concurrency: 4
shutdown_timeout: 8s
supported_pairs:
  - EUR/USD
providers:
  - name: second
    type: fawaz
    enabled: true
    priority: 20
    base_url: https://cdn.example.test/currency-api
    timeout: 7s
    rate_limit_per_second: 1
  - name: first
    type: frankfurter
    enabled: true
    priority: 10
    base_url: https://api.example.test
    timeout: 5s
    rate_limit_per_second: 2
`)

	if cfg.HTTP.Addr != ":9090" || cfg.Database.Driver != "postgres" || cfg.Database.URL != "postgres://example" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if len(cfg.HTTP.CORSAllowedOrigins) != 2 || cfg.HTTP.CORSAllowedOrigins[0] != "http://localhost:*" {
		t.Fatalf("unexpected CORS origins: %+v", cfg.HTTP.CORSAllowedOrigins)
	}
	if cfg.Worker.Interval != 3*time.Second || cfg.Worker.Concurrency != 4 {
		t.Fatalf("unexpected worker config: %+v", cfg.Worker)
	}
	if cfg.ShutdownTimeout != 8*time.Second {
		t.Fatalf("unexpected shutdown timeout: %s", cfg.ShutdownTimeout)
	}
	if _, ok := cfg.SupportedPairs["EUR/USD"]; !ok {
		t.Fatalf("supported pair was not loaded")
	}
	if cfg.Providers[0].Name != "first" || cfg.Providers[1].Name != "second" {
		t.Fatalf("providers were not sorted by priority: %+v", cfg.Providers)
	}
}

func TestLoadRequiresExistingFile(t *testing.T) {
	_, err := LoadFromPath(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadRejectsMissingRequiredFields(t *testing.T) {
	tests := map[string]string{
		"http addr": `
database:
  driver: postgres
  url: "postgres://example"
worker:
  interval: 3s
  concurrency: 4
shutdown_timeout: 8s
supported_pairs: [EUR/USD]
providers:
  - name: first
    type: frankfurter
    enabled: true
    priority: 1
    base_url: https://api.example.test
    timeout: 5s
    rate_limit_per_second: 2
`,
		"database driver": `
http:
  addr: ":9090"
database:
  url: "postgres://example"
worker:
  interval: 3s
  concurrency: 4
shutdown_timeout: 8s
supported_pairs: [EUR/USD]
providers:
  - name: first
    type: frankfurter
    enabled: true
    priority: 1
    base_url: https://api.example.test
    timeout: 5s
    rate_limit_per_second: 2
`,
		"worker interval": `
http:
  addr: ":9090"
database:
  driver: postgres
  url: "postgres://example"
worker:
  concurrency: 4
shutdown_timeout: 8s
supported_pairs: [EUR/USD]
providers:
  - name: first
    type: frankfurter
    enabled: true
    priority: 1
    base_url: https://api.example.test
    timeout: 5s
    rate_limit_per_second: 2
`,
		"supported pairs": `
http:
  addr: ":9090"
database:
  driver: postgres
  url: "postgres://example"
worker:
  interval: 3s
  concurrency: 4
shutdown_timeout: 8s
providers:
  - name: first
    type: frankfurter
    enabled: true
    priority: 1
    base_url: https://api.example.test
    timeout: 5s
    rate_limit_per_second: 2
`,
		"provider timeout": `
http:
  addr: ":9090"
database:
  driver: postgres
  url: "postgres://example"
worker:
  interval: 3s
  concurrency: 4
shutdown_timeout: 8s
supported_pairs: [EUR/USD]
providers:
  - name: first
    type: frankfurter
    enabled: true
    priority: 1
    base_url: https://api.example.test
    rate_limit_per_second: 2
`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := loadConfigErr(t, body)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestLoadRejectsNoEnabledProviders(t *testing.T) {
	_, err := loadConfigErr(t, `
http:
  addr: ":9090"
database:
  driver: postgres
  url: "postgres://example"
worker:
  interval: 3s
  concurrency: 4
shutdown_timeout: 8s
supported_pairs:
  - EUR/USD
providers:
  - name: frankfurter
    type: frankfurter
    enabled: false
    priority: 1
    base_url: https://api.example.test
    timeout: 5s
    rate_limit_per_second: 1
`)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadRejectsInvalidCORSOrigin(t *testing.T) {
	_, err := loadConfigErr(t, `
http:
  addr: ":9090"
  cors_allowed_origins:
    - localhost:3000
database:
  driver: postgres
  url: "postgres://example"
worker:
  interval: 3s
  concurrency: 4
shutdown_timeout: 8s
supported_pairs:
  - EUR/USD
providers:
  - name: frankfurter
    type: frankfurter
    enabled: true
    priority: 1
    base_url: https://api.example.test
    timeout: 5s
    rate_limit_per_second: 1
`)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadDoesNotOverrideYAMLFromEnv(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":7070")
	t.Setenv("DATABASE_URL", "postgres://env")

	cfg := loadConfig(t, `
http:
  addr: ":9090"
database:
  driver: postgres
  url: "postgres://yaml"
worker:
  interval: 3s
  concurrency: 4
shutdown_timeout: 8s
supported_pairs:
  - EUR/USD
providers:
  - name: first
    type: frankfurter
    enabled: true
    priority: 1
    base_url: https://api.example.test
    timeout: 5s
    rate_limit_per_second: 2
`)
	if cfg.HTTP.Addr != ":9090" || cfg.Database.URL != "postgres://yaml" {
		t.Fatalf("config was unexpectedly overridden from env: %+v", cfg)
	}
}

func loadConfig(t *testing.T, body string) Config {
	t.Helper()

	cfg, err := loadConfigErr(t, body)
	if err != nil {
		t.Fatalf("LoadFromPath returned error: %v", err)
	}
	return cfg
}

func loadConfigErr(t *testing.T, body string) (Config, error) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return LoadFromPath(path)
}
