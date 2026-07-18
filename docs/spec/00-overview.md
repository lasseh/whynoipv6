# 00 — Overview, Layout, Sizing & Conventions

_Status: Round 3.0 — API redesign folded in (docs/history/api-design-research.md, decisions 2026-07-09): clean root API, keyset pagination, RFC 9457, no legacy compat, no history import._

**Purpose:** The entry point to the WhyNoIPv6 backend implementation spec. It states what the system is, restates the hard constraints verbatim, fixes the final monorepo layout, and is the **single source** for two things every other file cites by name: the canonical sizing-constants table and the project glossary. It also carries the spec-file index, the reading order, and the spec conventions (single-source rules, `**Decision:**` markers, cross-reference form) that all ten content files obey.

**Deliverables:** This file governs no Go package. It defines the repository's top-level directory tree (every other file's package paths must resolve inside it), the named sizing constants, and the shared vocabulary. It produces documentation only.

**Companion files:** none are required to read this file — it is the root. An implementer starts here and then follows the reading order in §7. Every other spec file lists 00-overview.md as a companion because they cite its sizing constants and glossary.

---

## 1. What the system is

WhyNoIPv6 is a public, anonymous measurement service that answers, for the world's most-visited websites, *"does this site work over IPv6, and if not, why not?"* The new backend is **one Go module with three binaries** — `api` (the public HTTP surface), `crawler` (the autonomous scanning daemon), and `v6ctl` (the operator CLI) — over **PostgreSQL 18 + TimescaleDB 2.28 (Community edition)**, laid out hexagonally (pure domain core, port interfaces, adapters at the edge). The scanning core is the **v6audit checker engine lifted nearly verbatim** (15 checks, two-phase conditional execution, SSRF-pinned dialer, IPv6 self-preflight), with all scoring and grading deleted.

The heart of the system is a **crawl → confirm → publish pipeline.** Every scannable host is one row in the `domain` table with a `next_check_at`; crawler workers claim due rows with `FOR UPDATE SKIP LOCKED` — the table *is* the frontier, so there is no queue and no in-memory materialization of due domains (the failure mode that killed both prior schedulers). Each claimed host is scanned once per day no matter how many lists it is on. The two classification-critical lookups (apex AAAA and www AAAA) are resolved through **three public resolvers concurrently with a 2-of-3 quorum**; everything else goes through **local Unbound recursors**. Raw per-scan observations land append-only in TimescaleDB hypertables, but a dimension's **public status advances only when a new value is definitive, quorum-confirmed, and has held for N consecutive daily scans** — and only a confirmed transition writes a `changelog` row. From the confirmed statuses the system deterministically computes a public **classification** (hero / partial / sinner / inactive) and serves it — with no grades and no numeric scores — through a clean, OpenAPI-first HTTP API served at the root of `api.whynoipv6.com` (no `/v1` segment) that exposes the confirmed model directly: the real 4-value per-dimension statuses, keyset-paginated tier collections, RFC 9457 errors, and a `snake_case` wire. There is no legacy compatibility layer and no frozen-frontend contract.

## 2. Hard constraints (locked — restated verbatim)

These are the non-negotiable constraints of the whole build. They are restated in the files where they bite; no file may violate them.

- **Public and anonymous.** No accounts, no authentication, no authorization, no billing, no per-user state, no admin HTTP surface. Every operator action is a `v6ctl` verb with direct DB access, never an API endpoint.
- **3-state public status model.** Public IPv6 status is the 4-valued `ipv6_status` enum (`supported | unsupported | no_record | not_applicable`) — colloquially the "3-state" model (`not_applicable` is the absence case). The 7-valued internal `observation` enum (`+ partial | error | inconsistent`) never reaches public output.
- **Deterministic hero / partial / sinner ladder — NO grades or scores.** Classification is a materialized enum (`unknown | inactive | sinner | partial | hero`) computed by a deterministic first-match ladder over confirmed statuses. There is no numeric score, no letter grade, and no quality index anywhere in the system. `scoring.go` is deleted from the lifted engine and nothing may reintroduce a numeric quality signal.
- **Tranco-only, top-1M eTLD+1.** The only ranked-list source is Tranco (top 1,000,000 pay-level / eTLD+1 domains). There is no other list ingest. `tldbwriter` and its sources are gone.
- **Newest stack, pinned where a pin is load-bearing.** Current Go toolchain; PostgreSQL 18 + TimescaleDB 2.28 (modern columnstore API, never the deprecated `timescaledb.compress` API); `pgx/v5`; `log/slog`; `chi` v5; `cobra`; `viper`; `sqlc` (v2, pgx/v5) for all static SQL, plus `squirrel` building only the `/domains` list-family queries (the bounded carve-out, 05-schema.md — §10.2). The one hard version pin is the DNS library: **`github.com/miekg/dns`** (pinned at an exact newest `v1.1.x` — the API the lifted v6audit engine was written against, so the lift needs no DNS-library port; the pre-1.0 rewrite `codeberg.org/miekg/dns` is deliberately **deferred** to a possible future migration, and `github.com/miekg/dnsv2` is a dead path and must never be imported). **Decision (grilling round, 2026-07-10):** switched from the design's codeberg/v2 pin to maintained v1 — see 01-engine.md §7.
- **Modern, versionless read-only API surface.** The public HTTP API is a clean, OpenAPI-first surface served at the **root** of `api.whynoipv6.com` (no `/v1` segment, no doubled `/api/v1`): plural verb-free resource paths with short tier collections (`/heroes`, `/sinners`, `/saints`) and a general `/domains?class=…` filter, keyset/cursor pagination (no offset), a consistent `{items, page, meta}` list envelope (never a bare array or a `{"data":[…]}` quirk), `snake_case` on the wire, RFC 9457 `application/problem+json` errors, and per-dimension `{value, since}` status objects serving the real 4-value `ipv6_status` enum + JSON `null`. There is **no** legacy compatibility layer, no `legacyStatus` projection, no shortuuid URLs, and no frozen-frontend contract — the frontend is rebuilt and co-designed against the committed `openapi.yaml`.

## 3. Non-goals

- No user-facing accounts, dashboards behind login, saved searches, alerts-by-email, or API keys.
- No scoring, grading, ranking-by-quality, or "IPv6 readiness percentage per site."
- No second ranked list, no crowd-submitted ranking, no non-Tranco top list.
- No job queue, message broker, or external scheduler (River/Redis/NATS explicitly rejected — the frontier is a DB column + `SKIP LOCKED`).
- No horizontal DB sharding, multi-region write, or cloud-managed database — the target is one operator's VMs.
- No frontend work in *this* build: the Vue app is rebuilt and co-designed against the committed `openapi.yaml` as a separate deployment (its subtree is not compiled by this workflow); the cutover is a DNS flip.
- No mutation of history: `scan`, `scan_detail`, and `changelog` are append-only.

## 4. Monorepo layout (final form)

Two-repo monorepo per brief §2.5 and design §6: the Go module lives under `backend/`, with `frontend/`, `openapi/`, `deploy/`, and `docs/` as monorepo-root siblings so Go and Node tooling never collide. Every Go package path referenced anywhere in the spec is relative to the `backend/` module root (e.g. `internal/checker`) and resolves here. One-liners are the package's single responsibility; the owning spec file is named in brackets. `whynoipv6-new` is the seed of this monorepo.

```
whynoipv6/                     # monorepo root ("whynoipv6-new" is the seed)
  Makefile                     # universal interface, orchestrates backend + generate: make test|lint|build|… [09]
  compose.yaml                 # docker-compose dev env (PG18+Timescale, Unbound×2, api, crawler) [09]

  backend/                     # the Go module — all `make` targets run from here
    go.mod                     # module github.com/lasseh/whynoipv6; github.com/miekg/dns v1 pinned exact
    go.sum
    .golangci.yml              # single lint config [09]
    sqlc.yaml                  # sqlc v2 (pgx/v5) config [05]
    Dockerfile                 # multi-stage build of all three binaries; compose context ./backend [09]

    cmd/
      api/main.go              # HTTP server process wiring + slog install [07,09]
      crawler/main.go          # frontier workers + check-job consumer + daily tick + preflight [04]
      v6ctl/                   # cobra CLI: tranco, campaign, resource, shame, disable, service-candidates,
                               #   export, stats recalc, migrate (schema DDL) [05,06,08]

    internal/
      domain/                  # entities, enums, Canonicalize, classification ladder — PURE, zero deps [02,03,06]
      checker/                 # LIFTED from v6audit: engine + bulk resolver + SSRF dialer + preflight + seam types [01]
      consensus/               # multi-resolver quorum wrapper implementing checker.AAAAResolver [02]
      crawler/                 # claim loop, worker pool, commit machine, schedule, sweep, tick,
                               #   resource sweep, checkpoint metrics [03,04,06]
      ingest/                  # Tranco fetcher/parser/staging upserter/sanity guard [06]
      campaign/                # campaign-YAML parse + validation + idempotent Sync [06]
      repository/              # port interfaces (consumer-defined) [05]
      postgres/                # sqlc-generated queries (db/) + hand-written adapters [05]
      service/                 # use-case layer the api handlers call [07]
      api/                     # chi router, handlers, keyset pagination, feeds/badges/datasets/CSV serializers, gen/ (oapi-codegen) [07]
      geoip/                   # IPinfo Lite mmdb reader + attribution + hot reload [06]
      notify/                  # ops-webhook + healthchecks.io ping client [09]
      config/                  # viper loader, all three binaries [09]
      lock/                    # advisory-lock singleton coordination [04]

    db/
      migrations/              # golang-migrate: 001 base schema, 002 timescale, 003 seed + migrations.go go:embed [05]
      query/                   # sqlc query sources [05; contents owned by 03/04/06/07]

  frontend/                    # rebuilt Vue 3 app, co-designed against openapi/openapi.yaml; OUT OF SCOPE for this build — not compiled by the workflow
    package.json               #   separate Node manifest root; the Opus build never touches this subtree

  openapi/
    openapi.yaml               # spec-first source of truth for every endpoint [07]

  deploy/
    systemd/                   # *.service + *.timer unit files [09]
    nginx/                     # api.whynoipv6.com.conf (API + /datasets vhost) [09]
    unbound/                   # unbound@.service, unbound-base.conf, per-instance drop-ins [09]
    pgbackrest/                # pgbackrest.conf + whynoipv6-export.sh logical export [09]
    geoip/                     # v6ctl-geoip-update.service + .timer units [09]
    grafana/                   # alerts.yaml provisioned alert rules [09]

  docs/
    history/backend-design.md  # authoritative design (Round 2.0) — input to this spec set
    spec/                      # THIS spec set (00–11)
```

**Decision:** The Go module is rooted at `backend/`, not at repo top — this matches design §6, brief §2.5, and the plan's P0.1 (`backend/go.mod`), and lets the rebuilt `frontend/` sit as a sibling with its own Node manifest root. Every `make` target, `go build ./...`, and `sqlc`/lint invocation runs from `backend/`; `.golangci.yml` and `sqlc.yaml` therefore live with the module under `backend/`, while the orchestrating `Makefile` (`cd backend && …` plus generate targets) and `compose.yaml` sit at the monorepo root. The `frontend/` subtree is present for the monorepo's completeness but is **out of scope for this build** — the autonomous workflow never compiles it (brief §8); it is rebuilt and co-designed against `openapi/openapi.yaml`, and nothing in this build references it.

**Decision:** The final tree adds one `internal/` package the design §6 layout tree did not enumerate but its own body text delivers: `internal/lock` (advisory-lock coordination, owned by 04-lifecycle-scheduling.md). It is a load-bearing deliverable of its owning file; the §6 tree was illustrative, not exhaustive. (There is no `internal/migrate`: 08-migration-cutover.md collapses to a pure DNS-flip cutover with no data import — see §10.4 of the API redesign report.)

**Decision:** `internal/postgres/db/` holds the sqlc-generated code (never hand-edited) and `internal/api/gen/` holds the oapi-codegen output (committed); both are generated subpackages of their hand-written parent, consistent with the deliverables in 05-schema.md and 07-api.md. The campaign-definition repository (`.github/workflows/validate.yml` and the campaign YAML files) is a **separate repo** and is not part of this tree; 06-ingest.md governs the one workflow file this project contributes to it.

## 5. Canonical sizing constants (single source)

This table is the **only** place independent sizing values are defined. Every other file cites a row **by name** and never restates an independent number or range; if a derivation input changes, this table is re-derived and the citing sentences updated in the same commit. Names in the "Constant" column are the citation tokens (`WORKER_SLOTS`, `SCAN_RATE`, …).

### 5.1 Named constants

| Constant | Value | Meaning |
|---|---|---|
| `ENGINE_CHECKS` | **15** | Registered checkers in the lifted engine, defined by enumeration (01-engine.md — lift inventory), never by a carried-forward count. |
| `SCAN_RATE` | **~12 domains/s** sustained (1.03M/day) | Steady-state frontier throughput required for daily cadence over 1M ranked + ~30k other entities. |
| `CRAWLER_PROCS` | **2** | Crawler processes per machine — for resilience and deploy hygiene, not capacity. |
| `WORKER_SLOTS` | **64 per process → 128 provisioned** (2 procs); **~72 busy on average** | Concurrent *domain* slots per crawler process (config key `worker_slots`, default 64; registry: 09-ops.md). The 128−72 gap is tail-latency headroom. Distinct from the engine-internal per-check concurrency constants in 01-engine.md. |
| `MEAN_SCAN_DURATION` | **≈ 6 s** weighted | Weighted mean wall time per domain: phase-1-only (pure DNS) ≈ 2–4 s for ~775k v4-only names; full phase-2 incl. latency probes ≈ 10–25 s for ~258k v6 names. Drives `SCAN_RATE × MEAN_SCAN_DURATION = ~72` busy slots. |
| `PUBLIC_RESOLVER_QPS` | **~24 qps/provider** (71 qps total, ~6.2M queries/day) | Consensus load per public provider (Cloudflare/Google/Quad9), from apex+www AAAA × 3 providers. Google documents 1500 qps/IP (1.6% used); Quad9's contact threshold is 500 qps (4.8% used). |
| `BULK_RESOLVER_QPS` | **~140–190 qps** (~12–16M queries/day) | Local Unbound bulk load (A lookups, NS walk + NS-AAAA, MX + MX-AAAA, DS+SOA, TXT, PTR): ~12–16 lookups/domain. 1–3% of a tuned single-instance Unbound. |
| `RESOURCE_SWEEP_QPS` | **~2–4 qps** (~100–300k lookups/day) | Resource-host registry AAAA sweep — a separate, decoupled path through the bulk resolver; negligible. |
| `HTTP_FETCH_RATE` | **~35 fetches/s** (~3M/day) | HTTP(S)/TLS/parity/resource-page fetches plus v4+v6 latency TTFB probes: ≈ 11–12 per v6 domain × ~258k v6 domains. ~50–80 concurrent sockets. |
| `EGRESS_BW` | **~200–400 GB/day** (~25–45 Mbps avg) | Fetch egress (parity capped 2×1 MB, resource page capped 2 MB; typical pages ~200–500 KB). Trivial on operator hardware. |
| `DB_WRITE_RATE` | **~3.1M rows/day** (~36 rows/s) steady + **up to ~1M** guarded Tranco rank UPDATEs/day | 1 `scan` + 1 `scan_detail` + 1 state UPDATE per domain, batched; plus the daily Tranco import's conflict UPDATE, guarded to touch only rows whose rank/lifecycle actually changed (06-ingest.md — import cycle), worst-case ~1M non-HOT updates in the import transaction. Nothing for PG18. |

### 5.2 Population assumptions (inputs to the derivations above)

- Entity count: **~1,000,000 ranked** (Tranco top-1M) + **~30,000** campaign/subdomain entities.
- **~25%** of entities have apex or www AAAA (current adoption is ~20–28% depending on measure; 25% is the working figure). **~2 of 3 (~70%)** have MX.
- The consensus A-lookup fires only on a NOERROR-empty AAAA quorum (~75% of names); the PTR check fires only on the ~25% v6 names; latency probes are TTFB-only (body unread), so they add fetch count but not the `EGRESS_BW` row.

### 5.3 Capacity conclusion (locked)

Daily cadence for all 1M is comfortably achievable on **one machine**; the two crawler processes exist for resilience and deploy hygiene, not capacity. The path to Tranco-full (~4.5M) needs no schema change (`rank` is already nullable and the frontier is the `domain` table): ingest the full list and set per-band cadence. Public-resolver load stays bounded because only apex+www AAAA use consensus (4.5M daily ≈ 104 qps/provider — the point at which per-band cadence or a fourth resolver becomes necessary; 1M + tail-every-3d stays ≈ 40 qps/provider).

## 6. Glossary

The shared vocabulary. Definitions here are canonical; the owning file for each mechanism is named in brackets. Terms are never redefined elsewhere with a different meaning.

- **Entity / kind.** A scannable host = one row in `domain`. Its `kind` is `apex` (an eTLD+1 pay-level domain, the Tranco unit) or `subdomain` (a campaign-specified host below an apex). One entity is scanned once per day regardless of how many campaigns reference it. [04, 05]
- **eTLD+1 / pay-level domain.** The registrable domain one label below the effective TLD (e.g. `bbc.co.uk`, not `www.bbc.co.uk` and not `co.uk`). The Tranco top-1M unit and the meaning of `kind='apex'`. [06]
- **TLD.** The effective TLD / public suffix of a host (`com`, `no`, `co.uk`, `gov`), derived at ingest via publicsuffix and stored on `domain.tld`. Backs the TLD/ccTLD league tables and the `?tld=` filter axis. [05, 06]
- **DNS provider / hosting provider.** Two normalized provider attributions on a domain: the **DNS provider** resolved from the domain's nameserver host through the `ns_host → provider` mapping table (the highest-leverage pivot — one provider's default flips thousands of domains); the **hosting/CDN provider** derived from CNAME-chain CDN detection plus the resolved IP's ASN. Both back the `?provider=` filter and the provider league tables. Registrar attribution is deferred. [05, 06]
- **Tier collections.** The short canonical plural browse paths `/heroes`, `/sinners`, `/saints` — each a preset filtered view over the `/domains` leaderboard (`class=hero`, `class=sinner`, `saint=true`), sharing the same row shape, keyset pagination, and `?country=`/`?asn=` composition as `/domains`. They are aliases over the general `/domains?class=…` collection, not a second vocabulary. The partial and mail views have no tier paths — they are spelled `/domains?class=partial` and `/domains?class=hero&mx=supported` (ADR 0003). [07]
- **Keyset cursor.** The opaque base64url pagination token over a strict total order (`(rank, id)` on rank-sorted views, `host` on curated sets, a null-flag-first key on the nullable-rank dependents list), replacing offset pagination on every large collection. Encodes the synthetic `domain.id` internally; there is no exact `COUNT(*)` on the hot path. [07]
- **Mandate tags.** Free-form `tags` on a campaign (backing `?tag=` and the `/mandates` surface) that mark government-mandate / compliance-tracking campaigns. A campaign-metadata capability, not a per-domain classification signal. [05, 06]
- **Dimension.** One measured facet of a domain's IPv6 posture. **Core dimensions** (six): `base`, `www`, `ns`, `mx`, `conn`, `resources` — each carries a 5-column confirm/pending group on `domain` and can gate classification. **Informational dimensions**: `dnssec`, `ptr`, `smtp`, `parity`, `latency_v4_ms`, `latency_v6_ms` — latest observation only, never gate classification. [03, 05]
- **Observation.** A single per-dimension, per-scan raw outcome; the 7-valued internal `observation` enum (`supported | partial | unsupported | no_record | not_applicable | error | inconsistent`). Produced by the mapping/quorum layer before any commit. `partial`, `error`, and `inconsistent` never become public. [02]
- **Confirmed status.** The public, durable per-dimension value; the 4-valued `ipv6_status` enum (`supported | unsupported | no_record | not_applicable`). It changes only when a definitive observation has been confirmed by the state machine. "Observation vs confirmed status" is the raw-vs-trusted distinction at the core of the design. [03]
- **Definitive observation.** An observation NOT in `{error, inconsistent}`. Only definitive observations can advance confirmed state; non-definitive ones touch nothing. [03]
- **Classification.** The materialized public verdict per domain; the enum `classification` (`unknown | inactive | sinner | partial | hero`), computed by a deterministic first-match ladder over confirmed statuses in the commit transaction. `hero` = fully reachable over IPv6; `partial` = has IPv6 base but misses the hero bar; `sinner` = a ranked apex with no IPv6 at all; `inactive` = no working base (dead/parked); `unknown` = not yet classified. NO grades, NO scores. [03]
- **Saint.** A boolean refinement, not a class: `saint = (classification == hero AND resources ∈ {supported, not_applicable})`. It marks heroes that also serve their sub-resources over IPv6 (renamed from "gold", ADR 0003). Evaluates false everywhere while the resources feature is disabled. [03]
- **Frontier.** The set of scannable-and-due domains — which *is* the `domain` table, filtered by an eligibility predicate on `next_check_at`/`disabled`/`disabled_reason`. There is no separate queue or jobs table. [04]
- **Lease.** A worker's temporary claim on a domain row, recorded as `claimed_at` (the lease token). Claims are taken with `FOR UPDATE SKIP LOCKED`; a claim older than the reclaim window (30 min) is stale and reclaimable. The commit's **lease fence** discards a transaction whose lease token no longer matches, so a reclaimed row's loser write is dropped. [04, 03]
- **Quorum.** The 2-of-3 agreement rule over the reduced per-resolver symbols (`exists | empty | nxdomain | error`) from the three public resolvers, applied only to apex AAAA and www AAAA. ≥2 valid answers agree → that symbol wins; ≥2 valid answers with no agreement → `inconsistent`; ≤1 valid answer → `error`. Quorum is over *symbols*, not record sets (GeoDNS legitimately varies content). [02]
- **Consensus resolver vs bulk resolver.** The **consensus resolver** (`internal/consensus`) fans a lookup out to Cloudflare/Google/Quad9 and applies quorum — used ONLY for the two classification-critical AAAA lookups, no caching (every observation must be fresh). The **bulk resolver** (the lifted `checker.Resolver`) is a single recursive path to local Unbound used for everything else (A records, NS/MX chains, DS/SOA/TXT/PTR, the conditional A lookup, resource-sweep AAAA, and all DNS-pinned dialing). Unbound is the cache; the in-process TTL cache is deleted. [01, 02]
- **Lifecycle states.** A domain is either active (frontier-eligible at its cadence) or `disabled` with a `disabled_reason`:
  - `dead` — seven consecutive unresolvable scans; still scanned on a slow lane; re-enabled automatically by a later definitive base observation (full state reset). [03, 04]
  - `delisted` — lost all linkage (rank became NULL, no enabled-campaign membership, no children, no recent live-check); still scanned on a slow lane; re-enabled when any ingress restores linkage. Owned solely by the daily lifecycle sweep. [04]
  - `service` — detected service/CDN-style host that should not be publicly shamed; not claimable. [04]
  - `manual` — operator-disabled via `v6ctl`; not claimable. [04, 06]
- **Changelog.** Append-only public history: exactly one row per confirmed dimension transition. Non-definitive observations and unchanged confirmations write nothing. [03]
- **Campaign membership.** A campaign is a named collection; membership is a join row (`campaign_domain`) onto the shared `domain` entity — never a duplicated crawl target. [06]
- **Preflight.** The crawler's IPv6 self-check at claim-loop start: if the crawler's own IPv6 egress is broken, scanning would falsely report the whole world as v6-broken, so it refuses to scan until its vantage is healthy. [01, 04]

## 7. Reading order for an implementer

The spec is built to be implemented front-to-back with the pure core first. Recommended order:

1. **00-overview.md** (this file) — layout, constants, vocabulary, conventions.
2. **05-schema.md** — the schema is the substrate; stand up the migrations and sqlc first so every later package compiles against real types.
3. **01-engine.md** — lift the v6audit checker engine (pure-ish, testable against fake DNS) before anything that consumes it.
4. **02-observation-model.md** — the consensus wrapper and the Result→observation mapper that turn engine output into observations.
5. **03-state-machine.md** — the confirmed-status commit machine (the trust core; the subtlest component).
6. **04-lifecycle-scheduling.md** — the frontier, claim loop, scheduling, sweeps, and the crawler process wiring that drives 01–03.
7. **06-ingest.md** — how entities and metadata enter (Tranco, campaigns, resources, attribution).
8. **07-api.md** — the public HTTP surface: the clean modern API (root base, tier collections, keyset pagination, RFC 9457, `snake_case`, OpenAPI-first).
9. **09-ops.md** — config registry, packaging, deploy, backup, observability (read alongside 04/07 for the keys they introduce).
10. **08-migration-cutover.md** — the DNS-flip cutover runbook (last, because it depends on the full schema and API): stand up the new stack, let it crawl, flip DNS, re-seed `top_shame`. No data import — the system starts fresh.
11. **10-testing.md** — the fixtures and gates; consulted continuously, not once. Every acceptance criterion elsewhere resolves to a fixture table here.

## 8. Spec conventions (normative for all files)

### 8.1 Spec-file index

| File | Owns | Primary target packages |
|---|---|---|
| **00-overview.md** | System summary, hard constraints, non-goals, monorepo tree, **canonical sizing constants**, **glossary**, spec index, reading order, conventions. | (none — documentation) |
| **01-engine.md** | The v6audit checker lift: 15 checks, bulk resolver, SSRF dialer, two-phase runner, IPv6 preflight, the `AAAAResolver` seam types. | `internal/checker` |
| **02-observation-model.md** | Observation vocabulary, the consensus quorum resolver, engine-outcome→observation mapping, `conn` composition, `resources` roll-up. | `internal/consensus`, `internal/observe`, `internal/domain/observation.go` |
| **03-state-machine.md** | The confirmed-status commit machine: confirm/pending loop, dead signal, streaks, changelog, classification ladder, the one-`pgx.Batch` write unit. | `internal/crawler` (commit), `internal/domain/classify.go` |
| **04-lifecycle-scheduling.md** | Frontier, atomic claim query, cadence/recheck/backoff, dead/delisted lifecycles, daily sweep + tick, advisory locks, crawler process model. | `internal/crawler` (frontier/schedule/sweep/tick/metrics), `internal/lock`, `cmd/crawler` |
| **05-schema.md** | **ALL SQL DDL**: extensions, enums, tables, indexes, hypertables, columnstore, retention, continuous aggregate, seed; the `ns_host → provider` mapping table and the `tld` / DNS-provider / hosting-provider / campaign-`tags` pivot columns; migration + sqlc tooling. | `db/migrations`, `db/query` layout, `sqlc.yaml`, `internal/postgres`, `internal/repository`, `cmd/v6ctl migrate` |
| **06-ingest.md** | `Canonicalize`, Tranco import cycle, campaign sync (incl. mandate `tags`), resource registry/sweep, GeoIP/ASN attribution, `tld` derivation + DNS-provider (`ns_host → provider`) / hosting-provider attribution; the ingest v6ctl verbs. | `internal/domain/host.go`, `internal/ingest`, `internal/campaign`, `internal/geoip`, `internal/crawler/resourcesweep.go`, `cmd/v6ctl` |
| **07-api.md** | The complete HTTP contract: server baseline (root base, no `/v1`), the clean resource model + tier collections, keyset pagination + filter grammar, RFC 9457 errors, `snake_case` envelope, badge, async live check, change feeds, CSV export, static datasets + manifest, diff + `/mandates` surfaces, OpenAPI-3.0.3-first. | `internal/api`, `internal/service`, `openapi/openapi.yaml` |
| **08-migration-cutover.md** | The pure DNS-flip cutover runbook + rollback (no data import — start fresh); the `top_shame` re-seed; the cold-classification-start caveat; retained restore-drill hygiene. | `cmd/v6ctl` (`shame` re-seed) — no dedicated package |
| **09-ops.md** | **The consolidated config-key registry** (single source), systemd/nginx/Unbound/pgBackRest/GeoIP/Grafana deploy, logging, Makefile/CI. | `internal/config`, `internal/notify`, `deploy/**`, root `Makefile`/`.golangci.yml`/`compose.yaml` |
| **10-testing.md** | **ALL test fixtures and vectors**: quorum/mapping/classification tables, OpenAPI contract vectors (keyset-cursor codec, RFC 9457 shapes, Atom/JSON-Feed serializers, `manifest.json` schema, badge golden SVGs), integration scenarios, coverage bar. | `*_test.go`, `internal/api/testdata/**`, test Make targets |

### 8.2 Single-source rules (violating any is a defect)

- **DDL lives only in 05-schema.md.** No other file contains `CREATE`/`ALTER`/`DROP`. Other files reference tables/columns by name and may quote `SELECT`/`INSERT`/`UPDATE`/`DELETE`, never DDL. (The one sanctioned exception, the session-scoped `tranco_staging` TEMP table, is also defined in 05.)
- **The config-key registry lives only in 09-ops.md.** Other files introduce a key by name with its purpose and say "registry: 09-ops.md"; the canonical type + default + env-var mapping is defined once, in 09.
- **The canonical sizing-constants table lives only in 00-overview.md** (§5). Other files cite a constant by its name (`WORKER_SLOTS`, `SCAN_RATE`, …) and never restate an independent number or range.
- **Test fixtures live only in 10-testing.md**, with two sanctioned exceptions. Other files may state *acceptance criteria* (properties the code must have) but never fixture tables; 10 owns the concrete decision-table vectors that prove them (quorum, mapping, classification, API contract). The exceptions — because their fixtures are captured artifacts, not decision tables: **engine-lift fixtures** (fake-DNS/HTTP/TLS harness inputs and goldens captured from the v6audit reference repo) are owned by 01-engine.md §14, and **ingest fixtures** (Tranco CSV, campaign YAML, GeoIP/provider-mapping samples) by 06-ingest.md §9; 10-testing.md §12 delegates to both by reference.
- **The glossary lives only in 00-overview.md** (§6). Other files use the terms with the meanings fixed here and do not redefine them.

### 8.3 `**Decision:**` markers

Where the design doc is ambiguous or silent at a point a spec file must be concrete, the author resolves it — always toward the **simplest form consistent with the existing locked decisions** (matching how the spec-readiness audit resolved things) — and marks the resolving sentence with a bold `**Decision:**`. A `**Decision:**` marker means "this was not spelled out in the design; here is the binding resolution," making every such choice auditable. Implementers treat `**Decision:**` text as normative, identical in force to design-doc text.

### 8.4 Cross-reference form

- **Between spec files**, cite as `see NN-name.md — <section name>` (e.g. "see 05-schema.md — `domain`", "see 03-state-machine.md — classification ladder"). Cross-references between spec files are encouraged; they are how the self-contained files stay consistent.
- **Never** defer normative content to the design doc — no "see backend-design.md". Every file copies the normative content it needs IN. The design doc and the audit trail are provenance, not a live dependency of the implementer.
- **Reference repos** (the v6audit engine to lift, the production backend for domain-model provenance, the campaign repo) are cited by repo-relative path where code is lifted or a golden fixture is captured; they are the only external inputs an implementer reads besides `docs/spec/*`.
