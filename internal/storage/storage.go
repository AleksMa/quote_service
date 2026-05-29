package storage

import (
	"context"
	"time"

	"github.com/AleksMa/quote_service/internal/domain"
	"github.com/shopspring/decimal"
)

type Config struct {
	Driver string
	URL    string
}

type APIStore interface {
	CreateUpdateRequest(ctx context.Context, pair domain.Pair, idempotencyKey string) (domain.UpdateRequest, bool, error)
	GetUpdateRequest(ctx context.Context, id string) (domain.UpdateRequest, error)
	GetLatestQuote(ctx context.Context, pair domain.Pair) (domain.LatestQuote, error)
}

type WorkerStore interface {
	ClaimPending(ctx context.Context, limit int) ([]domain.UpdateJob, error)
	MarkSucceeded(ctx context.Context, jobID string, price decimal.Decimal, provider string, updatedAt time.Time) error
	MarkFailed(ctx context.Context, jobID string, message string, finishedAt time.Time) error
}

type Store interface {
	APIStore
	WorkerStore
	Close()
}
