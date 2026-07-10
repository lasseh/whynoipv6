GO      ?= go
GOFLAGS ?=
BINARIES := api crawler v6ctl

GOTESTSUM := $(shell command -v gotestsum 2>/dev/null)
ifdef GOTESTSUM
  TEST_CMD := gotestsum --format testdox --
else
  TEST_CMD := $(GO) test
endif

.PHONY: build build-linux test test-integration lint tidy vulncheck generate coverage compose-up compose-down clean help

##@ General
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Build
build: ## Build all three binaries
	@for b in $(BINARIES); do $(GO) build $(GOFLAGS) -o bin/$$b ./cmd/$$b; done

build-linux: ## Build static linux/amd64 release binaries
	@for b in $(BINARIES); do CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	  $(GO) build -ldflags="-s -w" -o dist/$$b ./cmd/$$b; done

##@ Quality
test: ## Run tests (race detector always on)
	$(TEST_CMD) -race ./...

lint: ## Run golangci-lint (vet, fmt, imports included)
	golangci-lint run ./...

test-integration: ## Integration tests (dockerized PG18+Timescale; //go:build integration files)
	$(TEST_CMD) -race -tags=integration ./...

coverage: ## Tests with coverage report
	$(TEST_CMD) -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

vulncheck: ## Scan for known vulnerabilities
	govulncheck ./...

##@ Codegen
generate: ## Regenerate sqlc + OpenAPI codegen (activates once openapi.yaml exists); fail if stale
	sqlc generate
	@if [ -f ../openapi/openapi.yaml ]; then \
	  oapi-codegen -config ../openapi/oapi-codegen.yaml ../openapi/openapi.yaml; \
	  openapi-typescript ../openapi/openapi.yaml -o ../openapi/schema.ts; \
	else \
	  echo "openapi/openapi.yaml not present yet (pre-P4.5) — sqlc only"; \
	fi
	@git diff --exit-code || (echo "generated code is out of date — run codegen and commit" && exit 1)

##@ Utility
tidy: ## Verify go.mod/go.sum are tidy
	$(GO) mod tidy
	@git diff --exit-code go.mod go.sum || (echo "go.mod/go.sum not tidy" && exit 1)

compose-up: ## Start the dev environment
	docker compose up --build -d

compose-down: ## Stop the dev environment and remove volumes
	docker compose down -v

clean: ## Remove build artifacts
	rm -rf bin dist coverage.out coverage.html

.DEFAULT_GOAL := help
