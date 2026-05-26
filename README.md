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

Locally, start PostgreSQL and export at least:

```bash
go run ./cmd/quote-service
```

The service applies its schema on startup.

To create the database and tables run:
```bash
./scripts/init-db.sh
```

## Configuration

Use `CONFIG_PATH` to point the service to a config file:

```bash
CONFIG_PATH=config.yaml go run ./cmd/quote-service
```

For browser-based OpenAPI tools, configure allowed CORS origins under `http.cors_allowed_origins`.
The local config uses `*` so hosted Swagger/OpenAPI tools can call the API during development.

The provider list is ordered by `priority`. The worker asks the first enabled provider and falls back to the next one if the call fails. Supported provider types:
- `frankfurter`: `https://api.frankfurter.dev`
- `exchangerate`: `https://exchangeratesapi.io/`, requires `api_key` or `api_key_env`
- `currencyapi`: `https://api.currencyapi.com`, requires `api_key` or `api_key_env`

## Development

```bash
go test ./...
go build ./cmd/quote-service
make db-init
```

Postgres integration tests are opt-in. Point them at a disposable database:

```bash
TEST_DATABASE_URL='postgres://quote_service:quote_service@localhost:5432/quote_service_test?sslmode=disable' \
  go test ./internal/storage/postgres
```

OpenAPI spec: `openapi/openapi.yaml`.
