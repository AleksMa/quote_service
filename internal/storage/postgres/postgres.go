package postgres

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AleksMa/quote_service/internal/domain"
)

//go:embed schema.sql
var schemaFS embed.FS

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}

	store := &Store{pool: pool}
	if err := store.ping(ctx); err != nil {
		store.Close()
		return nil, err
	}
	if err := store.migrate(ctx); err != nil {
		store.Close()
		return nil, err
	}
	if err := store.resetProcessing(ctx); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	if _, err := s.pool.Exec(ctx, string(schema)); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

func (s *Store) resetProcessing(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE quote_update_requests
		SET status = $1, started_at = NULL
		WHERE status = $2
	`, domain.StatusPending, domain.StatusProcessing)
	if err != nil {
		return fmt.Errorf("reset processing requests: %w", err)
	}
	return nil
}

func (s *Store) CreateUpdateRequest(ctx context.Context, pair domain.Pair, idempotencyKey string) (domain.UpdateRequest, bool, error) {
	id, err := domain.NewUUID()
	if err != nil {
		return domain.UpdateRequest{}, false, fmt.Errorf("generate request id: %w", err)
	}

	if idempotencyKey != "" {
		req, err := scanUpdate(s.pool.QueryRow(ctx, `
			INSERT INTO quote_update_requests (id, pair, base_currency, quote_currency, status, idempotency_key)
			VALUES ($1::uuid, $2, $3, $4, $5, $6)
			ON CONFLICT (pair, idempotency_key)
			WHERE idempotency_key IS NOT NULL AND idempotency_key <> ''
			DO NOTHING
			RETURNING id::text, pair, base_currency, quote_currency, status, price::text, provider, error, created_at, started_at, finished_at
		`, id, pair.Raw, pair.Base, pair.Quote, domain.StatusPending, idempotencyKey))
		if err == nil {
			return req, false, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.UpdateRequest{}, false, fmt.Errorf("insert update request: %w", err)
		}

		existing, err := scanUpdate(s.pool.QueryRow(ctx, `
			SELECT id::text, pair, base_currency, quote_currency, status, price::text, provider, error, created_at, started_at, finished_at
			FROM quote_update_requests
			WHERE pair = $1 AND idempotency_key = $2
		`, pair.Raw, idempotencyKey))
		if err != nil {
			return domain.UpdateRequest{}, false, translateNotFound(err, "find idempotent update request")
		}
		return existing, true, nil
	}

	req, err := scanUpdate(s.pool.QueryRow(ctx, `
		INSERT INTO quote_update_requests (id, pair, base_currency, quote_currency, status)
		VALUES ($1::uuid, $2, $3, $4, $5)
		RETURNING id::text, pair, base_currency, quote_currency, status, price::text, provider, error, created_at, started_at, finished_at
	`, id, pair.Raw, pair.Base, pair.Quote, domain.StatusPending))
	if err != nil {
		return domain.UpdateRequest{}, false, fmt.Errorf("insert update request: %w", err)
	}
	return req, false, nil
}

func (s *Store) GetUpdateRequest(ctx context.Context, id string) (domain.UpdateRequest, error) {
	req, err := scanUpdate(s.pool.QueryRow(ctx, `
		SELECT id::text, pair, base_currency, quote_currency, status, price::text, provider, error, created_at, started_at, finished_at
		FROM quote_update_requests
		WHERE id = $1::uuid
	`, id))
	if err != nil {
		return domain.UpdateRequest{}, translateNotFound(err, "get update request")
	}
	return req, nil
}

func (s *Store) GetLatestQuote(ctx context.Context, pair domain.Pair) (domain.LatestQuote, error) {
	var quote domain.LatestQuote
	var rawPair, base, target string
	err := s.pool.QueryRow(ctx, `
		SELECT pair, base_currency, quote_currency, price::text, provider, updated_at, request_id::text
		FROM latest_quotes
		WHERE pair = $1
	`, pair.Raw).Scan(&rawPair, &base, &target, &quote.Price, &quote.Provider, &quote.UpdatedAt, &quote.RequestID)
	if err != nil {
		return domain.LatestQuote{}, translateNotFound(err, "get latest quote")
	}
	quote.Pair = domain.Pair{Raw: rawPair, Base: base, Quote: target}
	return quote, nil
}

func (s *Store) ClaimPending(ctx context.Context, limit int) ([]domain.UpdateRequest, error) {
	rows, err := s.pool.Query(ctx, `
		WITH picked AS (
			SELECT id
			FROM quote_update_requests
			WHERE status = $1
			ORDER BY created_at ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE quote_update_requests q
		SET status = $3, started_at = now(), error = NULL
		FROM picked
		WHERE q.id = picked.id
		RETURNING q.id::text, q.pair, q.base_currency, q.quote_currency, q.status, q.price::text, q.provider, q.error, q.created_at, q.started_at, q.finished_at
	`, domain.StatusPending, limit, domain.StatusProcessing)
	if err != nil {
		return nil, fmt.Errorf("claim pending requests: %w", err)
	}
	defer rows.Close()

	var requests []domain.UpdateRequest
	for rows.Next() {
		req, err := scanUpdate(rows)
		if err != nil {
			return nil, fmt.Errorf("scan claimed request: %w", err)
		}
		requests = append(requests, req)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed requests: %w", err)
	}
	return requests, nil
}

func (s *Store) MarkSucceeded(ctx context.Context, id string, price string, provider string, updatedAt time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		WITH updated AS (
			UPDATE quote_update_requests
			SET status = $2, price = $3::numeric, provider = $4, error = NULL, finished_at = $5
			WHERE id = $1::uuid
			RETURNING id, pair, base_currency, quote_currency, price, provider
		)
		INSERT INTO latest_quotes (pair, base_currency, quote_currency, price, provider, updated_at, request_id)
		SELECT pair, base_currency, quote_currency, price, provider, $5, id
		FROM updated
		ON CONFLICT (pair) DO UPDATE
		SET base_currency = EXCLUDED.base_currency,
			quote_currency = EXCLUDED.quote_currency,
			price = EXCLUDED.price,
			provider = EXCLUDED.provider,
			updated_at = EXCLUDED.updated_at,
			request_id = EXCLUDED.request_id
	`, id, domain.StatusSucceeded, price, provider, updatedAt)
	if err != nil {
		return fmt.Errorf("mark request succeeded: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) MarkFailed(ctx context.Context, id string, message string, finishedAt time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE quote_update_requests
		SET status = $2, error = $3, finished_at = $4
		WHERE id = $1::uuid
	`, id, domain.StatusFailed, message, finishedAt)
	if err != nil {
		return fmt.Errorf("mark request failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

type updateScanner interface {
	Scan(dest ...any) error
}

func scanUpdate(row updateScanner) (domain.UpdateRequest, error) {
	var req domain.UpdateRequest
	var rawPair, base, quote string
	var status string
	var price, provider, errorMessage sql.NullString
	var startedAt, finishedAt sql.NullTime

	err := row.Scan(
		&req.ID,
		&rawPair,
		&base,
		&quote,
		&status,
		&price,
		&provider,
		&errorMessage,
		&req.CreatedAt,
		&startedAt,
		&finishedAt,
	)
	if err != nil {
		return domain.UpdateRequest{}, err
	}

	req.Pair = domain.Pair{Raw: rawPair, Base: base, Quote: quote}
	req.Status = domain.Status(status)
	if price.Valid {
		req.Price = price.String
	}
	if provider.Valid {
		req.Provider = provider.String
	}
	if errorMessage.Valid {
		req.Error = errorMessage.String
	}
	if startedAt.Valid {
		req.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		req.FinishedAt = &finishedAt.Time
	}
	return req, nil
}

func translateNotFound(err error, op string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}
