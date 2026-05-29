package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
	"github.com/shopspring/decimal"
)

type Rate struct {
	Pair      domain.Pair
	Price     decimal.Decimal
	Provider  string
	FetchedAt time.Time
}

type Client interface {
	FetchRate(ctx context.Context, pair domain.Pair) (Rate, error)
}

type NamedClient struct {
	Name   string
	Client Client
}

type Chain struct {
	clients []NamedClient
}

func NewChain(clients []NamedClient) (*Chain, error) {
	if len(clients) == 0 {
		return nil, fmt.Errorf("at least one provider client is required")
	}
	return &Chain{clients: clients}, nil
}

func (c *Chain) FetchRate(ctx context.Context, pair domain.Pair) (Rate, error) {
	var errs []error
	for _, client := range c.clients {
		rate, err := client.Client.FetchRate(ctx, pair)
		if err == nil {
			return rate, nil
		}
		errs = append(errs, fmt.Errorf("%s failed: %w", client.Name, err))
	}
	return Rate{}, fmt.Errorf("all providers failed: %w", errors.Join(errs...))
}

type RateLimitedClient struct {
	client  Client
	limiter *Limiter
}

func NewRateLimitedClient(client Client, perSecond int) Client {
	if perSecond <= 0 {
		return client
	}
	return &RateLimitedClient{client: client, limiter: NewLimiter(perSecond)}
}

func (c *RateLimitedClient) FetchRate(ctx context.Context, pair domain.Pair) (Rate, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return Rate{}, err
	}
	return c.client.FetchRate(ctx, pair)
}

type Limiter struct {
	interval time.Duration
	mu       sync.Mutex
	next     time.Time
}

func NewLimiter(perSecond int) *Limiter {
	if perSecond <= 0 {
		return &Limiter{}
	}
	return &Limiter{interval: time.Second / time.Duration(perSecond)}
}

func (l *Limiter) Wait(ctx context.Context) error {
	if l.interval <= 0 {
		return nil
	}

	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	waitUntil := l.next
	l.next = l.next.Add(l.interval)
	l.mu.Unlock()

	timer := time.NewTimer(time.Until(waitUntil))
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
