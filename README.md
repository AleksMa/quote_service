# Quotes service

HTTP JSON service for asynchronous currency quote updates.

## API

Create an update request:

```bash
curl -i -X POST http://localhost:8080/quote-updates \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-1' \
  -d '{"pair":"EUR/MXN"}'
```

Get an update request:

```bash
curl -i http://localhost:8080/quote-updates/<request_id>
```

Get the latest successful quote:

```bash
curl -i http://localhost:8080/quotes/latest/EUR/MXN
```

Supported pairs are configured in `config.yaml`

## Run

With Docker Compose:

```bash
docker compose up --build
```

Locally, start PostgreSQL and create database:

To create the database and tables run:
```bash
./scripts/init-db.sh
```

Then run the service:

```bash
go run ./cmd/quote-service
```

OpenAPI spec: `openapi/openapi.yaml`.

Run Swagger locally:

```bash
cd openapi && docker compose up
```

## Configuration

Use `CONFIG_PATH` to point the service to a config file:

```bash
CONFIG_PATH=config.yaml go run ./cmd/quote-service
```

The provider list is ordered by `priority`. The worker asks the first enabled provider and falls back to the next one if the call fails. Supported provider types:
- `frankfurter`: `https://api.frankfurter.dev`
- `exchangerate`: `https://exchangeratesapi.io/`, requires `api_key` or `api_key_env`

Worker settings:
- `worker.claim_limit`: maximum pending jobs claimed from PostgreSQL per tick.
- `worker.concurrency`: maximum jobs processed in parallel.
- `worker.interval`: worker tick interval in seconds.

## Details

### Strengths

- Client-side idempotency via optional `Idempotency-Key`: repeated requests with the same pair and key return the original `request_id`.
- Provider request deduplication by currency pair: multiple client requests for the same pair share one pending or processing background job.
- Multiple provider support: providers are configured in priority order, and the worker falls back to the next provider when a call fails.
- Docker Compose, OpenAPI, and tests are included for local verification and review.

### Known Limitations / Next Steps

- The worker processes at most `worker.claim_limit` jobs per `worker.interval`. A production service with a large queue could use a drain loop: keep claiming the next batch immediately while the previous batch was full, then fall back to interval polling when the queue is mostly empty.
- In-flight jobs stuck in `processing` are restored only on application restart. In production, a timeout-based recovery query should periodically return stale jobs to `pending`.
- For larger systems, quote update requests could be published through a durable broker such as Kafka. This would make queue events explicit and replayable, but would also require idempotent consumers and careful offset commits.
- Failed jobs are terminal. A production version would add retry policy and exponential backoff.
- A production service would use a migration tool and explicit migration history.
- In production, it is necessary to add metrics for queue depth, provider latency, failures, and deduplication rate.
