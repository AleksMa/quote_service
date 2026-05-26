package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
)

type HTTPClientConfig struct {
	Name           string
	BaseURL        string
	APIKey         string
	Timeout        time.Duration
	BuildRequest   RequestBuilder
	DecodeResponse ResponseDecoder
}

type RequestConfig struct {
	Name    string
	BaseURL string
	APIKey  string
}

type RequestBuilder func(ctx context.Context, cfg RequestConfig, pair domain.Pair) (*http.Request, error)
type ResponseDecoder func(resp *http.Response, cfg RequestConfig, pair domain.Pair) (string, error)

type HTTPClient struct {
	name           string
	baseURL        string
	apiKey         string
	httpClient     *http.Client
	now            func() time.Time
	buildRequest   RequestBuilder
	decodeResponse ResponseDecoder
}

func NewHTTPClient(cfg HTTPClientConfig) *HTTPClient {
	return &HTTPClient{
		name:    strings.TrimSpace(cfg.Name),
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:  strings.TrimSpace(cfg.APIKey),
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		now:            time.Now,
		buildRequest:   cfg.BuildRequest,
		decodeResponse: cfg.DecodeResponse,
	}
}

func (c *HTTPClient) FetchRate(ctx context.Context, pair domain.Pair) (Rate, error) {
	reqCfg := RequestConfig{
		Name:    c.name,
		BaseURL: c.baseURL,
		APIKey:  c.apiKey,
	}

	req, err := c.buildRequest(ctx, reqCfg, pair)
	if err != nil {
		return Rate{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Rate{}, fmt.Errorf("call %s: %w", c.name, err)
	}
	defer resp.Body.Close()

	price, err := c.decodeResponse(resp, reqCfg, pair)
	if err != nil {
		return Rate{}, err
	}
	if err := validatePrice(c.name, price); err != nil {
		return Rate{}, err
	}

	return Rate{
		Pair:      pair,
		Price:     price,
		Provider:  c.name,
		FetchedAt: c.now().UTC(),
	}, nil
}

func validatePrice(providerName string, price string) error {
	if price == "" {
		return fmt.Errorf("%s response does not contain rate", providerName)
	}
	if parsed, err := strconv.ParseFloat(price, 64); err != nil || parsed <= 0 {
		return fmt.Errorf("%s returned invalid rate %q", providerName, price)
	}
	return nil
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
