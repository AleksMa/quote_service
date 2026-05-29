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
		UPDATE quote_update_jobs
		SET status = $1, started_at = NULL
		WHERE status = $2
	`, domain.StatusPending, domain.StatusProcessing)
	if err != nil {
		return fmt.Errorf("reset processing jobs: %w", err)
	}
	return nil
}

func (s *Store) CreateUpdateRequest(ctx context.Context, pair domain.Pair, idempotencyKey string) (domain.UpdateRequest, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.UpdateRequest{}, false, fmt.Errorf("begin create update request: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if idempotencyKey != "" {
		existing, err := selectUpdateByIdempotency(ctx, tx, pair, idempotencyKey)
		if err == nil {
			if err := tx.Commit(ctx); err != nil {
				return domain.UpdateRequest{}, false, fmt.Errorf("commit idempotent update request: %w", err)
			}
			return existing, true, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return domain.UpdateRequest{}, false, err
		}
	}

	job, err := ensureActiveJob(ctx, tx, pair)
	if err != nil {
		return domain.UpdateRequest{}, false, err
	}

	requestID, err := domain.NewUUID()
	if err != nil {
		return domain.UpdateRequest{}, false, fmt.Errorf("generate request id: %w", err)
	}

	created, err := insertUpdateRequest(ctx, tx, requestID, job.ID, pair, idempotencyKey)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return domain.UpdateRequest{}, false, fmt.Errorf("commit update request: %w", err)
		}
		return created, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.UpdateRequest{}, false, fmt.Errorf("insert update request: %w", err)
	}
	if idempotencyKey == "" {
		return domain.UpdateRequest{}, false, fmt.Errorf("insert update request returned no rows")
	}

	existing, err := selectUpdateByIdempotency(ctx, tx, pair, idempotencyKey)
	if err != nil {
		return domain.UpdateRequest{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.UpdateRequest{}, false, fmt.Errorf("commit idempotent update request: %w", err)
	}
	return existing, true, nil
}

func (s *Store) GetUpdateRequest(ctx context.Context, id string) (domain.UpdateRequest, error) {
	req, err := scanUpdate(s.pool.QueryRow(ctx, `
		SELECT r.id::text, r.job_id::text, r.pair, r.base_currency, r.quote_currency,
			j.status, j.price::text, j.provider, j.error, r.created_at, j.started_at, j.finished_at
		FROM quote_update_requests r
		JOIN quote_update_jobs j ON j.id = r.job_id
		WHERE r.id = $1::uuid
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

func (s *Store) ClaimPending(ctx context.Context, limit int) ([]domain.UpdateJob, error) {
	rows, err := s.pool.Query(ctx, `
		WITH picked AS (
			SELECT id
			FROM quote_update_jobs
			WHERE status = $1
			ORDER BY created_at ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE quote_update_jobs j
		SET status = $3, started_at = now(), error = NULL
		FROM picked
		WHERE j.id = picked.id
		RETURNING j.id::text, j.pair, j.base_currency, j.quote_currency, j.status, j.price::text, j.provider, j.error, j.created_at, j.started_at, j.finished_at
	`, domain.StatusPending, limit, domain.StatusProcessing)
	if err != nil {
		return nil, fmt.Errorf("claim pending jobs: %w", err)
	}
	defer rows.Close()

	var jobs []domain.UpdateJob
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan claimed job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed jobs: %w", err)
	}
	return jobs, nil
}

func (s *Store) MarkSucceeded(ctx context.Context, jobID string, price string, provider string, updatedAt time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		WITH updated AS (
			UPDATE quote_update_jobs
			SET status = $2, price = $3::numeric, provider = $4, error = NULL, finished_at = $5
			WHERE id = $1::uuid
				AND status IN ($6, $7)
			RETURNING id, pair, base_currency, quote_currency, price, provider
		),
		request_for_job AS (
			SELECT r.id
			FROM quote_update_requests r, updated u
			WHERE r.job_id = u.id
			ORDER BY r.created_at ASC
			LIMIT 1
		)
		INSERT INTO latest_quotes (pair, base_currency, quote_currency, price, provider, updated_at, request_id, job_id)
		SELECT u.pair, u.base_currency, u.quote_currency, u.price, u.provider, $5, r.id, u.id
		FROM updated u
		JOIN request_for_job r ON true
		ON CONFLICT (pair) DO UPDATE
		SET base_currency = EXCLUDED.base_currency,
			quote_currency = EXCLUDED.quote_currency,
			price = EXCLUDED.price,
			provider = EXCLUDED.provider,
			updated_at = EXCLUDED.updated_at,
			request_id = EXCLUDED.request_id,
			job_id = EXCLUDED.job_id
	`, jobID, domain.StatusSucceeded, price, provider, updatedAt, domain.StatusPending, domain.StatusProcessing)
	if err != nil {
		return fmt.Errorf("mark job succeeded: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return s.terminalJobError(ctx, jobID)
	}
	return nil
}

func (s *Store) MarkFailed(ctx context.Context, jobID string, message string, finishedAt time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE quote_update_jobs
		SET status = $2, error = $3, finished_at = $4
		WHERE id = $1::uuid
			AND status IN ($5, $6)
	`, jobID, domain.StatusFailed, message, finishedAt, domain.StatusPending, domain.StatusProcessing)
	if err != nil {
		return fmt.Errorf("mark job failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return s.terminalJobError(ctx, jobID)
	}
	return nil
}

func (s *Store) terminalJobError(ctx context.Context, jobID string) error {
	var status domain.Status
	err := s.pool.QueryRow(ctx, `
		SELECT status
		FROM quote_update_jobs
		WHERE id = $1::uuid
	`, jobID).Scan(&status)
	if err != nil {
		return translateNotFound(err, "get terminal job status")
	}
	switch status {
	case domain.StatusSucceeded, domain.StatusFailed:
		return domain.ErrAlreadyFinished
	default:
		return domain.ErrNotFound
	}
}

func ensureActiveJob(ctx context.Context, tx pgx.Tx, pair domain.Pair) (domain.UpdateJob, error) {
	for attempts := 0; attempts < 3; attempts++ {
		jobID, err := domain.NewUUID()
		if err != nil {
			return domain.UpdateJob{}, fmt.Errorf("generate job id: %w", err)
		}

		job, err := scanJob(tx.QueryRow(ctx, `
			INSERT INTO quote_update_jobs (id, pair, base_currency, quote_currency, status)
			VALUES ($1::uuid, $2, $3, $4, $5)
			ON CONFLICT (pair)
			WHERE status IN ('pending', 'processing')
			DO NOTHING
			RETURNING id::text, pair, base_currency, quote_currency, status, price::text, provider, error, created_at, started_at, finished_at
		`, jobID, pair.Raw, pair.Base, pair.Quote, domain.StatusPending))
		if err == nil {
			return job, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.UpdateJob{}, fmt.Errorf("insert update job: %w", err)
		}

		job, err = selectActiveJob(ctx, tx, pair)
		if err == nil {
			return job, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return domain.UpdateJob{}, err
		}
	}
	return domain.UpdateJob{}, fmt.Errorf("ensure active update job for pair %s: %w", pair.Raw, domain.ErrNotFound)
}

func selectActiveJob(ctx context.Context, tx pgx.Tx, pair domain.Pair) (domain.UpdateJob, error) {
	job, err := scanJob(tx.QueryRow(ctx, `
		SELECT id::text, pair, base_currency, quote_currency, status, price::text, provider, error, created_at, started_at, finished_at
		FROM quote_update_jobs
		WHERE pair = $1 AND status IN ($2, $3)
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE
	`, pair.Raw, domain.StatusPending, domain.StatusProcessing))
	if err != nil {
		return domain.UpdateJob{}, translateNotFound(err, "select active update job")
	}
	return job, nil
}

func insertUpdateRequest(ctx context.Context, tx pgx.Tx, requestID string, jobID string, pair domain.Pair, idempotencyKey string) (domain.UpdateRequest, error) {
	if idempotencyKey != "" {
		return scanUpdate(tx.QueryRow(ctx, `
			WITH inserted AS (
				INSERT INTO quote_update_requests (id, job_id, pair, base_currency, quote_currency, idempotency_key)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)
				ON CONFLICT (pair, idempotency_key)
				WHERE idempotency_key IS NOT NULL AND idempotency_key <> ''
				DO NOTHING
				RETURNING id, job_id, pair, base_currency, quote_currency, created_at
			)
			SELECT i.id::text, i.job_id::text, i.pair, i.base_currency, i.quote_currency,
				j.status, j.price::text, j.provider, j.error, i.created_at, j.started_at, j.finished_at
			FROM inserted i
			JOIN quote_update_jobs j ON j.id = i.job_id
		`, requestID, jobID, pair.Raw, pair.Base, pair.Quote, idempotencyKey))
	}

	return scanUpdate(tx.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO quote_update_requests (id, job_id, pair, base_currency, quote_currency)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5)
			RETURNING id, job_id, pair, base_currency, quote_currency, created_at
		)
		SELECT i.id::text, i.job_id::text, i.pair, i.base_currency, i.quote_currency,
			j.status, j.price::text, j.provider, j.error, i.created_at, j.started_at, j.finished_at
		FROM inserted i
		JOIN quote_update_jobs j ON j.id = i.job_id
	`, requestID, jobID, pair.Raw, pair.Base, pair.Quote))
}

func selectUpdateByIdempotency(ctx context.Context, tx pgx.Tx, pair domain.Pair, idempotencyKey string) (domain.UpdateRequest, error) {
	req, err := scanUpdate(tx.QueryRow(ctx, `
		SELECT r.id::text, r.job_id::text, r.pair, r.base_currency, r.quote_currency,
			j.status, j.price::text, j.provider, j.error, r.created_at, j.started_at, j.finished_at
		FROM quote_update_requests r
		JOIN quote_update_jobs j ON j.id = r.job_id
		WHERE r.pair = $1 AND r.idempotency_key = $2
		ORDER BY r.created_at ASC
		LIMIT 1
	`, pair.Raw, idempotencyKey))
	if err != nil {
		return domain.UpdateRequest{}, translateNotFound(err, "select idempotent update request")
	}
	return req, nil
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
		&req.JobID,
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

func scanJob(row updateScanner) (domain.UpdateJob, error) {
	var job domain.UpdateJob
	var rawPair, base, quote string
	var status string
	var price, provider, errorMessage sql.NullString
	var startedAt, finishedAt sql.NullTime

	err := row.Scan(
		&job.ID,
		&rawPair,
		&base,
		&quote,
		&status,
		&price,
		&provider,
		&errorMessage,
		&job.CreatedAt,
		&startedAt,
		&finishedAt,
	)
	if err != nil {
		return domain.UpdateJob{}, err
	}

	job.Pair = domain.Pair{Raw: rawPair, Base: base, Quote: quote}
	job.Status = domain.Status(status)
	if price.Valid {
		job.Price = price.String
	}
	if provider.Valid {
		job.Provider = provider.String
	}
	if errorMessage.Valid {
		job.Error = errorMessage.String
	}
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		job.FinishedAt = &finishedAt.Time
	}
	return job, nil
}

func translateNotFound(err error, op string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}
