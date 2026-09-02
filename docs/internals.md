# Internals — a tour of the codebase

Code-level map for anyone working on the repo. System-level context is in
[`architecture.md`](architecture.md); the normative spec is [`spec/`](spec/) — most
type and function comments cite spec sections (e.g. `03-state-machine.md §5`), so the
code doubles as the spec's implementation index. Post-spec vocabulary lives in the
repo-root [`CONTEXT.md`](../CONTEXT.md).

## Repo layout

```
backend/     one Go module (github.com/lasseh/whynoipv6), three binaries
frontend/    Vue 3 + TS + Tailwind v4 SPA, its own Node manifest root
openapi/     the contract: openapi.yaml + codegen configs + generated schema.ts
deploy/      systemd / nginx / unbound / pgbackrest / grafana assets
docs/        this documentation, spec/, adr/, runbooks/
```

`make` is the universal interface — `make help` lists everything. Backend targets run
in `backend/`, `frontend-*` targets in `frontend/`.

## The three binaries (`backend/cmd/`)

- **`api`** — loads config, opens a pgx pool, serves `api.NewRouter` on `API_LISTEN`
  (default loopback `[::1]:8080`, nginx in front). Graceful 15s drain on SIGTERM.
- **`crawler`** — linear fail-fast startup wiring the whole scanning machine:
  GeoIP reader (missing mmdb is fatal) → bulk resolver + SSRF dialer → consensus
  resolver → checker runner + IPv6 preflight → committer → frontier + worker pool,
  plus goroutines for the coordinator (daily tick / Tranco schedules), metrics
  checkpointer, hourly GeoIP reload, live-check consumers, and the resource sweeper.
  SIGTERM stops claiming immediately, in-flight scans drain under an 80s budget.
- **`v6ctl`** — cobra CLI; every operator action in the system. Full verb catalog in
  [`deploy.md §2`](deploy.md).

## Backend package map (`backend/internal/`)

| Package | Responsibility |
| --- | --- |
| `domain` | The core vocabulary, bottom of the import graph (no `internal/` imports; `golang.org/x/net` idna/publicsuffix is its only non-stdlib dep): `Dimension`, `IPv6Status` (public 4-value), `Observation` (internal 7-value), `Classification`, `Kind`, `Canonicalize()` (PSL host normalization), the classification ladder (`Classify`), anti-flap thresholds (`ConfirmN`). Mirrors the DB enums exactly. |
| `checker` | The scan engine lifted from v6audit: 15 checks (AAAA base/www, NS, MX, PTR, DNSSEC, HTTP/HTTPS/TLS over v6, response parity, SMTP, SPF, latency, resource discovery), two-phase conditional execution, SSRF-pinned `SafeDialer`, IPv6 self-preflight. All scoring deleted. |
| `consensus` | The 2-of-3 quorum resolver (Cloudflare/Google/Quad9) used only for the two classification-critical AAAA lookups; per-provider rate caps and circuit breakers, plus a fast-lane breaker. |
| `observe` | The neutral seam between the engine and its two consumers: `MapObservations` (crawler commit path) and `MapLiveResult` (API live-check path) guarantee identical mapping; also the `LinkSet` constructors for the resources roll-up. The API never imports the crawler — they meet here. |
| `crawler` | The daemon logic: frontier claim loop (`frontier.go`), per-domain worker (`worker.go`), the commit machine (`commit.go`), scheduling/backoff (`schedule.go`), the daily tick (`tick.go`), lifecycle + resource sweeps, live-check consumers, metrics checkpoints. |
| `ingest` | Tranco fetch/parse/upsert with sanity guards (≥950k rows, ≤2% delist), and the DNS-provider / hosting-provider attribution mappings. |
| `campaign` | Campaign-YAML parse, validation (also used DB-less by CI), and the idempotent `Sync` into the DB. |
| `geoip` | IPinfo Lite mmdb reader with hourly hot-reload and insert-time country/ASN attribution (ccTLD beats GeoIP). |
| `postgres` | The DB adapter: sqlc-generated code in `postgres/db/` (~118-method `Querier`), and the three squirrel keyset builders (`domainlist.go`, `asnlist.go`, `changeloglist.go`) over the shared runner in `keysetquery.go` (see below). `pgtest/` is the testcontainers harness. |
| `api` | chi router + one handler file per resource; keyset pagination, `{items,page,meta}` envelopes, RFC 9457 problems, feeds/badges/CSV/datasets serializers; `gen/` is oapi-codegen output. |
| `export` | Nightly static dataset snapshots: 3 size tiers × CSV.gz + Parquet, self-describing (datapackage.json, SHA256SUMS), atomic manifest rewrite. |
| `config` | Two-tier viper loader (defaults < `/etc/whynoipv6/config.yaml` < env) shared by all three binaries; slog installer; startup key dump with secrets redacted. |
| `lock` | Postgres advisory-lock singletons (class 60660): daily tick, Tranco import, campaign sync, dataset export. |
| `notify` | Ops webhook + healthchecks.io pings, min-interval throttled. |

## Life of a scan

1. **Claim.** The frontier loop gates on the IPv6 preflight, then
   `ClaimBatch` (SQL, `FOR UPDATE SKIP LOCKED`, 30-min lease reclaim, batch 200)
   leases due `domain` rows ordered by rank. Each `ClaimedDomain` is a full
   snapshot — **the commit never re-SELECTs** the row.
2. **Scan.** A worker slot (64 max) runs the engine: phase 1 DNS, phase 2
   conditional HTTP/TLS/mail checks against the addresses phase 1 returned.
3. **Map.** `observe.MapObservations` reduces the raw `ScanResult` to the 7-valued
   per-dimension `Observations`; GeoIP attribution and the dead-domain signal are
   computed alongside.
4. **Commit.** `ComputeCommit` (a **pure function** — all state math, no I/O) runs
   the confirm/pending anti-flap loop, the classification ladder, dead-streak and
   error-streak triggers, and next-check scheduling, yielding one commit unit:
   domain UPDATE + changelog rows + scan + scan_detail + resource links.
   `Committer.flush` sends it as a single transaction whose first statement is a
   **lease-fenced UPDATE** — zero rows affected means the lease was lost and the
   whole unit is discarded.
5. **Observe the observers.** `Metrics.RecordScan` accumulates into periodic
   `crawler_metrics` checkpoints (what the Grafana alerts watch).

The daily tick (03:30 UTC, advisory-locked) runs the housekeeping sequence:
lifecycle sweep → stats rollup → service-candidate detection → campaign sync →
check-job purge → ops summary/heartbeat. The Tranco import cycle (23:15 UTC) is a
separate schedule with 2h retries.

`POST /check` is the one write path: it enqueues a `check_job`; the crawler's
live-check consumers run the same engine and map through the same `observe` package,
so a live result can never disagree in shape with a stored one.

## Database layer

- **Migrations:** numbered pairs under `backend/db/migrations/`, starting with
  `000001_base_schema`, `000002_timescaledb`, `000003_seed` and appended to as
  the schema moves. Embedded via `go:embed`, applied by `v6ctl migrate up`.
  Forward-only in production — there is deliberately no `down` verb, though the
  `.down.sql` files exist for the test harness's down/up round trip.
- **Hypertables:** `scan` (2y retention), `scan_detail` (90d), `changelog`
  (forever), `crawler_metrics` (90d), `unbound_stats` (30d), `stats_asn_daily`;
  plus the `scan_daily_adoption` continuous aggregate. Modern columnstore API only.
- **sqlc-first:** all static SQL lives in `backend/db/query/*.sql` (15 files) and
  generates into `internal/postgres/db`. Three documented escape hatches, all
  sanctioned by 05-schema.md §10.2: (a) the keyset list builders
  `postgres/{domainlist,asnlist,changeloglist}.go` — the `/domains` filter grammar
  alone would need hundreds of sqlc variants — plus `MaxRank`/`EXPLAIN` in
  `domainlist.go`; (b) the two multi-CTE resource statements
  `SQLUpsertDomainResource` / `SQLPruneDomainResources` as Go constants in
  `postgres/commitflush.go`; (c) the Tranco staging statements in `ingest/tranco.go`
  (a session temp table sqlc cannot type).
- **Testing:** `internal/postgres/pgtest` spins a real PG18+Timescale container and
  template-clones a freshly-migrated database per test; integration tests are
  `//go:build integration` files run by `make test-integration`.

## Config

The registry is a **pair**: `internal/config/defaults.go` holds the compiled-in
default and `docs/spec/09-ops.md` §2 holds the documented row — `TestRegistryCompleteness`
(`internal/config/registry_test.go`) fails if either side is missing. Adding a key =
one entry in `registryDefaults` **plus** one row in the matching 09-ops §2.x
subsection, using that subsection's column layout (dotted keys:
`` | `key` | `ENV_VAR` | Type | Default | From | Meaning | ``). Env-var names
derive mechanically from the dotted key (`anti_flap.min_confirm_spacing` →
`ANTI_FLAP_MIN_CONFIRM_SPACING`) and are never spelled by hand. Globals:
`DATABASE_URL` (required), `API_LISTEN`, `GEOIP_PATH`, `DATASETS_DIR`,
`PUBLIC_BASE_URL`, `LOG_LEVEL`. Tuning keys cover claiming, cadence,
anti-flap spacing, consensus QPS/breakers, lifecycle streaks, Tranco guards,
live-check budgets, and ops URLs — every key is logged at startup with secrets
redacted.

## Codegen & CI gates

`make generate` regenerates sqlc, the Go server bindings (oapi-codegen →
`internal/api/gen/api.gen.go`), and the frontend types (openapi-typescript →
`openapi/schema.ts`), then **fails if the tree changed** — the drift gate CI runs on
every PR, alongside `make lint`, `make spec-lint` (Spectral on `openapi.yaml`),
`make test`, `make test-integration`, `make vulncheck`, `make build-linux`, and the
frontend gates `make frontend-test` / `frontend-lint` / `frontend-build`
(see `.github/workflows/backend.yml` and `frontend.yml`).

## Conventions to keep

- **Speak `domain` types everywhere** — no parallel enums; `checker.Kind` is a type
  alias of `domain.Kind`.
- **The `Detail` contract** — a check's extra payload is a typed struct behind the
  `checker.Detail` interface, never a raw map; stored `scan_detail` JSON rehydrates
  to the same typed structs via `ScanResult.UnmarshalJSON`, and consumers use the
  typed accessors (`AAAABase()`, `NS()`, …).
- **Pure compute, fenced I/O** — state math belongs in pure functions
  (`ComputeCommit`, `Classify`, `schedule`); the I/O edge is thin and lease-fenced.
- **Ports are declared by their consumer** — there is no central `repository` or
  `service` layer (05-schema.md §10.3 records both as deleted); each consumer package
  declares the narrow interface it needs (`crawler.Scanner`/`PreflightState`/
  `CommitSink`/`Enricher`, `checker.AAAAResolver`, the per-package `ConfigSource`s),
  and `postgres` supplies the concrete adapter.
- **New singleton jobs need an advisory-lock key** in `internal/lock` — both crawler
  processes run every schedule.
- **Cite the spec** — new exported types/functions carry a comment pointing at the
  governing spec section, like the rest of the codebase.

## Frontend (`frontend/src/`)

Vue 3.5 `<script setup>` + TypeScript, Vite, Tailwind v4, vue-router 5. No Pinia —
state lives in composables.

- `api/` — `client.ts` is a small vendored typed-fetch `get()` (typed against the
  generated `openapi/schema.ts` via the `@openapi` alias); `index.ts` holds the
  narrow per-resource helpers pages import (and tests stub); `problem.ts` turns
  every non-2xx into a typed RFC 9457 `ApiProblem`.
- `pages/` — 14 lazy-loaded route views (`router.ts`); tier pages (`/heroes`,
  `/sinners`, `/saints`) are redirects into `/domains?filter=…`, all driven by the
  single `tiers.ts` table. Legacy singular paths 301 in nginx with a client-side
  redirect backstop.
- `components/` + `partials/` — reusable pieces (tables, status card, segmented
  tabs, breadcrumbs) vs. layout sections (header/footer/home sections).
- `composables/` — `useCursorList` (generic keyset-pagination list state),
  `useDomainDetail`, `usePageMeta` (router-driven titles), `useVisitorIp`.
- `utils/` — status icon/label/tooltip mapping, changelog wording, date formatting.
- Tests are Vitest; DOM tests opt into jsdom per-file with
  `// @vitest-environment jsdom`. `make frontend-test` / `frontend-lint` /
  `frontend-build` are the gates, and CI runs all three.

## Where to start reading

1. `internal/domain/types.go` — the vocabulary everything else speaks.
2. `internal/crawler/commit.go` — `ComputeCommit`, the heart of the confirm model.
3. `internal/api/router.go` — the public surface, one handler file per resource.
4. `openapi/openapi.yaml` — the contract both sides generate from.
5. `frontend/src/tiers.ts` + `frontend/src/api/index.ts` — how the frontend hangs
   off the same contract.
