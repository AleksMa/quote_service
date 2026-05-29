package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
)

func NewExchangeRateClient(name string, baseURL url.URL, apiKey string, timeout time.Duration) *HTTPClient {
	return NewHTTPClient(HTTPClientConfig{
		Name:           name,
		BaseURL:        baseURL,
		Timeout:        timeout,
		APIKey:         apiKey,
		BuildRequest:   buildExchangeRateRequest,
		DecodeResponse: decodeExchangeRateResponse,
	})
}

func buildExchangeRateRequest(ctx context.Context, cfg RequestConfig, pair domain.Pair) (*http.Request, error) {
	endpoint, err := url.JoinPath(cfg.BaseURL.String(), "v1", "latest")
	if err != nil {
		return nil, fmt.Errorf("build %s endpoint: %w", cfg.Name, err)
	}
	endpointURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse %s endpoint: %w", cfg.Name, err)
	}
	query := endpointURL.Query()
	query.Set("access_key", cfg.APIKey)
	query.Set("base", pair.Base)
	query.Set("symbols", pair.Quote)
	endpointURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", cfg.Name, err)
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func decodeExchangeRateResponse(resp *http.Response, cfg RequestConfig, pair domain.Pair) (decimal.Decimal, error) {
	var body struct {
		Success bool                   `json:"success"`
		Rates   map[string]json.Number `json:"rates"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := decodeJSONResponse(resp, cfg.Name, &body); err != nil {
		return decimal.Decimal{}, errors.Wrap(err, "decode exchangerate response")
	}

	if resp.StatusCode != http.StatusOK {
		return decimal.Decimal{}, statusError(resp, cfg.Name, body.Error.Message)
	}

	if !body.Success || body.Error.Message != "" {
		return decimal.Decimal{}, fmt.Errorf("%s response error: %s", cfg.Name, body.Error.Message)
	}

	rate, ok := body.Rates[pair.Quote]
	if !ok {
		return decimal.Decimal{}, fmt.Errorf("%s response does not contain rate", cfg.Name)
	}

	return parseDecimalRate(cfg.Name, rate)
}
