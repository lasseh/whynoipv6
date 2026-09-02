GO      ?= go
GOFLAGS ?=
BINARIES := api crawler v6ctl
# Everything `make generate` writes; the drift gate checks exactly this set.
GENERATED := internal/postgres/db internal/api/gen ../openapi/schema.ts

# Integration tests run against one container for the whole `go test ./...`
# run: eight per-binary servers overwhelmed the CI runner and killed Postgres
# mid-suite. pgtest boots its own only when PGTEST_DSN is unset.
PGTEST_IMAGE     ?= timescale/timescaledb:latest-pg18
PGTEST_CONTAINER ?= whynoipv6-testdb
PGTEST_PORT      ?= 15433
PGTEST_DSN       ?= postgres://postgres:integration@127.0.0.1:$(PGTEST_PORT)/postgres?sslmode=disable

GOTESTSUM := $(shell command -v gotestsum 2>/dev/null)
ifdef GOTESTSUM
  TEST_CMD := gotestsum --format testdox --
else
  TEST_CMD := $(GO) test
endif

.PHONY: build build-linux test test-integration testdb-up testdb-down lint tidy vulncheck generate spec-lint coverage compose-up compose-down clean help

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

# -count=1 because these tests read things go's cache does not track: the nginx
# vhosts under ../deploy (outside the module, so file-read tracking does not
# reach them) and the state of docker. Without it, editing a vhost and re-running
# reports a cached pass and TestNginxVhostsAgree never runs.
test-integration: ## Integration tests (one dockerized PG18+Timescale for the whole run; //go:build integration files)
	@$(MAKE) testdb-up
	@PGTEST_DSN="$(PGTEST_DSN)" $(TEST_CMD) -count=1 -race -tags=integration ./...; status=$$?; \
	  if [ $$status -ne 0 ]; then \
	    echo "==> test database state (exit code, then the last 200 log lines)"; \
	    docker ps -a --filter name=$(PGTEST_CONTAINER) --format '{{.Names}} {{.Status}}'; \
	    docker logs --tail 200 $(PGTEST_CONTAINER) 2>&1 || true; \
	  fi; \
	  $(MAKE) testdb-down; exit $$status

testdb-up: ## Start the integration-test Postgres
	@docker rm -f $(PGTEST_CONTAINER) >/dev/null 2>&1 || true
	@docker run -d --name $(PGTEST_CONTAINER) \
	  -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=integration -e POSTGRES_DB=postgres \
	  -p 127.0.0.1:$(PGTEST_PORT):5432 --shm-size=256m $(PGTEST_IMAGE) \
	  -c timescaledb.max_background_workers=0 -c max_connections=200 \
	  -c fsync=off -c synchronous_commit=off -c full_page_writes=off >/dev/null
	@printf 'waiting for %s' $(PGTEST_CONTAINER); \
	 for _ in $$(seq 1 90); do \
	   if docker exec $(PGTEST_CONTAINER) pg_isready -h 127.0.0.1 -U postgres -q 2>/dev/null; then \
	     echo ' ready'; exit 0; \
	   fi; \
	   printf '.'; sleep 1; \
	 done; \
	 echo ' timed out'; docker logs $(PGTEST_CONTAINER); exit 1

testdb-down: ## Remove the integration-test Postgres
	@docker rm -f $(PGTEST_CONTAINER) >/dev/null 2>&1 || true

coverage: ## Tests with coverage report
	$(TEST_CMD) -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

vulncheck: ## Scan for known vulnerabilities
	govulncheck ./...

##@ Codegen
generate: ## Regenerate sqlc + OpenAPI codegen; fail if the tree is stale (the CI drift gate)
	sqlc generate
	$(GO) tool oapi-codegen -config ../openapi/oapi-codegen.yaml ../openapi/openapi.yaml
	npx -y openapi-typescript@7 ../openapi/openapi.yaml -o ../openapi/schema.ts
	@# status --porcelain, not diff: a generator that emits a NEW file leaves
	@# it untracked, which diff does not see. Scoped so unrelated local edits
	@# cannot fail the gate.
	@test -z "$$(git status --porcelain -- $(GENERATED))" || \
	  (git status --porcelain -- $(GENERATED); echo "generated code is out of date — run codegen and commit" && exit 1)

spec-lint: ## Lint openapi.yaml with Spectral (snake_case, envelopes, problem+json)
	npx -y @stoplight/spectral-cli@6 lint ../openapi/openapi.yaml \
	  --ruleset ../openapi/.spectral.yaml --fail-severity=error

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
