# 08 — Cutover (DNS Flip)

_Status: Round 3.0 — API redesign folded in (decisions 2026-07-09): clean root API, keyset pagination, RFC 9457, no legacy compat, no history import._

**Purpose:** Specifies the one-time operational runbook that switches public traffic from
the old backend to the new one. The cutover is a **pure DNS/upstream flip with no
production data import** (OPEN-9, start fresh, design §9): both the `changelog` table and
the `scan` hypertable begin empty and fill forward as the fresh crawl accumulates
confirmed transitions. The runbook stands up the new stack, lets the crawler build
confirmed state and changelog history from scratch, re-seeds the ~12 editorial `top_shame`
hosts, verifies the new API against its OpenAPI contract, then flips DNS.

**Deliverables:**
- The phase-4 cutover runbook and rollback plan (this file; operationalized by the
  Ansible/nginx deploy in 09-ops.md).
- The cutover verification gate list (§4) the operator must pass before the DNS switch.
- No Go deliverables. `cmd/v6ctl/migrate_import.go` and `internal/migrate/` are **deleted**
  — there is no importer. The schema-migration runner (`v6ctl migrate up`) stays and is
  owned by 05-schema.md / 09-ops.md.

**Companion files:**
- 05-schema.md — the schema `v6ctl migrate up` applies; both `changelog` and `scan` start
  empty. This file issues no DDL and writes no rows.
- 09-ops.md — systemd/Ansible deploy, nginx vhost + DNS switch, restore drill, backups,
  ops webhook.
- 06-ingest.md — §2 (Tranco import), §3 (`campaign sync`), §7 (`v6ctl shame` — the
  `top_shame` re-seed this runbook invokes); all run in phases 1–3.
- 04-lifecycle-scheduling.md — §9 (the daily tick that writes `stats_*` rows) and the
  crawler soak this runbook waits on.
- 03-state-machine.md — the confirmed-status commit machine and first-confirmation rule
  that govern the cold classification start.
- 10-testing.md — the OpenAPI contract-test harness and the behavioral synthetics the §4
  gates name (10 owns the mechanics and fixtures).

---

## 1. Scope — a pure DNS flip, no data import

The cutover is a traffic switch, not a data migration.

**Decision (OPEN-9, design §9): start fresh — no history import.** There is no
`v6ctl migrate-import` command, no entity-resolution/orphan-create step, no
seed-from-production, no changelog transform, no per-scan history backfill, and no
day-0-from-production stats seed. Both the `changelog` table and the `scan` hypertable
begin empty and build forward from the first post-cutover crawl.

Consequences the operator must expect (all intended, none a bug):

- **Cold classification start.** No statuses are seeded from production; every domain sits
  at `unknown` until the anti-flap machine confirms each dimension over N consecutive crawl
  cycles (03-state-machine.md — first-confirmation rule). The day-1 dashboard's
  hero/saint/sinner counts start low and rise to their true values over the first ~N days.
  This is honest and self-heals; flag it so the low day-1 hero count reads as expected, not
  a snapshot bug.
- **Empty changelog history.** The "who went green when" archive — the `/changelog` feeds,
  the Atom/JSON-Feed change feeds, the per-domain history trajectory,
  and the State-of-IPv6 report — starts empty at launch and fills over the months following
  as the fresh crawl records confirmed transitions. A best-effort structured changelog
  import from the retained production dump remains a **deliberately-deferred future option**
  (revisit later, design §9); it is not part of this cutover.
- **Empty scan history.** Per-domain graphs and the fresh-scan latency overlay start empty
  and fill over ~90 days.

**One editorial exception (§3 step 2).** The ~12 curated `top_shame` hosts have no
crawl-derivable source and back the `/shame` tier list, so they are re-entered via
`v6ctl shame add` (06-ingest.md — §7). This is editorial data re-entry, not a history
import, and is a **required** cutover step (without it `/shame` is empty at launch).

---

## 2. Preconditions (phases 1–3 complete)

The cutover (phase 4) assumes the new stack is fully stood up and soaked. Verify before
starting:

1. **Schema migrated:** `v6ctl migrate up` applied on the new DB; `v6ctl migrate version`
   reports the head revision and is not dirty (05-schema.md — §13.1).
2. **Tranco ingested:** the daily top-1M import has run (06-ingest.md — §2); the `domain`
   table holds the ranked frontier.
3. **Campaigns synced:** `v6ctl campaign sync` has established the `campaign` /
   `campaign_domain` rows from the YAML repo (06-ingest.md — §3).
4. **Crawler soaked:** the new crawler has run ≥3 full passes on the (initially empty)
   frontier (04-lifecycle-scheduling.md), so confirmed state and native changelog rows have
   begun accumulating and the anti-flap machine is warm. (This is the **production cutover**
   precondition; the build-phase gate P4.G uses a bounded sample crawl — the two are
   different gates, 11-implementation-plan.md.)
5. **Backups live and restore-tested** (09-ops.md); the daily tick is writing `stats_*`
   rows.

---

## 3. Cutover runbook (order of operations)

Executed by the operator. There is no import command — DB steps use the ordinary
v6ctl/ops verbs; traffic steps are the 09-ops.md Ansible/nginx deploy. The old API stays up
throughout until the flip, so there is no public downtime.

**Phase 4 cutover sequence:**

1. **Fresh new stack, already crawling.** Confirm phases 1–3 (§2): the new DB is migrated,
   Tranco/campaigns ingested, and the crawler has soaked ≥3 passes so confirmed state and
   changelog are building forward from empty. **No production data is copied.**
2. **Re-seed the editorial shame list.** Re-enter the ~12 `top_shame` hosts via
   `v6ctl shame add <host>` (06-ingest.md — §7) — the curated set the old site carried
   (twitter.com, twitch.tv, ebay.com, imgur.com, imdb.com, wordpress.com, github.com,
   paypal.com, stackoverflow.com, soundcloud.com, nytimes.com, w3schools.com). Each host
   must already be a ranked apex `domain` row (the `shame add` predicate), so this runs
   after Tranco ingest. Without this step `/shame` is empty at launch. Then run
   `v6ctl stats recalc` (06-ingest.md — §10.7) to seed/refresh today's `stats_*`
   snapshot so the API's `meta.generation`/`as_of` and the day-0 dashboards serve a
   current rollup without waiting for the nightly tick.
3. **Verify the new API against its contract.** Run the OpenAPI contract-test suite and the
   behavioral synthetics (§4) against the new API pointed at the new DB, with the rebuilt
   frontend served from staging. **All §4 gates must be green.** Any red gate halts
   cutover — fix and re-run, do not switch.
4. **Restore-drill gate** (09-ops.md — restore drill). Restore the new DB's latest backup
   to a scratch instance and assert the API binary starts against it and serves. A backup
   that has not been restore-tested is assumed broken.
5. **DNS / nginx switch.** Point `api.whynoipv6.com` (and the site) at the new API per the
   09-ops.md nginx/Ansible deploy. This is the DNS-flip cutover; the rebuilt frontend is
   deployed alongside. Watch the ops webhook + Grafana for error-rate and latency
   regressions.
6. **Soak & confirm.** Over the first ~N crawl cycles the confirmed counts
   (heroes/saints/sinners) rise from the cold-start baseline to their true values (§1). Verify
   the daily tick writes a `stats_*` row and the `/changelog` feed begins recording native
   post-cutover transitions.
7. **Decommission the old backend** only after ≥1 week of clean operation on the new stack
   (rollback window, §5). Keep the retained production dump indefinitely (the only source
   for the deferred best-effort changelog import, design §9).

**Ordering invariants (never reorder):** verify-before-switch (steps 3–4 before 5); the old
API stays up until step 5, so there is no public downtime; the shame re-seed (step 2) runs
after Tranco ingest, since its FK target must exist.

**Decision (grilling round, 2026-07-10) — repo lineage.** The new monorepo's Go module path is
`github.com/lasseh/whynoipv6`, identical to the production repo it replaces, so at cutover this
repo becomes the canonical one. Preserving the production repo's git history (via
`git replace --graft` or a merge commit) is a **cutover-time** decision that changes no code and
is deferred to then; until cutover, development proceeds in `whynoipv6-new` with its own history.

---

## 4. Cutover verification gates

Acceptance criteria that must be green **before** the DNS switch (§3 steps 3–4). Fixture
mechanics and the contract-test harness live in 10-testing.md; this section names *which*
gates exist and *what* each asserts. All must be green to cut over.

**Gate C1 — OpenAPI contract conformance.** The new API's responses validate against the
committed OpenAPI 3.0.3 contract (07-api.md — §8): the `{items, page, meta}` list envelope,
`snake_case` field names/types, RFC 9457 `problem+json` error bodies, keyset cursor
round-trips, and the per-dimension status objects (`{value, since}`). Run by the
contract-test suite (10-testing.md).

**Gate C2 — membership ladder (synthetic).** Seed one entity with confirmed
`base = supported, www = unsupported` and one with `base = unsupported`. Assert the first
appears in `/domains?class=partial` and NOT in `/sinners`; the second appears in `/sinners`
and NOT in `/domains?class=partial`. Repeat for the country-scoped tier lists. (Reframed
against the new spec; fixtures owned by 10-testing.md.)

**Gate C3 — error/inconsistent exclusion (synthetic).** Assert the per-domain
history/timeline endpoint (07-api.md — §4.9) excludes `error`/`inconsistent` scan rows — the
one genuine invariant retained from the old serialization (design §10.1).

**Gate C4 — restore-drill (§3 step 4, ops hygiene).** The new DB's latest backup restores
to a scratch instance and the API starts against it and serves.

---

## 5. Rollback plan

Cutover is DNS/upstream-only (§3 step 5); rollback is therefore a traffic switch — fast and
lossless.

**Trigger:** any of — a red gate discovered post-switch, an error-rate/latency regression on
the new API that can't be hotfixed within the operator's tolerance, or data corruption on
the new DB.

**Procedure:**
1. **Revert the DNS/nginx upstream** to the old backend (09-ops.md; the old API was left
   running until §3 step 5, or is restarted from its still-present service unit).
2. **The old production DB was never touched** by the cutover — there is no import, so
   nothing on the new stack ever read or wrote it. The old stack is exactly as it was before
   the flip.
3. **Diagnose on the new stack offline.** The new DB, crawler, and API keep running
   privately (not public); fix forward, re-run the §4 gates, and re-attempt §3 step 5 when
   green.
4. **No data migration is undone.** Because the new DB is a separate system and the old DB
   is untouched, rollback never involves reversing writes — it is purely a traffic switch.

**Rollback window:** keep the old backend deployable for **≥1 week** after cutover (§3 step
7). Keep the retained production dump indefinitely (design §9 deferred-import option).

---

## 6. Deliverables & config summary

**Go packages / files:** none. `cmd/v6ctl/migrate_import.go` and `internal/migrate/` are
**deleted** (no importer). The schema-migration runner (`v6ctl migrate up`) and the
`v6ctl shame` editorial verb this runbook invokes are owned by 09-ops.md / 05-schema.md and
06-ingest.md respectively.

**Config keys:** none. The `migrate.source_dsn` and `migrate.history_window` keys are
**deleted** (no importer); the 09-ops.md config registry must drop them.

**Concurrency:** none — no importer, no advisory lock, no seed step.

**Verification gates:** C1–C4 (§4); all green is the precondition for the DNS switch (§3
step 5).
