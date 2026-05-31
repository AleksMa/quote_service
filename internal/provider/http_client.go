package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
)

type HTTPClientConfig struct {
	Name           string
	BaseURL        url.URL
	APIKey         string
	Timeout        time.Duration
	BuildRequest   RequestBuilder
	DecodeResponse ResponseDecoder
}

type RequestConfig struct {
	Name    string
	BaseURL url.URL
	APIKey  string
}

type RequestBuilder func(ctx context.Context, cfg RequestConfig, pair domain.Pair) (*http.Request, error)
type ResponseDecoder func(resp *http.Response, cfg RequestConfig, pair domain.Pair) (decimal.Decimal, error)

type HTTPClient struct {
	name           string
	baseURL        url.URL
	apiKey         string
	httpClient     *http.Client
	now            func() time.Time
	buildRequest   RequestBuilder
	decodeResponse ResponseDecoder
}

func NewHTTPClient(cfg HTTPClientConfig) *HTTPClient {
	return &HTTPClient{
		name:    strings.TrimSpace(cfg.Name),
		baseURL: cfg.BaseURL,
		apiKey:  strings.TrimSpace(cfg.APIKey),
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		now:            time.Now,
		buildRequest:   cfg.BuildRequest,
		decodeResponse: cfg.DecodeResponse,
	}
}

func (c *HTTPClient) FetchRate(ctx context.Context, pair domain.Pair) (domain.FetchedRate, error) {
	reqCfg := RequestConfig{
		Name:    c.name,
		BaseURL: c.baseURL,
		APIKey:  c.apiKey,
	}

	req, err := c.buildRequest(ctx, reqCfg, pair)
	if err != nil {
		return domain.FetchedRate{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.FetchedRate{}, fmt.Errorf("call %s: %w", c.name, err)
	}
	defer resp.Body.Close()

	price, err := c.decodeResponse(resp, reqCfg, pair)
	if err != nil {
		return domain.FetchedRate{}, err
	}

	err = validatePrice(c.name, price)
	if err != nil {
		return domain.FetchedRate{}, err
	}

	return domain.FetchedRate{
		Pair:      pair,
		Price:     price,
		Provider:  c.name,
		FetchedAt: c.now().UTC(),
	}, nil
}

func validatePrice(providerName string, price decimal.Decimal) error {
	if !price.GreaterThan(decimal.Zero) {
		return fmt.Errorf("%s returned invalid rate %q", providerName, price.String())
	}
	return nil
}

func parseDecimalRate(providerName string, rate json.Number) (decimal.Decimal, error) {
	raw := rate.String()
	if raw == "" {
		return decimal.Decimal{}, fmt.Errorf("%s response does not contain rate", providerName)
	}
	price, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Decimal{}, errors.Wrap(err, fmt.Sprintf("%s returned invalid rate %q", providerName, raw))
	}
	return price, nil
}

func decodeJSONResponse(resp *http.Response, providerName string, target any) error {
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s response: %w", providerName, err)
	}
	return nil
}

func statusError(resp *http.Response, providerName string, message string) error {
	if message == "" {
		message = resp.Status
	}
	return fmt.Errorf("%s returned %s: %s", providerName, resp.Status, message)
}
