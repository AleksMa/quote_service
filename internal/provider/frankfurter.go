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

func NewFrankfurterClient(name string, baseURL string, timeout time.Duration) *HTTPClient {
	return NewHTTPClient(HTTPClientConfig{
		Name:           name,
		BaseURL:        baseURL,
		Timeout:        timeout,
		BuildRequest:   buildFrankfurterRequest,
		DecodeResponse: decodeFrankfurterResponse,
	})
}

func buildFrankfurterRequest(ctx context.Context, cfg RequestConfig, pair domain.Pair) (*http.Request, error) {
	endpoint := fmt.Sprintf("%s/v2/rate/%s/%s", cfg.BaseURL, url.PathEscape(pair.Base), url.PathEscape(pair.Quote))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", cfg.Name, err)
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func decodeFrankfurterResponse(resp *http.Response, cfg RequestConfig, pair domain.Pair) (decimal.Decimal, error) {
	var body struct {
		Rate    json.Number `json:"rate"`
		Message string      `json:"message"`
	}
	if err := decodeJSONResponse(resp, cfg.Name, &body); err != nil {
		return decimal.Decimal{}, errors.Wrap(err, "decode frankfurter response")
	}

	if resp.StatusCode != http.StatusOK {
		return decimal.Decimal{}, statusError(resp, cfg.Name, body.Message)
	}
	return parseDecimalRate(cfg.Name, body.Rate)
}
