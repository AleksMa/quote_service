package provider

import (
	"fmt"
	"log/slog"

	"github.com/AleksMa/quote_service/internal/config"
)

func BuildChain(configs []config.ProviderConfig, logger *slog.Logger) (Client, error) {
	if logger == nil {
		logger = slog.Default()
	}
	clients := make([]NamedClient, 0, len(configs))
	for index, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		if cfg.Name == "" {
			logger.Warn("skip provider without name", "index", index, "type", cfg.Type)
			continue
		}

		var client Client
		switch cfg.Type {
		case FrankfurterProviderName:
			client = NewFrankfurterClient(cfg.Name, cfg.BaseURL, cfg.Timeout)
		case ExchangerateProviderName:
			client = NewExchangerateClient(cfg.Name, cfg.BaseURL, cfg.ResolvedAPIKey(), cfg.Timeout)
		default:
			return nil, fmt.Errorf("unsupported provider type %q for provider %q", cfg.Type, cfg.Name)
		}
		client = NewRateLimitedClient(client, cfg.RateLimitPerSecond)
		clients = append(clients, NamedClient{Name: cfg.Name, Client: client})
	}
	return NewChain(clients)
}
