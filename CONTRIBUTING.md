# Contributing

Thanks for helping shame the IPv6-less internet. Small fixes, checker improvements,
and frontend polish are all welcome — for bigger ideas, open an issue first.

## Getting started

Read [`docs/architecture.md`](docs/architecture.md) for how the system fits
together, then [`docs/deploy.md`](docs/deploy.md) to get the local Docker stack
running. [`docs/internals.md`](docs/internals.md) is the codebase tour.

`make` is the universal interface (`make help` lists all targets):

```sh
make compose-up        # local dev stack (Postgres+Timescale, Unbound×2, api, crawler)
make test              # backend unit tests (race detector on)
make test-integration  # backend integration tests (needs Docker)
make lint              # golangci-lint
make frontend-test     # frontend vitest suite
make frontend-lint     # type-check + eslint + prettier
```

## Before you open a PR

1. **Run the gates**: `make test && make lint` (backend) and/or
   `make frontend-test && make frontend-lint && make frontend-build` (frontend).
   CI runs these plus `make generate` and `make spec-lint`.
2. **Generated code is a build artifact.** Never hand-edit
   `backend/internal/api/gen/`, `backend/internal/postgres/db/`, or
   `openapi/schema.ts`. Change the source (`openapi/openapi.yaml` or
   `backend/db/query/*.sql`) and run `make generate` — CI fails if the tree drifts.
3. **API changes start in the contract.** Edit `openapi/openapi.yaml` first; both
   the Go server and the frontend types are generated from it.
4. **Commit style**: imperative, lowercase, `<type>: <description>` with
   `type ∈ feat|fix|refactor|test|docs|chore|ci|build`, subject ≤ 72 chars —
   e.g. `fix(crawler): reclaim stale leases before claiming`.

## What won't be merged

Some constraints are by design, not oversight (see
[`docs/architecture.md`](docs/architecture.md)): no user accounts or auth, no
numeric scores or letter grades, no ranked-list sources besides Tranco, no queues
or brokers, and no mutation of scan/changelog history.

## Campaigns

Want a country's banks or government sites tracked? Campaign lists are plain YAML
managed by pull request in
[whynoipv6-campaign](https://github.com/lasseh/whynoipv6-campaign) — no Go
required.
