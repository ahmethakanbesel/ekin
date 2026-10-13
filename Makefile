VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)"
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0

# LOAD_ENV exports .env into the recipe when it exists.
LOAD_ENV := set -a; [ -f .env ] && . ./.env; set +a;

.PHONY: generate build web run dev test lint fmt migrate clean

generate:
	oapi-codegen -config oapi-codegen.yaml api/openapi.yaml
	@if [ -f web/package.json ]; then cd web && bun run generate:client; fi

web:
	cd web && bun install --frozen-lockfile && bun run build

build: web
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/kurgu ./cmd/kurgu

# Laptop mode: API, web UI, scheduler and worker in one process.
run:
	@$(LOAD_ENV) go run $(LDFLAGS) ./cmd/kurgu all

# API + worker with the Vite dev server proxying /api to it.
dev:
	@$(LOAD_ENV) go run ./cmd/kurgu all & cd web && bun run dev; kill %1

test:
	go test -race ./...

lint:
	$(GOLANGCI_LINT) run ./...

fmt:
	gofmt -w cmd internal pkg web/embed.go

migrate:
	@$(LOAD_ENV) go run ./cmd/kurgu migrate up

clean:
	rm -rf bin
