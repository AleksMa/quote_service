package provider

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/AleksMa/quote_service/internal/config"
	"github.com/AleksMa/quote_service/internal/domain"
	"github.com/shopspring/decimal"
)

func TestChainFallsBackToNextProvider(t *testing.T) {
	pair := domain.Pair{Raw: "EUR/USD", Base: "EUR", Quote: "USD"}
	chain, err := NewChain([]NamedClient{
		{Name: "first", Client: staticProvider{err: errors.New("down")}},
		{Name: "second", Client: staticProvider{rate: Rate{Pair: pair, Price: decimal.RequireFromString("1.2"), Provider: "second", FetchedAt: time.Now()}}},
	})
	if err != nil {
		t.Fatalf("NewChain returned error: %v", err)
	}

	rate, err := chain.FetchRate(context.Background(), pair)
	if err != nil {
		t.Fatalf("FetchRate returned error: %v", err)
	}
	if rate.Provider != "second" || !rate.Price.Equal(decimal.RequireFromString("1.2")) {
		t.Fatalf("unexpected rate: %+v", rate)
	}
}

type staticProvider struct {
	rate Rate
	err  error
}

func (p staticProvider) FetchRate(ctx context.Context, pair domain.Pair) (Rate, error) {
	return p.rate, p.err
}

func TestBuildChainSkipsUnnamedProvider(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	client, err := BuildChain([]config.ProviderConfig{
		{
			Name:     "",
			Type:     config.ProviderTypeFrankfurter,
			Enabled:  true,
			Priority: 1,
			BaseURL:  mustURL("https://ignored.example.test"),
			Timeout:  time.Second,
		},
		{
			Name:     "frankfurter",
			Type:     config.ProviderTypeFrankfurter,
			Enabled:  true,
			Priority: 2,
			BaseURL:  mustURL("https://api.example.test"),
			Timeout:  time.Second,
		},
	}, logger)
	if err != nil {
		t.Fatalf("BuildChain returned error: %v", err)
	}
	if client == nil {
		t.Fatal("expected provider chain")
	}
	if !strings.Contains(logs.String(), "skip provider without name") {
		t.Fatalf("expected skip warning, got logs: %s", logs.String())
	}
}

func mustURL(value string) url.URL {
	parsed, err := url.Parse(value)
	if err != nil {
		panic(err)
	}
	return *parsed
}
