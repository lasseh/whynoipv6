# WhyNoIPv6 — Backend Rewrite Research Brief (for Fable 5)

**Status:** Round 1 — research & design proposal. Do NOT write implementation code yet.
**Deliverable:** a detailed design research report (see §10) that recommends *how* to build
the new backend, with tradeoffs and rationale, that we will then iterate on.
**Audience:** you (Fable 5) are the architect. Read the referenced repos, do additional
web research where noted, and produce the report.

---

## 0. What you're building and why

**WhyNoIPv6** is a public advocacy site — *"Shame as a Service" for IPv6*
(whynoipv6.com). It scans the world's most-visited websites and
community-submitted lists, measures whether they support IPv6, tracks changes over
time, and publishes it all — **heroes vs. sinners**, by domain, country, and ASN — to
pressure laggards into enabling IPv6. The mission is advocacy and open research data.

The product is three repos: a **Go backend** (API + crawler — *this rewrite*), a **Vue
frontend**, and a **campaign repo** (YAML domain lists submitted via GitHub).

This rewrite is **backend-first**. The crawler and its scanning logic are the heart of
the project and the main goal of this research.

---

## 1. Existing code to study (read these before proposing anything)

All under `~/code/go/src/github.com/lasseh/`:

| Path | What it is | Why it matters |
|---|---|---|
| `whynoipv6` | **Canonical production backend.** chi + pgx/v4 + sqlc + zerolog, cobra CLI `v6manage`. 4 DNS checks, 3-state status, Tranco 1M via external `tldbwriter`, campaigns, changelog + `domain_log` history, ASN/country stats. | The behavioral spec of the current product. Also the source of known pain: `LIKE '%x%'` search table-scans, **unbounded `campaign_domain_log`** (see its `cleanup_*.sql` scripts), and two crawlers duplicating ~90% of logic. |
| `claude/whynoipv6-team/backend` | **Best v2 rebuild.** TimescaleDB hypertables (compression/retention/continuous aggregates), hexagonal layering (domain → repository interfaces → postgres → service → api), pgx/v5, split crawler binary, **structured field-level changelog**, on-demand check queue, `domain_resource` tracking. ~258 tests. | **Recommended architecture base.** Study `db/migrations/001_schema.up.sql`, `002_timescaledb.up.sql`, `internal/repository/repository.go`, `internal/api/server.go`. |
| `v6audit/internal/checker` | **The richest crawler engine in existence for this problem.** 15 checks incl. real tcp6 HTTP/HTTPS reachability, full TLS handshake (expiry + hostname), SMTP EHLO, v4-vs-v6 **response parity**, sub-resource IPv6, latency v4/v6, DNSSEC, PTR. SSRF-safe DNS-pinned dialer, **IPv6 self-preflight**, custom miekg/dns resolver w/ TTL cache, deterministic idempotent `result_id`, split write model. Go 1.26. | **Reuse this engine.** Lift `runner.go`, `resolver.go`, `ssrf.go`, and the `*_ipv6.go` checkers. **Drop** `scoring.go` (see constraints — no A–F grade / no 0–100 score). |
| `claude/whynoipv6` (`prompts/`) | Prior redesign spec set: the **Resource Checker** spec (`11-resource-checker.md`), checkpointed `crawler_metrics`, FTS/trigram search, plus `REVIEW-REPORT.md`. | Design intent + a checklist of how the *last* attempt under-wired the resource checker (avoid repeating). |
| `whynoipv6-campaign` | 28 campaign YAML files: `title`, `description`, `uuid`, `domains[]` (bare pay-level hostnames), country+sector themed. | Campaign data model + ingestion source. |
| `whynoipv6-web` | Production Vue 3 frontend (Tailwind v3). Pages: home, `/domain`, `/domain/:d`, `/search`, `/metrics`, `/country`, `/campaign`, `/changelog`, `/faq`. | The API contract you must keep serving. **Its look must be preserved** — frontend is out of scope for this rewrite except keeping the API compatible. |

---

## 2. Locked product decisions (hard constraints — do not relitigate)

1. **Public, anonymous. No accounts, no auth, no orgs, no billing.** Drop all of v6audit's SaaS scaffolding — it is a different product. Campaigns remain anonymous UUID-keyed lists.
2. **3-state status model, NOT a score.** Every check result is `supported` / `unsupported` / `no_record` / `not_applicable`. **No A–F grade. No 0–100 score.** Classification is a deterministic rule-based ladder (hero / partial / sinner / inactive) — see **§5.5** for the full rules. Delete v6audit's scoring/grading entirely.
3. **Keep the richer *checks*, express them as status.** We want v6audit's real-reachability checks (the headline: **pure IPv6-only connection succeeds/fails**), TLS validity, SMTP, response parity, sub-resources — each reported as one of the 3-state values, not rolled into a number.
4. **Newest everything.** Current Go toolchain, Postgres 18, TimescaleDB (pg18), pgx/v5, `log/slog`, chi v5, cobra, viper, sqlc. Rolling Docker base tags. No version pins carried forward.
5. **Frontend look unchanged.** Out of scope here beyond API compatibility.

---

## 2.5 Repository layout (decided)

This project consolidates into **two repos** (down from three):

- **`whynoipv6` — a code monorepo containing backend + frontend.** They share one API
  contract, so the OpenAPI spec + generated TypeScript client live here and stay in sync
  in a single commit. `whynoipv6-new` is the seed of this monorepo.
- **`whynoipv6-campaign` — stays a separate data repo.** It is community-contributed via
  external GitHub PRs and drives automated ingestion on merge (Semaphore → backend
  import). Keeping it isolated preserves a clean, low-barrier contribution surface and
  avoids path-filtered CI gymnastics. Do NOT merge it into the monorepo.

Proposed monorepo structure (validate/refine):
```
whynoipv6/
  backend/     go.mod, cmd/{api,crawler,ctl}, internal/, db/migrations/, db/query/
  frontend/    package.json, src/            # imported from whynoipv6-web, look frozen
  openapi/     generated spec + TS client (committed, regenerated on `make generate`)
  docs/        this brief + future specs
  docker-compose.yml                          # db + api + crawler + frontend
  Makefile                                    # backend + frontend + generate targets
```
- Backend and frontend keep **separate module/manifest** roots so Go and Node tooling never collide.
- Existing repos should be imported preserving git history (subtree merge), not flat-copied.
- Frontend base = the current production `whynoipv6-web` (Vue 3, Tailwind 3), imported and modernized in place with its **visual look unchanged** (frontend is otherwise out of scope for this rewrite — see §8).

## 3. The crawler — the heart of the rewrite

### 3.1 Consensus model (Tier 1)
- **Multi-resolver DNS consensus from a single logical vantage.** For each DNS query, ask **3 independent public recursive resolvers concurrently** (Cloudflare `1.1.1.1`/`2606:4700:4700::1111`, Google `8.8.8.8`/`2001:4860:4860::8888`, Quad9 `9.9.9.9`/`2620:fe::fe`; ref https://www.ipfire.org/docs/dns/public-servers). Require a **quorum (2 of 3)** to set a dimension's status. These resolvers' anycast pops sit in different regions, giving cheap GeoDNS diversity for free.
- **Anti-flapping.** On a split/disagreeing verdict, mark the domain `inconsistent`, recheck sooner, and **do not** write a changelog transition. Consider also requiring stability across N consecutive scans before committing a status change. Research the best rule (this is the "layman's Byzantine generals" idea).
- **Pure IPv6-only connectivity check** (the headline new check): dial the AAAA over **tcp6 only, no IPv4 fallback**, and complete HTTP/HTTPS + TLS. Success genuinely means "reachable over v6."
- **Self-preflight (critical).** Keep v6audit's guard: the crawler skips a scan cycle if *its own* IPv6 is down, to avoid false `unsupported` verdicts. This is the #1 false-negative source.
- **SSRF-safe DNS-pinned dialer** (keep v6audit's `ssrf.go`): resolve once, validate the IP against reserved/metadata ranges, dial the validated IP. Essential when connecting to arbitrary domains at scale.
- **Honest, self-identifying User-Agent** with an opt-out contact URL, e.g. `WhyNoIPv6Bot/1.0 (+https://whynoipv6.com/bot)`.

### 3.2 Scale & throughput
- Target: **Tranco top 1M as the ranked core** (see §4). Design so Tranco's larger *full* (untruncated) list can be adopted later without a schema change (rank nullable). Tranco is the only data source — no third-party 10M lists.
- **Multiple crawler worker processes, all on one ASN** (operator has plenty of hardware). They are **throughput workers sharing one logical vantage** — NOT geo-diversity. Consensus comes from the 3 resolvers, not the machines.
- Redesign work distribution: the old/ v6audit schedulers materialize all-due-domains into memory and **do not scale to millions**. Research a **frontier table** claimed via `FOR UPDATE SKIP LOCKED` (or River, or another queue) with streaming enqueue.
- **Target cadence = daily for all 1M** (the old 3-day figure was an under-concurrent single-machine artifact, not a real limit). Throughput needed: 1M / 86,400s ≈ **~12 domains/sec sustained** — trivial with a few hundred workers given the conditional two-phase execution (most domains have no AAAA → DNS-only). Scheduler behavior: **complete a full pass within 24h; if it falls behind, process in rank order** so top-ranked domains stay freshest. Keep **cadence-per-rank-band as config** (default daily everywhere) as a pressure valve and for a future larger list.
- **DNS resolver-load strategy (the real scaling constraint at daily + consensus).** Naive daily×3-resolver consensus is ~15–20M public-resolver queries/day (~200 qps, ~70 qps/provider) — risks abuse-throttling. Recommended split: run **consensus against the 3 public resolvers ONLY on the classification-critical records (apex + www AAAA)** (~23 qps/provider, safe, keeps GeoDNS diversity), and do **all other lookups** (NS-chain, MX host AAAA, A records, the ≤50 sub-resource AAAA) through **your own local recursive resolvers** (Unbound/Knot — unlimited throughput). Single-ASN is fine: public resolvers supply geo-diversity, local recursors supply volume. Research/validate this split.
- **Anti-flap at daily cadence:** require a status change to hold for **2 consecutive scans** before writing a changelog transition (only +1 day latency at daily), on top of the quorum rule.
- Keep the conditional two-phase execution from v6audit (skip web/mail checks when there's no AAAA/MX → cheap for the no-v6 majority).

### 3.3 Checks to include (all reported 3-state)
Base AAAA (apex), www AAAA, nameserver v6, MX v6, **pure IPv6-only HTTP/HTTPS reachability**, TLS validity over v6 (expiry + hostname), SMTP-over-v6 (EHLO/STARTTLS), v4-vs-v6 response parity, **sub-resource IPv6** (issue #23, see §5), and informational: DNSSEC, PTR, latency v4/v6. Recommend which are core-classification vs informational-only.

---

## 4. Data source — replace `tldbwriter`

- Current: external `tldbwriter` tool ingests Tranco 1M into `lists`/`sites`. Replace with a **small in-repo Go ingester** that fetches the Tranco daily permalink list directly, upserts into the frontier table, and updates ranks.
- **Tranco is the only data source. No third-party lists (DomCop/OpenPageRank etc. are explicitly excluded).** Tranco standard = **top 1M pay-level domains** — that is the ranked core. Tranco also offers a *full* (untruncated) list of a few million; there is no Tranco 10M. Model **rank as nullable** so the full list can be adopted later without a schema change, but the default and primary target is the Tranco 1M.
- **Main ranked list = eTLD+1 (pay-level domains).** Ingest Tranco's pay-level list, not the subdomain-inclusive variant — the public shame list is registrable domains (cleaner, no duplicate apex/www entries, no CDN/infra-subdomain noise). Subdomains are still first-class entities (see §6 domain-entity model) but only enter via campaigns and #23 resources, where intent exists — never as auto-ranked shame entries.

---

## 5. Issue #23 — sub-resource / dependency checking (design this properly)

GitHub `lasseh/whynoipv6#23`: apex+www AAAA is insufficient — a site can still break on
IPv6-only because CSS/JS/fonts/API endpoints load from IPv4-only hosts. Two iterations
requested: (1) **manual** per-domain/campaign required-endpoint lists (cf. the
`ipv6-in-real-life` TOML), (2) **auto-discover** endpoints by crawling.

Both halves already exist in the codebase to lift:
- Auto-discover = v6audit `resource_ipv6` check (fetch page over IPv6, enumerate external sub-resource hosts, AAAA-check each).
- Manual = a `domain_resource`-style table (curated or discovered endpoints, each with its own 3-state status).

**Relationship, not entity:** a resource/dependency is a host a domain's page *depends on*
— frequently on a **different registrable domain** (a CDN like `fonts.googleapis.com`), not
a subdomain of the site. So resources are a **dependency relationship** (checked inline
during the resource check + an optional manual required-endpoints list, rolled up into the
domain's resource-status). They are **not** auto-promoted to independently-tracked domains,
and are **distinct** from the campaign subdomain parent/child hierarchy in §6 (the two only
overlap when a dependency happens to be a same-domain subdomain).

**Research task:** design the data model and classification so "fully IPv6 ready" =
apex+www+ns+mx **plus** required resources all supported — still 3-state, no score.
Address: how manual endpoints attach to a domain and/or a campaign, how discovered
endpoints are deduped/aggregated (unique external hosts), and how this affects the
`heroes`/`sinners` definitions. This must be wired through schema, service, API, and
crawler from the start (the last attempt bolted it on late — avoid that).

---

## 5.5 Classification rules — hero / partial / sinner (decided)

There is **no score**. Classification is a deterministic **rule-based ladder** over the
3-state dimension results. Evaluate top-down; the first matching tier wins.

**Evaluation principles (apply to every rule):**
- Only a **definitive, quorum-confirmed `unsupported`** counts against a domain. `error`
  (transient) and `not_applicable` (dimension doesn't apply, e.g. null-MX domain) are
  **never** held against it — they're excluded, not penalized. This is also the anti-flap guard.
- `disabled` / detected service domains are excluded from classification entirely.

**The ladder:**
1. **Inactive** — apex = `no_record` (no A *and* no AAAA: parked/dead). Not shamed, not a hero. Kept out of the shame list.
2. **Sinner (the shame list)** — apex has A but AAAA is `unsupported` (`base_domain = unsupported`). The classic "hasn't started" IPv4-only holdout. **This is the only shame trigger** — rich checks never lower the shame bar.
3. **Hero** — apex reached this branch (apex AAAA `supported`) **and** all of: www AAAA supported, nameserver v6 supported, the **pure IPv6-only connection succeeds**, and MX v6 supported *if the domain has mail* (MX `not_applicable` → skipped, doesn't block). A hero demonstrably *works* over IPv6, not merely publishes records.
   - **Gold (badge, not a separate tier)** — a Hero whose required **sub-resources (#23)** are also all v6. The "fully ready incl. dependencies" mark. Optional higher recognition; not required to be a Hero.
4. **Partial / "almost there"** — apex AAAA supported but not meeting the Hero bar. Carries **sub-reason flags** so the advocacy message is specific, notably:
   - **`broken_v6`** — publishes AAAA but the site does **not** load over IPv6-only (a distinct, valuable "fix your broken v6" signal). Explicitly **not** shamed.
   - plus flags like `www_missing`, `ns_missing`, `mail_missing`, `resources_v4only`.

**Surfacing:** `/domain` = sinners; `/domain/topsinner` = sinners ordered by Tranco rank
(+ `top_shame` for editorial picks); `/domain/heroes` = heroes by rank; add an
**"almost there"** view for the partial tier. Research whether these are materialized
columns (a `classification` enum + flags on the current-state table, recomputed each
scan) or views — recommend materialized for query speed at 1M.

## 6. Data model research

Propose a full schema (this is greenfield — improve freely):
- **Domain entity model (FQDN, not just eTLD+1).** A domain is a hostname with a **`kind`**
  (`apex` = registrable/eTLD+1, or `subdomain`) and a **`parent_id`** self-reference
  (subdomain → its registrable domain). Requirements:
  - **Sources:** registrable domains come from Tranco (ranked, eTLD+1 — **Tranco contributes
    NO subdomains**). Subdomain *entities* come **only from campaigns** (a campaign YAML may
    list `nettbank.dnb.no`), rank NULL, never auto-ranked. A plain Tranco domain with no
    campaign entry simply has no children. (Issue-#23 resources are a *separate* dependency
    relationship, not tracked subdomain entities — see §5.)
  - **Associate, don't collapse.** A campaign entry like `nettbank.dnb.no` is stored and
    checked **as given** (it carries intent the apex doesn't). Adding a subdomain
    **auto-ensures its registrable parent exists** (create `dnb.no` with rank NULL if it
    isn't already present) and links `parent_id` to it — so the parent page can list it.
  - **Drill-down display (required).** The domain-detail page/API for a registrable domain
    **lists its child subdomains** with each child's own status, so a user checking `dnb.no`
    sees and can drill into `nettbank.dnb.no` etc. on the same page.
  - **Check set adapts to `kind`.** For `subdomain` entities, **`www` is `not_applicable`**
    (`www.nettbank.dnb.no` is not a thing) and **`MX` is usually `not_applicable`**; the
    meaningful checks are host AAAA + nameserver (the NS walk climbs to the authoritative
    zone automatically) + connectivity + resources. Because `not_applicable` never counts
    against a domain (§5.5), a subdomain can still be a Hero on host+connectivity+NS.
  - **Classification is per-entity.** A registrable domain's hero/partial/sinner tier is
    based on **its own** checks; children are shown alongside for context (optionally an
    aggregate hint like "3 of 5 subdomains v6-ready") but a child's status does **not**
    change the parent's tier or the shame list.
- **TimescaleDB** for all time-series: per-scan history + metrics as hypertables with compression + **retention** policies + **continuous aggregates** for dashboards. This directly kills the old unbounded-`campaign_domain_log` bloat (no more manual `cleanup_*.sql`).
- **Split write model** (from v6audit): append-only per-scan log (JSONB results) + a materialized current-state table with typed per-check columns. Single vantage → no `location_id`/multi-location columns.
- **Confirmed status & trustworthy changelog (hard requirement).** The changelog is the
  site's credibility surface — **users must be able to trust every entry.** Therefore
  public classification (hero/partial/sinner), the heroes/sinners lists, and the changelog
  all derive from a domain's **confirmed status**, not the latest raw observation. A
  dimension only advances to a new confirmed value when it is **quorum-confirmed (2/3
  resolvers)** AND has **held for ≥2 consecutive scans** AND is **definitive** (never from
  an `error`/transient). A changelog entry is written **only on a confirmed transition** —
  never on a single observation or a fluctuation. Keep a separate `last_observed` value for
  debugging/telemetry, but it never drives public output. Consider stricter confirmation
  (e.g. 3 consecutive scans) for the noisier connectivity/resource checks. Research where
  "confirmed status" lives (a column pair on the current-state table + a pending-change
  counter) and how the crawler commits transitions.
- **Structured field-level changelog** (from v2-team): `field` + `old_value`/`new_value`, not free-text messages, one row per **confirmed** transition.
- **Search:** replace `LIKE '%x%'` with `pg_trgm` GIN (and/or FTS). Old search table-scanned.
- **Rank nullable**, source-tagged domains (Tranco vs tail).
- **Campaigns** (uuid/name/description/domains), campaign per-scan history + changelog, with the same retention treatment.
- **Stats & time-series for dashboards (hard requirement).** Persist crawler results as
  time-series so we can build graphs at every level. Design continuous aggregates / rollup
  tables to serve, cheaply, all of:
  - **Overall / global** — IPv6 adoption over time: counts by classification (hero/partial/sinner/inactive) and per-check-dimension adoption (apex/www/ns/mx/connectivity/resources), daily. This drives the public site's headline graphs and an operational overview dashboard.
  - **Per-domain** — a domain's status history over time (from the per-scan `domain_logs`), for the domain-detail page graph.
  - **Per-campaign** — each campaign's adoption trend over time (v6-ready count, per-dimension), for campaign graphs.
  - **Per-ASN and per-country** — adoption + rankings over time.
  Separate **product stats** (adoption trends — persisted, public, served by the API/frontend)
  from **operational stats** (crawler throughput/success-fail/duration/queue-depth — for
  Grafana). Recommend the hypertable + continuous-aggregate layout, refresh policies, and
  which are API-served vs Grafana-only. GeoIP via MaxMind mmdb (keep).
- **Service-domain detection** (research): auto-flag likely CDN/backend domains that shouldn't be shamed. Heuristics to evaluate: domains that consistently return `no_record` on both apex+www; high CNAME in-degree (target of many domains); known patterns. Flag *candidates for review* rather than auto-disabling. Keep the existing `service_domains.yml` manual list too.
- **Disabled-domain lifecycle** (research): a `disabled_reason` enum. `dead` (NXDOMAIN/SERVFAIL) → auto re-validate on a slow cadence and auto-re-enable if it returns; `service`/`manual` → stays until changed. (Answers "should we periodically reset the disable?" — yes, only for `dead`.)

---

## 7. API & features

- Keep the existing public API contract the frontend depends on (domains list/heroes/topsinner/detail/log/search; country + sinners/heroes; changelog family; campaign family; metric overview + ASN). Study `whynoipv6/internal/rest/` for exact shapes. Note the old `/metric` (singular) path.
- **Domain detail must include the domain's child subdomains** (from the §6 entity model) with each child's status, so the frontend can render the drill-down on the same page. Design whether this is embedded in the detail response or a `/domain/{domain}/subdomains` sub-resource.
- **Live "check any domain"** endpoint: on-demand run of the full engine for a domain not in the list (cf. ready.chair6.net, mythic-beasts health-check). Return per-check 3-state results. Design queueing/rate-limiting for public abuse resistance.
- **Downloadable datasets** for researchers: dated snapshots in **top 100k / top 1M / full** sizes, **CSV + Parquet**, with a data dictionary. Recommend generation (scheduled export job → static files) and hosting.
- **Automated campaign ingestion:** on campaign-repo PR merge, auto-pull and sync into the backend (operator can wire a Semaphore CI webhook). Design the trigger + idempotent import (create/update campaign, add/remove domains, write back generated UUID).
- **Stats endpoints** to serve the product graphs defined in §6 (overall adoption, per-domain, per-campaign, per-ASN, per-country over time). Crawler operational metrics (throughput, success/fail, duration, queue depth) go to Grafana, checkpointed during each pass, not the public API.
- **OpenAPI spec** generated and kept in sync automatically (e.g. on `make build`); also emit TypeScript types for the frontend.
- **Operational notifications** (keep it simple): healthcheck pings + a webhook for crawl summaries. No end-user notifications.

---

## 8. Explicitly out of scope for THIS research round
- Frontend redesign (only keep API compatibility; visual look is frozen).
- SEO / `llms.txt` / AI-markdown content workstream (important, but a separate later brief).
- Multi-region / geo-distributed connectivity probes (Tier 2 — a later addition; design the schema so it's *possible*, don't build it).
- A–F scoring / numeric grades (permanently cut).

---

## 9. Suggested target stack (validate, don't assume)
Newest Go toolchain; Postgres 18 + TimescaleDB (pg18); pgx/v5 + pgxpool; sqlc; chi v5;
cobra + viper; `log/slog`; miekg/dns; MaxMind mmdb GeoIP; golang-migrate. Hexagonal
layering per the v2-team backend. Binaries: at least `api` and `crawler` (+ a `ctl`
CLI for import/campaign/export/admin). Docker + docker-compose; systemd for prod.

---

## 10. What to deliver (the report)

Produce a markdown design report (propose filename under `docs/`) containing:
1. **Executive summary** — the recommended architecture in one page.
2. **Crawler design** — consensus algorithm (quorum + anti-flap), check set (core vs informational), two-phase execution, self-preflight, SSRF, UA, worker/frontier model, cadence-per-rank-band (default **daily for all 1M**), throughput math validating the daily target, the **DNS resolver-load split** (public-resolver consensus on apex/www only + own local recursors for the rest), and the path to Tranco's full list.
3. **Data-source plan** — Tranco-only ingester design (replacing `tldbwriter`); Tranco 1M as the primary target with the full-list path via nullable rank; rank/source modeling.
4. **Proposed database schema** — every table + column + type, hypertable/compression/retention/continuous-aggregate choices, indexes, the split write model, the **confirmed-status + trustworthy-changelog** model (§6), the **stats/continuous-aggregate layout** for overall/per-domain/per-campaign/per-ASN/per-country graphs (§6), service-domain + disabled-domain lifecycle, and the issue-#23 resource/endpoint model wired through.
5. **API surface** — endpoints (keeping the current contract), the live-check endpoint, dataset-download endpoints/files, OpenAPI approach.
6. **Package/binary layout** — hexagonal structure within `backend/` (per §2.5), which v6audit/v2-team code to lift verbatim vs adapt.
7. **Campaign automation** — PR-merge → import pipeline.
8. **Phased implementation plan** — milestones from empty repo to production, with what to build first (crawler core) and verification steps per phase.
9. **Open risks & decisions** — anything you couldn't resolve, with options and a recommendation.
10. **Explicit tradeoffs** — where you chose one approach over another and why.

For each major recommendation, state the alternative you rejected and why. Cite the
existing-code paths you're lifting from. This is round 1 — flag what needs a human
decision so we can iterate.

---

### Appendix: known gotchas from the current codebase (don't recreate)
- Old nameserver check queried NS on the naive last-two-labels TLD split — breaks for multi-label TLDs (`co.uk`) and checks the wrong zone. Fix: walk up to the actual zone apex (v6audit does this).
- Old API bound `[::1]:PORT` (IPv6 loopback only, behind nginx) — keep intentional but document.
- `v6_only`/`ts_v6_only` columns existed but were never populated — the pure-v6 connectivity check now fills that gap.
- Two near-identical crawlers (domain + campaign) — consolidate into one engine parameterized by target set.
- Unbounded log tables required manual batch-delete scripts — solved by TimescaleDB retention from day one.
