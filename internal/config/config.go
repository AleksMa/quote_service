package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/AleksMa/quote_service/internal/domain"
)

const DefaultPath = "config.yaml"

type Config struct {
	HTTP            HTTPConfig
	Database        DatabaseConfig
	Worker          WorkerConfig
	Providers       []ProviderConfig
	SupportedPairs  map[string]struct{}
	ShutdownTimeout time.Duration
}

type HTTPConfig struct {
	Addr string `yaml:"addr"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	URL    string `yaml:"url"`
}

type WorkerConfig struct {
	Interval    time.Duration `yaml:"interval"`
	Concurrency int           `yaml:"concurrency"`
}

type ProviderConfig struct {
	Name               string        `yaml:"name"`
	Type               string        `yaml:"type"`
	Enabled            bool          `yaml:"enabled"`
	Priority           int           `yaml:"priority"`
	BaseURL            string        `yaml:"base_url"`
	Timeout            time.Duration `yaml:"timeout"`
	RateLimitPerSecond int           `yaml:"rate_limit_per_second"`
	APIKey             string        `yaml:"api_key"`
	APIKeyEnv          string        `yaml:"api_key_env"`
}

type rawConfig struct {
	HTTP            HTTPConfig       `yaml:"http"`
	Database        DatabaseConfig   `yaml:"database"`
	Worker          WorkerConfig     `yaml:"worker"`
	Providers       []ProviderConfig `yaml:"providers"`
	SupportedPairs  []string         `yaml:"supported_pairs"`
	ShutdownTimeout duration         `yaml:"shutdown_timeout"`
}

type duration struct {
	time.Duration
}

func Load() (Config, error) {
	return LoadFromPath(getenv("CONFIG_PATH", DefaultPath))
}

func LoadFromPath(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}

	return validate(raw)
}

func validate(raw rawConfig) (Config, error) {
	if strings.TrimSpace(raw.HTTP.Addr) == "" {
		return Config{}, fmt.Errorf("http.addr is required")
	}
	raw.HTTP.Addr = strings.TrimSpace(raw.HTTP.Addr)

	raw.Database.Driver = strings.TrimSpace(raw.Database.Driver)
	if raw.Database.Driver == "" {
		return Config{}, fmt.Errorf("database.driver is required")
	}
	raw.Database.URL = strings.TrimSpace(raw.Database.URL)
	if raw.Database.URL == "" {
		return Config{}, fmt.Errorf("database.url is required")
	}

	if raw.Worker.Interval <= 0 {
		return Config{}, fmt.Errorf("worker.interval must be greater than zero")
	}
	if raw.Worker.Concurrency < 1 {
		return Config{}, fmt.Errorf("worker.concurrency must be greater than zero")
	}

	if raw.ShutdownTimeout.Duration <= 0 {
		return Config{}, fmt.Errorf("shutdown_timeout must be greater than zero")
	}

	if len(raw.SupportedPairs) == 0 {
		return Config{}, fmt.Errorf("supported_pairs must contain at least one pair")
	}
	supportedPairs, err := domain.PairSet(raw.SupportedPairs)
	if err != nil {
		return Config{}, fmt.Errorf("load supported pairs: %w", err)
	}

	providers, err := validateProviders(raw.Providers)
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTP:            raw.HTTP,
		Database:        raw.Database,
		Worker:          raw.Worker,
		Providers:       providers,
		SupportedPairs:  supportedPairs,
		ShutdownTimeout: raw.ShutdownTimeout.Duration,
	}, nil
}

func validateProviders(providers []ProviderConfig) ([]ProviderConfig, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("providers must contain at least one provider")
	}

	enabledCount := 0
	names := make(map[string]struct{}, len(providers))
	for i := range providers {
		provider := &providers[i]
		provider.Name = strings.TrimSpace(provider.Name)
		provider.Type = strings.TrimSpace(provider.Type)
		provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
		provider.APIKey = strings.TrimSpace(provider.APIKey)
		provider.APIKeyEnv = strings.TrimSpace(provider.APIKeyEnv)

		if provider.Name == "" {
			return nil, fmt.Errorf("provider %q name is required", i)
		}

		if _, ok := names[provider.Name]; ok {
			return nil, fmt.Errorf("provider %q is duplicated", provider.Name)
		}
		names[provider.Name] = struct{}{}

		if provider.Type == "" {
			return nil, fmt.Errorf("provider %q type is required", provider.Name)
		}
		if provider.BaseURL == "" {
			return nil, fmt.Errorf("provider %q base_url is required", provider.Name)
		}
		if provider.Timeout <= 0 {
			return nil, fmt.Errorf("provider %q timeout must be greater than zero", provider.Name)
		}
		if provider.RateLimitPerSecond < 0 {
			return nil, fmt.Errorf("provider %q rate_limit_per_second cannot be negative", provider.Name)
		}
		if provider.Enabled {
			enabledCount++
		}
	}
	if enabledCount == 0 {
		return nil, fmt.Errorf("at least one provider must be enabled")
	}

	sort.SliceStable(providers, func(i, j int) bool {
		return providers[i].Priority < providers[j].Priority
	})
	return providers, nil
}

func (c ProviderConfig) ResolvedAPIKey() string {
	if c.APIKeyEnv != "" {
		if value := strings.TrimSpace(os.Getenv(c.APIKeyEnv)); value != "" {
			return value
		}
	}
	return c.APIKey
}

func (d *duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode && value.Value == "" {
		return fmt.Errorf("duration must not be empty")
	}
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("must be a duration, got %q: %w", value.Value, err)
	}
	d.Duration = parsed
	return nil
}

func (d duration) MarshalYAML() (any, error) {
	return d.Duration.String(), nil
}

func (w *WorkerConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw struct {
		Interval    duration `yaml:"interval"`
		Concurrency int      `yaml:"concurrency"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	w.Interval = raw.Interval.Duration
	w.Concurrency = raw.Concurrency
	return nil
}

func (p *ProviderConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw struct {
		Name               string   `yaml:"name"`
		Type               string   `yaml:"type"`
		Enabled            bool     `yaml:"enabled"`
		Priority           int      `yaml:"priority"`
		BaseURL            string   `yaml:"base_url"`
		Timeout            duration `yaml:"timeout"`
		RateLimitPerSecond int      `yaml:"rate_limit_per_second"`
		APIKey             string   `yaml:"api_key"`
		APIKeyEnv          string   `yaml:"api_key_env"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	p.Name = raw.Name
	p.Type = raw.Type
	p.Enabled = raw.Enabled
	p.Priority = raw.Priority
	p.BaseURL = raw.BaseURL
	p.Timeout = raw.Timeout.Duration
	p.RateLimitPerSecond = raw.RateLimitPerSecond
	p.APIKey = raw.APIKey
	p.APIKeyEnv = raw.APIKeyEnv
	return nil
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
