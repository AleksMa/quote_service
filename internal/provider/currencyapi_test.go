package provider

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
)

func TestCurrencyAPIClientFetchRate(t *testing.T) {
	client := NewCurrencyAPIClient("currencyapi", "https://example.test", "secret", time.Second)
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v3/latest" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("base_currency") != "EUR" || r.URL.Query().Get("currencies") != "USD" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		if r.Header.Get("apikey") != "secret" {
			t.Fatalf("api key header was not sent")
		}
		return jsonResponse(http.StatusOK, `{"data":{"USD":{"code":"USD","value":1.2345}}}`), nil
	})}
	client.now = func() time.Time { return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) }

	rate, err := client.FetchRate(context.Background(), domain.Pair{Raw: "EUR/USD", Base: "EUR", Quote: "USD"})
	if err != nil {
		t.Fatalf("FetchRate returned error: %v", err)
	}
	if rate.Price != "1.2345" || rate.Provider != "currencyapi" {
		t.Fatalf("unexpected rate: %+v", rate)
	}
}

func TestCurrencyAPIClientRequiresAPIKey(t *testing.T) {
	client := NewCurrencyAPIClient("currencyapi", "https://example.test", "", time.Second)

	if _, err := client.FetchRate(context.Background(), domain.Pair{Raw: "EUR/USD", Base: "EUR", Quote: "USD"}); err == nil {
		t.Fatal("expected error")
	}
}
