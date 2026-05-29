package config

import (
	"fmt"
	"net/url"
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
	Addr            string `yaml:"addr"`
	SwaggerUIOrigin string `yaml:"swagger_ui_origin"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	URL    string `yaml:"url"`
}

type WorkerConfig struct {
	Interval    time.Duration `yaml:"interval"`
	ClaimLimit  int           `yaml:"claim_limit"`
	Concurrency int           `yaml:"concurrency"`
}

type ProviderConfig struct {
	Name               string        `yaml:"name"`
	Type               ProviderType  `yaml:"type"`
	Enabled            bool          `yaml:"enabled"`
	Priority           int           `yaml:"priority"`
	BaseURL            string        `yaml:"base_url"`
	Timeout            time.Duration `yaml:"timeout"`
	RateLimitPerSecond int           `yaml:"rate_limit_per_second"`
	APIKey             string        `yaml:"api_key"`
	APIKeyEnv          string        `yaml:"api_key_env"`
}

type ProviderType string

const (
	ProviderTypeFrankfurter  ProviderType = "frankfurter"
	ProviderTypeExchangeRate ProviderType = "exchangerate"
)

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

	return newConfig(raw)
}

func newConfig(raw rawConfig) (Config, error) {
	cfg, err := normalizeConfig(raw)
	if err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func normalizeConfig(raw rawConfig) (Config, error) {
	supportedPairs, err := normalizeSupportedPairs(raw.SupportedPairs)
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTP:            normalizeHTTPConfig(raw.HTTP),
		Database:        normalizeDatabaseConfig(raw.Database),
		Worker:          raw.Worker,
		Providers:       normalizeProviders(raw.Providers),
		SupportedPairs:  supportedPairs,
		ShutdownTimeout: raw.ShutdownTimeout.Duration,
	}, nil
}

func normalizeHTTPConfig(cfg HTTPConfig) HTTPConfig {
	return HTTPConfig{
		Addr:            strings.TrimSpace(cfg.Addr),
		SwaggerUIOrigin: strings.TrimSpace(cfg.SwaggerUIOrigin),
	}
}

func normalizeDatabaseConfig(cfg DatabaseConfig) DatabaseConfig {
	return DatabaseConfig{
		Driver: strings.TrimSpace(cfg.Driver),
		URL:    strings.TrimSpace(cfg.URL),
	}
}

func normalizeProviders(providers []ProviderConfig) []ProviderConfig {
	normalized := make([]ProviderConfig, len(providers))
	for i, provider := range providers {
		normalized[i] = normalizeProviderConfig(provider)
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return normalized[i].Priority < normalized[j].Priority
	})
	return normalized
}

func normalizeProviderConfig(cfg ProviderConfig) ProviderConfig {
	return ProviderConfig{
		Name:               strings.TrimSpace(cfg.Name),
		Type:               NormalizeProviderType(cfg.Type.String()),
		Enabled:            cfg.Enabled,
		Priority:           cfg.Priority,
		BaseURL:            strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		Timeout:            cfg.Timeout,
		RateLimitPerSecond: cfg.RateLimitPerSecond,
		APIKey:             strings.TrimSpace(cfg.APIKey),
		APIKeyEnv:          strings.TrimSpace(cfg.APIKeyEnv),
	}
}

func normalizeSupportedPairs(pairs []string) (map[string]struct{}, error) {
	supportedPairs, err := domain.PairSet(pairs)
	if err != nil {
		return nil, fmt.Errorf("load supported pairs: %w", err)
	}
	return supportedPairs, nil
}

func (c Config) validate() error {
	if err := c.HTTP.validate(); err != nil {
		return err
	}
	if err := c.Database.validate(); err != nil {
		return err
	}
	if err := c.Worker.validate(); err != nil {
		return err
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown_timeout must be greater than zero")
	}
	if len(c.SupportedPairs) == 0 {
		return fmt.Errorf("supported_pairs must contain at least one pair")
	}
	if err := validateProviders(c.Providers); err != nil {
		return err
	}
	return nil
}

func (c HTTPConfig) validate() error {
	if c.Addr == "" {
		return fmt.Errorf("http.addr is required")
	}
	return validateOptionalOrigin("http.swagger_ui_origin", c.SwaggerUIOrigin)
}

func (c DatabaseConfig) validate() error {
	if c.Driver == "" {
		return fmt.Errorf("database.driver is required")
	}
	if c.URL == "" {
		return fmt.Errorf("database.url is required")
	}
	return nil
}

func (c WorkerConfig) validate() error {
	if c.Interval <= 0 {
		return fmt.Errorf("worker.interval must be greater than zero")
	}
	if c.ClaimLimit < 1 {
		return fmt.Errorf("worker.claim_limit must be greater than zero")
	}
	if c.Concurrency < 1 {
		return fmt.Errorf("worker.concurrency must be greater than zero")
	}
	return nil
}

func validateProviders(providers []ProviderConfig) error {
	if len(providers) == 0 {
		return fmt.Errorf("providers must contain at least one provider")
	}

	enabledCount := 0
	names := make(map[string]struct{}, len(providers))
	for i, provider := range providers {
		if err := provider.validate(); err != nil {
			if provider.Name == "" {
				return fmt.Errorf("provider %d: %w", i, err)
			}
			return fmt.Errorf("provider %q: %w", provider.Name, err)
		}

		if _, ok := names[provider.Name]; ok {
			return fmt.Errorf("provider %q is duplicated", provider.Name)
		}
		names[provider.Name] = struct{}{}

		if provider.Enabled {
			enabledCount++
		}
	}
	if enabledCount == 0 {
		return fmt.Errorf("at least one provider must be enabled")
	}
	return nil
}

func (c ProviderConfig) validate() error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if err := c.Type.validate(); err != nil {
		return err
	}
	if c.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be greater than zero")
	}
	if c.RateLimitPerSecond < 0 {
		return fmt.Errorf("rate_limit_per_second cannot be negative")
	}
	return nil
}

func ParseProviderType(value string) (ProviderType, error) {
	normalized := NormalizeProviderType(value)
	if err := normalized.validate(); err != nil {
		return "", err
	}
	return normalized, nil
}

func NormalizeProviderType(value string) ProviderType {
	return ProviderType(strings.TrimSpace(value))
}

func (t ProviderType) validate() error {
	switch t {
	case ProviderTypeFrankfurter, ProviderTypeExchangeRate:
		return nil
	case "":
		return fmt.Errorf("provider type is required")
	default:
		return fmt.Errorf("unsupported provider type %q", t)
	}
}

func (t ProviderType) String() string {
	return string(t)
}

func validateOptionalOrigin(field string, value string) error {
	if value == "" {
		return nil
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s must be a valid URL: %w", field, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s scheme must be http or https", field)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s host is required", field)
	}
	if parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an origin without user info, path, query, or fragment", field)
	}
	return nil
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
		ClaimLimit  int      `yaml:"claim_limit"`
		Concurrency int      `yaml:"concurrency"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	w.Interval = raw.Interval.Duration
	w.ClaimLimit = raw.ClaimLimit
	w.Concurrency = raw.Concurrency
	return nil
}

func (p *ProviderConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw struct {
		Name               string       `yaml:"name"`
		Type               ProviderType `yaml:"type"`
		Enabled            bool         `yaml:"enabled"`
		Priority           int          `yaml:"priority"`
		BaseURL            string       `yaml:"base_url"`
		Timeout            duration     `yaml:"timeout"`
		RateLimitPerSecond int          `yaml:"rate_limit_per_second"`
		APIKey             string       `yaml:"api_key"`
		APIKeyEnv          string       `yaml:"api_key_env"`
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
