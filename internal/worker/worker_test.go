package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
	"github.com/AleksMa/quote_service/internal/provider"
)

func TestWorkerMarksSucceeded(t *testing.T) {
	req := domain.UpdateRequest{
		ID:     "3f90b1d3-e261-4ac6-b7ac-1a66dc67f747",
		Pair:   domain.Pair{Raw: "EUR/USD", Base: "EUR", Quote: "USD"},
		Status: domain.StatusProcessing,
	}
	store := &fakeWorkerStore{claimed: []domain.UpdateRequest{req}}
	fetchedAt := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	provider := fakeProvider{rate: provider.Rate{Pair: req.Pair, Price: "1.1", Provider: "test", FetchedAt: fetchedAt}}

	w := New(store, provider, Options{Concurrency: 1, Logger: discardLogger()})
	w.processBatch(context.Background())

	if store.succeededID != req.ID || store.succeededPrice != "1.1" || !store.succeededAt.Equal(fetchedAt) {
		t.Fatalf("request was not marked succeeded: %+v", store)
	}
	if store.failedID != "" {
		t.Fatalf("request unexpectedly failed: %+v", store)
	}
}

func TestWorkerMarksFailed(t *testing.T) {
	req := domain.UpdateRequest{
		ID:     "3f90b1d3-e261-4ac6-b7ac-1a66dc67f747",
		Pair:   domain.Pair{Raw: "EUR/USD", Base: "EUR", Quote: "USD"},
		Status: domain.StatusProcessing,
	}
	store := &fakeWorkerStore{claimed: []domain.UpdateRequest{req}}
	provider := fakeProvider{err: errors.New("provider unavailable")}

	w := New(store, provider, Options{Concurrency: 1, Logger: discardLogger()})
	w.processBatch(context.Background())

	if store.failedID != req.ID || store.failedMessage != "provider unavailable" {
		t.Fatalf("request was not marked failed: %+v", store)
	}
	if store.succeededID != "" {
		t.Fatalf("request unexpectedly succeeded: %+v", store)
	}
}

type fakeWorkerStore struct {
	claimed        []domain.UpdateRequest
	succeededID    string
	succeededPrice string
	succeededAt    time.Time
	failedID       string
	failedMessage  string
}

func (s *fakeWorkerStore) ClaimPending(ctx context.Context, limit int) ([]domain.UpdateRequest, error) {
	if len(s.claimed) > limit {
		return s.claimed[:limit], nil
	}
	return s.claimed, nil
}

func (s *fakeWorkerStore) MarkSucceeded(ctx context.Context, id string, price string, providerName string, updatedAt time.Time) error {
	s.succeededID = id
	s.succeededPrice = price
	s.succeededAt = updatedAt
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

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
