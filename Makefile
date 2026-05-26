GO ?= go

.PHONY: test run build db-init

test:
	$(GO) test ./...

run:
	$(GO) run ./cmd/quote-service

build:
	$(GO) build -o bin/quote-service ./cmd/quote-service

db-init:
	sh scripts/init-db.sh
