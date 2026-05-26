package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/AleksMa/quote_service/internal/storage/postgres"
)

const PostgresDriver = "postgres"

func Open(ctx context.Context, cfg Config) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case PostgresDriver:
		return postgres.Open(ctx, cfg.URL)
	default:
		return nil, fmt.Errorf("unsupported storage driver %q", cfg.Driver)
	}
}
