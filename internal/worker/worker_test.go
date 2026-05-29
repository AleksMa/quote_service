package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
	"github.com/AleksMa/quote_service/internal/provider"
	"github.com/shopspring/decimal"
)

func TestWorkerMarksSucceeded(t *testing.T) {
	job := domain.UpdateJob{
		ID:     "3f90b1d3-e261-4ac6-b7ac-1a66dc67f747",
		Pair:   domain.Pair{Raw: "EUR/USD", Base: "EUR", Quote: "USD"},
		Status: domain.StatusProcessing,
	}
	store := &fakeWorkerStore{claimed: []domain.UpdateJob{job}}
	fetchedAt := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	provider := fakeProvider{rate: provider.Rate{Pair: job.Pair, Price: decimal.RequireFromString("1.1"), Provider: "test", FetchedAt: fetchedAt}}

	w := New(store, provider, Options{Concurrency: 1, Logger: discardLogger()})
	w.processBatch(context.Background())

	if store.succeededID != job.ID || !store.succeededPrice.Equal(decimal.RequireFromString("1.1")) || !store.succeededAt.Equal(fetchedAt) {
		t.Fatalf("job was not marked succeeded: %+v", store)
	}
	if store.failedID != "" {
		t.Fatalf("job unexpectedly failed: %+v", store)
	}
}

func TestWorkerMarksFailed(t *testing.T) {
	job := domain.UpdateJob{
		ID:     "3f90b1d3-e261-4ac6-b7ac-1a66dc67f747",
		Pair:   domain.Pair{Raw: "EUR/USD", Base: "EUR", Quote: "USD"},
		Status: domain.StatusProcessing,
	}
	store := &fakeWorkerStore{claimed: []domain.UpdateJob{job}}
	provider := fakeProvider{err: errors.New("provider unavailable")}

	w := New(store, provider, Options{Concurrency: 1, Logger: discardLogger()})
	w.processBatch(context.Background())

	if store.failedID != job.ID || store.failedMessage != "provider unavailable" {
		t.Fatalf("job was not marked failed: %+v", store)
	}
	if store.succeededID != "" {
		t.Fatalf("job unexpectedly succeeded: %+v", store)
	}
}

func TestWorkerSeparatesClaimLimitAndConcurrency(t *testing.T) {
	jobs := []domain.UpdateJob{
		{ID: "3f90b1d3-e261-4ac6-b7ac-1a66dc67f747", Pair: domain.Pair{Raw: "EUR/USD", Base: "EUR", Quote: "USD"}},
		{ID: "d5f4e3b2-4b2f-4d7b-b1cc-441cd3f9fd1a", Pair: domain.Pair{Raw: "USD/EUR", Base: "USD", Quote: "EUR"}},
		{ID: "58a2e425-9d7c-48f6-a1a5-a6ef47861ac8", Pair: domain.Pair{Raw: "EUR/MXN", Base: "EUR", Quote: "MXN"}},
	}
	store := &fakeWorkerStore{claimed: jobs}
	provider := &trackingProvider{delay: 10 * time.Millisecond}

	w := New(store, provider, Options{ClaimLimit: 3, Concurrency: 1, Logger: discardLogger()})
	w.processBatch(context.Background())

	if store.claimLimit != 3 {
		t.Fatalf("expected claim limit 3, got %d", store.claimLimit)
	}
	if store.succeededCount != 3 {
		t.Fatalf("expected 3 succeeded jobs, got %d", store.succeededCount)
	}
	if provider.maxActive != 1 {
		t.Fatalf("expected provider concurrency 1, got %d", provider.maxActive)
	}
}

type fakeWorkerStore struct {
	claimed        []domain.UpdateJob
	claimLimit     int
	succeededID    string
	succeededPrice decimal.Decimal
	succeededAt    time.Time
	succeededCount int
	failedID       string
	failedMessage  string
}

func (s *fakeWorkerStore) ClaimPending(ctx context.Context, limit int) ([]domain.UpdateJob, error) {
	s.claimLimit = limit
	if len(s.claimed) > limit {
		return s.claimed[:limit], nil
	}
	return s.claimed, nil
}

func (s *fakeWorkerStore) MarkSucceeded(ctx context.Context, id string, price decimal.Decimal, providerName string, updatedAt time.Time) error {
	s.succeededID = id
	s.succeededPrice = price
	s.succeededAt = updatedAt
	s.succeededCount++
	return nil
}

func (s *fakeWorkerStore) MarkFailed(ctx context.Context, id string, message string, finishedAt time.Time) error {
	s.failedID = id
	s.failedMessage = message
	return nil
}

type fakeProvider struct {
	rate provider.Rate
	err  error
}

func (p fakeProvider) FetchRate(ctx context.Context, pair domain.Pair) (provider.Rate, error) {
	return p.rate, p.err
}

type trackingProvider struct {
	mu        sync.Mutex
	active    int
	maxActive int
	delay     time.Duration
}

func (p *trackingProvider) FetchRate(ctx context.Context, pair domain.Pair) (provider.Rate, error) {
	p.mu.Lock()
	p.active++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.mu.Unlock()

	time.Sleep(p.delay)

	p.mu.Lock()
	p.active--
	p.mu.Unlock()

	return provider.Rate{Pair: pair, Price: decimal.RequireFromString("1.1"), Provider: "test", FetchedAt: time.Now().UTC()}, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
