CREATE TABLE IF NOT EXISTS quote_update_requests (
    id uuid PRIMARY KEY,
    pair text NOT NULL,
    base_currency char(3) NOT NULL,
    quote_currency char(3) NOT NULL,
    status text NOT NULL CHECK (status IN ('pending', 'processing', 'succeeded', 'failed')),
    price numeric(20, 10),
    provider text,
    error text,
    idempotency_key text,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS quote_update_requests_idempotency_unique
    ON quote_update_requests (pair, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

CREATE INDEX IF NOT EXISTS quote_update_requests_status_created_at_idx
    ON quote_update_requests (status, created_at);

CREATE TABLE IF NOT EXISTS latest_quotes (
    pair text PRIMARY KEY,
    base_currency char(3) NOT NULL,
    quote_currency char(3) NOT NULL,
    price numeric(20, 10) NOT NULL,
    provider text NOT NULL,
    updated_at timestamptz NOT NULL,
    request_id uuid NOT NULL REFERENCES quote_update_requests(id)
);
