package application

import (
	"context"

	"github.com/AleksMa/quote_service/internal/domain"
)

type QuoteStore interface {
	CreateUpdateRequest(ctx context.Context, pair domain.Pair, idempotencyKey string) (domain.UpdateRequest, bool, error)
	GetUpdateRequest(ctx context.Context, id string) (domain.UpdateRequest, error)
	GetLatestQuote(ctx context.Context, pair domain.Pair) (domain.LatestQuote, error)
}

type QuoteService struct {
	store          QuoteStore
	supportedPairs map[string]struct{}
}

func NewQuoteService(store QuoteStore, supportedPairs map[string]struct{}) *QuoteService {
	return &QuoteService{
		store:          store,
		supportedPairs: supportedPairs,
	}
}

func (s *QuoteService) RequestQuoteUpdate(ctx context.Context, rawPair string, idempotencyKey string) (domain.UpdateRequest, bool, error) {
	pair, err := domain.ParsePair(rawPair, s.supportedPairs)
	if err != nil {
		return domain.UpdateRequest{}, false, err
	}
	return s.store.CreateUpdateRequest(ctx, pair, idempotencyKey)
}

func (s *QuoteService) GetQuoteUpdate(ctx context.Context, id string) (domain.UpdateRequest, error) {
	return s.store.GetUpdateRequest(ctx, id)
}

func (s *QuoteService) GetLatestQuote(ctx context.Context, rawPair string) (domain.LatestQuote, error) {
	pair, err := domain.ParsePair(rawPair, s.supportedPairs)
	if err != nil {
		return domain.LatestQuote{}, err
	}
	return s.store.GetLatestQuote(ctx, pair)
}
