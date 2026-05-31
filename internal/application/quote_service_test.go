package application

import (
	"context"
	"errors"
	"testing"

	"github.com/AleksMa/quote_service/internal/domain"
)

func TestQuoteServiceRequestQuoteUpdateNormalizesPair(t *testing.T) {
	store := &fakeQuoteStore{}
	service := NewQuoteService(store, map[string]struct{}{"EUR/MXN": {}})

	_, _, err := service.RequestQuoteUpdate(context.Background(), " eur/mxn ", "same-key")
	if err != nil {
		t.Fatalf("RequestQuoteUpdate returned error: %v", err)
	}
	if store.createdPair.Raw != "EUR/MXN" || store.idempotencyKey != "same-key" {
		t.Fatalf("unexpected store call: %+v", store)
	}
}

func TestQuoteServiceRequestQuoteUpdateRejectsUnsupportedPair(t *testing.T) {
	store := &fakeQuoteStore{}
	service := NewQuoteService(store, map[string]struct{}{"EUR/MXN": {}})

	_, _, err := service.RequestQuoteUpdate(context.Background(), "USD/EUR", "")

	if !errors.Is(err, domain.ErrUnsupportedPair) {
		t.Fatalf("expected ErrUnsupportedPair, got %v", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("store was called for unsupported pair")
	}
}

func TestQuoteServiceGetLatestQuoteNormalizesPair(t *testing.T) {
	store := &fakeQuoteStore{}
	service := NewQuoteService(store, map[string]struct{}{"EUR/MXN": {}})

	_, err := service.GetLatestQuote(context.Background(), " eur/mxn ")
	if err != nil {
		t.Fatalf("GetLatestQuote returned error: %v", err)
	}
	if store.latestPair.Raw != "EUR/MXN" {
		t.Fatalf("unexpected latest quote pair: %+v", store.latestPair)
	}
}

type fakeQuoteStore struct {
	createCalls    int
	createdPair    domain.Pair
	idempotencyKey string
	latestPair     domain.Pair
}

func (s *fakeQuoteStore) CreateUpdateRequest(ctx context.Context, pair domain.Pair, idempotencyKey string) (domain.UpdateRequest, bool, error) {
	s.createCalls++
	s.createdPair = pair
	s.idempotencyKey = idempotencyKey
	return domain.UpdateRequest{Pair: pair, Status: domain.StatusPending}, false, nil
}

func (s *fakeQuoteStore) GetUpdateRequest(ctx context.Context, id string) (domain.UpdateRequest, error) {
	return domain.UpdateRequest{ID: id}, nil
}

func (s *fakeQuoteStore) GetLatestQuote(ctx context.Context, pair domain.Pair) (domain.LatestQuote, error) {
	s.latestPair = pair
	return domain.LatestQuote{Pair: pair}, nil
}
