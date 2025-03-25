# Load environment variables from .env if the file exists
include ./app.env

.PHONY: run
run: ## Runs the application
	go run cmd/api/*.go

.PHONY: build
build: ## Builds the CLI application
	go build -o v6manage cmd/v6manage/main.go

.PHONY: install
install: ## Builds the CLI application
	go build -o $HOME/go/bin/v6manage cmd/v6manage/main.go
	go build -o $HOME/go/bin/v6-api cmd/api/main.go

.PHONY: test
test: ## Runs short tests
	go test ./... -short

.PHONY: test-all
test-all: ## Runs all tests
	go test ./...

.PHONY: upgrade
upgrade: ## Upgrades dependencies
	go get -u ./...
	go mod tidy

.PHONY: lint
lint: ## Runs the linter
	gofumpt -w .
	goimports -w .
	golines -w .
	golangci-lint run
	govulncheck ./...

.PHONY: migrateup
migrateup: ## Migrates up the database
	@command -v migrate >/dev/null 2>&1 || { \
		echo >&2 "migrate command not found, installing..."; \
		go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest; \
	}
	@echo "📥 Running database migrations (up)..."
	migrate -path ./db/migrations -database $(DB_SOURCE) -verbose up
	@echo "✅ Migrations applied successfully!"

.PHONY: migratedown
migratedown: ## Migrates down the database
	migrate -path ./db/migrations -database $(DB_SOURCE) -verbose down

.PHONY: pgcli
pgcli: ## Launches pgcli tool
	pgcli $(DB_SOURCE)

.PHONY: pgdump
pgdump: ## Dumps the database
	pg_dump -d "$(DB_SOURCE)" --format plain --data-only --use-set-session-authorization --quote-all-identifiers --column-inserts --file "tmp/dump-$$(date +%Y%m%d).sql"

.PHONY: tldbwriter
tldbwriter: ## Runs the tldbwriter tool
	@command -v tldbwriter >/dev/null 2>&1 || { \
		echo >&2 "tldbwriter command not found, installing..."; \
		go install -v github.com/eest/tranco-list-api/cmd/tldbwriter@latest; \
	}
	tldbwriter -config=tldbwriter.toml

.PHONY: sqlc
sqlc: ## Generates Go code from SQL
	sqlc generate

.PHONY: live
live: ## Live reload of the application
	air .

## Help display.
## Pulls comments from beside commands and prints a nicely formatted
## display with the commands and their usage information.
.DEFAULT_GOAL := help

help: ## Prints this help
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
