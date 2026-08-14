# Monorepo orchestrator — frontend-* targets run in frontend/, everything else
# delegates to the backend Go module.
# compose.yaml lives here at the root; docker compose finds it from backend/ too.

.DEFAULT_GOAL := help

%:
	@$(MAKE) -C backend $@

help:
	@$(MAKE) -C backend help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Frontend
.PHONY: frontend-dev frontend-build frontend-test frontend-lint

frontend-dev: ## Run the frontend dev server
	cd frontend && npm run dev

frontend-build: ## Type-check and build the frontend bundle
	cd frontend && npm run build

frontend-test: ## Run the frontend vitest suite
	cd frontend && npm run test

frontend-lint: ## Blocking gate: type-check + eslint (zero warnings) + prettier check
	cd frontend && npm run type-check && npm run lint:ci && npm run format:check
