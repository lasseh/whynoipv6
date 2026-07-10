# 11 — Implementation Plan (the build task graph)

_Status: Round 3.0 — API redesign folded in (docs/api-design-research.md, decisions 2026-07-09): clean root API, keyset pagination, RFC 9457, no legacy compat, no history import._

**Purpose:** This file is the executable task graph that drives the autonomous build of the
WhyNoIPv6 backend from spec files 01–10. It decomposes design §8's seven phases into
individually-shippable tasks, each with a stable ID, governing spec sections, dependencies,
exact deliverable paths, and mechanically-checkable acceptance criteria. It also fixes the
phase gates, the workflow-runner contract the driving loop must obey, the whole-build
definition of done, the parallelization map, and the risk register (the early validation
experiments that de-risk the frontier and resolver designs before the crawler is built).

**Deliverables:** This file governs **no Go packages** — it is the index and control-flow over
the other spec files. Its "deliverables" are the task graph, the phase-gate definitions, the
workflow-runner contract, the definition of done, and the requirement-coverage matrix
(Appendix A). Every Go package/file/artifact named in a task below is *owned* (its normative
content defined) by the spec file cited in that task's **Governs** line; this file only
sequences their construction and states when each is *done*.

**Companion files:** an implementer executing this plan must have open, per task, the spec
files named in that task's **Governs** line. The four files every task ultimately depends on:
05-schema.md (all DDL), 09-ops.md (config-key registry, Makefile/CI, deploy), 10-testing.md
(every fixture and test-name this file's acceptance criteria cite), 00-overview.md (canonical
sizing constants cited by name, e.g. `WORKER_SLOTS`).

---

## 1. How to read this plan (task schema + conventions)

Every task below is written to a fixed schema so the workflow runner can parse it:

- **ID** — stable, phase-prefixed (`P0.1`, `P2.5`, gate tasks `P2.G3`). IDs never change or get
  reused; a dropped task's ID is retired, not recycled.
- **Title** — imperative one-liner.
- **Governs** — the spec file(s) and named sections whose normative content this task
  implements, in the form `see NN-file.md — <section name>`. This is where the implementer
  reads *what* to build; this plan only says *when* and *done-when*.
- **Depends** — task IDs that must be **done** (all acceptance criteria green, committed) before
  this task starts. A task with `Depends: —` is a roots task.
- **Deliverables** — exact monorepo paths (design §6 layout; `backend/` root elided — all Go
  paths are relative to the backend module `github.com/lasseh/whynoipv6`). A path already
  listed by an earlier task means "extend the existing file", never "recreate".
- **Acceptance** — the mechanically-checkable gate. Every criterion is a command whose exit
  status or output is the pass/fail signal: `make <target>`, `go build ./...`, a named test
  function passing, a `docker compose` health state, a SQL assertion (`psql -c … ` returning a
  pinned value), an `EXPLAIN` plan predicate, a grep gate, or a byte-equality diff. Test
  *names* cited here are the functions delivered by 10-testing.md; the *fixture tables* they
  run live only in 10-testing.md (this file never restates a fixture).

**Conventions honoured throughout (single-source rules):**

- No DDL appears in this file. Tables/columns are referenced by name; their `CREATE`/`ALTER`
  lives only in 05-schema.md.
- Config keys are cited by name; their type/default/env-var live only in the consolidated
  registry in 09-ops.md — §2. A task that "introduces" keys means its owning spec file
  registered them; this plan only requires the key be wired.
- Sizing constants (`WORKER_SLOTS`, `SCAN_RATE`, resolver-load rows, etc.) are cited by name;
  their values live only in 00-overview.md. Engine compile-time constants (e.g.
  `PreflightFreshness` = 5m) are not sizing constants — they are cited from their owning spec
  section (01-engine.md — §13), never looked up in 00-overview.
- Fixture/vector tables and golden files live only in 10-testing.md.

**Commit granularity.** One task = one commit (or one tight commit series that ends green),
conventional-commit message, scope = the task's package. Gate tasks that only *verify* (run a
diff, an EXPLAIN, a chaos script) commit their recorded evidence (a checked-in report file
under `docs/gates/` or a test fixture) so the gate result is auditable, never silently passed.

---

## 2. Workflow-runner contract

The driving workflow executes this plan as follows. These rules are normative for the runner,
not the implementer:

1. **One task per agent invocation.** Spawn a fresh agent per task ID with that task's
   **Governs** spec files and this task block as its brief. Do not batch two task IDs into one
   agent unless Appendix B lists them as a fused pair.
2. **Never reorder across a dependency edge.** A task may start only when every ID in its
   **Depends** line is marked done. Independent tasks (same phase, disjoint dependency
   closure — see the §4 parallelization map) may run concurrently.
3. **Run acceptance before marking done.** After the agent reports completion, the runner
   itself executes the task's **Acceptance** commands. Marked done only if every one exits
   green. An agent's self-report is never sufficient.
4. **Commit per completed task.** On green acceptance, commit the working tree with a
   conventional-commit message (`<type>(<scope>): <task title>`, body citing the task ID and
   the governing spec sections, the standard `Claude-Session:` trailer). Never leave a done
   task uncommitted; never commit a task whose acceptance is red.
5. **Phase gates are hard barriers.** A phase's gate task(s) (`PN.G*`) depend on every non-gate
   task in phase N. No task in phase N+1 starts until phase N's gates are all green — except
   where §4 explicitly lists a cross-phase task as gate-independent (e.g. the phase-1 resolver
   spike, which only needs the compose environment).
6. **Stop-and-report on repeated gate failure.** If a task's or a gate's acceptance fails,
   the runner may re-dispatch it **once** (fresh agent, same brief plus the failure output). If
   it fails a **second** time, stop the phase, do not proceed, and report: the task ID, both
   failure transcripts, and the governing spec section — for human triage. Never "work around"
   a red gate by editing the acceptance criteria or the spec.
7. **Idempotent re-entry.** Every task's deliverables are constructed so re-running the task
   agent on a partially-built tree converges (migrations are `IF NOT EXISTS`/versioned, imports
   are `ON CONFLICT`, codegen is regenerated). A re-dispatched agent must not duplicate rows or
   files.
8. **Never mutate production.** No task reads or writes the live production database or DNS.
   There is **no data import** (start-fresh, OPEN-9): the cutover is a pure DNS flip — a fresh
   DB is migrated, the crawler builds confirmed state from scratch, the OpenAPI contract tests
   go green, then DNS/upstream is switched (see 08-migration-cutover.md — DNS-flip cutover).
   The only non-crawl-derived cutover step is the `top_shame` re-seed (P4.11). Cutover is the
   single traffic switch at P4.G.

---

## 3. Global gates and the definition of done

**Every-commit gates (enforced by the runner on all tasks).** `make lint` and `make test`
(the offline unit tier, `go test -race ./...`) must be green on every commit — the global
workflow rule. Tasks that add generated code additionally require `make generate` to leave a
clean tree (the CI staleness gate, see 09-ops.md — Makefile, `.golangci.yml`, CI).

**Definition of done for the whole build.** The build is complete when **all** of the
following hold:

- Every task P0.1 … P7.4 is marked done and committed.
- Every phase gate P0.G … P7.G is green.
- `make lint`, `make test`, `make test-integration`, `make vulncheck`, and `make build-linux`
  all pass on the default branch (CI green — see 09-ops.md — Makefile, `.golangci.yml`, CI).
- The **OpenAPI contract gate** is green: `make generate` (oapi-codegen `chi-server` +
  `strict-server`, openapi-typescript, sqlc) leaves a clean tree and the Spectral/vacuum spec
  lint passes (see 07-api.md — OpenAPI-first workflow; 08-migration-cutover.md — DNS-flip
  cutover). There is **no** legacy/parity gate: the frozen-frontend byte-parity workstream is
  dropped (start-fresh, DNS-flip cutover — the rebuilt frontend is co-designed against
  `openapi.yaml`, not driven against a frozen contract).
- The config-key registry is complete: every key present in any spec file is registered and
  appears in the startup summary (09-ops.md — Acceptance, item 1), verified by task **P7.4**.
- The requirement-coverage matrix (Appendix A) has no `UNREACHED` row.

---

## 4. Parallelization map

Tasks that share a phase but have disjoint dependency closures may run concurrently. The
independent fan-outs the runner should exploit:

- **P0:** P0.1 (module skeleton) must land first; then P0.3 (Makefile), P0.4 (compose),
  P0.5 (CI), P0.6 (config loader) are mutually independent and parallel. (P0.2 — the frozen
  frontend subtree import — is **retired**; the frontend is rebuilt and co-designed against
  `openapi.yaml`, so nothing in this repo depends on it.)
- **P1:** P1.1→P1.2→P1.3→P1.4 (migrations) is a chain; P1.5 (sqlc) depends on P1.1; P1.6
  (Canonicalize) is independent and can start at phase open; P1.7/P1.8/P1.9 (Tranco/campaign/
  geoip) are mutually independent once P1.4+P1.5+P1.6 are done; P1.13 (ns_host→provider mapping)
  and P1.14 (hosting/CDN provider tag) depend on P1.4+P1.5 and run parallel to the ingest
  fan-out; P1.10 (integration harness) depends only on P1.4; **P1.11 (claim-plan spike)**
  depends on P1.4 (schema loadable) and **P1.12 (resolver-latency spike)** depends only on P0.4
  (compose+Unbound) — both are gate-independent early experiments and should run as soon as
  their single dependency lands.
- **P2:** P2.1 (checker lift) and P2.4 (classify, pure) are independent roots. P2.2 (consensus)
  depends on P2.1's seam. P2.3 (mapper) depends on P2.1+P2.2. P2.6 (frontier) and P2.10 (lock)
  depend only on schema and run parallel to the engine track. P2.5 (commit) is the join point
  (needs P2.3+P2.4+P2.6). P2.7/P2.8/P2.9/P2.11/P2.12/P2.13 follow the commit/frontier core.
- **P3:** P3.1 (Unbound), P3.2 (Grafana), P3.3 (backups), P3.4 (deploy units/nginx) are
  independent deploy-artifact tasks; P3.5 (rate smoothing) and P3.6 (the 1M run) are sequential
  and gated on P3.1–P3.4.
- **P4:** OpenAPI-first, so the contract is authored early. P4.1 (baseline) → P4.5 (author
  `openapi.yaml` + codegen + drift gate) → P4.13 (keyset/cursor engine) are the spine; the
  endpoint tasks (P4.3 core resources + tier collections, P4.14 changelog/timeline, P4.15
  feeds/CSV, P4.16 /mandates + tags, P4.4 /ip) all implement against the generated strict
  interfaces and can fan out once P4.5+P4.13 land. P4.11 (v6ctl shame + top_shame re-seed) is
  independent of the API track. There is **no migration track** — the P4.6–P4.10, P4.12
  dump-import tasks are retired (start-fresh, no import).
- **P5:** P5.1 (registry/sweep) and P5.4 (endpoints) are independent; P5.2 depends on P5.1;
  P5.3 (enable+gold) depends on P5.1 + the classify ladder from P2.4.
- **P6:** P6.1 (live check), P6.2 (stats), P6.3 (datasets), P6.4 (badge) are mutually
  independent.
- **P7:** P7.1 (validate Action), P7.2 (webhook), P7.3 (runbooks), P7.4 (registry gate) are
  mutually independent.

---

## 5. Risk register (and the early validation experiments)

Design §9's decision log names four load-bearing risks. Each is mitigated by named tasks; the
two experiments the design calls "phase-0/1 validation" are scheduled as the earliest
gate-independent tasks so a design flaw surfaces before the crawler is built.

| Risk (design §9) | Mitigating task(s) | Early experiment |
|---|---|---|
| Frontier claim query degrades at 1M rows / on backlog churn | **P1.11** (claim-plan EXPLAIN spike), re-run at **P2.G4** and **P3.G4** with real churn + `pgstattuple` bloat check | **P1.11** — runs the moment P1.4 lands |
| Public-resolver load / consensus latency infeasible | **P1.12** (resolver-latency spike), verified at scale in **P3.5** | **P1.12** — runs the moment P0.4 (compose Unbound) lands |
| Confirmed-status machine is novel code | Heaviest unit coverage in the repo: **P2.4/P2.5** + the **P2.G1** commit-machine table gate | — |
| `miekg/dns` v2 is newer code in a load-bearing spot | **P2.1** pins it; **P2.G1/P2.G2** + the P3 soak are the check; v1-API revert is the mechanical escape hatch (design OPEN-9) | — |
| Cold classification start (no import): every domain sits at `unknown` until N crawl cycles confirm each dimension, so day-1 hero/adoption counts read low | Accepted consciously (start-fresh, OPEN-9); flagged so the day-1 dashboard's low hero count is expected, not a bug (08-migration-cutover.md — DNS-flip cutover; verified at **P4.G**) | — |

**P1.11 and P1.12 are risk gates, not optional spikes:** if P1.11 cannot show an index scan on
`idx_domain_due` under `50 ms` at 1M rows, or P1.12 measures per-provider latency that makes
the `~24 qps/provider` consensus budget infeasible, the runner stops and reports (contract
rule 6) — the frontier/consensus design must be revisited before Phase 2, exactly as the
design intends.

---

## Phase 0 — Repo scaffolding

**Goal:** a building monorepo with the compose dev-environment green. Ships nothing user-facing.

### P0.1 — Monorepo module skeleton
- **Governs:** design §6 (Package & binary layout); see 00-overview.md — monorepo layout.
- **Depends:** —
- **Deliverables:** `backend/go.mod` (module `github.com/lasseh/whynoipv6`, current Go
  toolchain, no version pins carried forward), `backend/go.sum`; empty-but-compiling
  `cmd/api/main.go`, `cmd/crawler/main.go`, `cmd/v6ctl/main.go`; empty package dirs with a
  `doc.go` each for `internal/{domain,checker,consensus,crawler,ingest,campaign,repository,postgres,service,api,geoip,notify,config,lock}`;
  `db/migrations/`, `db/query/` dirs.
- **Acceptance:** `go build ./...` succeeds; `go vet ./...` clean; directory tree matches
  design §6 (a `find backend -type d` diff against the layout has no missing package dir).

### P0.2 — RETIRED (was: import frozen frontend as a subtree)
- **Retired in Round 3.0.** The frozen-frontend compatibility constraint is dropped: the
  frontend is **rebuilt and co-designed against `openapi.yaml`** (07-api.md — OpenAPI-first
  workflow), not imported frozen and kept byte-compatible. No task depends on a frozen
  `frontend/` subtree, and the openapi-typescript bindings (P4.5) are the frontend's typed
  data layer. The ID is retired, not recycled.

### P0.3 — Makefile + `.golangci.yml`
- **Governs:** see 09-ops.md — Makefile, `.golangci.yml`, CI (§14.1, §14.2).
- **Depends:** P0.1
- **Deliverables:** `Makefile` (targets `build build-linux test lint tidy vulncheck generate
  coverage compose-up compose-down clean help`, verbatim per 09-ops §14.1, three-binary
  `build`); `.golangci.yml` (house config verbatim per 09-ops §14.2).
- **Acceptance:** `make help` lists every target; `make build` produces `bin/api bin/crawler
  bin/v6ctl`; `make lint` runs golangci-lint and exits green on the skeleton; `make tidy`
  leaves `go.mod`/`go.sum` unchanged.

### P0.4 — Dockerfile + compose dev environment
- **Governs:** see 09-ops.md — docker-compose dev environment (§9); Dockerfile (§14.3).
- **Depends:** P0.1
- **Deliverables:** `Dockerfile` (multi-stage, builds all three binaries, `golang:alpine` →
  distroless nonroot, verbatim per 09-ops §14.3); `compose.yaml` (db =
  `timescale/timescaledb:latest-pg18`, unbound ×2, api, crawler — rolling tags, no pins).
- **Acceptance:** `make compose-up` brings `db`, `unbound1`, `unbound2`, `api`, `crawler` to
  healthy (`docker compose ps --format json` shows all `healthy`/`running`); `docker compose
  exec db pg_isready` succeeds; `make compose-down` removes volumes cleanly.

### P0.5 — CI pipeline
- **Governs:** see 09-ops.md — Makefile, `.golangci.yml`, CI (§14.4).
- **Depends:** P0.3
- **Deliverables:** `.github/workflows/ci.yml` running, in order, `make tidy` → `make lint` →
  `make generate` (staleness gate — self-gating: sqlc always, the OpenAPI codegen steps
  activate once `openapi/openapi.yaml` lands in P4.5; 09-ops §14.1) → `make test` →
  `make test-integration` → `make vulncheck` → `make build-linux`, on
  every PR and on the default branch.
- **Acceptance:** the workflow file parses (`actionlint` clean if available, else YAML lint);
  a dry push of the skeleton passes every stage; any stale generated output / untidy go.mod /
  lint finding / test failure fails the pipeline (verified by a deliberate injected failure in
  a scratch branch, reverted).

### P0.6 — Config loader + slog install
- **Governs:** see 09-ops.md — Configuration model (§1), Logging conventions (§13).
- **Depends:** P0.1
- **Deliverables:** `internal/config/` (viper two-tier loader: `/etc/whynoipv6/config.yaml`
  optional + env override, `DATABASE_URL` required, secret-redacting startup summary); the
  `slog` handler installation + startup config summary in each `cmd/{api,crawler,v6ctl}/main.go`.
- **Acceptance:** unit test `TestConfigDefaults` (config loads with no YAML present, env
  overrides applied); `TestConfigRedaction` (startup summary logs `DATABASE_URL` host+db only,
  webhook/ping URLs as `set`/`unset` — grepping the log for a password or full webhook URL
  returns nothing — 09-ops §15.3); each binary exits non-zero with a clear message when
  `DATABASE_URL` is empty (09-ops §15.4). Registry-completeness is deferred to P7.4 (keys are
  added by later tasks).

### P0.G — Phase-0 gate
- **Governs:** design §8 Phase 0 verify.
- **Depends:** P0.1, P0.3, P0.4, P0.5, P0.6
- **Acceptance:** `make build` green (three binaries) **and** `make compose-up` green (all
  services healthy) **and** `make lint` + `make test` green. Record the `docker compose ps`
  output to `docs/gates/P0.txt`.

---

## Phase 1 — Schema + ingestion

**Goal:** a populated database (1M Tranco + ~30k campaign entities) with correct
kinds/parents/ranks, idempotent re-import, and the integration harness live. Includes the two
early risk experiments.

### P1.1 — Migration 000001 (base schema)
- **Governs:** see 05-schema.md — Migration 000001 — base schema (§3); Conventions and
  invariants (§1).
- **Depends:** P0.1
- **Deliverables:** `db/migrations/000001_base_schema.up.sql`, `…down.sql` (extensions, all
  enums, all tables, indexes incl. `idx_domain_due`, storage parameters, CHECK constraints).
  Reflects the Round-3.0 schema: **no** `changelog.legacy_message`/`legacy_status` columns and
  **no** `changelog_legacy_chk`/`changelog_old_value_chk`/`changelog_new_value_chk` CHECKs
  (`old_value`/`new_value` are plain `NOT NULL`); **no** `'legacy'` value in the
  `changelog.field` domain; **no** `created_by = 'import'` enum value. Includes the new
  pivots: `domain.tld`, the DNS-provider reference + `ns_host → provider` mapping table, the
  hosting/CDN provider tag, `campaign.tags` (TEXT[] + GIN, 05-schema.md), and the stats-rollup
  `generated_at TIMESTAMPTZ` (see 05-schema.md — drop changelog legacy columns; add pivots +
  tags).
- **Acceptance:** migration applies on a fresh `timescale/timescaledb:latest-pg18` container;
  `\d domain` / `\d changelog` etc. show every column in 05-schema §12's cross-check inventory
  (incl. `domain.tld` and the provider tag columns, and **without** the dropped `legacy_*`
  columns); `changelog.old_value`/`new_value` are `NOT NULL` and a NULL insert is rejected;
  the `ns_host → provider` mapping table and `campaign` tagging surface exist; a bad-enum
  insert on `changelog.field` (`'legacy'`) and on `created_by` (`'import'`) is rejected
  (values removed); `.down.sql` drops cleanly.

### P1.2 — Migration 000002 (TimescaleDB)
- **Governs:** see 05-schema.md — Migration 000002 — hypertables, columnstore, retention,
  continuous aggregate (§4).
- **Depends:** P1.1
- **Deliverables:** `db/migrations/000002_timescaledb.up.sql`, `…down.sql` (6 hypertable
  conversions, 5 columnstore + 4 retention policies, the `scan_daily_adoption` continuous
  aggregate + refresh policy).
- **Acceptance:** `timescaledb_information.hypertables` lists exactly 6 hypertables (the cagg
  materialization hypertable excluded); the `proc_name`-filtered policy query shows
  5 columnstore + 4 retention + 1 cagg-refresh = 10 policies (excluding the built-in telemetry
  job); `timescaledb_information.continuous_aggregates` lists `scan_daily_adoption`
  (05-schema §13.2).

### P1.3 — Migration 000003 (seed data)
- **Governs:** see 05-schema.md — Migration 000003 — seed data (§5).
- **Depends:** P1.2
- **Deliverables:** `db/migrations/000003_seed.up.sql`, `…down.sql` (sentinel ASN `number=0`,
  251-row country reference incl. `code='UN'`, the single `stats_global_daily` day-0 row). Does
  NOT populate `top_shame` (FK requires phase-1 ingestion — design §6 note).
- **Acceptance:** `SELECT count(*) FROM country` = 251; `SELECT count(*) FROM asn WHERE
  number=0` = 1; `SELECT count(*) FROM country WHERE code='UN'` = 1; `SELECT count(*) FROM
  stats_global_daily` = 1 (05-schema §13.3).

### P1.4 — Migration embedding + `v6ctl migrate`
- **Governs:** see 05-schema.md — Migration framework (§2); design §6 (`v6ctl migrate`).
- **Depends:** P1.3
- **Deliverables:** `db/migrations/migrations.go` (`go:embed` of the `.sql` files);
  `cmd/v6ctl/migrate.go` (cobra `migrate up|version` with golang-migrate over the embedded FS).
- **Acceptance:** `v6ctl migrate up` applies 000001→000003 green on a fresh container; re-run
  is a no-op; `v6ctl migrate version` reports `3` and not dirty (05-schema §13.1).

### P1.5 — sqlc config + data-access skeleton
- **Governs:** see 05-schema.md — sqlc configuration and data-access layout (§10).
- **Depends:** P1.1
- **Deliverables:** `sqlc.yaml` (sqlc v2, pgx/v5); `db/query/` file layout (empty query files
  per 05-schema §10, contents owned by later tasks); `internal/postgres/db/` (generated,
  never hand-edited); `internal/repository/` (port-interface package, contents defined by
  consumers); `internal/postgres/` (hand-written adapter skeleton).
- **Acceptance:** `sqlc generate` runs clean against the three up-files and the `db/query/`
  set; generated code compiles (`go build ./internal/postgres/...`) — 05-schema §13.4;
  `make generate` leaves a clean tree.

### P1.6 — `Canonicalize(host)`
- **Governs:** see 06-ingest.md — Canonicalize(host) — the single canonicalization rule (§1).
- **Depends:** P0.1
- **Deliverables:** `internal/domain/host.go` (the one `Canonicalize` function — lowercasing,
  IDN→punycode, scheme/port/path rejection, eTLD+1 derivation) plus the sibling **eTLD-suffix
  (`tld`) extractor** (publicsuffix, e.g. `com`/`no`/`gov`) that feeds the `domain.tld` pivot
  written at ingest (see 05-schema.md — add pivots + tags; 06-ingest.md — Canonicalize).
- **Acceptance:** `TestCanonicalize` passes the full §2 vector table (see 10-testing.md —
  Canonicalize(host) vectors), including the `tld` extraction cases (multi-label suffixes like
  `co.uk` map to the correct registry suffix); lint grep gate: no `strings.ToLower` on
  hostnames anywhere outside `internal/domain/host.go` (06-ingest §9.1, enforced by lint per
  10-testing §11).

### P1.7 — Tranco ingester + `v6ctl tranco`
- **Governs:** see 06-ingest.md — Tranco import (§2); see 05-schema.md — Ephemeral DDL — the
  Tranco staging table (§7).
- **Depends:** P1.4, P1.5, P1.6
- **Deliverables:** `internal/ingest/` (fetcher, parser, staging upserter, sanity guard, retry
  cycle); `cmd/v6ctl/` verbs `tranco import`, `tranco status`.
- **Acceptance:** `TestTrancoImport` integration case (behind `integration` tag) on a fixture
  CSV with CRLF, `_wildcard_.ph`, mixed-case duplicates, and an IDN line yields correct
  `line_count/rejected_count/duplicate_count/imported_count/delisted`, lowest-rank-wins fold,
  24h-spread `next_check_at`, and a populated `domain.tld` on every inserted apex (06-ingest
  §9.2); re-import of the same list ID is a no-op and
  `--force` re-imports (§9.3); a `>2%` delist fixture aborts with `aborted=true` and leaves
  ranks unchanged, `--force` applies (§9.4); the re-entry cases of §9.5 hold. Fixtures:
  10-testing.md.

### P1.8 — Campaign sync + `v6ctl campaign sync`
- **Governs:** see 06-ingest.md — Campaign repo sync (§3); design §7.2 (UUID trust rule); see
  04-lifecycle-scheduling.md — Delist lifecycle & re-entry semantics (membership re-entry).
- **Depends:** P1.4, P1.5, P1.6
- **Deliverables:** `internal/campaign/` (YAML parse tolerating the format variance, idempotent
  `Sync`, uuid write-back plumbing, **`tags`/`mandate` parsing into `campaign.tags`** —
  OPEN-12, see 05-schema.md — Campaign mandate tagging; 06-ingest.md — Campaign repo sync);
  `cmd/v6ctl/` verb `campaign sync` (`--adopt-unknown-uuids`).
- **Acceptance:** `TestCampaignSync` integration case covers new-file-without-uuid (insert +
  write-back), rename (source_file update), deletion (soft-disable via uuid-set diff),
  re-appearance (re-enable, no membership churn), duplicate uuid across files (source_file
  match wins), unknown uuid rejected without the flag, subdomain entry (parent auto-created,
  `created_by='parent_link'`, `parent_id` set), the membership re-entry rule (06-ingest §9.6),
  **and `tags` from a tagged campaign YAML landing in `campaign.tags` (empty/NULL when
  untagged, updated idempotently on re-sync)**; a full run over the 28 real campaign YAMLs with
  `--adopt-unknown-uuids` imports ~30k entities with correct parents. Fixtures: 06-ingest.md §9
  (ingest fixtures; 00 §8.2 exception).

### P1.9 — GeoIP / ASN attribution
- **Governs:** see 06-ingest.md — GeoIP / ASN attribution (§6); design OPEN-5 (ccTLD
  precedence, GeoIP fallback).
- **Depends:** P1.4, P1.5, P1.6
- **Deliverables:** `internal/geoip/` (MaxMind mmdb readers, attribution algorithm, hot
  reload).
- **Acceptance:** `TestAttribution` covers ccTLD-beats-GeoIP, deferred scans leaving
  `asn_id/country_id` untouched, insert-time attribution = ccTLD-or-sentinel country + sentinel
  ASN, and sentinel ids resolved by lookup not literals (06-ingest §9.10). Fixtures:
  06-ingest.md §9 (ingest fixtures; 00 §8.2 exception).

### P1.13 — DNS-provider mapping (`ns_host → provider`) + attribution
- **Governs:** see 06-ingest.md — DNS-provider mapping; see 05-schema.md — add pivots + tags
  (`ns_host → provider` mapping table + `domain.dns_provider_id`); design OPEN-4 (RESOLVED
  YES), §5.6 (DNS-provider league table), §5.3 (additional per-domain attributes).
- **Depends:** P1.4, P1.5
- **Deliverables:** `internal/ingest/provider.go` (the seeded `ns_host → provider` mapping +
  `ProviderForNSHost` lookup, longest-suffix match) and the read-only **attribution writer**
  that stamps `domain.dns_provider_id` from a domain's observed NS hosts. This is an
  attribution step (like GeoIP, P1.9); it reads NS observations read-only and **never** touches
  the commit/trust machine. Also the operator verb group **`v6ctl provider add|remove|list`**
  and the optional `dns_provider.seed_path` YAML loader (06-ingest §6.11) — the table's single
  write path; without it the mapping is empty and every domain attributes to NULL.
- **Acceptance:** `TestProviderMapping` covers longest-suffix precedence, unknown-NS →
  NULL/sentinel provider, and multi-NS agreement/disagreement handling per 06-ingest; the
  stamping writer sets `domain.dns_provider_id` without writing any `scan`/`changelog`/
  `*_status` column (grep/read-back assertion); `provider add` then `provider list` round-trips
  a suffix set and `provider remove` leaves stamped domains self-healing per 06 §6.10.
  Fixtures: 06-ingest.md §9 (ingest fixtures; 00 §8.2 exception).

### P1.14 — Hosting/CDN provider tag
- **Governs:** see 06-ingest.md — hosting/CDN provider tag; see 05-schema.md — add pivots +
  tags (hosting/CDN provider column); design §5.3 (additional per-domain attributes), §5.6.
- **Depends:** P1.4, P1.5, P1.9
- **Deliverables:** `internal/ingest/hosting.go` (normalize a hosting/CDN provider tag from the
  checker's CNAME-chain CDN detection + the resolved IP's ASN — data already collected) and the
  read-only writer that stamps the `domain` hosting/CDN provider column. Attribution-only; does
  not touch the commit/trust machine.
- **Acceptance:** `TestHostingTag` derives the correct normalized provider for a CNAME-CDN
  fixture and for an ASN-only fixture, `NULL`/unknown when neither resolves, and writes no
  confirmed-status column (read-back assertion). Fixtures: 06-ingest.md §9 (ingest fixtures;
  00 §8.2 exception).

### P1.10 — Integration harness (testcontainers)
- **Governs:** see 10-testing.md — Integration harness (testcontainers + TimescaleDB) (§9);
  see 09-ops.md — Makefile (`make test-integration`).
- **Depends:** P1.4
- **Deliverables:** `internal/postgres/testmain_test.go` (or equivalent shared harness) — a
  `timescale/timescaledb:latest-pg18` testcontainer that runs migrations then yields a pool;
  the `integration` build tag; the `make test-integration` target already exists (09-ops §14.1)
  and is exercised here.
- **Acceptance:** `make test-integration` boots the container, applies 000001→000003, and a
  smoke test (`TestHarnessBoot`) runs one `SELECT` against the migrated schema green.

### P1.11 — Claim-plan EXPLAIN spike (risk gate)
- **Governs:** see 04-lifecycle-scheduling.md — The claim query (§3), Acceptance (§17.1); see
  05-schema.md — Acceptance (§13.5); design §8 Phase 2(e) / §9 risk.
- **Depends:** P1.4, P1.10
- **Deliverables:** `internal/postgres/claimplan_integration_test.go` (`TestClaimPlanGate`) +
  a recorded plan report `docs/gates/claim-plan-P1.txt`. Seeds a 1M-row `domain` table (rank
  distribution per 00-overview constants) with a **preliminary** claim SELECT matching the
  04-lifecycle claim query.
- **Acceptance:** `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` of the claim inner SELECT with a
  near-empty due backlog (<1k due) shows an Index Scan on `idx_domain_due` with `next_check_at
  <= now()` as index condition, a top-N sort on `(rank NULLS LAST, next_check_at)`, buffers/rows
  proportional to the due set (not the table), execution `< 50 ms`; the empty-frontier case
  is `< 5 ms`; the full-backlog case (all 1M due) is exercised and its `O(due)` cost recorded.
  **Stop-and-report if the plan is a seq scan or `> 50 ms`.**

### P1.12 — Resolver-latency spike (risk gate)
- **Governs:** design §2.4 (DNS resolver-load split), §2.7 (throughput math), §9 risk; see
  02-observation-model.md — The consensus resolver (target budget).
- **Depends:** P0.4
- **Deliverables:** a throwaway harness `internal/consensus/latencyspike/main.go` (build tag
  `spike`, not shipped) + a recorded measurement report `docs/gates/resolver-latency-P1.md`.
  Fires AAAA lookups at the 3 public consensus providers and at the compose Unbound instances,
  measuring per-provider latency and sustainable qps.
- **Acceptance:** the report records measured per-provider p50/p95 latency and confirms the
  `~24 qps/provider` consensus budget and the bulk-via-Unbound path are latency-feasible for a
  <24h 1M pass (design §2.7 math). **Stop-and-report if measured latency makes the budget
  infeasible** (the design's v1-revert / 4th-resolver escape hatches would then apply).

### P1.G — Phase-1 gate
- **Governs:** design §8 Phase 1 verify.
- **Depends:** P1.1, P1.2, P1.3, P1.4, P1.5, P1.6, P1.7, P1.8, P1.9, P1.10, P1.11, P1.12,
  P1.13, P1.14
- **Acceptance:** after a full Tranco import + campaign sync on the integration DB:
  `SELECT count(*) FROM domain WHERE kind='apex' AND rank IS NOT NULL` ≈ 1,000,000;
  `SELECT count(*) FROM domain WHERE tld IS NOT NULL` ≈ the same (every ranked apex has a
  derived `tld`); `SELECT count(*) FROM campaign_domain` ≈ the sum across the 28 YAMLs; a
  tagged campaign YAML lands non-NULL `campaign.tags`; spot-check that
  subdomain entities have non-NULL `parent_id` and `created_by IN ('parent_link','campaign')`;
  re-running both imports changes zero rows (idempotency); junk-rejection counts are non-zero
  and logged; `make test-integration` green; P1.11 and P1.12 reports checked in and green.
  Record summary to `docs/gates/P1.txt`.

---

## Phase 2 — Crawler core (the heart)

**Goal:** the full scan→observe→commit pipeline: the lifted engine, consensus quorum, the
observation mapper, the confirmed-status commit machine, the frontier claim/commit loop, the
daily tick, preflight, metrics, and heartbeats. Gated on the heaviest unit coverage in the
repo plus the lease-fence chaos test.

### P2.1 — Lift the checker engine
- **Governs:** see 01-engine.md — Lift inventory (§2) through The IPv6 self-preflight (§12),
  and Acceptance (§14).
- **Depends:** P0.1
- **Deliverables:** `internal/checker/` in full: `checker.go`, `constants.go`, `resolver.go`,
  `ssrf.go`, `runner.go`, `seam.go`, `preflight.go`, and the 15 check implementation files
  (incl. `resource_discovery.go`, adapted `resource_ipv6.go`); the `codeberg.org/miekg/dns`
  go.mod pin (config keys `checks.max_ns_lookups`, `checks.max_mx_lookups`,
  `resolver.bulk_upstreams`, `preflight.probe_host` — registry: 09-ops.md).
- **Acceptance:** all nine numbered criteria of 01-engine §14 pass: `codeberg.org/miekg/dns`
  pinned in go.mod at one exact version — the newest `v0.6.*` patch at implementation time
  (the gate asserts an exact-version pin exists, not a specific patch number) — with no
  `github.com/miekg/dns` import anywhere (grep gate); the
  `Score/Grade/…/v6audit` grep gate is clean; `TestRunnerNoAAAA`, `TestRunnerSubdomain`,
  `TestCheckPanicIsolation`, `TestHTTPErrorTypes`, `TestResourceDiscovery`,
  `TestPreflightFreshness` pass. Fixtures/fake-servers: 01-engine.md §14 (engine-lift
  fixtures — the sanctioned 00 §8.2 exception; 10-testing §12 delegates by reference).

### P2.2 — Consensus resolver (quorum + breakers)
- **Governs:** see 02-observation-model.md — The consensus resolver (§2), Adapted
  `dns_aaaa_base`/`dns_aaaa_www` (§3), Acceptance (§8 items 1–5); see 01-engine.md — The
  consensus resolver seam (§9).
- **Depends:** P2.1
- **Deliverables:** `internal/consensus/` (provider fan-out, symbol reduction, quorum,
  conditional A lookup, per-provider token buckets, fast-lane breaker, provider breaker +
  canary); the `checker` seam types (`AAAAAnswer`, `QuorumInfo`, `ErrQuorumInconsistent`,
  `AAAAResolver`) consumed here. Provider addresses are package constants, not config (09-ops).
- **Acceptance:** `TestQuorumTruthTable` covers every 3-symbol permutation from {exists, empty,
  nxdomain, timeout, error} incl. all 2-1 splits, no-quorum→`ErrQuorumInconsistent`, ≤1-valid→
  plain error, and the 2-provider degraded mode; `TestNonAnswerClassification` (REFUSED/SERVFAIL
  never `empty`, REFUSED in `Rcodes`); `TestQuorumByteIdentical`; `TestConditionalALookup`;
  `TestBreakers` (fast-lane + provider breaker + canary, never drops a 2nd provider) — 02
  §8.1–8.5. Fixtures (fake-DNS): 10-testing.md.

### P2.3 — Observation mapper
- **Governs:** see 02-observation-model.md — Observation mapping tables (§4), conn composition
  (§5), resources roll-up (§6), the mapper (§7), Acceptance (§8 item 6).
- **Depends:** P2.1, P2.2, P2.4
- **Deliverables:** `internal/crawler/observe.go` (`MapObservations` incl. `conn` composition
  and `resources` roll-up); `internal/domain/observation.go` (the `Observation` enum + pure
  helper predicates).
- **Acceptance:** `TestMapObservations` covers every row of the §4 base/www/ns/mx tables, every
  row of the §5 conn table (both preflight guard downgrades), and every §6 roll-up branch
  (conn-error defer, conn-unsupported→not_applicable, dead-reference exclusion, NULL-host defer,
  empty→not_applicable, any-unsupported, all-supported, `resources.enabled=false` gate); a
  totality guard proves `partial` appears only for `ptr`/`parity` (02 §8.6). Fixtures:
  10-testing.md.

### P2.4 — Classification ladder (pure)
- **Governs:** see 03-state-machine.md — Classification: ladder, flags, gold (§10),
  Acceptance (§17.8); the `Dimension`/`IPv6Status`/`Observation`/`Classification` Go types.
- **Depends:** P0.1
- **Deliverables:** `internal/domain/classify.go` (pure ladder + flags + gold, zero deps) plus
  the enum types mirroring the DB enums.
- **Acceptance:** `TestClassify` reproduces the 03 §10 truth table over the full cross-product,
  incl. NULL conn→`partial` no-flag, `www=no_record` counts toward hero, `base=no_record`→
  `inactive` regardless of others, gold false whenever `resources` is NULL; a `switch` with no
  `default` over the closed enums makes an unmapped value fail at compile/first-run (10-testing
  §12 totality guard). Fixtures: 10-testing.md — Classification truth-table vectors.

### P2.5 — Confirmed-status commit machine
- **Governs:** see 03-state-machine.md — the whole file: Commit algorithm (§5), Step R (§6),
  Counting gate (§7), Streak maintenance (§8), Changelog write rules (§11), the one-`pgx.Batch`-
  in-one-`pgx.Tx` write unit (§12), The lease fence (§13), scan/scan_detail contents (§14),
  Acceptance (§17).
- **Depends:** P2.3, P2.4, P2.6, P1.5
- **Deliverables:** `internal/crawler/commit.go`; `db/query/commit.sql` (commit DML — sqlc,
  contents owned by 03) + generated `internal/postgres` code; resource-link persistence
  statements (transaction machinery here; discovery statements added in P5.1).
- **Acceptance:** `TestCommit*` unit table covers every transition of 03 §17.1–§17.10
  (anti-flap N(d) spacing, first-observation immediate + no-changelog, error/inconsistent
  no-ops, lease-lost writes nothing, changelog old≠new never-NULL invariant, idempotent
  re-commit, 7-scan dead + step-R recovery, gold-NULL-resources, `dependent_count` accounting);
  the integration case `TestCommitTxn` (behind `integration`) proves the single-round-trip
  batch write + fence against real DDL (10-testing §10). Fixtures: 10-testing.md — Anti-flap /
  commit-machine sequences.

### P2.6 — Frontier claim + worker pool
- **Governs:** see 04-lifecycle-scheduling.md — Frontier state & eligibility (§2), The claim
  query (§3), Worker pool & claim loop (§12), Acceptance (§17.2).
- **Depends:** P1.5, P2.1
- **Deliverables:** `internal/crawler/frontier.go` (claim query via `SKIP LOCKED` lease, claim
  loop, `WORKER_SLOTS`-bounded worker pool).
- **Acceptance:** `TestClaimAtomicity` (integration): two concurrent claimers from an
  overlapping due set never return the same row within a lease window; a row whose `claimed_at`
  is `>30 min` old is re-claimed (04 §17.2). The claim query re-satisfies the P1.11 EXPLAIN
  predicate (re-verified at P2.G4).

### P2.7 — Scheduling (`cadence` + post-commit)
- **Governs:** see 04-lifecycle-scheduling.md — cadence(rank) (§4), Post-commit scheduling
  (§5), Acceptance (§17.3).
- **Depends:** P2.5
- **Deliverables:** `internal/crawler/schedule.go` (`cadence(rank)`, post-commit scheduling
  decision: recheck pull-in, error_streak backoff, breaker-open cadence lane, slow-lane
  override for disabled rows, cadence bands incl. NULL rank).
- **Acceptance:** `TestSchedule` reproduces the §5.2 two backoff progressions exactly, the
  inconsistent-beats-error lane choice, breaker-open (cadence lane, `error_streak` still
  increments), the disabled slow-lane override, and NULL-rank band matching (04 §17.3).
  Fixtures: 10-testing.md.

### P2.8 — Dead lifecycle + delist re-entry
- **Governs:** see 04-lifecycle-scheduling.md — Dead lifecycle (§6), Delist lifecycle &
  re-entry semantics (§7), Acceptance (§17.4, §17.6); see 03-state-machine.md — Dead-signal
  computation (§4), Step R (§6).
- **Depends:** P2.5, P2.7
- **Deliverables:** dead-signal + re-enable behavior wired into `commit.go`/`schedule.go`
  (no new file; extends P2.5/P2.7); the §7 re-entry matrix behavior.
- **Acceptance:** `TestDeadLifecycle`: an NXDOMAIN-scripted domain dies on the 7th daily scan;
  an all-SERVFAIL domain dies on the 7th backoff-spaced scan; three timeouts never increment
  `dead_streak`; a NOERROR-empty apex never increments it; recovery runs step R exactly once
  with no changelog rows (04 §17.4); `TestReEntryMatrix` covers every cell of §7 at its owning
  ingress (04 §17.6).

### P2.9 — Daily sweep + tick coordinator
- **Governs:** see 04-lifecycle-scheduling.md — The daily lifecycle sweep (§8), The daily tick
  — canonical step order (§9), Acceptance (§17.5).
- **Depends:** P2.5, P2.8, P1.7, P1.8
- **Deliverables:** `internal/crawler/sweep.go` (S1–S5 lifecycle sweep = tick step 1),
  `internal/crawler/tick.go` (daily-tick coordinator: canonical step order, per-step failure
  containment, invokes Tranco import + campaign sync from P1.7/P1.8), and the
  **service-candidate detection writes** (`db/query/service_candidate.sql`, 05-schema.md —
  service_candidate; the operator triage verbs over that table are P2.14). The stats-rollup
  step stamps the `generated_at TIMESTAMPTZ` freshness signal on the daily stats row (the
  deterministic source for the API envelope `meta.as_of` — see 05-schema.md — add pivots +
  tags; 07-api.md — envelope + list-response shape).
- **Acceptance:** `TestSweep` (integration) verifies S1–S5 in isolation and as a sequence:
  monotonic grace stamping, live-check rows skipping grace, disabled campaigns not pinning
  members, S2 re-enabling a delisted member on campaign re-enable, and a same-day second run
  changing zero rows (04 §17.5).

### P2.10 — Advisory-lock singleton coordination
- **Governs:** see 04-lifecycle-scheduling.md — Singleton coordination — advisory locks (§10),
  Acceptance (§17.7).
- **Depends:** P1.5
- **Deliverables:** `internal/lock/lock.go` (`TryRun`/`Run`, lock-key registry).
- **Acceptance:** `TestLock` (integration smoke): with two pools, exactly one of two
  simultaneous `TryRun(JobDailyTick)` runs `fn`, the other returns `ErrHeld`; killing the
  winner's connection frees the lock; `Run` waits then executes (04 §17.7; 10-testing §12 lock
  smoke).

### P2.11 — Preflight wiring + shutdown + process topology
- **Governs:** see 04-lifecycle-scheduling.md — Self-preflight wiring (§11), Crawler process
  topology (§13), Graceful shutdown (§14), Acceptance (§17.8); see 01-engine.md — The IPv6
  self-preflight (§12).
- **Depends:** P2.6, P2.9, P2.10
- **Deliverables:** preflight integration in `frontier.go` (consumes `checker.Preflight`, owns
  claim-loop gating + failure/retry, does not define the type); `cmd/crawler/main.go` (goroutine
  topology, signal handling, graceful shutdown).
- **Acceptance:** `TestShutdown` (integration): SIGTERM during a full batch commits all
  in-flight domains `≤80 s`, writes an `is_final` metrics row, leaves no row with a fresh lease;
  a restarted process reclaims the expired leases (04 §17.8). Preflight gating: a stale-preflight
  claim loop pauses (01 §14.9 freshness flip).

### P2.12 — Checkpoint metrics + heartbeats
- **Governs:** see 04-lifecycle-scheduling.md — Operational metrics (§15), Acceptance (§17.9);
  see 09-ops.md — Crawl liveness, heartbeats & Grafana alert rules (§12).
- **Depends:** P2.11, P2.13
- **Deliverables:** `internal/crawler/metrics.go` (checkpointed `crawler_metrics` writer +
  latency histogram, idle-checkpoint rule).
- **Acceptance:** `TestMetrics` (integration): checkpoint rows appear every 1000 domains and
  within 5 min of idleness; `succeeded + failed = processed` on every row; a forced lease-fence
  abort shows in `failed` and `dim_counters.lease_lost` (04 §17.9); the idle-checkpoint rule
  lands a row within 5 min on an empty frontier so alert A1 does not false-fire (09-ops §15.10).

### P2.13 — Notify client (webhook + healthchecks ping)
- **Governs:** see 09-ops.md — Deliverables (`internal/notify`), Crawl liveness & heartbeats
  (§12); design §11.3 (notifications ship phases 2–3).
- **Depends:** P0.6
- **Deliverables:** `internal/notify/` (ops-webhook + healthchecks.io ping client, used by
  crawler and v6ctl).
- **Acceptance:** `TestNotify` (the notifier posts to a stub webhook and pings a stub
  healthchecks URL; URLs redacted in logs); wired into the crawler heartbeat path (consumed by
  P2.12).

### P2.14 — Operator lifecycle verbs (`v6ctl service-candidates`, `disable`, `stats recalc`)
- **Governs:** see 04-lifecycle-scheduling.md — service/manual lifecycle (glossary: 00 §6);
  see 06-ingest.md — the ingest v6ctl verbs (`disable`, §10.7 `stats recalc`); see
  05-schema.md — service_candidate.
- **Depends:** P2.9, P1.5
- **Deliverables:** `cmd/v6ctl/` verb groups: **`service-candidates list|confirm|dismiss`**
  (triage over the P2.9 detection table; `confirm` disables the domain with
  `disabled_reason='service'`, `dismiss` clears the candidate), **`disable [--service-list]`**
  (operator `manual` disable / the service-list batch form), and **`stats recalc`** (re-runs
  the 06 §10 stats rollups on demand — the cutover runbook invokes it, 08 §3 step 2).
- **Acceptance:** `confirm` flips the domain out of the frontier (`disabled`,
  `disabled_reason='service'`, not claimable — claim-query read-back); `dismiss` leaves the
  domain untouched; `disable` sets `manual` and is reversible per 06's re-enable rules;
  `stats recalc` upserts today's `stats_*` rows (incl. `generated_at`) idempotently — a
  second run changes only `generated_at`.

### P2.G1 — Gate: commit-machine + quorum unit coverage
- **Governs:** design §8 Phase 2(a)(b); 03/02 acceptance.
- **Depends:** P2.2, P2.3, P2.4, P2.5
- **Acceptance:** `make test` green with the exhaustive commit-machine table (03 §17,
  `TestCommit*`) and the fake-DNS quorum table (02 §8, `TestQuorum*`/`TestBreakers`) both
  passing, including every 2/3-agree, split, and timeout combination.

### P2.G2 — Gate: 10k parity diff vs production
- **Governs:** design §8 Phase 2(c).
- **Depends:** P2.5, P2.6, P2.7, P2.9, P2.11
- **Deliverables:** `docs/gates/phase2-parity-diff.md` (recorded divergence classes).
- **Acceptance:** a sample run of 10k mixed-rank domains, results diffed against production's
  current statuses; every divergence class is investigated and recorded as either an expected
  deviation (co.uk multi-label-TLD NS fix; the stricter conn-based connectivity definition) or a defect to fix.
  Gate is green only when no unexplained divergence class remains.

### P2.G3 — Gate: lease-fence chaos + no-double-changelog
- **Governs:** design §8 Phase 2(d); see 03-state-machine.md — The lease fence (§13),
  Acceptance (§17.4, §17.6).
- **Depends:** P2.5, P2.11
- **Deliverables:** `internal/crawler/chaos_integration_test.go` (`TestLeaseFenceChaos`).
- **Acceptance:** kill a worker mid-batch; after lease expiry the batch is reclaimed by another
  process; assert **no double changelog** (`SELECT count(*) FROM changelog WHERE (domain_id,ts,
  field) …` shows no duplicate for any reclaimed domain) and no orphaned fresh lease; the
  `lease_lost` counter increments for the killed worker's in-flight domains (03 §17.4, §17.6).

### P2.G4 — Gate: claim-plan at 1M (steady state)
- **Governs:** design §8 Phase 2(e); see 04-lifecycle-scheduling.md — Acceptance (§17.1); see
  05-schema.md — Acceptance (§13.5).
- **Depends:** P2.6, P2.7
- **Deliverables:** `docs/gates/claim-plan-P2.txt`.
- **Acceptance:** with the table at ≥1M rows and a near-empty due backlog (<1k due), `EXPLAIN
  (ANALYZE, BUFFERS)` of the production claim query shows an index scan on `idx_domain_due`,
  buffers/rows proportional to the due set, execution `<50 ms`; the empty-frontier case `<5 ms`
  and the full-backlog case (rank-ordered claiming, `O(due)` cost) are both exercised and
  recorded; no ranked-list read query plan uses `idx_domain_due`.

### P2.G5 — Gate: per-process liveness
- **Governs:** design §8 Phase 2(f); see 09-ops.md — Crawl liveness & heartbeats (§12).
- **Depends:** P2.12, P2.13
- **Acceptance:** with two crawler processes running, kill one — its healthchecks.io check
  flips to "down" within `≤45 min` (heartbeat + idle-checkpoint rule) while the other stays up.
  Recorded to `docs/gates/P2.txt`.

---

## Phase 3 — Full-scale daily crawl

**Goal:** 1M domains crawled daily on production hardware, with Unbound tuned, dashboards +
alerts provisioned, backups live, and three consecutive sub-24h passes. This is the phase that
declares the crawler operational.

### P3.1 — Unbound deployment + tuning + stats scrape
- **Governs:** see 09-ops.md — Unbound deployment + stats collection (§8).
- **Depends:** P0.4
- **Deliverables:** `deploy/unbound/` (`unbound@.service`, `unbound-base.conf`, per-instance
  drop-ins); the `unbound_stats` scrape timer/service writing to the `unbound_stats` table.
- **Acceptance:** `unbound-checkconf` passes on the generated configs; on the compose/staging
  env the crawler's bulk resolver answers through the Unbound services (09-ops §15.8); the
  `unbound_stats` scrape lands rows (`SELECT count(*) FROM unbound_stats` increases over two
  timer ticks).

### P3.2 — Grafana dashboards + alert rules A1–A5
- **Governs:** see 09-ops.md — Crawl liveness, heartbeats & Grafana alert rules (§12).
- **Depends:** P2.12
- **Deliverables:** `deploy/grafana/alerts.yaml` (provisioned rules A1–A5) + dashboards
  (throughput, error rates, resolver latencies, queue depth, Unbound stats, and a claim-query
  duration panel with a 250 ms threshold).
- **Acceptance:** the alert YAML validates (Grafana provisioning lint); A1 fires when both
  crawlers are stopped; A5 fires when the unbound-stats timer is disabled (09-ops §15;
  exercised on staging). Recorded to `docs/gates/P3-alerts.md`.

### P3.3 — Backups live (pgBackRest + logical export)
- **Governs:** see 09-ops.md — Backup & restore (§10).
- **Depends:** P1.4
- **Deliverables:** `deploy/pgbackrest/` (`pgbackrest.conf`, `whynoipv6-export.sh` weekly
  logical export + its systemd timer).
- **Acceptance:** the phase-3 backup gate (09-ops §15.9): stanza-create succeeds, a full backup
  completes, `pgbackrest check` passes with WAL archiving confirmed, and one scratch restore
  starts the API and returns `changelog` rows. Recorded to `docs/gates/P3-backup.md`.

### P3.4 — systemd units + nginx vhost + deploy procedure
- **Governs:** see 09-ops.md — Filesystem & user layout (§3), systemd service units (§4),
  Timer inventory (§5), Deploy procedure (§6), nginx vhost (§7), Logging conventions (§13).
- **Depends:** P2.11, P0.5
- **Deliverables:** `deploy/systemd/*.{service,timer}` (all units + timers); `deploy/nginx/
  api.whynoipv6.com.conf`; the Ansible deploy order (copy → `v6ctl migrate up` → restart
  crawler → restart api → verify).
- **Acceptance:** `systemd-analyze verify` passes on every file in `deploy/systemd/`; every
  oneshot service sets `OnFailure=whynoipv6-notify@%n.service`; `nginx -t` passes on the vhost;
  the deploy dry-run on a scratch host brings both units to `active` and `curl -6
  http://[::1]:8080/livez` + `curl -6 http://[::1]:8080/readyz` both return `200`
  (07-api.md §2.7; 09-ops §15.5, §15.6, §15.7 proxy half).

### P3.5 — Public-resolver rate smoothing + Cloudflare courtesy email
- **Governs:** design §2.7 (throughput math), §8 Phase 3; §2.4 (resolver-load split).
- **Depends:** P3.1, P3.4
- **Deliverables:** `docs/gates/P3-resolver-rate.md` (measured qps).
- **Acceptance:** during a real pass the per-provider consensus rate measures `~24 qps/provider`
  (rate smoothing verified); the Cloudflare courtesy email is sent (recorded/attested in the
  report). Confirms the P1.12 spike at production scale.

### P3.6 — Full 1M daily crawl run
- **Governs:** design §8 Phase 3 verify.
- **Depends:** P3.1, P3.2, P3.3, P3.4, P3.5
- **Deliverables:** `docs/gates/P3-run.md` (pass timings + transition counts).
- **Acceptance:** 3 consecutive full passes each complete `<24 h`; confirmed-transition volume
  is plausible (~1–3k/day); zero preflight false-negative incidents; compression + retention
  jobs are observed running (`SELECT * FROM timescaledb_information.jobs` shows executions).

### P3.G — Phase-3 gate
- **Governs:** design §8 Phase 3 verify (backup gate + churned claim-plan + bloat).
- **Depends:** P3.1, P3.2, P3.3, P3.4, P3.5, P3.6
- **Deliverables:** `docs/gates/P3.txt`, `docs/gates/claim-plan-P3.txt`.
- **Acceptance:** all of: the P3.6 three-pass result green; the P3.3 backup gate green (declared
  before the first production sweep is "done"); alert rules A1/A5 exercised (P3.2); **and** the
  churned claim-plan re-run — after ≥3M `next_check_at` updates through `idx_domain_due`, the
  EXPLAIN stays `<50 ms` and `pgstattuple`/`pgstatindex` on `idx_domain_due` show bloat bounded
  (index size stable across passes, not monotonically growing); the claim-query duration panel
  exists with a 250 ms alert threshold.

---

## Phase 4 — API + DNS-flip cutover

**Goal:** the clean, OpenAPI-first read API at the root of `api.whynoipv6.com` — the **real**
confirmed model on the wire (per-dimension `{value,since}` status objects, `classification`,
`gold`, `class_flags[]`), keyset/cursor pagination, RFC 9457 `problem+json` errors, the short
tier collections, the changelog/timeline surface, change feeds + CSV, the DNS-provider
league table, `/mandates`, and the redesigned `/ip` — then a **pure DNS-flip cutover** with
**no data import** (start-fresh, OPEN-9) plus the `top_shame` re-seed. Produces the shippable
replacement. There is no legacy/compat surface and no production-parity gate; the drift gate is
the OpenAPI contract.

### P4.1 — API server baseline
- **Governs:** see 07-api.md — server baseline, cross-cutting conventions (envelope,
  `snake_case`, RFC 9457 errors, HTTP semantics, health, caching by endpoint class).
- **Depends:** P1.5
- **Deliverables:** `internal/api/router.go` (chi v5 router + middleware stack); `internal/api/
  http.go` (the `{items,page,meta}` / `{points,meta}` envelope helpers; the RFC 9457
  `application/problem+json` writer + the fixed `problem` type-URI set incl. the
  `validation-error`/`scope-required` split; the endpoint-class `Cache-Control` +
  deterministic `ETag`-from-crawl-`generation` helpers); `internal/api/health.go` (`/livez` +
  `/readyz` at the root, outside the OpenAPI + CDN); the `internal/service/` use-case layer
  skeleton; `cmd/api/main.go` wiring (timeouts, CORS; no root health page — health is
  `/livez`/`/readyz` only, 07-api.md §2.7).
- **Acceptance:** `TestBaseline` — every 4xx/5xx is `application/problem+json` with `status`
  equal to the HTTP status line; zero-result reads are `200` with an empty `items` array (never
  a bug-compat 404); `/livez`/`/readyz` return the z-page split and are `no-store`; the
  endpoint-class cache table (07-api.md — caching) is applied and `ETag` derives deterministically
  from the crawl `generation` (`= YYYYMMDD` of `max(stats_global_daily.day)`). Disabled entities
  are invisible in every collection (asserted per-endpoint as those endpoints land). Fixtures:
  10-testing.md.

### P4.2 — RETIRED (was: legacy serialization helpers + shortuuid codec)
- **Retired in Round 3.0.** The legacy compatibility surface is deleted (07-api.md —
  deletions): no `legacyStatus` 3-string projection, no `renderChangelog` 16-row message ladder
  or reverse message-map, no shortuuid codec, no `{"data":[…]}` search envelope, no zero-time
  NULL encoding. The API serves the **real** 4-value enum via `{value,since}` status objects,
  structured changelog rows, raw UUIDs, and the single `{items,page,meta}` envelope. The only
  retained legacy invariant — `error`/`inconsistent` never reaching public output — is enforced
  natively in the resource serializers (P4.3) and the masking rule, not a legacy helper. The ID
  is retired, not recycled.

### P4.3 — Core resource endpoints + tier collections
- **Governs:** see 07-api.md — the resource model (domain status objects, summary/detail, the
  ranked tier lists, country, asn/provider, campaign, resource dependencies), resource naming,
  filter/sort grammar; design §5, §3.2.
- **Depends:** P4.5, P4.13, P2.4, P1.13
- **Deliverables:** `internal/api/{domain,country,asn,campaign,resource,provider}.go` and their
  data access (implemented against the P4.5 generated strict interfaces; each endpoint's schema
  is added to `openapi.yaml` and the drift gate stays green). Data access is split per the
  05-schema §10.2 carve-out: detail/curated reads are sqlc `db/query/` queries; the
  **`/domains` list family** (leaderboards, tier presets, scoped sub-collections, `?q=`) is
  the **squirrel builder** `internal/postgres/domainlist.go` — literal scope/residual/seek
  predicates per request (partial-index verbatim rule), rows scanned via
  `pgx.CollectRows`/`RowToStructByName`:
  - `GET /domains` — the general filterable leaderboard
    (`?class=`/`?country=`/`?asn=`/`?tld=`/`?provider=`/`?gold=`/`?rank_min=`/`?rank_max=`/`?q=`/
    `?sort=`/`?fields=`/`?format=`, with the **`scope-required` guardrail** on bare `flag=`/
    per-dimension status filters) — and `GET /domains/{host}` detail (the six `{value,since}`
    status objects, the **masked** `informational` block, `?include=evidence`);
  - the short **tier collection paths** `GET /heroes` `GET /sinners` `GET /gold` `GET /almost`
    `GET /mail` — presets over `/domains` sharing the same keyset pagination + `?country=`/
    `?asn=` composition;
  - `GET /countries` + `GET /countries/{code}` + `GET /countries/{code}/domains`;
  - `GET /asns` + `GET /asns/{number}` + `GET /asns/{number}/domains`, plus the **DNS-provider
    league table** `GET /providers` + `GET /providers/{id}/domains` (OPEN-4, backed by the P1.13
    `ns_host → provider` mapping);
  - `GET /campaigns` + `GET /campaigns/{uuid}` (composite: metadata + paged members + adoption)
    + `GET /campaigns/{uuid}/domains`, incl. the `?tag=` filter (OPEN-12);
  - `GET /resources/{host}` + the resource-dependency sub-collections owned by P5.4;
  - `GET /shame` (bounded editorial list, exact `meta.count`).
- **Acceptance:** `TestDomains`/`TestTiers`/`TestCountries`/`TestAsns`/`TestProviders`/
  `TestCampaigns` — status objects carry the real 4-value enum **+ JSON `null`** (no
  `legacyStatus` collapse, no `0001-01-01` zero-time); `rank` is `int`-or-`null` (never the
  legacy `0`); each tier path returns the identical row shape + pagination as its `?class=`
  equivalent (`/sinners?country=no` ≡ `/domains?class=sinner&country=no`); `?tld=`/`?provider=`
  filter and the `422 scope-required` on a bare `flag=`/`mx=` hold; the `informational` block
  masks `error`/`inconsistent`→`null` and `partial` to `null` except on `ptr`/`parity`;
  `TestVisibility` — disabled entities and rank-NULL rows are absent from the top-level
  leaderboard but resolve on entity + sub-collection endpoints (07-api.md — visibility).
  Fixtures: 10-testing.md — API serialization vectors.

### P4.4 — `/ip` client-IP echo
- **Governs:** see 07-api.md — client-IP echo; design §5.12.
- **Depends:** P4.1
- **Deliverables:** the redesigned `/ip` handler in `internal/api/misc.go`.
- **Acceptance:** `TestIP` — `GET /ip` returns the object `{ "ip": "<bracketless>",
  "family": "ipv4|ipv6" }` with `family` derived server-side; `Cache-Control: no-store`;
  verified over IPv6 and IPv4 client addresses.

### P4.5 — OpenAPI 3.0.3 contract + codegen + drift gate
- **Governs:** see 07-api.md — OpenAPI-first workflow; see 09-ops.md — Makefile (`generate`
  staleness gate + Spectral/vacuum lint); design §8.
- **Depends:** P4.1
- **Deliverables:** `openapi.yaml` (hand-authored OpenAPI **3.0.3** at `openapi/openapi.yaml`,
  the monorepo `openapi/` directory (00 §4) — the
  single source of truth, `nullable: true` for the `null` status values). It establishes the
  reusable **`components`**: the `{items,page,meta}` / `{points,meta}` envelopes, the keyset
  cursor grammar, the RFC 9457 `problem` shapes (incl. `scope-required`), the `manifest.json`
  schema (§6.3), and the badge/feed representations — plus the initial resource paths. Each
  later endpoint task **extends** `openapi.yaml` for its paths and re-runs the drift gate;
  `oapi-codegen.yaml`; `internal/api/gen/` (committed oapi-codegen **`chi-server` +
  `strict-server`** output); the openapi-typescript output path (the rebuilt frontend's typed
  data layer); the Spectral/vacuum ruleset (enforcing `snake_case`, the two envelopes, the
  `problem+json` schema).
- **Acceptance:** `make generate` runs oapi-codegen + openapi-typescript + sqlc and leaves a
  clean tree (`git diff --exit-code` green — the CI drift gate); the Spectral/vacuum spec lint
  passes; `go build ./internal/api/gen/...` compiles the generated strict interface + types
  (endpoint handlers implement it in the later endpoint tasks, each re-running the drift gate
  green). **This is the OpenAPI contract gate that replaces the retired production-parity
  gates** (07/10 — no `Test*Parity` golden replay, no frozen-frontend E2E).

### P4.6 — RETIRED (was: migrate command core + preconditions + entity resolution)
- **Retired in Round 3.0.** No data import (start-fresh, OPEN-9): the `cmd/v6ctl/migrate_import.go`
  command, the `internal/migrate/resolve.go` entity-resolution/orphan-create step, and the
  `migrate.source_dsn`/`migrate.history_window` config keys are all deleted (08-migration-cutover.md
  — DNS-flip cutover; 09-ops.md — config registry drops the importer keys). The cutover is a pure
  DNS flip (P4.G); the only non-crawl-derived step is the `top_shame` re-seed (P4.11). The ID is
  retired, not recycled.

### P4.7 — RETIRED (was: seed confirmed statuses + `*_since`)
- **Retired in Round 3.0.** No import means **no status seed** from a dump: the crawler builds
  confirmed state from scratch. The consequence is a **cold classification start** — every domain
  sits at `unknown` until N consecutive crawl cycles confirm each dimension — consciously
  accepted and flagged (§5 risk register; 08-migration-cutover.md — DNS-flip cutover). The ID is
  retired, not recycled.

### P4.8 — RETIRED (was: campaign + campaign_domain migration)
- **Retired in Round 3.0.** No import: campaigns and members come **fresh** from the campaign
  repo sync (P1.8, keyed by raw UUID), not migrated from a dump; shortuuid preservation is moot
  (shortuuid is deleted). The ID is retired, not recycled.

### P4.9 — RETIRED (was: changelog history transform / credibility archive)
- **Retired in Round 3.0.** No import: the `changelog` hypertable begins **empty** and fills
  over the months following launch as the fresh crawl accumulates confirmed transitions. The
  reverse-map transform, the `--verify-changelog` byte-equality gate (old **G1**), and
  `internal/migrate/changelog.go` are all deleted (08-migration-cutover.md — DNS-flip cutover;
  the changelog-sourced features §5.8/§5.9/§6.4/§6.6 launch empty). The ID is retired, not
  recycled.

### P4.10 — RETIRED (was: per-scan history import, trailing 90 days)
- **Retired in Round 3.0.** No import: the `scan` hypertable begins **empty**; the §5.9 latency
  overlay fills over ~90 days as fresh scans accumulate. `internal/migrate/history.go` and the
  `migrate.history_window` key are deleted. The ID is retired, not recycled.

### P4.11 — `top_shame` re-seed + `v6ctl shame` CLI
- **Governs:** see 08-migration-cutover.md — DNS-flip cutover (the `top_shame` re-seed step);
  see 06-ingest.md — v6ctl shame; design §6 (`v6ctl shame add|remove|list`), §5.4 (`/shame`).
- **Depends:** P1.7
- **Deliverables:** `cmd/v6ctl/` verbs `shame add|remove|list`. The ~12 curated editorial shame
  hosts have **no crawl-derivable source**, so they are **re-entered via the CLI at cutover**
  (there is no dump import — the old `internal/migrate/shame.go` importer is deleted).
- **Acceptance:** `TestShameCLI` (integration): `shame add` rejects non-apex/rank-NULL/disabled
  hosts (exit 1), is idempotent, and writes no changelog; `shame list` shows the computed
  `visible` column; `shame remove` deletes the row. The re-seed is a **required** cutover step —
  `/shame` (P4.3) is empty at launch otherwise (08-migration-cutover.md — DNS-flip cutover).

### P4.12 — RETIRED (was: day-0 stats snapshot from dump)
- **Retired in Round 3.0.** No import: the day-0 `stats_*` seed row ships in migration 000003
  (P1.3) and the daily tick's stats-rollup step (P2.9) produces subsequent rows from the fresh
  crawl. `internal/migrate/snapshot.go` is deleted. The ID is retired, not recycled.

### P4.13 — Keyset/cursor pagination engine
- **Governs:** see 07-api.md — pagination, filtering, sorting (keyset/cursor, cursor design,
  filter/sort grammar, count strategy); design §4.
- **Depends:** P4.1
- **Deliverables:** `internal/api/paginate.go` — the opaque base64url cursor codec carrying
  `{v,g,s,f,k}`; the **three** strict-total-order seek shapes (`(rank,id)`, `host`, and the
  null-flag-first `(rank IS NULL, rank, id)` for `dependents`); the N+1 `has_more` fetch; the
  `after_rank`/`around_rank` deep-link range scans (rank-ordered views only); filter-fingerprint
  validation + stale-generation re-anchoring; and the count strategy (`max(rank)` headline
  estimate, `reltuples`/plan-row estimate for filtered/scoped, exact `count` only for the
  bounded curated sets). The decoded seek tuple feeds the P4.3 squirrel list builder
  (05-schema §10.2 carve-out), which emits it as literal SQL alongside the scope/residual
  predicates.
- **Acceptance:** `TestCursor` round-trips all three orderings; a cursor whose filter fingerprint
  `f` mismatches the request → `400 invalid-parameter`; a stale-`g` cursor re-anchors on
  `last_rank`; the null-flag-first ordering never drops the rank-NULL tail; a bare unscoped
  `flag=`/per-dimension status filter → `422 scope-required`; `after_rank` is rejected on the
  `sort=host` ordering. Fixtures: 10-testing.md — keyset cursor vectors.

### P4.14 — Changelog + timeline + diff endpoints
- **Governs:** see 07-api.md — changelog event (structured), per-domain timeline/history
  (changelog reconstruction), diff (OPEN-7); design §5.8, §5.9, §6.6.
- **Depends:** P4.5, P4.13
- **Deliverables:** `internal/api/{changelog,history}.go` + `db/query/`:
  - `GET /changelog` (global, cursor on `ts DESC`, `?field=`/`?from=`/`?to=`) and the per-scope
    feeds `GET /domains/{host}/changelog`, `GET /campaigns/{uuid}/changelog`,
    `GET /campaigns/{uuid}/domains/{host}/changelog`, `GET /countries/{code}/changelog`
    (scoped feeds **capped to the latest-50 recent window**, OPEN-15);
  - `GET /domains/{host}/history` — the per-dimension trajectory **reconstructed from the
    `changelog`** (confirmed transitions replayed + the classification ladder applied per point),
    carrying the `scan` **latency overlay only** (never raw `scan` observation values);
  (`GET /diff` is **cut** — OPEN-7 re-resolved, 07-api.md §5.6: `/changelog` + the change
  feeds already carry the "who went green" information.)
- **Acceptance:** `TestChangelog` serves the structured row (`ts,host,field,old_value,new_value`;
  raw 4-value enum; always non-null and distinct; incl. `conn`/`resources`/`not_applicable`
  transitions — no coverage filter, no synthetic epoch id); `TestHistory` reconstructs from the
  changelog (asserts `error`/`inconsistent` never appear; `classification` per point is the
  ladder over the reconstructed confirmed state); both
  return `200` with an empty collection on the fresh (empty-changelog) DB; the contract test
  asserts `GET /diff` is absent from `openapi.yaml` (10-testing §8.6). Fixtures: 10-testing.md
  — confirmed-state reconstruction vectors.

### P4.15 — Change feeds (Atom + JSON-Feed) + CSV export
- **Governs:** see 07-api.md — change feeds (Atom + JSON-Feed per scope), CSV export via content
  negotiation; design §6.4, §6.5.
- **Depends:** P4.14
- **Deliverables:** `internal/api/{feed,csv}.go` + templates:
  - the four-scope × two-format feed matrix (`/changelog.atom`, `/changelog.feed.json`, and the
    per-domain/campaign/country `.atom` / `.feed.json` suffix URLs), each a fixed **latest-50**
    window, item id = composite `(host, ts, field)`, human `title`/`content_text` derived
    server-side at render time from `(field, old_value, new_value)`;
  - `?format=csv` on the list endpoints (`/domains*`, `/countries`, `/asns`, `/changelog`,
    search), `text/csv; charset=utf-8` + `Content-Disposition: attachment`, a defined column set
    per list.
- **Acceptance:** `TestFeeds` — every scope×format feed carries the required top-level members
  (Atom `<id>`/`<updated>`/`<title>`/self+alternate links; JSON-Feed `version`/`title`/
  `home_page_url`/`feed_url`/`items`), the latest-50 window, and the composite item id;
  `TestCSV` — the defined column set + attachment disposition, and a stable cursor/`after_rank`-
  anchored URL reproduces the same view. Fixtures: 10-testing.md — Atom/JSON-Feed serializer
  vectors.

### P4.16 — `/mandates` surface + campaign `?tag=` (OPEN-12)
- **Governs:** see 07-api.md — new/flagged special endpoints (`/mandates` + `?tag=`); design
  §6.6, OPEN-12.
- **Depends:** P4.3
- **Deliverables:** `internal/api/mandates.go` + `db/query/` — the `/mandates` surface over
  tagged campaigns (with citations). The `?tag=` campaign filter itself ships in P4.3; this task
  adds the dedicated `/mandates` view.
- **Acceptance:** `TestMandates` — `/mandates` lists the tagged/mandate campaigns; `?tag=`
  filters `/campaigns` to the tag; an unknown tag returns `200` with an empty collection (not
  404). Fixtures: 10-testing.md.

### P4.G — Phase-4 gate (DNS-flip cutover)
- **Governs:** see 08-migration-cutover.md — DNS-flip cutover (cutover runbook + rollback); see
  07-api.md — Acceptance, OpenAPI-first workflow; design §8 Phase 4 verify, OPEN-9.
- **Depends:** P4.1, P4.3, P4.4, P4.5, P4.11, P4.13, P4.14, P4.15, P4.16
- **Deliverables:** `docs/gates/P4-cutover.md` (the thin cutover checklist + record).
- **Acceptance:** a pure DNS-flip cutover with **no data import** — the whole gate is the four
  steps below (the legacy/parity gates G1–G7 are deleted; the **OpenAPI contract gate** is the
  replacement):
  1. **Fresh DB → migrations:** a fresh `timescale/timescaledb:latest-pg18` DB is created and
     `v6ctl migrate up` applies 000001→latest green (both `changelog` and `scan` start empty).
  2. **Crawl builds state:** the crawler builds confirmed state from scratch (a bounded sample
     crawl suffices for **this build gate**; the full 1M pass is Phase 3, and the *production
     cutover* precondition is ≥3 full frontier passes — 08-migration-cutover.md §2.4, a
     different gate). The **cold classification
     start** is expected and flagged (§5 risk register) — day-1 hero/adoption counts read low
     until N crawl cycles confirm each dimension; this is not a bug.
  3. **OpenAPI contract tests green:** the P4.5 drift gate is clean (`make generate` →
     `git diff --exit-code`), the Spectral/vacuum lint passes, and the endpoint tests
     (P4.3/P4.4/P4.13/P4.14/P4.15/P4.16) are green.
  4. **`top_shame` re-seed (P4.11) applied** so `/shame` is non-empty at launch.
  - Then DNS/upstream is switched. The old backend stays deployable through the rollback window
    (08-migration-cutover.md — DNS-flip cutover). A restore-drill (backup → scratch instance →
    API starts, `GET /changelog` reachable) is retained as ops hygiene (P3.3 / 09-ops backup
    gate), not a production-parity gate.

---

## Phase 5 — #23 resources + classification surfacing

**Goal:** the resource-dependency dimension, the gold tier, and the resource forward/dependents
surface (the `/almost` tier collection itself is a classification preset and ships in P4.3).
`crawler.resources.enabled` flips to `true` at deploy; until then the crawler wrote
`resources = not_applicable` and no gold badges existed (verified from Phase 2).

### P5.1 — Resource-host registry + sweep worker
- **Governs:** see 06-ingest.md — Resource-host registry (§5), Acceptance (§9.8); see
  02-observation-model.md — resources roll-up (§6); design §4.6 / OPEN-3.
- **Depends:** P2.5, P2.9
- **Deliverables:** `internal/crawler/resourcesweep.go` (the resource-host sweep worker running
  inside `cmd/crawler`); the discovery/prune/`dependent_count` statements A–C executed inside
  the per-domain commit transaction (machinery from P2.5); `db/query/` additions.
- **Acceptance:** `TestResourceDiscovery` (integration): statements A–C + prune maintain
  `dependent_count` exactly (property test: count equals `SELECT count(*) FROM domain_resource
  WHERE resource_host_id=X` after arbitrary interleavings); manual links survive prune;
  `required=FALSE` links are excluded from the roll-up input (06 §9.8).

### P5.2 — `v6ctl resource add|remove`
- **Governs:** see 06-ingest.md — Resource-host registry (§5, manual endpoints); design §4.6
  (operator `v6ctl resource add` only).
- **Depends:** P5.1
- **Deliverables:** `cmd/v6ctl/` verbs `resource add`, `resource remove`.
- **Acceptance:** `TestResourceCLI` (integration): a manually-added resource link survives the
  sweep's prune; `resource remove` deletes it and decrements `dependent_count` (06 §9.8).

### P5.3 — Enable the resources dimension + gold badge
- **Governs:** see 03-state-machine.md — Classification: gold (§10), Acceptance (§17.9); see
  04-lifecycle-scheduling.md — config `crawler.resources.enabled`; design §4.6.
- **Depends:** P5.1, P2.4
- **Deliverables:** flip `crawler.resources.enabled=true` in the deploy config (registry:
  09-ops.md); no new logic (the roll-up and gold computation shipped disabled in Phase 2).
- **Acceptance:** with the flag true, `scan.resources` is computed (not forced
  `not_applicable`), `domain.resources_*` columns populate, and gold is computed by `classify`;
  the Phase-2 invariant (`gold=false` while disabled — 03 §17.9) no longer applies. Verified on
  the P5.G fixture site.

### P5.4 — Subdomains + resource-dependency endpoints
- **Governs:** see 07-api.md — resource dependencies (`/domains/{host}/subdomains`,
  `/domains/{host}/resources`, `/resources/{host}/dependents`); design §5.11, §5.3. (The
  `/almost` tier collection is **not** here — it is a classification preset over `/domains` and
  ships in P4.3.)
- **Depends:** P4.3, P4.13, P5.3
- **Deliverables:** `internal/api/resource.go` — the `/domains/{host}/subdomains` handler
  (parent-resolved, children `WHERE parent_id=$parent AND NOT disabled ORDER BY host ASC`,
  keyset-paged on `host`), `GET /domains/{host}/resources` (forward: bounded, exact
  `meta.count`), and `GET /resources/{host}/dependents` (reverse advocacy surface, keyset-paged
  over the **null-flag-first** ordering from P4.13, `count_estimate` + headline
  `dependent_count`); `db/query/` additions; `openapi.yaml` additions (regenerate — drift gate
  clean).
- **Acceptance:** `TestSubdomains` — `/domains/{host}/subdomains` returns the summary rows for an
  apex's non-disabled children in `host ASC` order, `200` empty for a childless or
  `kind='subdomain'` parent, and `404 application/problem+json` (`type=.../not-found`) for an
  unknown/disabled/uncanonicalizable parent; `TestDependents` — the reverse list uses the
  null-flag-first ordering (rank-NULL dependents are not dropped) and reports
  `resource.dependent_count`; `TestForwardResources` — forward resources carry an exact
  `meta.count`; `make generate` clean.

### P5.G — Phase-5 gate
- **Governs:** design §8 Phase 5 verify.
- **Depends:** P5.1, P5.2, P5.3, P5.4
- **Deliverables:** `docs/gates/P5.md`.
- **Acceptance:** a known-fixture hero with v4-only fonts/CDN classifies as **hero + not-gold**
  with `resources_v4only`; the resource-host dedup ratio is measured and recorded;
  classification counts are stable across 3 days (no flap storm from the new dimension — it
  only affects gold).

---

## Phase 6 — Public features

**Goal:** the anonymous live "check any domain" flow, stats endpoints, dataset export, and the
status badge.

### P6.1 — Live check (`POST /check` / `GET /check/{id}`) + consumer/reaper
- **Governs:** see 07-api.md — live check (async job lifecycle); see 04-lifecycle-scheduling.md
  (check-job consumer placement inside `cmd/crawler`); design §6.1, §7.3 (rate limits).
- **Depends:** P4.5, P2.11
- **Deliverables:** `internal/api/check.go` — `POST /check` (`202 Accepted` + `Location:
  /check/{id}`, the **`BIGINT` `check_job.id`** on the wire) and `GET /check/{id}` (poll to
  terminal), domain-side + job-side dedupe, RFC 9457 errors (invalid host →
  `400 invalid-parameter`; non-JSON body → `415 unsupported-media-type`; over quota →
  `429 rate-limited` + `Retry-After` + the `RateLimit`/`RateLimit-Policy` structured-field
  headers), per-IP **and per-/64-prefix** + global rate limits, and **terminal-poll caching**
  (`done`/`failed` → `Cache-Control: public, max-age=60`; in-flight → `no-store`); the check-job
  consumer goroutines + reaper wired into `cmd/crawler`; `db/query/` for `check_job`.
- **Acceptance:** `TestLiveCheck` (integration): **Rule-0 (locked)** — a completed check job
  leaves every `domain` confirmed-state column, `scan`, and `changelog` untouched except
  `last_requested_at`/`next_check_at`/re-enable (07-api.md — Rule 0; design §6.1); dedupe
  (domain- and job-side, 1 h window) returns `"cached": true` with no new job rows; the reaper
  flips stale jobs to `failed` `≤15 min`; the rate-limit fixtures (10/IP/h, 500/h global, /64
  keying) hold and emit `429 problem+json` + `Retry-After`; a terminal poll is
  `public, max-age=60` while an in-flight poll is `no-store`. Fixtures: 10-testing.md.

### P6.2 — Stats / overview endpoints
- **Governs:** see 07-api.md — stats / overview (adoption over time); design §5.10.
- **Depends:** P4.5, P2.9
- **Deliverables:** `internal/api/stats.go` + `db/query/` reads over the `stats_*` snapshot
  tables — the routes `GET /stats/overview` (`stats_global_daily`),
  `GET /countries/{code}/stats`, `GET /campaigns/{uuid}/stats` (incl. `v6_ready%`),
  `GET /asns/{number}/stats` (exposing the `count_v6`/`count_total` wire names), one query
  contract (`?from=&to=&interval=daily|weekly`, ascending, no pagination, `≤366 rows/yr`, zero
  rows → `200 {"points":[]}`); the **`{points,meta}`** time-series envelope with
  `meta.source: "confirmed_state"`; `openapi.yaml` additions.
- **Acceptance:** `TestStatsEndpoints` — responses use the `{points,meta}` envelope and match
  the snapshot tables (`SELECT`-equal); `meta.source` is `"confirmed_state"`; public graphs equal
  public lists (design §5.10 invariant); the `scan_daily_adoption` cagg is **not** exposed
  (OPEN-5); `make generate` clean. (Per-domain `/domains/{host}/history` is owned by P4.14.)

### P6.3 — Static dataset export + manifest + nginx datasets location
- **Governs:** see 07-api.md — datasets (static bulk + manifest + citation); see 09-ops.md —
  nginx vhost (`/datasets/` location split); design §6.3.
- **Depends:** P4.5, P3.4
- **Deliverables:** the nightly `v6ctl export` job (atomic tmp-dir `rename(2)`) producing **3
  size tiers** (`top100k`/`top1m`/`full`) × **CSV.gz + Parquet**, each snapshot shipping a
  Frictionless **`datapackage.json`** (per-file `path`/`bytes`/`hash: "sha256:<digest>"` +
  Table Schema), a `SHA256SUMS`, and a `DICTIONARY.md`; the top-level **`manifest.json`** (the
  pinned schema — `schema_version`, `generated_at`, `generation`, `license`, `attribution`,
  `latest`, `snapshots[]`; its schema lives in `openapi.yaml` `components`); `internal/api/
  datasets.go` (`GET /datasets` re-reads `$DATASETS_DIR/manifest.json` from disk every request,
  `public, max-age=300`, missing/unparseable → **`503 manifest-unavailable`** — the only 503);
  the `/datasets/` nginx location split + its systemd timer.
- **Acceptance:** `TestDatasetExport` — generated CSV.gz + Parquet validate against
  `datapackage.json`/`DICTIONARY.md`; `SHA256SUMS` verifies; `manifest.json` conforms to the
  pinned schema (contract-tested via the OpenAPI `components`); `GET /datasets` returns the
  manifest and `503 manifest-unavailable` when it is missing; `nginx -t` passes and a request to
  a dated `…/whynoipv6-top1m.csv.gz` is served from disk with `Cache-Control: …immutable` while
  exact `=/datasets` proxies to the API (09-ops nginx vhost); `make generate` clean.

### P6.4 — Embeddable status badge (`GET /badge/{host}.svg` + `.json`)
- **Governs:** see 07-api.md — embeddable SVG badge (the normative `classification`→label table,
  shields.io endpoint-JSON variant); design §6.2 (badge promoted to committed).
- **Depends:** P4.5
- **Deliverables:** `internal/api/badge.go` + the **six** precompiled byte-deterministic SVG
  variants (one per `classification`, plus the `gold` overlay on `hero`) and the `.json` shields
  endpoint variant (deliberate **camelCase** `schemaVersion`/`cacheSeconds`/`isError` — the one
  sanctioned exception); `internal/api/testdata/badge/**` byte-exact fixtures.
- **Acceptance:** `TestBadge` — byte-exact SVG per the normative table (`hero`→`IPv6: supported`
  brightgreen; `hero+gold`→`IPv6: gold`; `partial`→`IPv6: partial` yellow; `sinner`→`IPv6: no
  IPv6` red; `inactive`→`IPv6: inactive` lightgrey; `unknown`→`IPv6: unknown` lightgrey +
  `isError:true`); `Content-Type: image/svg+xml` + `Cache-Control: public, max-age=86400` +
  `X-Content-Type-Options: nosniff` + `ETag` from generation; **no rate-limit**; a *valid* host
  is **always `200`** (disabled/unknown → gray `IPv6: unknown`), a **malformed** host →
  `400 invalid-parameter` (the declared exception to 404-on-canonicalize-failure), a suffix-less
  path → `404`; the host label is XML-escaped into the SVG; **read-only zero side effects** (no
  domain row inserted, no check_job enqueued, `last_requested_at` untouched — read-back
  assertion). Fixtures: 10-testing.md — badge golden SVGs.

### P6.G — Phase-6 gate
- **Governs:** design §8 Phase 6 verify.
- **Depends:** P6.1, P6.2, P6.3, P6.4
- **Deliverables:** `docs/gates/P6.md`.
- **Acceptance:** an abuse test on `/check` under scripted load confirms the rate limits hold
  (10/IP/h + 500/h global enforced, no state leakage); datasets validate against `DICTIONARY.md`;
  stats endpoints match the snapshot tables.

---

## Phase 7 — Campaign automation + ops polish

**Goal:** the contributor pipeline (PR validation → merge webhook → bot UUID write-back) and the
remaining runbooks. Closes the whole build.

### P7.1 — Campaign PR-validation Action + `v6ctl campaign validate`
- **Governs:** see 06-ingest.md — PR-validation GitHub Action (§4); design §7.2.
- **Depends:** P1.8
- **Deliverables:** `.github/workflows/validate.yml` in the **campaign repository**;
  `cmd/v6ctl/` verb `campaign validate`.
- **Acceptance:** `TestCampaignValidate` reproduces every §4.2 blocking check on fixture PRs
  (added-file-with-uuid, modified-uuid, rename-with-preserved-uuid, within-file duplicate,
  oversize file) and never fails on cross-file duplicates (06 §9.7). Fixtures: 06-ingest.md §9
  (ingest fixtures; 00 §8.2 exception).

### P7.2 — Merge-trigger sync path + bot UUID write-back
- **Governs:** see 06-ingest.md — Campaign repo sync (§3): the webhook/merge path (repo-dispatch
  on merge to main → operator CI runs `v6ctl campaign sync` on the backend host), step 6
  write-back (bot commit `chore: assign campaign uuids [skip ci]` + push via deploy key), and
  the advisory-lock serialization against the daily tick (`internal/lock`).
- **Depends:** P1.8, P2.9
- **Deliverables:** the campaign-repo merge workflow (`.github/workflows/sync-dispatch.yml` in
  the campaign repository) that repo-dispatches on push to main; the operator-CI job invoking
  `v6ctl campaign sync`; the bot UUID write-back is already implemented in `internal/campaign`
  (P1.8) — this task wires and verifies the trigger + deploy-key push end-to-end. No new Go
  package (the sync is the single `internal/campaign.Sync`, per 06-ingest §3).
- **Acceptance:** end-to-end (staging): a test PR → merge → repo-dispatch triggers the sync →
  the new domains appear scanned within 24h with the UUID committed back to the campaign repo
  by the bot commit (design §8 Phase 7 verify); the sync is serialized with the daily tick via
  the advisory lock (no double-run). Recorded to `docs/gates/P7-e2e.md`.

### P7.3 — Runbooks + GeoLite2 lifecycle
- **Governs:** see 09-ops.md — GeoLite2 lifecycle (§11); design §11 (Unbound, Timescale jobs,
  frontier surgery runbooks).
- **Depends:** P3.1, P1.9
- **Deliverables:** `deploy/geoip/GeoIP.conf` template + `geoipupdate.timer` override;
  `docs/runbooks/{unbound,timescale-jobs,frontier-surgery}.md`.
- **Acceptance:** `systemd-analyze verify` passes on the geoip timer override; the runbooks
  exist and each documents its trigger + recovery steps (design §11 topics covered).

### P7.4 — Config-registry completeness gate
- **Governs:** see 09-ops.md — Consolidated config registry (§2), Acceptance (§15.1, §15.2).
- **Depends:** all key-introducing tasks (P0.6, P2.1, P2.2, P2.12, P5.3, P6.1, P6.3) —
  practically every phase.
- **Deliverables:** `internal/config/registry_test.go` (`TestRegistryCompleteness`).
- **Acceptance:** every key in 09-ops §2 is registered via `viper.SetDefault` at startup,
  resolves from its documented env var, and appears in the startup config summary; a config key
  present in any spec file but absent from §2 fails the test (09-ops §15.1); env overrides for
  `WORKER_SLOTS`, `CONSENSUS_PER_PROVIDER_QPS`, `CRAWLER_RESOURCES_ENABLED`,
  `RESOLVER_BULK_UPSTREAMS` apply with no YAML present (09-ops §15.2). The `migrate.*` importer
  keys no longer exist (start-fresh, OPEN-9 — 09-ops drops them from the registry), so the
  former `migrate.*` startup-summary exemption is removed.

### P7.G — Phase-7 gate + whole-build DoD
- **Governs:** design §8 Phase 7 verify; §3 (definition of done) above.
- **Depends:** P7.1, P7.2, P7.3, P7.4, and every prior phase gate.
- **Acceptance:** the P7.2 end-to-end campaign flow green; `make lint test test-integration
  vulncheck build-linux` all green on the default branch; the **OpenAPI contract gate** (P4.5
  drift + Spectral) green and **every phase gate P0.G–P7.G** green (the legacy parity gates
  G1–G7 are deleted); Appendix A has no `UNREACHED` row. Record the final DoD checklist to
  `docs/gates/DONE.md`.

---

## Appendix A — Requirement coverage matrix (spec section → task)

Every normative spec section must be reachable from at least one task. A verifier checks this
matrix against the §-headings of files 01–10; any section with no task ID is a plan defect.

| Spec file — section | Task(s) |
|---|---|
| 01-engine — §2–§12 (engine lift, checks, resolver, ssrf, runner, preflight) | P2.1 |
| 01-engine — §9 (consensus seam types) | P2.2 |
| 02-observation-model — §2 (consensus resolver) | P2.2 |
| 02-observation-model — §3–§7 (mapper, conn, resources roll-up) | P2.3 |
| 03-state-machine — §5–§14 (commit machine, fence, scan rows) | P2.5 |
| 03-state-machine — §4, §6 (dead signal, step R) | P2.8 |
| 03-state-machine — §10 (classification ladder/gold) | P2.4, P5.3 |
| 04-lifecycle-scheduling — §2,§3,§12 (frontier, claim, worker pool) | P2.6 |
| 04-lifecycle-scheduling — §4,§5 (cadence, post-commit schedule) | P2.7 |
| 04-lifecycle-scheduling — §6,§7 (dead + delist re-entry) | P2.8 |
| 04-lifecycle-scheduling — §8,§9 (sweep, daily tick) | P2.9 |
| 04-lifecycle-scheduling — §10 (advisory locks) | P2.10 |
| 04-lifecycle-scheduling — §11,§13,§14 (preflight, topology, shutdown) | P2.11 |
| 04-lifecycle-scheduling — §15 (metrics, heartbeats) | P2.12 |
| 04-lifecycle-scheduling — §17.1 (claim-plan gate) | P1.11, P2.G4, P3.G |
| 05-schema — §3 (base schema) | P1.1 |
| 05-schema — §4 (timescale) | P1.2 |
| 05-schema — §5 (seed) | P1.3 |
| 05-schema — §2 (migration framework) | P1.4 |
| 05-schema — §7 (Tranco staging DDL) | P1.7 |
| 05-schema — §9 (`updated_at` maintenance rule, application-side) | P2.5, P1.5 |
| 05-schema — §10 (sqlc + data-access) | P1.5 |
| 05-schema — drop changelog legacy columns; add pivots + tags (tld, `ns_host→provider` table, provider tag, campaign.tags, `generated_at`) | P1.1, P1.8, P1.13, P1.14, P2.9 |
| 06-ingest — §1 (Canonicalize, incl. tld extraction) | P1.6 |
| 06-ingest — §2 (Tranco import, incl. tld write) | P1.7 |
| 06-ingest — §3 (campaign sync, incl. tags) | P1.8 |
| 06-ingest — §4 (PR-validation Action) | P7.1 |
| 06-ingest — §5 (resource-host registry) | P5.1 |
| 06-ingest — §6 (GeoIP/ASN attribution) | P1.9 |
| 06-ingest — DNS-provider mapping (`ns_host→provider`) | P1.13 |
| 06-ingest — §6.11 (v6ctl provider verbs + seed file) | P1.13 |
| 06-ingest — hosting/CDN provider tag | P1.14 |
| 06-ingest — §7 (v6ctl shame + resource verbs) | P4.11, P5.2 |
| 06-ingest — §10.7 (v6ctl stats recalc) + `disable` verb | P2.14 |
| 04-lifecycle-scheduling — service/manual lifecycle triage (service-candidates) | P2.9, P2.14 |
| 07-api — server baseline, cross-cutting (envelope, snake_case, RFC 9457, HTTP, health, caching) | P4.1, P4.5 |
| 07-api — pagination/filtering/sorting (keyset/cursor, count strategy) | P4.13 |
| 07-api — resource model (domains + tier collections, country, asn/provider, campaign, resources) | P4.3, P5.4 |
| 07-api — changelog event / per-domain timeline (OPEN-7 `/diff`: cut, 07 §5.6) | P4.14 |
| 07-api — change feeds (Atom + JSON-Feed) + CSV export | P4.15 |
| 07-api — /mandates + `?tag=` campaign filter (OPEN-12) | P4.16, P4.3 |
| 07-api — /ip client-IP echo | P4.4 |
| 07-api — embeddable SVG badge (+ shields JSON) | P6.4 |
| 07-api — live check (async job lifecycle) | P6.1 |
| 07-api — stats / overview (time-series envelope) | P6.2 |
| 07-api — datasets (static bulk + manifest) | P6.3 |
| 07-api — OpenAPI-first workflow | P4.5 |
| 08-migration-cutover — DNS-flip cutover (fresh DB → migrate → crawl → contract tests → flip; cutover + rollback) | P4.G |
| 08-migration-cutover — `top_shame` re-seed step | P4.11 |
| 09-ops — §1 (config model) | P0.6 |
| 09-ops — §2 (config registry) | P7.4 |
| 09-ops — §3,§4,§5,§6 (filesystem, systemd, timers, deploy) | P3.4 |
| 09-ops — §7 (nginx vhost) | P3.4, P6.3 |
| 09-ops — §8 (Unbound + stats) | P3.1 |
| 09-ops — §9 (docker-compose) | P0.4 |
| 09-ops — §10 (backup & restore) | P3.3 |
| 09-ops — §11 (GeoLite2 lifecycle) | P7.3 |
| 09-ops — §12 (liveness, heartbeats, alerts) | P2.12, P3.2 |
| 09-ops — §13 (logging conventions) | P0.6 |
| 09-ops — §14 (Makefile, golangci, CI) | P0.3, P0.5 |
| 10-testing — unit vectors (Canonicalize+tld, quorum, commit machine, classify) | co-located with P1.6, P2.1–P2.5 |
| 10-testing — keyset cursor vectors (three orderings) | P4.13 |
| 10-testing — RFC 9457 problem+json shapes (incl. `scope-required`) | P4.1 |
| 10-testing — API serialization vectors (status objects, structured changelog) | P4.3, P4.14 |
| 10-testing — confirmed-state reconstruction vectors (§5.9) | P4.14 |
| 10-testing — Atom/JSON-Feed serializer vectors | P4.15 |
| 10-testing — `manifest.json` schema vectors | P6.3 |
| 10-testing — badge golden SVGs (public status vocabulary, six variants) | P6.4 |
| 10-testing — provider mapping + hosting-tag vectors | P1.13, P1.14 |
| 10-testing — integration harness + scenarios | P1.10 + every integration test |
| 10-testing — make targets, coverage bars | P0.3, enforced by all test tasks |

---

## Appendix B — Fused task pairs (single-agent exceptions)

The runner may execute these pairs in one agent invocation (tight coupling, shared file):

- **P2.8 is fused into P2.5+P2.7** where the implementation lands in `commit.go`/`schedule.go`
  (P2.8 defines no new file); the runner may dispatch P2.8's acceptance as a follow-up check on
  the same tree rather than a separate build.
- **P4.15 + P4.14** (change feeds/CSV over the changelog endpoints) may share an agent — the
  feed/CSV serializers are thin renderers over the changelog reads P4.14 lands.

All other tasks are one-agent-per-ID.
