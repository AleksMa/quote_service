package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
)

const CurrencyAPIProviderName = "currencyapi"

func NewCurrencyAPIClient(name string, baseURL string, apiKey string, timeout time.Duration) *HTTPClient {
	return NewHTTPClient(HTTPClientConfig{
		Name:           name,
		BaseURL:        baseURL,
		APIKey:         apiKey,
		Timeout:        timeout,
		BuildRequest:   buildCurrencyAPIRequest,
		DecodeResponse: decodeCurrencyAPIResponse,
	})
}

func buildCurrencyAPIRequest(ctx context.Context, cfg RequestConfig, pair domain.Pair) (*http.Request, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("%s api key is required", cfg.Name)
	}

	endpoint, err := url.Parse(cfg.BaseURL + "/v3/latest")
	if err != nil {
		return nil, fmt.Errorf("build %s URL: %w", cfg.Name, err)
	}
	query := endpoint.Query()
	query.Set("base_currency", pair.Base)
	query.Set("currencies", pair.Quote)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", cfg.Name, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("apikey", cfg.APIKey)
	return req, nil
}

func decodeCurrencyAPIResponse(resp *http.Response, cfg RequestConfig, pair domain.Pair) (string, error) {
	var body struct {
		Message string `json:"message"`
		Data    map[string]struct {
			Code  string      `json:"code"`
			Value json.Number `json:"value"`
		} `json:"data"`
	}
	if err := decodeJSONResponse(resp, cfg.Name, &body); err != nil {
		return "", err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", statusError(resp, cfg.Name, body.Message)
	}

	return body.Data[pair.Quote].Value.String(), nil
}
