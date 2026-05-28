CREATE TABLE IF NOT EXISTS quote_update_jobs (
    id uuid PRIMARY KEY,
    pair text NOT NULL,
    base_currency char(3) NOT NULL,
    quote_currency char(3) NOT NULL,
    status text NOT NULL CHECK (status IN ('pending', 'processing', 'succeeded', 'failed')),
    price numeric(20, 10),
    provider text,
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz
);

CREATE TABLE IF NOT EXISTS quote_update_requests (
    id uuid PRIMARY KEY,
    job_id uuid REFERENCES quote_update_jobs(id),
    pair text NOT NULL,
    base_currency char(3) NOT NULL,
    quote_currency char(3) NOT NULL,
    idempotency_key text,
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE quote_update_requests
    ADD COLUMN IF NOT EXISTS job_id uuid REFERENCES quote_update_jobs(id);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'quote_update_requests'
          AND column_name = 'status'
    ) THEN
        ALTER TABLE quote_update_requests ALTER COLUMN status DROP NOT NULL;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS quote_update_requests_idempotency_unique
    ON quote_update_requests (pair, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

CREATE INDEX IF NOT EXISTS quote_update_jobs_status_created_at_idx
    ON quote_update_jobs (status, created_at);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'quote_update_requests'
          AND column_name = 'status'
    ) THEN
        INSERT INTO quote_update_jobs (
            id,
            pair,
            base_currency,
            quote_currency,
            status,
            price,
            provider,
            error,
            created_at,
            started_at,
            finished_at
        )
        SELECT
            r.id,
            r.pair,
            r.base_currency,
            r.quote_currency,
            r.status,
            r.price,
            r.provider,
            r.error,
            r.created_at,
            r.started_at,
            r.finished_at
        FROM quote_update_requests r
        WHERE r.job_id IS NULL
          AND r.status NOT IN ('pending', 'processing')
        ON CONFLICT (id) DO NOTHING;

        WITH active_roots AS (
            SELECT DISTINCT ON (pair)
                id,
                pair,
                base_currency,
                quote_currency,
                status,
                price,
                provider,
                error,
                created_at,
                started_at,
                finished_at
            FROM quote_update_requests
            WHERE job_id IS NULL
              AND status IN ('pending', 'processing')
            ORDER BY pair, created_at ASC
        )
        INSERT INTO quote_update_jobs (
            id,
            pair,
            base_currency,
            quote_currency,
            status,
            price,
            provider,
            error,
            created_at,
            started_at,
            finished_at
        )
        SELECT
            id,
            pair,
            base_currency,
            quote_currency,
            status,
            price,
            provider,
            error,
            created_at,
            started_at,
            finished_at
        FROM active_roots
        ON CONFLICT (id) DO NOTHING;

        UPDATE quote_update_requests r
        SET job_id = r.id
        WHERE r.job_id IS NULL
          AND r.status NOT IN ('pending', 'processing')
          AND EXISTS (
              SELECT 1
              FROM quote_update_jobs j
              WHERE j.id = r.id
          );

        WITH active_roots AS (
            SELECT DISTINCT ON (pair) id, pair
            FROM quote_update_requests
            WHERE job_id IS NULL
              AND status IN ('pending', 'processing')
            ORDER BY pair, created_at ASC
        )
        UPDATE quote_update_requests r
        SET job_id = roots.id
        FROM active_roots roots
        WHERE r.job_id IS NULL
          AND r.pair = roots.pair
          AND r.status IN ('pending', 'processing');
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS quote_update_jobs_active_pair_unique
    ON quote_update_jobs (pair)
    WHERE status IN ('pending', 'processing');

CREATE TABLE IF NOT EXISTS latest_quotes (
    pair text PRIMARY KEY,
    base_currency char(3) NOT NULL,
    quote_currency char(3) NOT NULL,
    price numeric(20, 10) NOT NULL,
    provider text NOT NULL,
    updated_at timestamptz NOT NULL,
    request_id uuid NOT NULL REFERENCES quote_update_requests(id),
    job_id uuid REFERENCES quote_update_jobs(id)
);

ALTER TABLE latest_quotes
    ADD COLUMN IF NOT EXISTS job_id uuid REFERENCES quote_update_jobs(id);

UPDATE latest_quotes l
SET job_id = r.job_id
FROM quote_update_requests r
WHERE l.request_id = r.id
  AND l.job_id IS NULL;
