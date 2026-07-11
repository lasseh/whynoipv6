# Monorepo orchestrator — frontend-* targets run in frontend/, everything else
# delegates to the backend Go module.
# compose.yaml lives here at the root; docker compose finds it from backend/ too.

.DEFAULT_GOAL := help

%:
	@$(MAKE) -C backend $@

help:
	@$(MAKE) -C backend help

.PHONY: frontend-dev frontend-build frontend-test frontend-lint frontend-check

frontend-dev: ## Run the frontend dev server
	cd frontend && npm run dev

frontend-build: ## Type-check and build the frontend bundle
	cd frontend && npm run build

frontend-test: ## Run the frontend vitest suite
	cd frontend && npm run test

frontend-lint: ## Blocking gate: type-check + eslint errors + prettier check
	cd frontend && npm run type-check && npm run lint:quiet && npm run format:check

frontend-check: ## Advisory full-warning ESLint run
	cd frontend && npm run lint
