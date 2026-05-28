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
