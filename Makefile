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

##@ Release
.PHONY: release

release: ## Cut a release (prompts for vX.Y.Z; gates, tags, pushes — release.yml builds binaries, the GitHub release, and the GHCR image)
	@if [ -n "$$(git status --porcelain)" ]; then echo "error: working tree not clean — commit first"; exit 1; fi
	@branch=$$(git rev-parse --abbrev-ref HEAD); \
	last=$$(git tag -l 'v*' --sort=-v:refname | head -n1); \
	echo "Branch:       $$branch"; \
	echo "Last release: $${last:-<none>}"; \
	printf "New release version (e.g. v1.2.3): "; \
	read version; \
	case "$$version" in \
		v[0-9]*.[0-9]*.[0-9]*) ;; \
		*) echo "error: version must look like vX.Y.Z"; exit 1 ;; \
	esac; \
	if git rev-parse "$$version" >/dev/null 2>&1; then echo "error: tag $$version already exists"; exit 1; fi; \
	echo "==> running gates"; \
	$(MAKE) lint test frontend-lint frontend-test || exit 1; \
	echo "==> tagging $$version and pushing to origin"; \
	git tag -a "$$version" -m "Release $$version" || exit 1; \
	git push origin "$$version" || exit 1; \
	echo "==> pushed $$version — release.yml now builds binaries, the GitHub release, and the GHCR image"
