package application

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
	"github.com/shopspring/decimal"
)

func TestProcessorMarksSucceeded(t *testing.T) {
	job := testJob()
	store := &fakeJobStore{claimed: []domain.UpdateJob{job}}
	fetchedAt := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	provider := fakeProvider{rate: domain.FetchedRate{Pair: job.Pair, Price: decimal.RequireFromString("1.1"), Provider: "test", FetchedAt: fetchedAt}}

	processor := NewProcessor(store, provider, discardLogger())
	if err := processor.ProcessPending(context.Background(), 1, 1); err != nil {
		t.Fatalf("ProcessPending returned error: %v", err)
	}

	if store.succeededID != job.ID || !store.succeededPrice.Equal(decimal.RequireFromString("1.1")) || !store.succeededAt.Equal(fetchedAt) {
		t.Fatalf("job was not marked succeeded: %+v", store)
	}
}

func TestProcessorMarksFailed(t *testing.T) {
	job := testJob()
	store := &fakeJobStore{claimed: []domain.UpdateJob{job}}
	provider := fakeProvider{err: errors.New("provider unavailable")}

	processor := NewProcessor(store, provider, discardLogger())
	if err := processor.ProcessPending(context.Background(), 1, 1); err != nil {
		t.Fatalf("ProcessPending returned error: %v", err)
	}

	if store.failedID != job.ID || store.failedMessage != "provider unavailable" {
		t.Fatalf("job was not marked failed: %+v", store)
	}
}

func TestProcessorLimitsConcurrency(t *testing.T) {
	jobs := []domain.UpdateJob{
		testJob(),
		{ID: "d5f4e3b2-4b2f-4d7b-b1cc-441cd3f9fd1a", Pair: domain.Pair{Raw: "USD/EUR", Base: "USD", Quote: "EUR"}},
		{ID: "58a2e425-9d7c-48f6-a1a5-a6ef47861ac8", Pair: domain.Pair{Raw: "EUR/MXN", Base: "EUR", Quote: "MXN"}},
	}
	store := &fakeJobStore{claimed: jobs}
	provider := &trackingProvider{delay: 10 * time.Millisecond}

	processor := NewProcessor(store, provider, discardLogger())
	if err := processor.ProcessPending(context.Background(), 3, 1); err != nil {
		t.Fatalf("ProcessPending returned error: %v", err)
	}

	if store.claimLimit != 3 || store.succeededCount != 3 || provider.maxActive != 1 {
		t.Fatalf("unexpected processing result: store=%+v provider=%+v", store, provider)
	}
}

func TestProcessorRequeuesProcessingJobs(t *testing.T) {
	store := &fakeJobStore{}
	processor := NewProcessor(store, fakeProvider{}, discardLogger())

	if err := processor.RequeueProcessing(context.Background()); err != nil {
		t.Fatalf("RequeueProcessing returned error: %v", err)
	}
	if !store.requeued {
		t.Fatal("processing jobs were not requeued")
	}
}

func TestProcessorRejectsInvalidOptions(t *testing.T) {
	processor := NewProcessor(&fakeJobStore{}, fakeProvider{}, discardLogger())

	tests := []struct {
		name        string
		limit       int
		concurrency int
	}{
		{name: "claim limit", limit: 0, concurrency: 1},
		{name: "concurrency", limit: 1, concurrency: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := processor.ProcessPending(context.Background(), tt.limit, tt.concurrency); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func testJob() domain.UpdateJob {
	return domain.UpdateJob{
		ID:     "3f90b1d3-e261-4ac6-b7ac-1a66dc67f747",
		Pair:   domain.Pair{Raw: "EUR/USD", Base: "EUR", Quote: "USD"},
		Status: domain.StatusProcessing,
	}
}

type fakeJobStore struct {
	claimed        []domain.UpdateJob
	claimLimit     int
	requeued       bool
	succeededID    string
	succeededPrice decimal.Decimal
	succeededAt    time.Time
	succeededCount int
	failedID       string
	failedMessage  string
}

func (s *fakeJobStore) RequeueProcessingJobs(ctx context.Context) error {
	s.requeued = true
	return nil
}

func (s *fakeJobStore) ClaimPending(ctx context.Context, limit int) ([]domain.UpdateJob, error) {
	s.claimLimit = limit
	if len(s.claimed) > limit {
		return s.claimed[:limit], nil
	}
	return s.claimed, nil
}

func (s *fakeJobStore) MarkSucceeded(ctx context.Context, id string, price decimal.Decimal, providerName string, updatedAt time.Time) error {
	s.succeededID = id
	s.succeededPrice = price
	s.succeededAt = updatedAt
	s.succeededCount++
	return nil
}

func (s *fakeJobStore) MarkFailed(ctx context.Context, id string, message string, finishedAt time.Time) error {
	s.failedID = id
	s.failedMessage = message
	return nil
}

type fakeProvider struct {
	rate domain.FetchedRate
	err  error
}

func (p fakeProvider) FetchRate(ctx context.Context, pair domain.Pair) (domain.FetchedRate, error) {
	return p.rate, p.err
}

type trackingProvider struct {
	mu        sync.Mutex
	active    int
	maxActive int
	delay     time.Duration
}

func (p *trackingProvider) FetchRate(ctx context.Context, pair domain.Pair) (domain.FetchedRate, error) {
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

	return domain.FetchedRate{Pair: pair, Price: decimal.RequireFromString("1.1"), Provider: "test", FetchedAt: time.Now().UTC()}, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
