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

const ExchangerateProviderName = "exchangerate"

func NewExchangerateClient(name string, baseURL string, apiKey string, timeout time.Duration) *HTTPClient {
	return NewHTTPClient(HTTPClientConfig{
		Name:           name,
		BaseURL:        baseURL,
		Timeout:        timeout,
		APIKey:         apiKey,
		BuildRequest:   buildExchangerateRequest,
		DecodeResponse: decodeExchangerateResponse,
	})
}

func buildExchangerateRequest(ctx context.Context, cfg RequestConfig, pair domain.Pair) (*http.Request, error) {
	endpoint := fmt.Sprintf("%s/v1/latest?access_key=%s&base=%s&symbols=%s", cfg.BaseURL, cfg.APIKey, url.PathEscape(pair.Base), url.PathEscape(pair.Quote))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", cfg.Name, err)
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func decodeExchangerateResponse(resp *http.Response, cfg RequestConfig, pair domain.Pair) (string, error) {
	var body struct {
		Success bool                   `json:"success"`
		Rates   map[string]json.Number `json:"rates"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := decodeJSONResponse(resp, cfg.Name, &body); err != nil {
		return "", err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", statusError(resp, cfg.Name, body.Error.Message)
	}

	if !body.Success || body.Error.Message != "" {
		return "", fmt.Errorf("%s response error: %s", cfg.Name, body.Error.Message)
	}

	return body.Rates[pair.Quote].String(), nil
}
