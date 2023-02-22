include ./app.env

.PHONY: run
run: ## runs the application
	go run cmd/api/*.go

.PHONY: build
build: ## builds the cli application
	go build -o v6manage cmd/v6manage/main.go

.PHONY: test
test: ## runs short tests
	go test ./... -short

.PHONY: test-all
test-all: ## runs all tests
	go test ./...

.PHONY: upgrade
upgrade: ## upgrade deps
	go get -u ./...
	go mod tidy

.PHONY: lint
lint: ## run linter
	golangci-lint run

.PHONY: migrateup
migrateup: ## migrates up the database
	migrate -path ./db/migrations -database $(DB_SOURCE) -verbose up

.PHONY: migratedown
migratedown: ## migrates down the database
	migrate -path ./db/migrations -database $(DB_SOURCE) -verbose down

.PHONY: pgcli
pgcli: ## pgcli tool
	pgcli $(DB_SOURCE)

.PHONY: pgdump
pgdump: ## dump database
	pg_dump -d "$(DB_SOURCE)" --format plain --data-only --use-set-session-authorization --quote-all-identifiers --column-inserts --file "tmp/dump-$$(date +%Y%m%d).sql"

.PHONY: sqlc
sqlc: ## generates go code from sql
	sqlc generate

.PHONY: live
live: ## live reload of application
	air .

## Help display.
## Pulls comments from beside commands and prints a nicely formatted
## display with the commands and their usage information.
.DEFAULT_GOAL := help

help: ## prints this help
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
