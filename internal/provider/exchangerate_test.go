package provider

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
	"github.com/shopspring/decimal"
)

func TestExchangeRateClientFetchRate(t *testing.T) {
	client := NewExchangeRateClient("exchangerate", mustURL("https://example.test"), "secret", time.Second)
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/latest" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("base") != "EUR" || r.URL.Query().Get("symbols") != "USD" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("access_key") != "secret" {
			t.Fatalf("api key header was not sent")
		}
		return jsonResponse(http.StatusOK, `{"success":true,"rates":{"USD":1.2345}}`), nil
	})}
	client.now = func() time.Time { return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) }

	rate, err := client.FetchRate(context.Background(), domain.Pair{Raw: "EUR/USD", Base: "EUR", Quote: "USD"})
	if err != nil {
		t.Fatalf("FetchRate returned error: %v", err)
	}
	if !rate.Price.Equal(decimal.RequireFromString("1.2345")) || rate.Provider != "exchangerate" {
		t.Fatalf("unexpected rate: %+v", rate)
	}
}

func TestExchangeRateClientRequiresAPIKey(t *testing.T) {
	client := NewExchangeRateClient("exchangerate", mustURL("https://example.test"), "", time.Second)

	if _, err := client.FetchRate(context.Background(), domain.Pair{Raw: "EUR/USD", Base: "EUR", Quote: "USD"}); err == nil {
		t.Fatal("expected error")
	}
}
