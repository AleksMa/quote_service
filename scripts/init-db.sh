#!/usr/bin/env sh
set -eu

DB_NAME="${DB_NAME:-quote_service}"
DB_USER="${DB_USER:-quote_service}"
DB_PASSWORD="${DB_PASSWORD:-quote_service}"
POSTGRES_URL="${POSTGRES_URL:-postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable}"
DATABASE_URL="${DATABASE_URL:-postgres://${DB_USER}:${DB_PASSWORD}@localhost:5432/${DB_NAME}?sslmode=disable}"
SCHEMA_PATH="${SCHEMA_PATH:-internal/storage/postgres/schema.sql}"

psql "$POSTGRES_URL" \
  -v db_name="$DB_NAME" \
  -v db_user="$DB_USER" \
  -v db_password="$DB_PASSWORD" \
  -f scripts/create_database.sql

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$SCHEMA_PATH"
