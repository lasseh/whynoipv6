include ./app.env

.PHONY: run
run: ## Runs the application
	go run cmd/api/*.go

.PHONY: build
build: ## Builds the CLI application
	go build -o v6manage cmd/v6manage/main.go

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
	golangci-lint run

.PHONY: migrateup
migrateup: ## Migrates up the database
	migrate -path ./db/migrations -database $(DB_SOURCE) -verbose up

.PHONY: migratedown
migratedown: ## Migrates down the database
	migrate -path ./db/migrations -database $(DB_SOURCE) -verbose down

.PHONY: pgcli
pgcli: ## Launches pgcli tool
	pgcli $(DB_SOURCE)

.PHONY: pgdump
pgdump: ## Dumps the database
	pg_dump -d "$(DB_SOURCE)" --format plain --data-only --use-set-session-authorization --quote-all-identifiers --column-inserts --file "tmp/dump-$$(date +%Y%m%d).sql"

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
