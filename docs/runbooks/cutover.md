# P4.G — Phase-4 gate: DNS-flip cutover checklist

Status: **build gate GREEN (steps 1–3 verified 2026-07-10); production
cutover PENDING (operator)**. A pure DNS flip, no data import
(08-migration-cutover.md §1). The legacy parity gates G1–G7 are deleted;
the OpenAPI contract gate (P4.5) is the replacement.

## Build-gate record (verified in this repo)

- [x] **1. Fresh DB → migrations.** A fresh `timescale/timescaledb` PG18
  instance takes `v6ctl migrate up` 000001→latest green; `changelog` and
  `scan` start empty. Exercised on every integration run — the pgtest
  harness template-clones a freshly-migrated database per test
  (`internal/postgres/pgtest`), and TestMigrations covers up/down.
- [x] **2. Crawl builds state (bounded sample).** Confirmed state builds
  from scratch through the commit machine (commit/chaos/shutdown
  integration suites in `internal/crawler`). The cold classification
  start is expected and flagged (08 §5): day-1 hero/adoption counts read
  low until N crawl cycles confirm each dimension — not a bug. The full
  ≥3-frontier-pass precondition is the *production* cutover gate (08
  §2.4), recorded below.
- [x] **3. OpenAPI contract tests green.** `make generate` leaves a clean
  tree (drift gate), `make spec-lint` (Spectral) passes at error
  severity, `TestOpenAPIRouteCoverage` binds every documented operation
  to a registered route and vice versa, and the endpoint suites
  (P4.3/P4.4/P4.13/P4.14/P4.15/P4.16) are green — 2026-07-10.
- [ ] **4. `top_shame` re-seed applied** (production step): run
  `v6ctl shame add <host> --reason ...` for the curated picks so `/shame`
  is non-empty at launch (P4.11; the list has no crawl-derivable source).

## Production cutover checklist (operator; 08 §2–§4)

Preconditions:

- [ ] ≥3 full frontier passes completed on production hardware (08 §2.4);
  classification counts stable across 3 consecutive days.
- [ ] P3 operator gates recorded green (P3.5 sizing, P3.6 alerts, P3.G).
- [ ] pgBackRest full backup taken + restore drill green (P3.3 / 09-ops
  §10.4).
- [ ] `top_shame` seeded (step 4 above); `/shame` returns the picks.

Flip (08 §3):

- [ ] Lower the `api.whynoipv6.com` TTL 24 h ahead.
- [ ] Switch DNS/upstream to the new nginx vhost.
- [ ] Verify the §4 gates: `/readyz` 200; `GET /domains` serves the §4.2
  shape; `GET /changelog` non-empty; badge renders for a known hero;
  `POST /check` round-trips to `done`.
- [ ] Keep the old backend deployable through the rollback window; roll
  back = revert the DNS record (08 §5).

Record date, operator, and observations here when executed.
