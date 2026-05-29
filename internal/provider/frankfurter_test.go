package provider

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
	"github.com/shopspring/decimal"
)

func TestFrankfurterClientFetchRate(t *testing.T) {
	client := NewFrankfurterClient("frankfurter", "https://example.test", time.Second)
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v2/rate/EUR/USD" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{"base":"EUR","quote":"USD","rate":1.2345}`), nil
	})}
	client.now = func() time.Time { return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) }

	rate, err := client.FetchRate(context.Background(), domain.Pair{Raw: "EUR/USD", Base: "EUR", Quote: "USD"})
	if err != nil {
		t.Fatalf("FetchRate returned error: %v", err)
	}
	if !rate.Price.Equal(decimal.RequireFromString("1.2345")) || rate.Provider != "frankfurter" {
		t.Fatalf("unexpected rate: %+v", rate)
	}
}

func TestFrankfurterClientReturnsHTTPErrorMessage(t *testing.T) {
	client := NewFrankfurterClient("frankfurter", "https://example.test", time.Second)
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, `{"message":"rate not found"}`), nil
	})}
	_, err := client.FetchRate(context.Background(), domain.Pair{Raw: "EUR/USD", Base: "EUR", Quote: "USD"})
	if err == nil {
		t.Fatal("expected error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}
