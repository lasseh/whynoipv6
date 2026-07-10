# Monorepo orchestrator — every target delegates to the backend Go module.
# compose.yaml lives here at the root; docker compose finds it from backend/ too.

.DEFAULT_GOAL := help

%:
	@$(MAKE) -C backend $@

help:
	@$(MAKE) -C backend help
