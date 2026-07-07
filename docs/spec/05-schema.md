# 05 — Database Schema, Migrations & Data Access

**Purpose:** This file is the single source of truth for ALL SQL DDL in the system: every extension, enum, table, index, constraint, storage parameter, hypertable conversion, columnstore setting, retention policy, continuous aggregate, and seed row — organized as three runnable golang-migrate migration files. It also pins the migration tooling (golang-migrate embedded in `v6ctl`), the sqlc configuration and package layout, and the application-side `updated_at` maintenance rule. No other spec file may contain `CREATE`/`ALTER`/`DROP` statements; other files reference tables and columns by name and quote their own `SELECT`/`INSERT`/`UPDATE`/`DELETE` statements.

**Deliverables:**

- `db/migrations/000001_base_schema.up.sql` / `.down.sql` — extensions, enums, all tables, indexes, storage parameters
- `db/migrations/000002_timescaledb.up.sql` / `.down.sql` — hypertable conversions, columnstore settings + policies, retention policies, the `scan_daily_adoption` continuous aggregate
- `db/migrations/000003_seed.up.sql` / `.down.sql` — sentinel ASN, country reference data, day-0 stats rows
- `db/migrations/migrations.go` — `go:embed` package exposing the migration files to `v6ctl`
- `sqlc.yaml` — sqlc v2 configuration (pgx/v5)
- `db/query/*.sql` — sqlc query source files (file layout defined here; query *contents* are owned by 03/04/06/07)
- `internal/postgres/db/` — sqlc-generated code (never hand-edited)
- `internal/postgres/` — hand-written adapters implementing the `internal/repository` port interfaces
- `internal/repository/` — port interfaces (package named here; interface contents defined by their consumers)
- `cmd/v6ctl` `migrate` subcommand behavior (defined here; cobra wiring per the v6ctl file)

**Companion files:** 00-overview.md (canonical sizing constants; spec-file map), 03 (confirmed-state commit machine — quotes the commit DML), 04 (frontier, claim loop, lifecycle — quotes the claim DML), 06 (ingest: Tranco import, campaign sync, resource sweep, daily tick — quotes its DML), 07 (API surface — quotes its read queries), 08 (migration & cutover — phase-4 importers), 09-ops.md (config-key registry, deploy, backup), 10-testing.md (migration/round-trip test fixtures).

---

## 1. Conventions and invariants (normative)

1. **Single-source DDL.** All DDL lives in this file's three migrations. The one sanctioned exception is the session-scoped `tranco_staging` TEMPORARY table (§7), whose definition also lives here; 06 quotes only DML against it.
2. **Stack pin.** PostgreSQL 18, TimescaleDB 2.28.x (Community/TSL edition — free for self-hosted use), `pg_trgm` (contrib). TimescaleDB uses the **modern columnstore API** (`timescaledb.enable_columnstore`, `timescaledb.orderby`, `CALL add_columnstore_policy`) — never the deprecated `timescaledb.compress` API.
3. **No FKs on hypertables.** `scan`, `scan_detail`, `changelog`, `crawler_metrics`, `unbound_stats`, `stats_asn_daily` carry no foreign keys (TimescaleDB constraint).
4. **No segmentby anywhere.** Every columnstore table sets `timescaledb.orderby` explicitly and leaves segmentby unset: at ~1 row/domain/day a `domain_id` segment holds 1–7 rows and compression collapses; `orderby = 'domain_id, ts DESC'` co-locates each domain's rows and still gives min/max sparse-index pruning. Because `timescaledb.orderby` is set explicitly on each table, TimescaleDB ≥ 2.20 does NOT auto-select a default segmentby (PR #8033); no `segmentby = ''` override is required.
5. **Identity, not serial.** All synthetic keys are `GENERATED ALWAYS AS IDENTITY`. Seeded reference rows (sentinel ASN, countries) are therefore resolved **by lookup at process startup** (`asn.number = 0`, `country.code = 'UN'`), never by literal id.
6. **`updated_at` is application-maintained.** No triggers exist anywhere in this schema (see §9).
7. **Predicate textual match.** The partial-index predicate on `idx_domain_due` must **textually match** the claim query's eligibility predicate (04 quotes it). The classification/country partial-index predicates must be spelled out **verbatim** (`AND rank IS NOT NULL AND NOT disabled`) in every query that should use them, so the planner's predicate-implication check is trivial.
8. **Index invariant (locked):** no full (non-partial) index with leading column `rank` may ever be added — a rank-led index usable by the claim query hands the planner a pathological plan (rank-ordered full-index walk with `next_check_at <= now()` as a per-tuple filter). Any future schema work touching `domain` indexes must re-run the phase-2 claim-plan gate (10-testing.md — claim-plan gate).
9. **Host storage form.** `domain.host` and `resource_host.host` store exactly one form: lowercase punycode (ASCII/A-label) FQDN, no trailing dot, ≤253 octets, ≥2 labels — the output of `Canonicalize()` (`internal/domain/host.go`). **No CHECK constraint enforces this** (deliberate): it is application-enforced, with a single write path per table.
10. **Spec-file references** in the consumer map (§11) use the file numbers of this spec set as listed in 00-overview.md — spec map (03 commit machine, 04 frontier/lifecycle, 06 ingest, 07 API, 08 cutover, 09 ops, 10 testing).

---

## 2. Migration framework

### 2.1 Tooling

- **Library:** `github.com/golang-migrate/migrate/v4` with the **`pgx/v5` database driver** (`github.com/golang-migrate/migrate/v4/database/pgx/v5`) and the **`iofs` source driver** over an embedded filesystem.
- **File naming (golang-migrate sequential convention):**

  ```
  db/migrations/000001_base_schema.up.sql
  db/migrations/000001_base_schema.down.sql
  db/migrations/000002_timescaledb.up.sql
  db/migrations/000002_timescaledb.down.sql
  db/migrations/000003_seed.up.sql
  db/migrations/000003_seed.down.sql
  ```

- **Embedding.** `db/migrations/` in the repo is the dev/sqlc source of truth; the deploy artifact set is exactly the three binaries, so migrations ship embedded in `v6ctl`:

  ```go
  // db/migrations/migrations.go
  // Package migrations embeds the golang-migrate SQL files so v6ctl can
  // run them via the iofs source driver without shipping a directory.
  package migrations

  import "embed"

  //go:embed *.sql
  var Files embed.FS
  ```

- **`v6ctl migrate`** (cobra verbs; connection from config key `DATABASE_URL` — registry: 09-ops.md):
  - `v6ctl migrate up` — apply all pending migrations. Exit 0 when already up to date.
  - `v6ctl migrate version` — print current version + dirty flag.
  - `v6ctl migrate force <version>` — set version after manual repair of a dirty state (operator-only; prints a warning).
  - There is deliberately **no `v6ctl migrate down`** verb. **Decision:** the down files exist for golang-migrate completeness and dev use only (via the library API in integration tests); production is forward-only per the deploy contract below, and omitting the verb makes the contract structurally unviolable from the CLI.
  - **Decision:** `v6ctl migrate` connects by rewriting the `DATABASE_URL` scheme `postgres://`→`pgx5://` (the golang-migrate pgx/v5 driver's URL scheme) and calling `migrate.NewWithSourceInstance(iofs, url)`. No second config key.

- **Forward-only deploy contract (restated from the deploy procedure, 09-ops.md):** `v6ctl migrate up` runs before binaries restart; every migration shipped with release N must keep release N−1 binaries functional (expand/contract), because old binaries run between migrate and restart. Rollback = redeploy previous binaries only. Never migrate down in production.
- **Locking:** golang-migrate takes its own single-bigint advisory lock during `up`. This is a different key encoding from the application's `(ClassID 60660, job)` two-int lock registry — no collision, no interaction.

### 2.2 Execution constraints (TimescaleDB × golang-migrate)

- The pgx/v5 driver executes each migration file as **one `Exec` over the simple protocol**, i.e. one implicit transaction per file. No `x-multi-statement` option is used. **Decision:** keep this default — each migration is atomic; a mid-file failure rolls the whole file back (golang-migrate then marks the version dirty, repaired via `v6ctl migrate force`).
- Consequences, all satisfied by construction in the files below:
  - The continuous aggregate is created **`WITH NO DATA`** — required for creation inside a transaction block (TimescaleDB documented constraint). Its first refresh comes from the `add_continuous_aggregate_policy` background job, never from the migration.
  - `refresh_continuous_aggregate()` is **never** called in a migration (it cannot run inside a transaction block).
  - `create_hypertable()`, `add_retention_policy()`, `add_continuous_aggregate_policy()` (functions) and `CALL add_columnstore_policy()` (procedure) are background-job registrations and run inside the migration's implicit transaction on TimescaleDB 2.28.
- `CREATE EXTENSION IF NOT EXISTS timescaledb` (top of 000001) requires `shared_preload_libraries = 'timescaledb'` in the server config — provisioning is an ops concern (09-ops.md); the dev/CI image `timescale/timescaledb:latest-pg18` has it preset.
- **Acceptance criterion (fixtures in 10-testing.md):** all three migrations apply green, in order, via `v6ctl migrate up` against a fresh `timescale/timescaledb:latest-pg18` container; the `proc_name`-filtered policy-job query in §4 returns exactly the **10** policy jobs defined in 000002 (**5 columnstore**, 4 retention, 1 cagg refresh) — the built-in telemetry job is excluded by that filter; a second `v6ctl migrate up` is a no-op.

---

## 3. Migration 000001 — base schema

Complete contents of `db/migrations/000001_base_schema.up.sql`. Statement order is dependency order: extensions → enums → `asn`/`country` → `domain` → campaign tables → resource tables → operational/editorial tables → stats tables → time-series tables (converted to hypertables in 000002).

```sql
-- =============================================================================
-- 000001_base_schema.up.sql — WhyNoIPv6 base schema
-- All DDL is owned by docs/spec/05-schema.md. Do not add DDL elsewhere.
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- -----------------------------------------------------------------------------
-- Enums
-- -----------------------------------------------------------------------------

-- Public status model: what the API/classification ever sees.
CREATE TYPE ipv6_status AS ENUM ('supported', 'unsupported', 'no_record', 'not_applicable');

-- Raw observation outcomes; internal only. 'partial', 'error' and 'inconsistent'
-- never reach public output (classification and the API read only the confirmed
-- ipv6_status columns, which remain 4-valued).
CREATE TYPE observation AS ENUM
  ('supported', 'partial', 'unsupported', 'no_record', 'not_applicable',
   'error', 'inconsistent');

CREATE TYPE domain_kind     AS ENUM ('apex', 'subdomain');
CREATE TYPE created_by      AS ENUM ('tranco', 'campaign', 'parent_link', 'live_check',
                                     'import');  -- 'import': phase-4 history import only
CREATE TYPE classification  AS ENUM ('unknown', 'inactive', 'sinner', 'partial', 'hero');
CREATE TYPE disabled_reason AS ENUM ('dead', 'service', 'manual', 'delisted');
CREATE TYPE resource_source AS ENUM ('discovered', 'manual');
CREATE TYPE check_job_status AS ENUM ('pending', 'processing', 'done', 'failed');

-- -----------------------------------------------------------------------------
-- Reference tables (must precede domain: asn_id/country_id are NOT NULL FKs)
-- -----------------------------------------------------------------------------

CREATE TABLE asn (
  id          INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  number      BIGINT NOT NULL UNIQUE,
  name        TEXT NOT NULL DEFAULT 'Unknown',
  count_total INT NOT NULL DEFAULT 0,   -- recomputed by the daily tick (06)
  count_v6    INT NOT NULL DEFAULT 0    -- recomputed by the daily tick (06)
);

CREATE TABLE country (
  id      INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  code    CHAR(2) NOT NULL UNIQUE,
  name    TEXT NOT NULL,
  tld     TEXT,                          -- dot-prefixed uppercase, e.g. '.NO'
  sites   INT NOT NULL DEFAULT 0,        -- recomputed by the daily tick (06)
  v6sites INT NOT NULL DEFAULT 0,        -- recomputed by the daily tick (06)
  percent NUMERIC(5,2) NOT NULL DEFAULT 0  -- (5,2): kills the legacy pgtype ÷10 hack
);

-- -----------------------------------------------------------------------------
-- domain — entity + confirmed state + frontier (one wide table, ~60 columns)
-- -----------------------------------------------------------------------------

CREATE TABLE domain (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  host          TEXT NOT NULL UNIQUE,          -- lowercase punycode FQDN (Canonicalize)
  kind          domain_kind NOT NULL DEFAULT 'apex',
  parent_id     BIGINT REFERENCES domain(id),  -- subdomain -> registrable parent
  rank          INT,                           -- Tranco rank; NULL = unranked
  created_by    created_by NOT NULL,

  -- Confirmed status per core dimension (NULL = never confirmed).
  -- One 5-column group per dimension d in {base, www, ns, mx, conn, resources}:
  --   d_status        confirmed value -> public
  --   d_observed      last raw observation (debug/telemetry only)
  --   d_pending       candidate value awaiting confirmation
  --   d_pending_count consecutive counted observations of d_pending
  --   d_since         when the confirmed value last changed
  base_status              ipv6_status,
  base_observed            observation,
  base_pending             ipv6_status,
  base_pending_count       SMALLINT NOT NULL DEFAULT 0,
  base_since               TIMESTAMPTZ,

  www_status               ipv6_status,
  www_observed             observation,
  www_pending              ipv6_status,
  www_pending_count        SMALLINT NOT NULL DEFAULT 0,
  www_since                TIMESTAMPTZ,

  ns_status                ipv6_status,
  ns_observed              observation,
  ns_pending               ipv6_status,
  ns_pending_count         SMALLINT NOT NULL DEFAULT 0,
  ns_since                 TIMESTAMPTZ,

  mx_status                ipv6_status,
  mx_observed              observation,
  mx_pending               ipv6_status,
  mx_pending_count         SMALLINT NOT NULL DEFAULT 0,
  mx_since                 TIMESTAMPTZ,

  conn_status              ipv6_status,
  conn_observed            observation,
  conn_pending             ipv6_status,
  conn_pending_count       SMALLINT NOT NULL DEFAULT 0,
  conn_since               TIMESTAMPTZ,

  resources_status         ipv6_status,
  resources_observed       observation,
  resources_pending        ipv6_status,
  resources_pending_count  SMALLINT NOT NULL DEFAULT 0,
  resources_since          TIMESTAMPTZ,

  -- Informational dimensions: latest observation only, no confirmation machinery.
  dnssec_observed  observation,
  ptr_observed     observation,
  smtp_observed    observation,
  parity_observed  observation,
  latency_v4_ms    INT,
  latency_v6_ms    INT,

  -- Materialized classification, recomputed on every confirmed commit (03).
  classification  classification NOT NULL DEFAULT 'unknown',
  class_flags     TEXT[] NOT NULL DEFAULT '{}',   -- broken_v6, www_missing, ns_missing,
                                                  -- mail_missing, resources_v4only
  gold            BOOLEAN NOT NULL DEFAULT FALSE, -- hero + all resources v6 (badge)

  asn_id      INT NOT NULL REFERENCES asn(id),     -- sentinel row when unknown (§6);
  country_id  INT NOT NULL REFERENCES country(id), --   no serializer ever handles NULL

  disabled        BOOLEAN NOT NULL DEFAULT FALSE,
  disabled_reason disabled_reason,
  disabled_at     TIMESTAMPTZ,

  -- Lifecycle bookkeeping (04)
  dead_streak       SMALLINT NOT NULL DEFAULT 0, -- consecutive unresolvable scans
  orphaned_at       TIMESTAMPTZ,                 -- linkage lost; starts the 30d delist grace
  last_requested_at TIMESTAMPTZ,                 -- last POST /check for this host

  -- Frontier / scheduling (04)
  next_check_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  claimed_at      TIMESTAMPTZ,                 -- worker lease; reclaim after 30 min
  last_checked_at TIMESTAMPTZ,
  last_counted_at TIMESTAMPTZ,                 -- last scan that advanced anti-flap counters (03)
  error_streak    SMALLINT NOT NULL DEFAULT 0, -- consecutive non-definitive base/www scans

  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_domain_host_trgm ON domain USING gin (host gin_trgm_ops); -- search
-- Claim-path index: leading range column = next_check_at, so the claim scans
-- ONLY the due set. Predicate must textually match the claim query (04).
CREATE INDEX idx_domain_due ON domain (next_check_at)
  WHERE NOT disabled OR disabled_reason IN ('dead', 'delisted');
CREATE INDEX idx_domain_rank    ON domain (rank) WHERE rank IS NOT NULL;
CREATE INDEX idx_domain_sinners ON domain (rank)
  WHERE classification = 'sinner' AND rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_heroes  ON domain (rank)
  WHERE classification = 'hero'   AND rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_partial ON domain (rank)
  WHERE classification = 'partial' AND rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_parent  ON domain (parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX idx_domain_country ON domain (country_id, classification, rank)
  WHERE rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_asn     ON domain (asn_id);

-- Table storage settings: the claim/commit cycle updates every active row >=2x/day —
-- claimed_at at claim, next_check_at + status columns at commit; the commit update
-- is always non-HOT because next_check_at is indexed.
ALTER TABLE domain SET (
  fillfactor = 90,
  autovacuum_vacuum_scale_factor = 0.02,
  autovacuum_analyze_scale_factor = 0.02
);

-- -----------------------------------------------------------------------------
-- Campaigns — membership, not duplication
-- -----------------------------------------------------------------------------

CREATE TABLE campaign (
  id          INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  uuid        UUID NOT NULL UNIQUE,              -- from YAML; API uses shortuuid encoding
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  source_file TEXT,                              -- provenance: YAML filename
  disabled    BOOLEAN NOT NULL DEFAULT FALSE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE campaign_domain (
  campaign_id INT NOT NULL REFERENCES campaign(id) ON DELETE CASCADE,
  domain_id   BIGINT NOT NULL REFERENCES domain(id),
  added_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (campaign_id, domain_id)
);
CREATE INDEX idx_campaign_domain_domain ON campaign_domain (domain_id);

-- -----------------------------------------------------------------------------
-- Resources (issue #23) — globally deduped host registry + dependency links
-- -----------------------------------------------------------------------------

CREATE TABLE resource_host (
  id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  host               TEXT NOT NULL UNIQUE,       -- lowercase punycode (Canonicalize)
  aaaa_status        ipv6_status,                -- confirmed (host-level N=2 anti-flap)
  aaaa_pending       ipv6_status,
  aaaa_pending_count SMALLINT NOT NULL DEFAULT 0,
  last_checked_at    TIMESTAMPTZ,
  next_check_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  dependent_count    INT NOT NULL DEFAULT 0      -- maintained on link/unlink (06)
);
CREATE INDEX idx_resource_host_due ON resource_host (next_check_at);

CREATE TABLE domain_resource (
  domain_id        BIGINT NOT NULL REFERENCES domain(id) ON DELETE CASCADE,
  resource_host_id BIGINT NOT NULL REFERENCES resource_host(id),
  source           resource_source NOT NULL,     -- discovered | manual
  required         BOOLEAN NOT NULL DEFAULT TRUE, -- manual entries can be advisory-only
  first_seen       TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen        TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (domain_id, resource_host_id)      -- operator add upgrades source to 'manual'
);
CREATE INDEX idx_domain_resource_host ON domain_resource (resource_host_id);
                                                 -- "who depends on X" (advocacy gold)

-- -----------------------------------------------------------------------------
-- Service-domain review queue (candidates, never auto-disable)
-- -----------------------------------------------------------------------------

CREATE TABLE service_candidate (
  domain_id   BIGINT PRIMARY KEY REFERENCES domain(id),
  reasons     TEXT[] NOT NULL,
  detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  dismissed   BOOLEAN NOT NULL DEFAULT FALSE
);

-- -----------------------------------------------------------------------------
-- On-demand live checks (07), SKIP LOCKED consumer with a claimed_at lease
-- -----------------------------------------------------------------------------

CREATE TABLE check_job (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  host         TEXT NOT NULL,                 -- validated, lowercase punycode
  requester_ip INET NOT NULL,
  status       check_job_status NOT NULL DEFAULT 'pending',
  claimed_at   TIMESTAMPTZ,                   -- consumer lease; reclaim after 5 min
  result       JSONB,                         -- shared-mapper output (07)
  error        TEXT,                          -- set when status = 'failed'
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ
);
CREATE INDEX idx_check_job_claim ON check_job (created_at)
  WHERE status IN ('pending','processing');                         -- claim + reaper
CREATE INDEX idx_check_job_requester ON check_job (requester_ip, created_at); -- rate limiting
CREATE INDEX idx_check_job_host_done ON check_job (host, completed_at DESC)
  WHERE status = 'done';                                            -- host-side dedupe

-- -----------------------------------------------------------------------------
-- Editorial picks (written only by v6ctl shame + the phase-4 importer)
-- -----------------------------------------------------------------------------

CREATE TABLE top_shame (
  domain_id BIGINT PRIMARY KEY REFERENCES domain(id),
  reason    TEXT,
  added_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- -----------------------------------------------------------------------------
-- Tranco import provenance
-- -----------------------------------------------------------------------------

CREATE TABLE tranco_import (
  id              INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  list_id         TEXT NOT NULL,
  list_date       DATE NOT NULL,
  line_count      INT,                        -- raw CSV lines
  imported_count  INT,                        -- rows in the upsert insert
  delisted        INT,                        -- ranked yesterday, absent today
  rejected_count  INT,                        -- failed Canonicalize/validation
  duplicate_count INT,                        -- staging fold count (>0 is normal)
  aborted         BOOLEAN NOT NULL DEFAULT FALSE,  -- import sanity guard
  note            TEXT,                       -- abort reason
  imported_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- One successful import per list; abort rows may repeat (a --force retry of an
-- aborted list must still be able to record its success). Target of the
-- idempotent-write guard's ON CONFLICT (list_id) WHERE NOT aborted DO NOTHING (06).
CREATE UNIQUE INDEX idx_tranco_import_list ON tranco_import (list_id) WHERE NOT aborted;

-- -----------------------------------------------------------------------------
-- Product stats snapshots (public, API-served, forever). Snapshots of CONFIRMED
-- state, written by the daily tick (06); serve the /metric and /stats endpoints (07).
-- Plain tables except stats_asn_daily (~50-80k rows/day -> hypertable in 000002).
-- -----------------------------------------------------------------------------

CREATE TABLE stats_global_daily (
  day DATE PRIMARY KEY,
  domains INT, sinners INT, partial INT, heroes INT, gold INT, inactive INT,
  unknown INT, disabled INT,
  base_supported INT, www_supported INT, ns_supported INT, mx_supported INT,
  conn_supported INT, resources_supported INT,
  top_heroes INT,        -- Tranco top-1000 with web-facing IPv6
  top_nameserver INT     -- Tranco top-1000 with IPv6-capable nameservers
);

CREATE TABLE stats_country_daily (
  day DATE, country_id INT, domains INT, sinners INT, partial INT, heroes INT,
  base_supported INT, conn_supported INT,
  PRIMARY KEY (day, country_id)
);

CREATE TABLE stats_campaign_daily (
  day DATE, campaign_id INT, domains INT, v6_ready INT, sinners INT, partial INT,
  heroes INT, base_supported INT, www_supported INT, ns_supported INT,
  mx_supported INT, conn_supported INT,
  PRIMARY KEY (day, campaign_id)
);

CREATE TABLE stats_asn_daily (
  day TIMESTAMPTZ NOT NULL, asn_id INT NOT NULL,
  domains INT, v6_domains INT, sinners INT, heroes INT,
  PRIMARY KEY (asn_id, day)
);

-- -----------------------------------------------------------------------------
-- Time-series tables (hypertable conversion + policies in 000002; no FKs)
-- -----------------------------------------------------------------------------

-- Slim per-scan history: typed statuses only. Long retention; drives per-domain graphs.
CREATE TABLE scan (
  domain_id  BIGINT NOT NULL,
  ts         TIMESTAMPTZ NOT NULL DEFAULT now(),
  base observation NOT NULL, www observation NOT NULL, ns observation NOT NULL,
  mx observation NOT NULL, conn observation NOT NULL, resources observation NOT NULL,
  dnssec observation, ptr observation, smtp observation, parity observation,
  latency_v4_ms INT, latency_v6_ms INT,
  classification classification NOT NULL,     -- confirmed class stamped at scan time
  country_id INT, asn_id INT,                 -- denormalized: caggs can't track JOINs
  PRIMARY KEY (domain_id, ts)
);
-- No extra (domain_id, ts DESC) index: the primary key (domain_id, ts) already
-- serves backward per-domain scans; an additional index is pure write/storage overhead.

-- Fat scan payload: full engine Details JSONB (per-check evidence, record sets,
-- TLS cert info, resource host lists). Short retention; debugging + detail page.
-- tls and spf deliberately have NO typed columns anywhere: informational-only,
-- they live exclusively in details JSONB (accepted design, not an omission).
CREATE TABLE scan_detail (
  domain_id   BIGINT NOT NULL,
  ts          TIMESTAMPTZ NOT NULL,
  details     JSONB NOT NULL,
  duration_ms INT,
  PRIMARY KEY (domain_id, ts)
);
-- No result_id column: idempotency is the worker-fixed timestamp T +
-- ON CONFLICT (domain_id, ts) DO NOTHING under the PK (03); no unique constraint
-- on a synthetic id is possible on a hypertable and nothing consumes it.

-- Structured field-level changelog. FOREVER — the credibility surface.
CREATE TABLE changelog (
  domain_id BIGINT NOT NULL,
  ts        TIMESTAMPTZ NOT NULL DEFAULT now(),
  field     TEXT NOT NULL,                     -- base|www|ns|mx|conn|resources|legacy
  old_value ipv6_status,                       -- never NULL on native rows: first
                                               --   confirmation writes no row at all (03);
                                               --   NULL only for field='legacy' import rows
  new_value ipv6_status,                       -- NULL only on field='legacy' rows
  legacy_message TEXT,                         -- verbatim production message (field='legacy' only)
  legacy_status  TEXT,                         -- verbatim production ipv6_status (field='legacy' only)
  PRIMARY KEY (domain_id, ts, field),
  CONSTRAINT changelog_legacy_chk
    CHECK ( (field = 'legacy') = (legacy_message IS NOT NULL) ),
  CONSTRAINT changelog_old_value_chk
    CHECK ( field = 'legacy' OR old_value IS NOT NULL ),
  CONSTRAINT changelog_new_value_chk
    CHECK ( field = 'legacy' OR new_value IS NOT NULL )
);
-- field='legacy' is the import escape hatch for unmappable production rows (08);
-- native (post-cutover) rows never set the legacy columns.
CREATE INDEX idx_changelog_ts ON changelog (ts DESC);  -- global recent-changes feed
-- (The PK leads with domain_id and cannot serve the sitewide GET /changelog feed.)

-- Operational crawler metrics (Grafana only). Checkpoint rows per run.
CREATE TABLE crawler_metrics (
  ts TIMESTAMPTZ NOT NULL DEFAULT now(),
  run_id UUID NOT NULL, worker TEXT NOT NULL,
  processed INT, succeeded INT, failed INT, qps REAL,
  p50_ms INT, p99_ms INT, active_slots INT, queue_depth INT,
  dim_counters JSONB,                          -- per-dimension tallies; includes the
                                               --   lease_lost counter (03 fence aborts)
  is_final BOOLEAN NOT NULL DEFAULT FALSE
);

-- Unbound resolver metrics (Grafana only), one row/min/host from
-- `v6ctl ops unbound-stats` parsing `unbound-control stats` (resetting variant,
-- so every row holds per-interval deltas). Mechanism: 09-ops.md.
CREATE TABLE unbound_stats (
  ts                    TIMESTAMPTZ NOT NULL DEFAULT now(),
  host                  TEXT NOT NULL,
  num_queries           BIGINT,
  cache_hits            BIGINT,
  cache_miss            BIGINT,
  rcode_servfail        BIGINT,
  rcode_nxdomain        BIGINT,
  recursion_time_avg_ms REAL,        -- total.recursion.time.avg * 1000
  requestlist_avg       REAL,
  raw                   JSONB        -- full stats dump for ad-hoc panels
);
```

**Decision:** the design's anonymous `CREATE INDEX ON <table> (...)` statements are given explicit names here (`idx_campaign_domain_domain`, `idx_resource_host_due`, `idx_domain_resource_host`, `idx_check_job_claim`, `idx_check_job_requester`, `idx_check_job_host_done`) so that plan gates, monitoring queries, and `DROP INDEX` statements can reference them deterministically. Definitions are otherwise byte-equivalent to the design.

Complete contents of `db/migrations/000001_base_schema.down.sql` (dev-only; production is forward-only):

```sql
-- 000001_base_schema.down.sql — dev-only full teardown (reverse dependency order).
DROP TABLE IF EXISTS unbound_stats;
DROP TABLE IF EXISTS crawler_metrics;
DROP TABLE IF EXISTS changelog;
DROP TABLE IF EXISTS scan_detail;
DROP TABLE IF EXISTS scan;
DROP TABLE IF EXISTS stats_asn_daily;
DROP TABLE IF EXISTS stats_campaign_daily;
DROP TABLE IF EXISTS stats_country_daily;
DROP TABLE IF EXISTS stats_global_daily;
DROP TABLE IF EXISTS tranco_import;
DROP TABLE IF EXISTS top_shame;
DROP TABLE IF EXISTS check_job;
DROP TABLE IF EXISTS service_candidate;
DROP TABLE IF EXISTS domain_resource;
DROP TABLE IF EXISTS resource_host;
DROP TABLE IF EXISTS campaign_domain;
DROP TABLE IF EXISTS campaign;
DROP TABLE IF EXISTS domain;
DROP TABLE IF EXISTS country;
DROP TABLE IF EXISTS asn;
DROP TYPE IF EXISTS check_job_status;
DROP TYPE IF EXISTS resource_source;
DROP TYPE IF EXISTS disabled_reason;
DROP TYPE IF EXISTS classification;
DROP TYPE IF EXISTS created_by;
DROP TYPE IF EXISTS domain_kind;
DROP TYPE IF EXISTS observation;
DROP TYPE IF EXISTS ipv6_status;
-- Extensions are deliberately NOT dropped (shared server objects).
```

---

## 4. Migration 000002 — hypertables, columnstore, retention, continuous aggregate

Complete contents of `db/migrations/000002_timescaledb.up.sql`. Grouped per table: conversion → columnstore setting → columnstore policy → retention policy; the continuous aggregate last (it requires the `scan` hypertable).

```sql
-- =============================================================================
-- 000002_timescaledb.up.sql — hypertable conversions, policies, cagg
-- Modern columnstore API only (enable_columnstore / orderby /
-- CALL add_columnstore_policy). segmentby is deliberately unset on every
-- columnstore table (1 row/domain/day would collapse compression); because
-- orderby is set explicitly, TimescaleDB >= 2.20 does not auto-select a
-- segmentby (PR #8033).
-- =============================================================================

-- scan: ~1M slim rows/day, 2y retention (single-digit GB compressed).
SELECT create_hypertable('scan', by_range('ts', INTERVAL '7 days'));
ALTER TABLE scan SET (timescaledb.enable_columnstore,
                      timescaledb.orderby = 'domain_id, ts DESC');
CALL add_columnstore_policy('scan', after => INTERVAL '14 days');
SELECT add_retention_policy('scan', drop_after => INTERVAL '2 years');

-- scan_detail: 1-2 GB/day raw JSONB, 90d retention (~15-40 GB compressed).
SELECT create_hypertable('scan_detail', by_range('ts', INTERVAL '1 day'));
ALTER TABLE scan_detail SET (timescaledb.enable_columnstore,
                             timescaledb.orderby = 'domain_id, ts DESC');
CALL add_columnstore_policy('scan_detail', after => INTERVAL '3 days');
SELECT add_retention_policy('scan_detail', drop_after => INTERVAL '90 days');

-- changelog: ~1-3k rows/day, kept FOREVER — no retention policy.
SELECT create_hypertable('changelog', by_range('ts', INTERVAL '30 days'));
ALTER TABLE changelog SET (timescaledb.enable_columnstore,
                           timescaledb.orderby = 'ts DESC, domain_id');
CALL add_columnstore_policy('changelog', after => INTERVAL '60 days');

-- crawler_metrics: Grafana-only checkpoints; no columnstore (tiny), 90d retention.
SELECT create_hypertable('crawler_metrics', by_range('ts', INTERVAL '7 days'));
SELECT add_retention_policy('crawler_metrics', drop_after => INTERVAL '90 days');

-- unbound_stats: ~1,440 rows/day/host; no columnstore, 30d retention.
SELECT create_hypertable('unbound_stats', by_range('ts', INTERVAL '7 days'));
SELECT add_retention_policy('unbound_stats', drop_after => INTERVAL '30 days');

-- stats_asn_daily: ~50-80k rows/day, kept forever — no retention policy.
SELECT create_hypertable('stats_asn_daily', by_range('day', INTERVAL '90 days'));
ALTER TABLE stats_asn_daily SET (timescaledb.enable_columnstore,
                                 timescaledb.orderby = 'asn_id, day DESC');
CALL add_columnstore_policy('stats_asn_daily', after => INTERVAL '180 days');

-- ---------------------------------------------------------------------------
-- Observed-adoption continuous aggregate (Grafana + research; NOT public stats).
-- Measurement-flavored: counts observations over ALL scanned entities,
-- unfiltered — deliberately NOT comparable to the confirmed-state stats_* tables
-- (DICTIONARY.md must state this; see the datasets section of 07).
-- WITH NO DATA is required to create a cagg inside the migration's implicit
-- transaction; the policy below performs the first refresh.
-- ---------------------------------------------------------------------------
CREATE MATERIALIZED VIEW scan_daily_adoption
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 day', ts) AS day, country_id,
       count(*) AS scanned,
       count(*) FILTER (WHERE base = 'supported') AS base_v6,
       count(*) FILTER (WHERE conn = 'supported') AS conn_v6,
       count(*) FILTER (WHERE classification = 'hero')   AS heroes,
       count(*) FILTER (WHERE classification = 'sinner') AS sinners
FROM scan GROUP BY 1, 2 WITH NO DATA;
-- Stable policy API only (timescaledb_experimental.add_policies is early-access,
-- SELECT-invoked, and has no schedule-interval parameter — do not use it).
-- Ordering rule: cagg refresh start_offset (3d) < scan retention (2y). OK.
-- Real-time aggregation stays off (default since TS 2.13) — yesterday's data is fine.
SELECT add_continuous_aggregate_policy('scan_daily_adoption',
  start_offset      => INTERVAL '3 days',
  end_offset        => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 hour');
ALTER MATERIALIZED VIEW scan_daily_adoption
  SET (timescaledb.enable_columnstore,
       timescaledb.orderby = 'day DESC, country_id');
CALL add_columnstore_policy('scan_daily_adoption', after => INTERVAL '90 days');
```

Notes (normative):

- The auto-columnstore-policy convenience of TimescaleDB ≥ 2.23 applies only to the `CREATE TABLE ... WITH (tsdb.enable_columnstore)` form. The `ALTER TABLE ... SET` form used here creates **no** automatic policy; the explicit `CALL add_columnstore_policy` statements above are the complete policy set. Verification query (also used by the 10-testing migration fixture): `SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name IN ('policy_compression','policy_retention','policy_refresh_continuous_aggregate')` = **10** — **5** columnstore (one each on `scan`, `scan_detail`, `changelog`, `stats_asn_daily`, and the `scan_daily_adoption` materialization hypertable; columnstore-policy jobs keep the legacy `proc_name = 'policy_compression'`), 4 retention (`scan`, `scan_detail`, `crawler_metrics`, `unbound_stats`), and 1 cagg refresh (`scan_daily_adoption`). The `proc_name` filter excludes the built-in TimescaleDB telemetry job, which is always present in `timescaledb_information.jobs`.
- `crawler_metrics` and `unbound_stats` intentionally get no columnstore settings (short retention, trivial volume, Grafana reads recent rows).
- Every PK on a hypertable includes the partition column (`scan`/`scan_detail`: `(domain_id, ts)`; `changelog`: `(domain_id, ts, field)`; `stats_asn_daily`: `(asn_id, day)`) — a TimescaleDB requirement; `crawler_metrics` and `unbound_stats` have no PK by design.

Complete contents of `db/migrations/000002_timescaledb.down.sql` (dev-only):

```sql
-- 000002_timescaledb.down.sql — dev-only. Drops the cagg so 000001.down can drop
-- the scan hypertable. Hypertable conversions and policies are not individually
-- reversed: policies and chunks are destroyed with their tables in 000001.down;
-- a full dev reset is `dropdb`.
DROP MATERIALIZED VIEW IF EXISTS scan_daily_adoption;
```

---

## 5. Migration 000003 — seed data

Seeds **static reference data only**: the sentinel ASN, the country list, and the day-0 stats rows. It deliberately seeds **no** `domain`, `campaign`, or `top_shame` rows — `top_shame.domain_id` FK requires phase-1 Tranco ingestion; `top_shame` population is the phase-4 importer (08) plus `v6ctl shame add` thereafter.

Complete contents of `db/migrations/000003_seed.up.sql`:

```sql
-- =============================================================================
-- 000003_seed.up.sql — reference data seeds
-- Sentinels land with the asn/country seed data, BEFORE any domain row can
-- exist (domain.asn_id / domain.country_id are NOT NULL FKs). IDENTITY assigns
-- ids; binaries resolve sentinel ids once at startup by lookup
-- (asn.number = 0, country.code = 'UN'), never by literal id.
-- =============================================================================

-- Sentinel ASN (attribution fallback; appears in /metric/asn as in production).
INSERT INTO asn (number, name) VALUES (0, 'Unknown');

-- Country reference data: 251 rows, including the sentinel
-- (code 'UN', name 'Unknown', tld '.UN'). Lifted from production
-- whynoipv6/db/migrations/02_data.up.sql with this exact transform:
--   (id, country_name, country_code, country_tld, continent)
--   -> (name = country_name, code = country_code, tld = country_tld);
--   id and continent are dropped (IDENTITY assigns ids; the new schema has no
--   continent column). Row count MUST be 251 and MUST include the 'UN' sentinel.
INSERT INTO country (name, code, tld) VALUES
('Georgia', 'GE', '.GE'),
('Afghanistan', 'AF', '.AF'),
('Åland Islands', 'AX', '.AX'),
-- ... [all remaining rows from production 02_data.up.sql, same transform,
--      in file order, ending with:] ...
('Unknown', 'UN', '.UN');

-- Day-0 stats rows: the /metric/overview endpoint (07) reads the latest
-- stats_global_daily row and MUST always find one, even on first boot before
-- the first nightly snapshot.
INSERT INTO stats_global_daily (
  day, domains, sinners, partial, heroes, gold, inactive, unknown, disabled,
  base_supported, www_supported, ns_supported, mx_supported, conn_supported,
  resources_supported, top_heroes, top_nameserver)
VALUES (CURRENT_DATE, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
ON CONFLICT (day) DO NOTHING;
```

The elided country rows are **not optional**: the shipped migration file contains all 251 `(name, code, tld)` tuples verbatim (source: `/…/lasseh/whynoipv6/db/migrations/02_data.up.sql`, lines 5–255). The 10-testing migration fixture asserts `SELECT count(*) FROM country` = 251 and that both sentinels resolve.

**Decision (day-0 stats semantics).** The design requires the seed migration to "write day-0 rows for all stats_* tables" so `/metric/overview` always has a row. Day-0 rows are defined as *the output of the nightly snapshot job run against the freshly migrated (empty) database*: one all-zeros `stats_global_daily` row for `CURRENT_DATE`, and **zero rows** for the keyed tables (`stats_country_daily`, `stats_campaign_daily`, `stats_asn_daily`) — the snapshot job writes keyed rows only for keys with members, and an empty database has none. `/metric/overview` is the only endpoint requiring a guaranteed row; the keyed endpoints serve arrays and tolerate empty series. At phase-4 cutover, `v6ctl stats recalc` (06) overwrites the day-0 row with real values — safe because every stats insert is `ON CONFLICT (<pk>) DO UPDATE`.

Complete contents of `db/migrations/000003_seed.down.sql` (dev-only; assumes no dependent data — fails on FK violations by design):

```sql
-- 000003_seed.down.sql — dev-only.
DELETE FROM stats_global_daily;
DELETE FROM country;
DELETE FROM asn;
```

---

## 6. Seed-data consumers (context, non-DDL)

- **Sentinels.** `asn (number 0, name 'Unknown')` and `country (code 'UN', name 'Unknown', tld '.UN')` are the attribution fallbacks: entity insert (Tranco import, campaign sync, live-check row creation) runs attribution with no input IP, yielding ccTLD-or-sentinel country and sentinel ASN, so `domain.asn_id`/`domain.country_id` are never NULL and no serializer ever handles NULL. The crawler resolves sentinel ids **once at startup by lookup** (`SELECT id FROM asn WHERE number = 0`; `SELECT id FROM country WHERE code = 'UN'`), never by literal id. Both sentinels appear in `/metric/asn` and `/country` listings exactly as in production (the frontend already renders them).
- **`country.tld` matching.** Stored dot-prefixed uppercase (`'.NO'`); the crawler's ccTLD attribution normalizes its probe to `"." + strings.ToUpper(label)` before comparing (algorithm in 03's attribution section).
- **New ASNs** are auto-registered by the crawler at scan commit (`INSERT (number, name) ON CONFLICT (number) DO NOTHING`, then re-read); existing names are never updated on later scans. The `asn` table therefore grows organically from the seed of one sentinel row.

---

## 7. Ephemeral DDL — the Tranco staging table

**Decision (name + form pinned).** The Tranco import (06) uses a session-scoped temporary staging table so the 1M-row upsert is one set-based statement. Its definition is owned here; 06 quotes only DML against it:

```sql
CREATE TEMPORARY TABLE tranco_staging (
  rank INT  NOT NULL,
  host TEXT NOT NULL          -- already canonicalized in Go; garbage lines dropped
) ON COMMIT DROP;
```

Created inside the import transaction (`ON COMMIT DROP` guarantees cleanup on both commit and abort paths); loaded via `pgx` `CopyFrom`; the insert's source SELECT dedupes with `DISTINCT ON (host) … ORDER BY host, rank ASC` (lowest rank wins the canonicalization fold) — full import algorithm and upsert SQL in 06. This is the **only** DDL statement any binary executes at runtime.

---

## 8. Enum registry (semantics summary)

| Enum | Values | Visibility |
|---|---|---|
| `ipv6_status` | `supported`, `unsupported`, `no_record`, `not_applicable` | Public status model. The only status type the API and classification read. Stored in `domain.*_status`/`*_pending`, `resource_host.aaaa_*`, `changelog.old_value`/`new_value`. |
| `observation` | `supported`, `partial`, `unsupported`, `no_record`, `not_applicable`, `error`, `inconsistent` | Internal only. Raw per-scan outcomes (`scan.*`, `domain.*_observed`). `partial`, `error`, `inconsistent` never reach public output. `partial` exists for the informational `parity`/`ptr` dimensions. |
| `domain_kind` | `apex`, `subdomain` | Tranco contributes only `apex`; campaign YAML entries may be subdomains (PSL split at import time, 06). |
| `created_by` | `tranco`, `campaign`, `parent_link`, `live_check`, `import` | Origin audit. `import` is used only by the phase-4 history importer (08). |
| `classification` | `unknown`, `inactive`, `sinner`, `partial`, `hero` | Materialized output of the deterministic ladder (03). No grades, no scores. |
| `disabled_reason` | `dead`, `service`, `manual`, `delisted` | Lifecycle table in 04. `dead`/`delisted` stay claimable on the slow lane (the `idx_domain_due` predicate admits them); `service`/`manual` leave the frontier entirely. |
| `resource_source` | `discovered`, `manual` | `manual` links are never pruned; operator add upgrades `discovered`→`manual` (06). |
| `check_job_status` | `pending`, `processing`, `done`, `failed` | Live-check job lifecycle (07). |

Enum evolution rule: adding values is `ALTER TYPE … ADD VALUE` in a future migration (expand/contract-safe); values are never removed or renamed.

---

## 9. `updated_at` maintenance rule (normative, application-side)

Two tables carry `updated_at`: `domain` and `campaign`. There are **no triggers**; maintenance is application-side:

- **Rule:** every `UPDATE` statement against `domain` or `campaign` sets `updated_at = now()` — with exactly **one exception**: the frontier claim's lease stamp (`UPDATE domain SET claimed_at = now() WHERE id IN (…)`, 04) deliberately omits it. `claimed_at` is unindexed precisely so the lease stamp can be a HOT update; touching `updated_at` there would also corrupt the legacy `ts_updated` API field (which maps `domain.updated_at`, 07) into a claim timestamp. **Decision:** the design demonstrates `updated_at = now()` in the commit UPDATE, the Tranco upsert, and campaign sync; this rule generalizes it to *all* non-claim writers (lifecycle sweep, `v6ctl disable`, etc.) as the simplest consistent form.
- Write paths bound by the rule (each quotes its own DML): the fenced commit UPDATE (03-state-machine.md §12.1 — Statement 1), the Tranco upsert and lifecycle sweep (06), campaign sync enable/disable/rename updates (06), and every `v6ctl` verb that mutates these tables.
- `INSERT` paths rely on the column defaults (`DEFAULT now()`); no explicit value needed.
- 10-testing.md carries a repository-level test asserting the claim query leaves `updated_at` unchanged while the commit bumps it.

---

## 10. sqlc configuration and data-access layout

### 10.1 `sqlc.yaml` (repo root)

```yaml
version: "2"
sql:
  - schema:
      - "db/migrations/000001_base_schema.up.sql"
      - "db/migrations/000002_timescaledb.up.sql"
      - "db/migrations/000003_seed.up.sql"
    queries: "db/query"
    engine: "postgresql"
    gen:
      go:
        package: "db"
        sql_package: "pgx/v5"
        out: "internal/postgres/db"
        emit_json_tags: true
        emit_interface: true
        emit_empty_slices: true
        emit_exported_queries: true
        emit_pointers_for_null_types: true
```

**Decision:** the schema list enumerates the three `.up.sql` files explicitly (not the directory) so sqlc never parses the `.down.sql` files — lexical directory ordering would feed sqlc each `NNN_*.down.sql` *before* its `.up.sql`. **000002 is deliberately in the schema set, not just 000001+000003**: the `scan_daily_adoption` continuous aggregate is created only in 000002, and `db/query/stats.sql` reads from it (§10.2), so sqlc needs 000002 to type those rows — excluding it would break `sqlc generate`. The TimescaleDB extension DDL in 000002 is verified to parse clean under sqlc's embedded pg_query engine (v2 config, current sqlc release): the `SELECT create_hypertable(...)` / `add_retention_policy(...)` calls, the dotted-namespace reloptions in `ALTER TABLE … SET (timescaledb.enable_columnstore, timescaledb.orderby = …)`, the `CALL add_columnstore_policy(…)` procedure calls, and the `CREATE MATERIALIZED VIEW … WITH (timescaledb.continuous) AS … WITH NO DATA` continuous aggregate are all accepted as ordinary PostgreSQL syntax, and sqlc infers the cagg's column types from its `SELECT` list over `scan` — satisfying acceptance criterion 4 (§13). Flags are the v2 rebuild's proven set carried forward: `emit_empty_slices` supports the API's `[]`-instead-of-`null` cleanup (07); `emit_exported_queries` exports the SQL string constants so the commit adapter can assemble mixed-statement `pgx.Batch` units (below); `emit_pointers_for_null_types` gives `*T` for nullable columns instead of `pgtype` wrappers.

### 10.2 `db/query/` file layout

One file per subject area. This file owns only the layout; the query contents are owned by the files in parentheses and must carry sqlc annotations (`-- name: X :one|:many|:exec|:execrows|:copyfrom`):

| File | Queries for | Owned by |
|---|---|---|
| `db/query/domain.sql` | frontier claim, commit UPDATE, entity lookups/ensure, list endpoints (sinners/heroes/partial/almost), search, subdomains, disable/enable | 03, 04, 06, 07 |
| `db/query/scan.sql` | scan + scan_detail inserts, per-domain history reads, latest-detail read | 03, 07 |
| `db/query/changelog.sql` | changelog inserts, the five legacy feeds, dataset reads | 03, 07 |
| `db/query/campaign.sql` | campaign list/detail/search, membership upserts, sync soft-delete | 06, 07 |
| `db/query/resource.sql` | resource_host upsert/claim/confirm, domain_resource link/prune, dependents | 06, 07 |
| `db/query/check_job.sql` | enqueue, claim (SKIP LOCKED), complete/fail, reaper, purge, rate-limit counts, host dedupe | 07 |
| `db/query/stats.sql` | the four `stats_*` snapshot upserts, stats read endpoints, `scan_daily_adoption` reads | 06, 07 |
| `db/query/country.sql` | country list, counter recompute, sentinel lookup | 06, 07 |
| `db/query/asn.sql` | asn ensure/lookup, metric list/search, counter recompute, sentinel lookup | 03, 06, 07 |
| `db/query/tranco.sql` | tranco_import provenance insert/reads, staleness probe, delist UPDATE | 06 |
| `db/query/shame.sql` | top_shame upsert/delete/list, topsinner read | 06, 07 |
| `db/query/service_candidate.sql` | candidate upsert (`ON CONFLICT DO NOTHING`), list/confirm/dismiss | 06 |
| `db/query/metrics.sql` | crawler_metrics checkpoint insert, unbound_stats insert | 04, 09-ops.md |

The Tranco staging DML (`CREATE TEMPORARY TABLE` excepted — it is executed as a literal statement, §7) and the `DISTINCT ON` upsert live in `db/query/tranco.sql` where sqlc can type them; if sqlc cannot parse a temp-table statement, that single statement is embedded as a Go constant in the ingest adapter — the SQL text is still the one quoted in 06.

### 10.3 Package layout and the batch-commit pattern

```
internal/repository/   port interfaces (consumer-defined; one interface per service need)
internal/postgres/     hand-written adapters implementing repository interfaces
internal/postgres/db/  sqlc output (generated; never hand-edited; regenerated by `sqlc generate`)
```

- Layering (locked, from the design): `domain` ← `repository` ← {`postgres`, `service`, `crawler`} ← {`api`, `cmd`}. The crawler depends only on interfaces.
- **Batch-commit pattern (03's transaction, stated here because it shapes the generated-code usage):** each domain's commit unit — fenced `domain` UPDATE, 0–6 `changelog` inserts, `scan` insert, `scan_detail` insert — is queued as **one `pgx.Batch` inside one `pgx.Tx`** (single round trip). The adapter assembles the batch from the **exported sqlc query constants** (`emit_exported_queries`); sqlc's own `:batchexec` annotation is not used for this (it cannot mix different statements in one batch). If `RowsAffected` of the fenced UPDATE is 0, the transaction is rolled back and nothing is written (lease fence, 03).
- Integration tests for `internal/postgres` (every sqlc query + the claim/commit transaction) run against dockerized Postgres+TimescaleDB via the embedded migrations — fixture plan in 10-testing.md.

---

## 11. Table consumer map

Every table, its writers, its readers, and the spec files that quote DML against it. "Tick" = the 03:30 UTC daily tick (06).

| Table | Written by | Read by | Spec files |
|---|---|---|---|
| `asn` | seed (§5); crawler attribution auto-register (03); tick counter recompute (06) | API `/metric/asn*` (07); crawler sentinel lookup | 03, 06, 07 |
| `country` | seed (§5); tick counter recompute (06) | API `/country*` (07); crawler ccTLD attribution (03) | 03, 06, 07 |
| `domain` | Tranco upsert + lifecycle sweep (06); claim stamp + scan commit (04, 03); campaign sync entity-ensure (06); live-check `last_requested_at` touch (07); `v6ctl` disable/enable | claim query (04); all public list/detail/search endpoints + badge (07); stats snapshot + candidate detection (06); datasets (07) | 03, 04, 06, 07 |
| `campaign` | campaign sync (06) | campaign endpoints (07); lifecycle sweep linkage (06) | 06, 07 |
| `campaign_domain` | campaign sync membership diff (06) | campaign endpoints, campaign changelog/scan joins (07); lifecycle sweep linkage (06); stats snapshot (06) | 06, 07 |
| `resource_host` | discovery upsert (inside scan commit, 03/06); sweep worker confirm (06) | roll-up read (03); `/domain/{domain}/resources`, `/resource/{host}/dependents` (07); service-candidate heuristic (06) | 03, 06, 07 |
| `domain_resource` | discovery link/refresh/prune (06); `v6ctl resource add/remove` | roll-up read (03); resource endpoints (07) | 03, 06, 07 |
| `service_candidate` | tick candidate detection (`ON CONFLICT DO NOTHING`, 06); `v6ctl service-candidates confirm/dismiss` | `v6ctl service-candidates list`; weekly webhook digest (09-ops.md) | 06 |
| `check_job` | `POST /check` enqueue (07); consumer claim/complete (07); tick 30d purge (06) | `GET /check/{id}` (07); reaper + rate-limit counts (07) | 06, 07 |
| `top_shame` | `v6ctl shame add/remove`; phase-4 importer (08) | `GET /domain/topsinner` (07) | 07, 08 |
| `tranco_import` | import provenance insert (06) | unchanged-list short-circuit, staleness alert (06); `v6ctl tranco status` | 06 |
| `stats_global_daily` | seed day-0 row (§5); tick snapshot upsert; `v6ctl stats recalc` (06) | `GET /metric/overview`, `GET /stats/overview` (07) | 06, 07 |
| `stats_country_daily` | tick snapshot upsert (06) | `GET /stats/country/{code}` (07) | 06, 07 |
| `stats_campaign_daily` | tick snapshot upsert (06; skips disabled campaigns) | `GET /stats/campaign/{uuid}` (07) | 06, 07 |
| `stats_asn_daily` | tick snapshot upsert (06) | `GET /stats/asn/{number}` (07) | 06, 07 |
| `scan` | scan commit insert (03); phase-4 90d history import (08) | per-domain graphs `/stats/domain/{domain}` + `/domain/{domain}/log` (07); `scan_daily_adoption` cagg; datasets (07) | 03, 07, 08 |
| `scan_detail` | scan commit insert (03) | detail page latest-row read (07); debugging | 03, 07 |
| `changelog` | scan commit insert (03); phase-4 history import (08) | five legacy `/changelog*` feeds (07); campaign changelog joins (07); datasets | 03, 07, 08 |
| `crawler_metrics` | worker checkpoint rows every 1000 domains + idle checkpoints (04) | Grafana panels + alert rules A1/A3 (09-ops.md) | 04, 09-ops.md |
| `unbound_stats` | `v6ctl ops unbound-stats` timer (09-ops.md) | Grafana + alert rule A5 (09-ops.md) | 09-ops.md |
| `scan_daily_adoption` (cagg) | TimescaleDB refresh policy | Grafana; research datasets (07) | 07, 09-ops.md |
| `tranco_staging` (temp, §7) | import `CopyFrom` (06) | import upsert source SELECT (06) | 06 |

---

## 12. Column cross-check inventory

Columns whose existence is mandated by exactly one consumer elsewhere in the spec — listed so an implementer (or verifier) can confirm nothing was dropped in the merge of the design's schema deltas:

- `domain.last_counted_at` — the 12h `min_confirm_spacing` counting gate (03).
- `domain.error_streak` — non-definitive base/www backoff, `next_check_at = now() + min(lane × 2^(error_streak−1), recheck_backoff_max)` (04).
- `domain.dead_streak` — unresolvable-scan counter feeding the `disabled_reason='dead'` trigger at `lifecycle.dead_streak` (03, 04). Supersedes any `nxdomain_streak` naming from earlier drafts.
- `domain.orphaned_at` — 30-day delist grace; set/cleared ONLY by the lifecycle sweep (06), never by Tranco import or campaign sync.
- `domain.last_requested_at` — 7-day live-check frontier linkage (`lifecycle.live_check_linkage`), touched by `POST /check` (07), read by the sweep (06).
- `domain.claimed_at` — lease fence token; deliberately **unindexed** so lease stamping is HOT (04).
- `domain.class_flags` — `broken_v6`, `www_missing`, `ns_missing`, `mail_missing`, `resources_v4only`; TEXT[] rather than an enum array so flags can be added without migration (03 owns the truth table).
- `domain.gold` — badge + stats `gold` counter; false for all domains until `crawler.resources.enabled` flips at phase 5 (registry: 09-ops.md).
- `domain.updated_at` — serialized as legacy `ts_updated`; `domain.last_checked_at` as `ts_check`; `*_since` as `ts_aaaa`/`ts_www`/`ts_ns`/`ts_mx`/`ts_curl` (07's R3 mapping).
- `country.percent NUMERIC(5,2)` — direct serialization for `/country` (kills production's pgtype ÷10 hack, 07).
- `asn.count_total` / `asn.count_v6`, `country.sites` / `country.v6sites` / `country.percent` — recomputed by tick step 3 over the publicly-ranked predicate (06).
- `campaign.source_file` — sync provenance; rename detection is uuid-based, not file-based (06).
- `scan.country_id` / `scan.asn_id` — denormalized at commit time because caggs cannot track JOINs; feed `scan_daily_adoption`'s country slice.
- `scan.classification` — confirmed class stamped at scan time (dataset + cagg dimension), NOT recomputed from the row's observations.
- `changelog.legacy_message` / `changelog.legacy_status` + the three CHECK constraints — phase-4 unmappable-row escape hatch (08); native rows never set them, and the CHECKs make that machine-enforced.
- `check_job.result` — the shared-mapper JSON served verbatim by `GET /check/{id}` (07).
- `tranco_import.line_count` / `rejected_count` / `duplicate_count` / `imported_count` / `delisted` / `aborted` / `note` — import provenance + sanity guard + staleness alert (06).
- `resource_host.dependent_count` — maintained ±1 on link/unlink in the same statements (06); service-candidate heuristic (b) threshold input; sweep claim predicate `dependent_count > 0`.
- `crawler_metrics.dim_counters` — JSONB per-dimension tallies **including the `lease_lost` counter** for fence aborts (03). **Decision:** `lease_lost` lives inside `dim_counters` (the design offered "a `lease_lost` counter in `dim_counters` or a dedicated column"; the JSONB key is the simplest form and needs no DDL change to extend).
- `stats_global_daily.top_heroes` / `top_nameserver` — top-1k metrics with the pinned `rank <= 1000` / `base = 'supported'` semantics (06 owns the snapshot SQL; 07 serves them in `/metric/overview`).
- `stats_global_daily.disabled` — the one stats column scoped `rank IS NOT NULL AND disabled` (visibility into suppression); all other stats columns use `rank IS NOT NULL AND NOT disabled` (06).

---

## 13. Acceptance criteria (verified by 10-testing.md fixtures)

1. `v6ctl migrate up` applies 000001→000003 green on a fresh `timescale/timescaledb:latest-pg18` container; re-run is a no-op; `migrate version` reports 3, not dirty.
2. `timescaledb_information.hypertables` lists exactly 6 hypertables (the `scan_daily_adoption` materialization hypertable is internal and does NOT appear here); the `proc_name`-filtered policy query (§4) shows 5 columnstore + 4 retention + 1 cagg-refresh policies = 10 (excluding the built-in telemetry job); `timescaledb_information.continuous_aggregates` lists `scan_daily_adoption`.
3. `SELECT count(*) FROM country` = 251; sentinel lookups (`asn.number = 0`, `country.code = 'UN'`) return exactly one row each; `SELECT count(*) FROM stats_global_daily` = 1.
4. `sqlc generate` runs clean against the three up-files and the full `db/query/` set; generated code compiles.
5. EXPLAIN of the claim query (04) on a seeded 1M-row fixture uses `idx_domain_due` (index scan bounded by `next_check_at <= now()`), and no query plan for the ranked list endpoints uses `idx_domain_due` — the claim-plan gate.
6. Inserting a native changelog row with `old_value NULL`, or a `field='legacy'` row with `legacy_message NULL`, fails the CHECK constraints.
7. The commit UPDATE bumps `domain.updated_at`; the claim stamp does not (§9).
