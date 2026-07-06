# WhyNoIPv6 Backend Rewrite — Design Report

**Status:** Round 1.2 — all round-1 open items resolved in operator review on 2026-07-06
(decision log in §9); OPEN-3 subsequently revised: no campaign `resources:` syntax,
campaigns express endpoint intent via `domains:` entries + the subdomain entity model.
Still a proposal, not implementation.
**Input:** `docs/backend-research-brief.md` (authoritative), study of the production
`whynoipv6` backend, `claude/whynoipv6-team/backend` (v2 rebuild), `v6audit`
(checker engine), `claude/whynoipv6/prompts` + `REVIEW-REPORT.md`, `whynoipv6-campaign`,
`whynoipv6-web`, plus web research on Tranco, public-resolver limits, Unbound at scale,
Go DNS/HTTP-over-IPv6, and TimescaleDB 2.28/PG18 (all findings cited inline).
**Convention:** every major recommendation states the rejected alternative. Open items
are marked **OPEN-n** and resolved in the §9 decision log.

---

## 1. Executive summary

The new backend is **one Go module with three binaries** (`api`, `crawler`, `v6ctl`)
over **Postgres 18 + TimescaleDB 2.28 (Community edition)**, laid out hexagonally per
the v2-team rebuild, with the **v6audit checker engine lifted nearly verbatim** as the
scanning core (minus scoring).

The heart of the design is the **crawl → confirm → publish pipeline**:

1. **Frontier scheduling in the database.** Every scannable host is a row in `domain`
   with a `next_check_at`. Crawler workers claim batches with
   `FOR UPDATE SKIP LOCKED` — no in-memory materialization of due domains (the thing
   that killed both old schedulers), no external queue. Multiple crawler processes on
   one ASN scale linearly.
2. **One engine, every target.** The production codebase's twin crawlers (domain +
   campaign, ~90% duplicated) collapse into a single engine because campaign
   membership becomes a **join table onto the same `domain` entity** — a domain is
   checked once per day no matter how many lists it is on. The engine is v6audit's
   `internal/checker` (16 checks, two-phase conditional execution, SSRF-pinned dialer,
   IPv6 self-preflight, NS zone walk) with scoring deleted.
3. **Split DNS load.** The classification-critical records (apex + www AAAA) are
   resolved through **3 public resolvers concurrently (Cloudflare/Google/Quad9) with a
   2-of-3 quorum**; everything else (NS chains, MX hosts, A records, sub-resource AAAA)
   goes through **local Unbound recursors**. That's ~23 qps per public provider
   (validated safe: Google documents 1500 qps/IP, Quad9's contact threshold is
   500 qps) and ~150–200 qps on Unbound (an order of magnitude of headroom on one box).
4. **Confirmed status is the only public truth.** Raw observations land in an
   append-only `scan` hypertable. A dimension's public status advances only when a new
   value is quorum-confirmed, definitive (never from `error`), and has held for 2
   consecutive daily scans (3 for connectivity/resources). Only a confirmed transition
   writes a `changelog` row. Classification (hero/partial/sinner/inactive) is a
   materialized enum column recomputed from confirmed statuses in the same transaction.
5. **TimescaleDB kills the unbounded-log problem from day one.** Per-scan history,
   scan details (JSONB), changelog, crawler metrics and stats series are hypertables
   with columnstore compression and retention policies. Product stats (adoption graphs
   at global/country/ASN/campaign/domain level) are served from nightly snapshot
   rollups of confirmed state; observed-adoption continuous aggregates and operational
   crawler metrics feed Grafana.
6. **API compatibility is preserved at the root paths** (`/domain`, `/campaign`,
   `/country`, `/changelog`, `/metric` — singular), including the quirks the Vue
   frontend depends on (offset pagination at fixed page size 50, shortuuid URLs, the
   `{"data": [...]}` search envelope, the `{campaign, domains}` composite). New
   surfaces are added alongside: subdomains drill-down, resources, live check, stats,
   datasets, an "almost there" view.

**Cost honesty:** the crawler at daily cadence is computationally trivial
(~12 domains/s sustained, ~60–130 concurrent domain slots, ~1–2 GB/day of raw scan
detail before compression). The real costs are (a) the confirmed-status state machine —
the one genuinely new, subtle component, (b) operating Unbound as production
infrastructure, and (c) API-compat parity testing against the old backend. Everything
else is assembly of parts that already exist in the studied repos.

---

## 2. Crawler design

### 2.1 The engine: lift v6audit's checker

The scanning core is `v6audit/internal/checker` — the per-check inventory, statuses,
timeouts and payloads were confirmed by direct code reading. Lift **verbatim**:

| Path | What it is |
|---|---|
| `internal/checker/resolver.go` | DNS resolver: EDNS0, UDP→TCP-on-truncation, retry-once-on-next-upstream, TTL cache (30s–300s clamp, RFC 2308 negative caching), CNAME chase ≤10 hops with loop detection. Behavior lifted 1:1; API ported to miekg/dns **v2** (§2.4, OPEN-9) |
| `internal/checker/ssrf.go` | `SafeDialer`: full v4+v6 blocklists (RFC1918, CGNAT, link-local/metadata, Teredo/6to4, NAT64, ULA, AWS v6 metadata), DNS-pinned dialing — resolve once, validate, dial the literal IP |
| `internal/checker/dns_aaaa_base.go`, `dns_aaaa_www.go` | apex/www AAAA with NXDOMAIN→`not_applicable`, CDN detection on www CNAME chain |
| `internal/checker/dns_ns_ipv6.go` | NS with **label walk-up zone discovery** — fixes the production `co.uk` bug without a PSL |
| `internal/checker/dns_mx_ipv6.go` | MX with RFC 5321 implicit-MX fallback and RFC 7505 null-MX → `not_applicable` |
| `internal/checker/http_ipv6.go`, `https_ipv6.go`, `tls_ipv6.go` | **pure tcp6-only reachability** (the headline check), TLS handshake w/ expiry+hostname; only the UA constant changes |
| `internal/checker/response_parity.go` | v4-vs-v6 fetch comparison (status/content-type/±10% body length) |
| `internal/checker/resource_ipv6.go` | sub-resource discovery: IPv6-pinned page fetch, streaming HTML tokenizer, external-host dedup, ≤50 hosts, concurrent AAAA checks |
| `internal/checker/smtp_ipv6.go`, `dns_ptr_ipv6.go`, `dns_dnssec.go`, `spf_ipv6.go`, `latency.go` | SMTP EHLO over v6, PTR+FCrDNS, resolver-validated DNSSEC (AD flag), SPF v6 mechanics, TTFB v4/v6 |
| `cmd/v6agent/main.go:356-380` (`checkIPv6Connectivity`) | the **IPv6 self-preflight** — moved in front of every claim cycle |
| `runner.go` `runPhase`/`runCheck` | bounded-errgroup phase execution with per-check panic recovery |

**Adapt** (not verbatim): `checker.go` (drop `Category()`, drop `ScanResult.Score/Grade`,
drop `DBColumnToChecker`), `runner.go` (remove the `ComputeScore` call; keep two-phase
gating and skip-reasons), and the resolver wiring (§2.4). **Delete**: `scoring.go`, all
of v6audit's auth/billing/alerting, the `_global` synthetic-row score merging.

*Rejected alternative — rewrite the engine fresh:* the checker files are small,
tested-in-anger, and encode a lot of RFC edge-casing (null MX, implicit MX, negative-TTL
caching, FCrDNS, SSRF v4-mapped-address traps) that a rewrite would re-learn by bug.
*Rejected alternative — extend the production resolver:* it has the naive
last-two-labels TLD split, hardcoded Cloudflare-only upstreams, and no reachability
checks at all; there is nothing to keep except its globally-routable-AAAA filter
(worth porting into `dns_aaaa_*` — reject loopback/link-local/ULA answers,
`whynoipv6/internal/resolver/resolver.go:486-514`).

### 2.2 Check set: core vs informational

Engine statuses are 5-valued (`supported/unsupported/partial/error/not_applicable`).
The public model is the brief's 4-valued `ipv6_status`
(`supported/unsupported/no_record/not_applicable`); `error` exists only on raw
observations and never becomes public. The mapping is **explicit per dimension**
(the last rewrite's mistake was collapsing `partial→supported` silently in
`workers.go:341-420`):

| Dimension (public) | Engine source | `partial` maps to | Role |
|---|---|---|---|
| `base` (apex AAAA) | `dns_aaaa_base` | n/a | **core** — the sinner trigger |
| `www` (www AAAA) | `dns_aaaa_www` | n/a | **core** — hero gate |
| `ns` (nameserver v6) | `dns_ns_ipv6` | `supported` (≥1 NS with AAAA ⇒ zone resolvable over v6; all-NS detail kept in scan payload) | **core** — hero gate |
| `mx` (mail v6) | `dns_mx_ipv6` | `supported` (≥1 MX host with AAAA ⇒ mail deliverable over v6) | **core** — hero gate when mail exists |
| `conn` (pure IPv6-only reachability) | `https_ipv6`, fallback `http_ipv6` if the site is http-only | n/a | **core** — hero gate; failure ⇒ `broken_v6` flag |
| `resources` (#23 dependencies) | `resource_ipv6` + manual endpoints (§4.6) | `unsupported` (any v4-only required host ⇒ not fully ready) + `resources_v4only` flag | **core for Gold badge only** — never affects hero/sinner |
| `tls` validity | `tls_ipv6` | n/a | informational (an invalid cert already fails `conn` via https) |
| `smtp` EHLO over v6 | `smtp_ipv6` | `unsupported` | informational |
| `parity` v4-vs-v6 | `response_parity` | kept as-is in payload | informational |
| `dnssec`, `ptr`, `spf`, `latency_v4/v6` | respective checks | kept as-is | informational |

*Rejected — strict all-NS/all-MX = supported:* a zone with one v6-capable NS **is**
resolvable from an IPv6-only network, and one v6 MX **does** accept mail over v6;
requiring all hosts would shame operators who are functionally v6-ready. The all-hosts
detail stays visible in the scan payload for the detail page. (**OPEN-1: decided** —
≥1-host rule adopted.)

*Rejected — dropping SPF/PTR/DNSSEC (not in brief §3.3's core list):* they cost 1–3
cheap local-resolver queries each, come free with the engine, and feed the detail page.
They are informational-only and never gate classification.

**Two-phase conditional execution** (lifted from `runner.go:60-128`): phase 1 always
runs `base, www, ns, mx, dnssec, spf, latency_v4`; phase 2 (`conn/tls/parity/
resources/latency_v6/ptr` gated on an AAAA existing, `smtp` gated on MX v6) is skipped
with recorded `not_applicable` results. Since ~72–75% of the top-1M has no AAAA, most
domains cost only DNS.

**Kind-aware checks (§6 entity model):** for `kind = subdomain` entities
(`nettbank.dnb.no` from campaigns), `www` is forced `not_applicable` and the MX check
**skips the implicit-MX fallback** (explicit MX → evaluate normally; no MX →
`not_applicable`, not "the AAAA accepts mail"). NS walk-up already climbs to the
authoritative zone automatically. Because `not_applicable` never counts against a
domain, a subdomain can be a Hero on host + NS + conn.

### 2.3 Consensus (Tier 1) and anti-flap

**Quorum applies only to the two classification-critical lookups: apex AAAA and www
AAAA.** For each, query Cloudflare (`1.1.1.1`/`2606:4700:4700::1111`), Google
(`8.8.8.8`/`2001:4860:4860::8888`) and Quad9 (`9.9.9.9`/`2620:fe::fe`) **concurrently**
(2s per-resolver timeout, one retry). Each resolver's answer is reduced to a status —
`supported` (≥1 globally-routable AAAA), `unsupported` (NOERROR, no AAAA),
`no_record`/`not_applicable` (NXDOMAIN) — and the quorum is taken **over statuses, not
record sets** (GeoDNS legitimately returns different AAAA contents per region; what
must agree is *whether v6 exists*).

- 3 answers, ≥2 agree → that's the observation.
- 2 answers (one timeout), both agree → observation stands (2 of 3 configured).
- Otherwise → observation = `inconsistent`: **treated exactly like `error`** — recorded
  in the scan log, never advances confirmed state, never writes changelog, and the
  domain's `next_check_at` is pulled in to +2h for a sooner recheck.

**Anti-flap / confirmation rule (the "layman's Byzantine generals" answer):** a
dimension's **confirmed** value changes only when a new definitive value has been
observed on **N consecutive scans** — N=2 for DNS dimensions (`base/www/ns/mx`), N=3
for the noisier `conn` and `resources`. At daily cadence that is +1/+2 days of
transition latency, which is the right trade for a changelog users must trust. The
full commit algorithm and storage are in §4.3. Web research found no measurement
project publishing a stricter rule — APNIC smooths over 30 days, Cloudflare Radar over
7; the N-consecutive pattern is codified prior art from Nagios/Icinga
(`max_check_attempts`) and the IPv6-hitlist literature's "informed repeated probing."
Our rule is stricter than everything published, which is the point.

*Rejected — consensus on every lookup:* 3× the public-resolver load (~70 qps/provider,
into Cloudflare's undocumented throttle territory) for records that don't gate
classification. *Rejected — running quorum across crawler machines instead of
resolvers:* all machines share one ASN/vantage; the three anycast resolver networks
provide the geo-diversity, machines only provide throughput (brief §3.2, locked).

### 2.4 DNS resolver-load split

Two resolver instances are injected into the engine (a small adaptation of
`checker.Resolver` construction — the checks themselves don't change):

- **Consensus resolver** — used *only* by `dns_aaaa_base` and `dns_aaaa_www`. Fans out
  to the 3 public resolvers, applies quorum, returns the agreed answer + a
  disagreement annotation.
- **Bulk resolver** — used by everything else (NS chain + NS-host AAAA, MX + MX-host
  AAAA, A records, PTR, TXT/SPF, DNSSEC probes, the ≤50 sub-resource AAAA lookups).
  Points at **two local Unbound instances** (round-robin, retry-on-next — the existing
  `resolver.go` behavior needs zero changes, just different upstream addresses).

Load math at 1M/day (see §2.7 for the full table): consensus = 2 records × 3 resolvers
× 1M = 6M queries/day ≈ **69 qps total, ~23 qps per provider**. Research verdict:
Google documents **1500 qps/IP** (we'd use 1.5%); Quad9 documents a **500 qps contact
threshold** (4.6%); Cloudflare publishes no number but flags "security scanning"
patterns and SERVFAIL storms — mitigations: smooth the rate (no bursts), never retry a
SERVFAIL'ing domain in a tight loop, and send a courtesy note to
resolver@cloudflare.com describing the research use (cheap insurance, recommended).

Bulk = ~12–18M queries/day ≈ **140–210 qps** against Unbound. Tuned per NLnet Labs
guidance (libevent build, `outgoing-range: 8192`, `num-queries-per-thread: 4096`,
multi-GB `rrset-cache-size` — the top-1M shares a small set of NS/MX providers, so
cache hit rates are high; keep `qname-minimisation` on, keep negative caching on),
a single instance handles 6–20k qps; we run two for redundancy, not capacity.
**DNSSEC note:** `dns_dnssec.go` relies on a *validating* upstream (AD flag) — enable
DNSSEC validation in Unbound (default) and the check works unchanged.

Politeness toward authoritatives (OpenINTEL model): the daily pass is already spread
over 24h by the scheduler, and claim batches are effectively rank-ordered rather than
zone-clustered; Unbound's infra-cache backs off from lame/slow servers automatically.
Source-IP spread is unnecessary at this rate.

*Rejected — all queries through public resolvers:* ~15–20M/day ≈ 70 qps/provider,
inside the risk zone for Cloudflare and pointless — geo-diversity only matters for the
classification-critical AAAA. *Rejected — all queries through local recursors:* loses
the 3-network GeoDNS diversity exactly where it matters (a CDN could serve AAAA to one
region only). *Rejected — zdns as the bulk resolver library:* solid tool (it's how
research scans do iterative resolution), but v6audit's resolver already exists, is
integrated with the checks and the SSRF dialer. During the lift the resolver is
**ported to miekg/dns v2** (Codeberg — actively developed, ~2× faster; v1 is
maintenance-mode; **OPEN-9: decided**): the port is mechanical per the official
v1→v2 migration guide, and reverting to the v1 API is equally mechanical if v2
misbehaves during phase-2 verification.

### 2.5 Worker / frontier model

**The frontier is the `domain` table itself.** Scheduling state = `next_check_at`
(+ `rank`) with a partial index; workers claim atomically:

```sql
UPDATE domain SET claimed_at = now()
WHERE id IN (
  SELECT id FROM domain
  WHERE NOT disabled
    AND next_check_at <= now()
    AND (claimed_at IS NULL OR claimed_at < now() - interval '30 minutes')
  ORDER BY rank ASC NULLS LAST, next_check_at ASC
  LIMIT $1
  FOR UPDATE SKIP LOCKED
) RETURNING id, host, kind, ...;
```

This is the v2-team `ClaimBatch` pattern (`internal/postgres/domain_repo.go`),
hardened: a separate `claimed_at` lease (instead of v2's "claiming sets `ts_check`")
means a crashed worker's batch is reclaimed after 30 minutes rather than lost for a
day. On successful commit the worker sets `next_check_at = now() + cadence(rank)` and
clears the lease. `ORDER BY rank ASC NULLS LAST` implements the brief's fall-behind
policy directly: when due-domains exceed capacity, top-ranked domains are refreshed
first and the tail's effective interval stretches — graceful degradation, no separate
mode. (The starvation risk this creates for the tail under *permanent* undercapacity
is accepted per brief; a config flag can flip the sort to `next_check_at ASC` as an
aging pressure valve.)

Process model: **K crawler processes × M concurrent domain slots each** (start: 2
processes × 64 slots; §2.7 shows ~60 average concurrency suffices, headroom for tail
latency). Each process: preflight → claim batch (~200) → feed an in-process worker
pool running `Runner.Run` per domain → commit results per domain in one transaction
(§4.3) → batch-write scan rows with `pgx.Batch`/`CopyFrom` (the v2 rebuild's
2M single-row round-trips/day is a known wart) → claim next batch.

**Cadence-per-rank-band** is config, default daily everywhere:

```yaml
cadence:
  default: 24h
  bands: []              # e.g. [{max_rank: 10000, every: 12h}, {min_rank: 1000001, every: 72h}]
recheck_inconsistent: 2h
recheck_error: 6h
```

**Self-preflight:** before *every* claim cycle the process runs
`checkIPv6Connectivity` (AAAA + tcp6 dial to a probe host, default
`one.one.one.one:443`); on failure it claims nothing, alerts via the ops webhook, and
retries in 60s. v6audit only had this in the remote agent — the internal worker gap is
explicitly closed here, since a v6-dark crawler mass-producing false `unsupported` is
the #1 false-negative source. Belt-and-suspenders: `conn=unsupported` observations
additionally require the preflight to have passed within the last 5 minutes.

**User-Agent:** `WhyNoIPv6Bot/1.0 (+https://whynoipv6.com/bot)` on every HTTP fetch;
`/bot` page documents purpose, opt-out contact, and crawl behavior. SMTP EHLO name
`whynoipv6.com`.

*Rejected — River (or any job queue):* the work is a flat, uniform, periodic sweep
over a known set — a frontier column + SKIP LOCKED is the whole requirement. A queue
adds a jobs table that must be *filled by a scheduler* (v6audit's scheduler died
exactly there: materializing 1M due domains into memory and millions of job-row
inserts per tick, `workers.go:1067-1193`, 2-minute job timeout). The check-queue for
on-demand live checks (§5.3) is the one place queue semantics are real, and SKIP
LOCKED covers that too. *Rejected — Redis/NATS work distribution:* new stateful infra
to operate for a problem Postgres already solves at this scale.

### 2.6 Crawl pass, stats, and notifications

There is no "pass" barrier in the hot path — workers run continuously against
`next_check_at`. A **daily tick** (03:30 UTC, after most of the day's Tranco delta has
settled) runs in `crawler`'s coordinator goroutine:

1. Snapshot product stats from confirmed state into the `stats_*` tables (§4.7).
2. Recompute country/ASN counter columns (ported `update_country_metrics` /
   `update_asn_metrics`, fixed: v6 definition = classification-based, v4 count =
   actual v4-only count — the production proc counted *all* domains as v4).
3. Service-domain candidate detection (§4.8).
4. Ops summary → webhook (domains scanned, confirmed transitions, error rate, queue
   depth) + healthcheck ping (healthchecks.io pattern, lifted from production's
   heartbeat, IRC dropped).

Checkpointed **operational metrics** stream continuously: each process writes a
`crawler_metrics` row every 1000 domains (run_id, processed/success/fail, qps,
p50/p99 durations, per-dimension counters) — the prompts-spec design
(`11-resource-checker.md`) minus its unbounded in-memory latency slices (use a
streaming histogram/t-digest). Grafana-only, never the public API.

### 2.7 Throughput math (validating daily cadence)

Assume 1M ranked + ~30k campaign/subdomain entities; ~25% have apex or www AAAA
(current adoption ~20–28% depending on measure — use 25%), ~70% have MX.

| Stage | Volume/day | Rate | Sizing |
|---|---|---|---|
| Domains scanned | 1.03M | **~12/s sustained** | — |
| Public-resolver queries (apex+www AAAA × 3) | 6.2M | 71 qps ÷ 3 ≈ **24 qps/provider** | Google 1.6% of limit, Quad9 4.8% of threshold |
| Local-resolver queries (A×2, NS walk ~2 + NS-AAAA ≤4, MX 1 + MX-AAAA ≤5·70%, DS+SOA 2, TXT 1, PTR ≤3·25%, resources ≤50·25%·(dedup ≈ ~8 effective)) | ~14–18M | **160–210 qps** | Unbound: 1–3% of tuned single-instance capacity |
| HTTP(S) fetches (http+https+tls+parity×2+resource page ≈ 5–6 per v6 domain × 258k) | ~1.5M | ~17/s | ~50–80 concurrent sockets |
| Egress bandwidth (parity 2×1MB cap, resource page 2MB cap; typical pages ~200–500KB) | ~200–400 GB | ~25–45 Mbps avg | trivial on operator hardware |
| Worker concurrency: phase-1-only ≈ 2–4s wall (775k), full phase-2 ≈ 10–25s (258k) → weighted ≈ 6s/domain | — | 12/s × 6s = **~72 slots avg** | provision 128 slots (2 procs × 64) for tail latency |
| DB writes: 1 scan + 1 detail row/domain + state UPDATE, batched | ~3.1M rows | ~36/s | nothing for PG18 |

Conclusion: **daily for all 1M is comfortably achievable on one machine**; two crawler
processes are for resilience and deploy hygiene, not capacity. The old 3-day figure
was, as the brief says, a concurrency artifact (10 workers × sequential checks × 20s
DNS timeouts).

**Path to Tranco full (~4.5M):** rank is already nullable and the frontier is the
domain table, so adopting the full list is: ingest `download/{ID}/full` (verified
anonymous, ~109 MB), set band cadence (e.g. rank ≤1M daily, >1M every 3–7 days), and
multiply the math above by the band factors — public-resolver load stays bounded
because only apex+www AAAA use consensus (4.5M daily would be ~104 qps/provider —
that's when per-band cadence or a 4th resolver becomes necessary; at 1M+tail-every-3d
it stays ≈40 qps/provider). No schema change required, by construction.

---

## 3. Data-source plan (Tranco-only, replacing `tldbwriter`)

All Tranco mechanics below were **verified live** (2026-07-06) against
tranco-list.eu, not just docs.

**Ingester** = `v6ctl tranco import` (also invoked by a daily systemd timer /
`crawler` coordinator at **23:15 UTC** — the daily list is generated 22:00–23:00 UTC):

1. `GET https://tranco-list.eu/top-1m-id` → plain-text list ID (e.g. `94VW2`).
   If unchanged from `tranco_import.list_id` of the last run → done (retry in 2h).
2. `GET https://tranco-list.eu/top-1m.csv.zip` with **conditional GET**
   (`If-None-Match`; the endpoint serves strong ETag + Last-Modified and honors 304 —
   verified). This is the **standard list = pay-level domains** (its config is
   `filterPLD: "on"` — the eTLD+1 requirement is the default artifact, no variant
   selection needed). One inner file, always named `top-1m.csv`.
3. Parse: `rank,domain` CSV, **CRLF line endings**, no header. Normalize: lowercase;
   already punycode (1,452 `xn--` entries, pure ASCII). **Validate and reject
   garbage** — the live list contains `_wildcard_.ph`-style entries and mixed-case
   junk; reject `_`, empty labels, >253 chars, non-LDH.
4. Upsert in one transaction (staging table + set-based SQL, not 1M row-by-row):
   `INSERT ... ON CONFLICT (host) DO UPDATE SET rank = excluded.rank`; new domains get
   `next_check_at` **spread across the next 24h** (production's
   `InitSpaceTimestamps` idea, kept — prevents a thundering herd);
   domains present yesterday but absent today → `rank = NULL` (delisting lifecycle
   §4.8). Record provenance in `tranco_import` (list_id, date, counts) — the list ID
   is Tranco's requested citation unit.
5. Attribution: cite the Tranco NDSS'19 paper + list permalink on the site's FAQ/about
   page. Note: upstream provider licenses include CC BY-NC (Cloudflare Radar) —
   fine for this non-commercial project, worth a line on the about page.

Rank modeling: `rank INT NULL` on `domain`; NULL = unranked (campaign-only entities,
delisted domains, future full-list tail can carry ranks >1M). Source provenance is
derivable (rank ⇒ Tranco; campaign join ⇒ campaign; parent linkage ⇒ auto-created) —
a `created_by` enum column records origin for audit.

*Rejected — keeping the external `tldbwriter` + `lists`/`sites` tables:* an external
binary writing a two-table structure the API then RIGHT-JOINs (production bug: domains
silently vanish from the API when they drop off the list; duplicate rows if two lists
ever coexist). Rank belongs on the domain row. *Rejected — the `/api/lists/date/latest`
metadata API as the primary trigger:* it works, but is rate-limited (~1 r/s, observed
429s); the plain-text `top-1m-id` + conditional-GET zip is simpler and cache-friendly.
*Rejected — subdomain-inclusive variant:* locked out by brief (shame list = registrable
domains); campaign YAMLs are the only subdomain source.

## 4. Proposed database schema

Base: the v2-team `001_schema.up.sql`/`002_timescaledb.up.sql` (its split write model,
enum status type, no-FK hypertables, partial indexes and trigram search are the crown
jewels), reworked for: the domain-entity model, confirmed status, the resource model,
current TimescaleDB 2.28 columnstore API, and one correction from research — **do not
`segmentby = domain_id`** (v2 did): at 1 row/domain/day a segment holds 1–7 rows and
compression collapses; the documented fix is `orderby = 'domain_id, ts DESC'` with no
(or coarse) segmentby, which co-locates each domain's rows and still gives min/max
sparse-index pruning for per-domain queries.

Stack facts (verified by research): TimescaleDB **2.28.x supports PG18**
(full support since 2.23); everything needed — columnstore compression, continuous
aggregates, retention, background jobs — is **Community (TSL) edition, free for
self-hosted use** (TSL only forbids reselling TimescaleDB as a DBaaS); vendor renamed
to TigerData (2025), extension unchanged. Use the modern columnstore API
(`enable_columnstore` + `add_columnstore_policy`), not the deprecated
`timescaledb.compress` API.

### 4.1 Enums

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- Public status model (brief §2): what the API/classification ever sees.
CREATE TYPE ipv6_status AS ENUM ('supported', 'unsupported', 'no_record', 'not_applicable');

-- Raw observation outcomes; error/inconsistent never reach public output.
CREATE TYPE observation AS ENUM
  ('supported', 'unsupported', 'no_record', 'not_applicable', 'error', 'inconsistent');

CREATE TYPE domain_kind     AS ENUM ('apex', 'subdomain');
CREATE TYPE created_by      AS ENUM ('tranco', 'campaign', 'parent_link', 'live_check');
CREATE TYPE classification  AS ENUM ('unknown', 'inactive', 'sinner', 'partial', 'hero');
CREATE TYPE disabled_reason AS ENUM ('dead', 'service', 'manual', 'delisted');
CREATE TYPE resource_source AS ENUM ('discovered', 'manual');
CREATE TYPE check_job_status AS ENUM ('pending', 'processing', 'done', 'failed');
```

### 4.2 `domain` — entity + confirmed state + frontier (one table)

```sql
CREATE TABLE domain (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  host          TEXT NOT NULL UNIQUE,          -- lowercase punycode FQDN
  kind          domain_kind NOT NULL DEFAULT 'apex',
  parent_id     BIGINT REFERENCES domain(id),  -- subdomain -> registrable parent
  rank          INT,                           -- Tranco rank; NULL = unranked
  created_by    created_by NOT NULL,

  -- Confirmed status per core dimension (NULL = never confirmed).
  -- One 5-column group per dimension d in {base, www, ns, mx, conn, resources}:
  base_status         ipv6_status,             -- CONFIRMED value -> public
  base_observed       observation,             -- last raw observation (debug/telemetry only)
  base_pending        ipv6_status,             -- candidate value awaiting confirmation
  base_pending_count  SMALLINT NOT NULL DEFAULT 0,
  base_since          TIMESTAMPTZ,             -- when confirmed value last changed
  -- www_* , ns_* , mx_* , conn_* , resources_*   (identical 5-column groups)

  -- Informational dimensions: latest observation only, no confirmation machinery.
  dnssec_observed  observation,
  ptr_observed     observation,
  smtp_observed    observation,
  parity_observed  observation,
  latency_v4_ms    INT,
  latency_v6_ms    INT,

  -- Materialized classification (brief §5.5), recomputed on every confirmed commit.
  classification  classification NOT NULL DEFAULT 'unknown',
  class_flags     TEXT[] NOT NULL DEFAULT '{}',   -- broken_v6, www_missing, ns_missing,
                                                  -- mail_missing, resources_v4only
  gold            BOOLEAN NOT NULL DEFAULT FALSE, -- hero + all resources v6 (badge)

  asn_id      INT REFERENCES asn(id),
  country_id  INT REFERENCES country(id),

  disabled        BOOLEAN NOT NULL DEFAULT FALSE,
  disabled_reason disabled_reason,
  disabled_at     TIMESTAMPTZ,

  -- Frontier / scheduling
  next_check_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  claimed_at      TIMESTAMPTZ,                 -- worker lease; reclaim after 30 min
  last_checked_at TIMESTAMPTZ,

  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_domain_host_trgm ON domain USING gin (host gin_trgm_ops); -- search
CREATE INDEX idx_domain_frontier  ON domain (rank ASC NULLS LAST, next_check_at ASC)
  WHERE NOT disabled;                                                      -- claim
CREATE INDEX idx_domain_rank      ON domain (rank) WHERE rank IS NOT NULL;
CREATE INDEX idx_domain_sinners   ON domain (rank) WHERE classification = 'sinner';
CREATE INDEX idx_domain_heroes    ON domain (rank) WHERE classification = 'hero';
CREATE INDEX idx_domain_partial   ON domain (rank) WHERE classification = 'partial';
CREATE INDEX idx_domain_parent    ON domain (parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX idx_domain_country   ON domain (country_id, classification);
CREATE INDEX idx_domain_asn       ON domain (asn_id);
```

Design points:

- **One wide table, not entity/status/frontier splits.** 1M rows × ~40 columns is
  small; every hot list query (`sinners by rank`, `heroes by rank`, country drill-down)
  hits exactly one table + the classification partial indexes. *Rejected — a 1:1
  `domain_status` side table:* saves nothing at this row count and adds a join to every
  endpoint. *Rejected — normalized `(domain_id, dimension, status…)` rows:* elegant for
  the commit algorithm, but turns every list row into a 6-way pivot; the wide-column
  layout is what sqlc/typed Go wants anyway.
- **Frontier eligibility** (enforced in the claim query): `rank IS NOT NULL` OR
  campaign membership OR `parent_id`-linked children exist OR live-check origin within
  7 days. Delisted, orphaned entities stop being scanned (§4.8) without being deleted.
- **Entity rules (brief §6):** Tranco contributes only `kind='apex'` rows. Campaign
  import stores each YAML entry **as given**; if PSL says it's a subdomain
  (`publicsuffix-go`, needed only at import time — the crawler's NS zone walk needs no
  PSL), it auto-ensures the registrable parent (`created_by='parent_link'`, rank NULL)
  and sets `parent_id`. Classification is per-entity; children never change a parent's
  tier. The detail API lists children with their own statuses (§5.2).

### 4.3 The confirmed-status commit (the trust machine)

Per scanned domain, in **one transaction** (worker-side, after `Runner.Run`):

```
for each core dimension d:
  O = observation(d)                      # quorum already applied for base/www
  if O in {error, inconsistent}:          # non-definitive: touch nothing
      set d_observed = O; continue        #   (pending survives transient noise)
  if O == d_status:                       # steady state
      d_pending = NULL, d_pending_count = 0
  elif O == d_pending:
      d_pending_count += 1
      if d_pending_count >= N(d):         # N=2 dns dims, N=3 conn/resources
          INSERT changelog(domain_id, ts, field=d, old=d_status, new=O)
          d_status = O; d_since = now(); d_pending = NULL; d_pending_count = 0
  else:
      d_pending = O; d_pending_count = 1
  d_observed = O

recompute classification + class_flags + gold from confirmed d_status values (§5.5 ladder)
INSERT scan row (slim, typed)  +  scan_detail row (JSONB)
UPDATE domain (state cols, classification, next_check_at, claimed_at = NULL)
```

First-ever scan of a domain: confirmed columns are NULL, so the first **definitive**
observation commits immediately (`old_value` NULL → changelog suppressed; the public
never sees a "changed from nothing" entry) — new domains appear with a status after
one scan, and the anti-flap rule applies from the second scan onward.

Classification ladder (§5.5, deterministic, first match wins), evaluated over
**confirmed** values only; `not_applicable` and NULL-confirmed dimensions are skipped,
never counted against:

1. `inactive` — base = `no_record` (no A and no AAAA).
2. `sinner` — base = `unsupported` (A exists, AAAA definitively absent). **Only** shame trigger.
3. `hero` — base = `supported` AND www ∈ {supported, not_applicable} AND ns = supported
   AND conn = supported AND mx ∈ {supported, not_applicable}.
   `gold = hero AND resources ∈ {supported, not_applicable}`.
4. `partial` — base = `supported`, hero bar not met. Flags: `broken_v6`
   (conn = unsupported), `www_missing`, `ns_missing`, `mail_missing`
   (mx = unsupported), `resources_v4only`.
5. `unknown` — base not yet confirmed (fresh domain, or persistent errors).

Note www: engine maps www NXDOMAIN → `not_applicable` (site doesn't use www), which
per the evaluation principles must not block hero — this deviates from a literal
reading of §5.5's "www AAAA supported" (**OPEN-2: decided** — `not_applicable` skips; a site without www can be a Hero,
and www `no_record` gets the same treatment).

*Rejected — deriving confirmed status in views from the scan log ("last N rows
agree"):* correct but forces windowed queries over a hypertable on every list/detail
request and makes the changelog write-point ambiguous. The column-pair + pending-count
is O(1) per scan, transactionally atomic with the changelog row, and directly
inspectable. *Rejected — confirming in a separate batch job:* splits scan and commit,
introducing the exact race/bolt-on wiring failures the last attempt suffered; the
worker owns the whole domain lifecycle per scan.

### 4.4 Time-series hypertables

No FKs on hypertables (TimescaleDB constraint, v2-team convention kept).

```sql
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
SELECT create_hypertable('scan', by_range('ts', INTERVAL '7 days'));
ALTER TABLE scan SET (timescaledb.enable_columnstore,
                      timescaledb.orderby = 'domain_id, ts DESC');  -- no segmentby (see above)
CALL add_columnstore_policy('scan', after => INTERVAL '14 days');
SELECT add_retention_policy('scan', drop_after => INTERVAL '2 years');
CREATE INDEX ON scan (domain_id, ts DESC);

-- Fat scan payload: full engine Details JSONB (per-check evidence, record sets,
-- TLS cert info, resource host lists). Short retention; debugging + detail page.
CREATE TABLE scan_detail (
  domain_id BIGINT NOT NULL,
  ts        TIMESTAMPTZ NOT NULL,
  result_id TEXT NOT NULL,        -- sha256(domain_id:ts-hour) — idempotent re-submits
  details   JSONB NOT NULL,
  duration_ms INT,
  PRIMARY KEY (domain_id, ts)
);
SELECT create_hypertable('scan_detail', by_range('ts', INTERVAL '1 day'));
ALTER TABLE scan_detail SET (timescaledb.enable_columnstore,
                             timescaledb.orderby = 'domain_id, ts DESC');
CALL add_columnstore_policy('scan_detail', after => INTERVAL '3 days');
SELECT add_retention_policy('scan_detail', drop_after => INTERVAL '90 days');

-- Structured field-level changelog. FOREVER — the credibility surface.
CREATE TABLE changelog (
  domain_id BIGINT NOT NULL,
  ts        TIMESTAMPTZ NOT NULL DEFAULT now(),
  field     TEXT NOT NULL,                     -- base|www|ns|mx|conn|resources
  old_value ipv6_status,                       -- NULL on first confirmation (not published)
  new_value ipv6_status NOT NULL,
  PRIMARY KEY (domain_id, ts, field)
);
SELECT create_hypertable('changelog', by_range('ts', INTERVAL '30 days'));
ALTER TABLE changelog SET (timescaledb.enable_columnstore,
                           timescaledb.orderby = 'ts DESC, domain_id');
CALL add_columnstore_policy('changelog', after => INTERVAL '60 days');
-- no retention policy: kept forever (~1-3k rows/day)

-- Operational crawler metrics (Grafana only). Checkpoint rows per run.
CREATE TABLE crawler_metrics (
  ts TIMESTAMPTZ NOT NULL DEFAULT now(),
  run_id UUID NOT NULL, worker TEXT NOT NULL,
  processed INT, succeeded INT, failed INT, qps REAL,
  p50_ms INT, p99_ms INT, active_slots INT, queue_depth INT,
  dim_counters JSONB,                          -- per-dimension supported/unsupported tallies
  is_final BOOLEAN NOT NULL DEFAULT FALSE
);
SELECT create_hypertable('crawler_metrics', by_range('ts', INTERVAL '7 days'));
SELECT add_retention_policy('crawler_metrics', drop_after => INTERVAL '90 days');
```

Sizing (research-validated): `scan` ≈ 1M slim rows/day → ~100 MB/day raw, compresses
extremely well (enum/int columns, delta-encoded `domain_id` via orderby); 2y retention
≈ single-digit GB compressed. `scan_detail` ≈ 1–2 GB/day raw; JSONB compresses ~3–8×
(not the headline 95% — that's why the typed columns are hoisted out); 90d retention
caps it at ~15–40 GB compressed. **This is the design answer to production's unbounded
`domain_log`/`campaign_domain_log`** (production has *no* cleanup anywhere — the
"cleanup_*.sql" scripts referenced in the brief don't exist in the repo; nothing
prunes at all today).

The **split write model** is therefore three-way: `scan` (slim, long) +
`scan_detail` (fat, short) + `domain` (materialized current state). *Rejected — one
scan table with JSONB column:* retention drops whole chunks, so slim-long/fat-short
requires two tables; the alternative (keep everything 2y) is 500+ GB of JSONB for
data nobody reads after 90 days.

### 4.5 Campaigns — membership, not duplication

```sql
CREATE TABLE campaign (
  id INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  uuid UUID NOT NULL UNIQUE,                   -- from YAML; API uses shortuuid encoding
  name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
  source_file TEXT,                            -- provenance: YAML filename
  disabled BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE campaign_domain (
  campaign_id INT NOT NULL REFERENCES campaign(id) ON DELETE CASCADE,
  domain_id   BIGINT NOT NULL REFERENCES domain(id),
  added_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (campaign_id, domain_id)
);
CREATE INDEX ON campaign_domain (domain_id);
```

**This is the single biggest structural change vs all predecessors**: production and
v2 both mirrored the whole status-column set into `campaign_domain` and ran a second,
near-identical crawler over it (production duplicates ~470 lines; its campaign crawler
re-crawls even disabled domains every 2h with no scheduling filter). Here a campaign
domain **is** a `domain` row; membership is a join. Consequences: one crawler, one
status truth (a domain in Tranco *and* a campaign shows identical status in both
views), campaign changelog = `changelog JOIN campaign_domain`, campaign per-scan
history = `scan JOIN campaign_domain`, and TimescaleDB retention applies uniformly —
`campaign_domain_log` (the unbounded-bloat poster child) ceases to exist.

Cost: campaign API responses need a join (trivial at these row counts), and campaign
YAML domains that are junk (don't resolve) become domain rows too — handled by the
normal dead-domain lifecycle rather than import-time rejection. *Rejected — keeping
separate campaign status tables for "campaign domains may not be in the main list":*
that was only ever a reason for a shared *entity* table, not for duplication; rank
NULL already models "not in the main list."

### 4.6 Issue #23 — resources as a dependency relationship

Per brief §5: a resource is a host a page depends on (usually a *different*
registrable domain) — a relationship, not a tracked entity. Two-table normalized
model with a **globally deduped host registry** so `fonts.googleapis.com` is checked
once per day, not once per dependent site:

```sql
CREATE TABLE resource_host (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  host TEXT NOT NULL UNIQUE,                   -- lowercase punycode
  aaaa_status ipv6_status,                     -- confirmed (same N=2 anti-flap)
  aaaa_pending ipv6_status, aaaa_pending_count SMALLINT NOT NULL DEFAULT 0,
  last_checked_at TIMESTAMPTZ, next_check_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  dependent_count INT NOT NULL DEFAULT 0       -- maintained on link/unlink; CNAME-in-degree signal
);
CREATE INDEX ON resource_host (next_check_at);

CREATE TABLE domain_resource (
  domain_id  BIGINT NOT NULL REFERENCES domain(id) ON DELETE CASCADE,
  resource_host_id BIGINT NOT NULL REFERENCES resource_host(id),
  source     resource_source NOT NULL,         -- discovered | manual
  required   BOOLEAN NOT NULL DEFAULT TRUE,    -- manual entries can be advisory-only
  first_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (domain_id, resource_host_id)    -- operator add upgrades source to 'manual'
);
CREATE INDEX ON domain_resource (resource_host_id);   -- "who depends on X" (advocacy gold)
```

Wiring — **through the whole stack from day one** (the REVIEW-REPORT's core finding
was that the last attempt specified this in prompt 11 and wired it into nothing:
missing sqlc queries, models, services, routes; and v2's `domain_resource` table
existed with zero writers):

- **Crawler:** `resource_ipv6` runs in phase 2; its external-host list upserts
  `resource_host` rows and refreshes `domain_resource` links (`last_seen = now()`;
  links not seen for 30 days and `source='discovered'` are pruned — sites change
  CDNs). Host AAAA checks go through the **bulk resolver** and are *decoupled*: a
  small dedicated worker sweeps `resource_host.next_check_at` daily (the ~100–300k
  unique external hosts cost ~2–4 qps). The domain's `resources` dimension is then
  computed as: all `required` linked hosts `supported` → `supported`; any
  `unsupported` → `unsupported` (+ flag); no links or conn not supported →
  `not_applicable`. It enters the same confirmation machinery (N=3) as other dims.
- **Manual endpoints (OPEN-3: revised in round 1.2):** operator-only —
  `v6ctl resource add <domain> <host> [--advisory]` writes `source='manual'` links
  (never pruned; an operator add on an already-discovered pair upgrades it to
  `manual`); removal only via `v6ctl resource remove`. **There is deliberately no
  campaign YAML syntax for resources.** Campaigns express endpoint intent through the
  `domains:` list itself: listing `api.dnb.no` alongside `dnb.no` makes it a
  first-class `kind=subdomain` entity, auto-linked `parent_id → dnb.no` (§4.2 entity
  model — the parent is auto-created if absent), checked with its own status and shown
  on the parent's drill-down. Cross-domain dependencies (`fonts.googleapis.com`,
  `cdn.sanity.io`) are exactly what auto-discovery finds without curation — and
  hand-curated CDN lists in YAML go stale. One known gap: XHR/API endpoints that
  never appear in static HTML aren't auto-discovered — covered by listing them in
  `domains:` (per-entity check) or an operator `v6ctl resource add` (feeds the
  parent's resources roll-up), whichever intent fits.
  *Rejected — a campaign `resources:` map (round-1.1 design):* redundant with the
  subdomain entity model for same-organization endpoints, contributor-facing schema
  complexity, and a provenance/lifecycle machinery (per-campaign link ownership)
  whose only real payload was third-party CDN hosts the crawler discovers anyway.
- **Classification:** resources affect **Gold only** (hero bar unchanged, shame bar
  unchanged — locked by §5.5); `resources_v4only` becomes a partial-tier flag for
  heroes-except-resources domains? No — a domain meeting the hero bar with bad
  resources is still `hero`, `gold=false`, flag visible on detail. (Exactly per brief.)
- **API:** `GET /domain/{domain}/resources` (per-host status list),
  reverse lookup `GET /resource/{host}/dependents` (paginated), both in §5.
- **Discovery parser scope:** v6audit's streaming tokenizer (script/img/link/iframe/
  source/video/audio/object/embed + `<base href>`) is the v1 scope. The prompts-spec's
  CSS-following/`srcset`/inline-style parsing (`11-resource-checker.md`) is a
  documented **later enhancement** — it roughly doubles fetch cost for a minority of
  additional hosts. *Rejected for v1 — full CSS recursion:* complexity and fetch
  volume vs marginal advocacy signal; revisit with data.

### 4.7 Stats & dashboards

Two families, cleanly separated (brief §6):

**Product stats (public, API-served, forever):** nightly snapshots of **confirmed**
state — not scan-derived — because public graphs must match the public lists exactly
(a cagg over observations wobbles with scan timing and includes unconfirmed values).

```sql
CREATE TABLE stats_global_daily (
  day DATE PRIMARY KEY,
  domains INT, sinners INT, partial INT, heroes INT, gold INT, inactive INT,
  unknown INT, disabled INT,
  base_supported INT, www_supported INT, ns_supported INT, mx_supported INT,
  conn_supported INT, resources_supported INT
);
CREATE TABLE stats_country_daily (
  day DATE, country_id INT, domains INT, sinners INT, partial INT, heroes INT,
  base_supported INT, conn_supported INT, PRIMARY KEY (day, country_id)
);
CREATE TABLE stats_campaign_daily (
  day DATE, campaign_id INT, domains INT, v6_ready INT, sinners INT, partial INT,
  heroes INT, base_supported INT, www_supported INT, ns_supported INT,
  mx_supported INT, conn_supported INT, PRIMARY KEY (day, campaign_id)
);
CREATE TABLE stats_asn_daily (       -- ~50-80k ASNs/day -> hypertable
  day TIMESTAMPTZ NOT NULL, asn_id INT NOT NULL,
  domains INT, v6_domains INT, sinners INT, heroes INT,
  PRIMARY KEY (asn_id, day)
);
SELECT create_hypertable('stats_asn_daily', by_range('day', INTERVAL '90 days'));
ALTER TABLE stats_asn_daily SET (timescaledb.enable_columnstore,
                                 timescaledb.orderby = 'asn_id, day DESC');
CALL add_columnstore_policy('stats_asn_daily', after => INTERVAL '180 days');
```

Global/country/campaign tables are a few hundred rows/day — plain tables, no
hypertable ceremony. Per-domain graphs come straight from `scan` (already slim,
indexed `(domain_id, ts DESC)`, 2y depth).

**Observed-adoption continuous aggregate (Grafana + research):** one cagg over `scan`
gives the measurement-flavored series and exercises the dimension columns:

```sql
CREATE MATERIALIZED VIEW scan_daily_adoption
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 day', ts) AS day, country_id,
       count(*) AS scanned,
       count(*) FILTER (WHERE base = 'supported') AS base_v6,
       count(*) FILTER (WHERE conn = 'supported') AS conn_v6,
       count(*) FILTER (WHERE classification = 'hero')   AS heroes,
       count(*) FILTER (WHERE classification = 'sinner') AS sinners
FROM scan GROUP BY 1, 2 WITH NO DATA;
CALL add_policies('scan_daily_adoption', refresh_start_offset => INTERVAL '3 days',
     refresh_end_offset => INTERVAL '1 hour', refresh_schedule_interval => INTERVAL '1 hour',
     compress_after => INTERVAL '90 days');
-- Ordering rule (research): cagg refresh start_offset (3d) < scan retention (2y). OK.
-- Real-time aggregation stays off (default since TS 2.13) — yesterday's data is fine.
```

**Ops metrics** (`crawler_metrics`, queue depth, error rates) are Grafana-only,
never public. Current-state counters on `country`/`asn` rows (for the existing
`/country`, `/metric/asn` endpoints) are recomputed by the ported stored procedures at
the daily tick.

*Rejected — caggs as the only stats mechanism:* caggs aggregate *observations*; the
brief requires public output to derive from *confirmed* status, and a JOIN-cagg can't
track campaign membership changes (TimescaleDB only invalidates on hypertable
changes). Snapshot-from-current-state is trivially correct and costs four aggregate
queries per night. *Rejected — snapshot-only:* keeps Grafana blind to intra-day
behavior and loses the cheap country-sliced measurement series; the single cagg is
nearly free.

### 4.8 Lifecycles: disabled domains & service-domain detection

**Disabled lifecycle** (`disabled_reason`):

| Reason | Set by | Re-enable |
|---|---|---|
| `dead` | Crawler: apex NXDOMAIN (both A and AAAA absent **and** NS walk finds no zone) on **7 consecutive scans** (i.e. a week of NXDOMAIN — production disabled on a single TXT SERVFAIL, which is how transient failures became permanent deletions) | **Auto:** slow-lane revalidation — `next_check_at` set to +30d instead of exclusion; a successful resolution re-enables and resets state to `unknown` |
| `delisted` | Tranco import: rank became NULL and no campaign/children/live-check linkage; 30-day grace | Auto: regains rank or campaign membership |
| `service` | Operator confirms a detection candidate, or `service_domains.yml` import (`v6ctl disable --service-list`) | Manual only |
| `manual` | `v6ctl disable <host> --reason=...` | Manual only |

Disabled domains are excluded from classification, lists, and stats; `dead`/`delisted`
stay in the frontier on the slow lane, `service`/`manual` leave it entirely.

**Service-domain detection** (candidates, never auto-disable — brief §6): nightly job
flags into a review table:

```sql
CREATE TABLE service_candidate (
  domain_id BIGINT PRIMARY KEY REFERENCES domain(id),
  reasons TEXT[] NOT NULL, detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  dismissed BOOLEAN NOT NULL DEFAULT FALSE
);
```

Heuristics (each contributes a reason tag): (a) apex+www both `no_record` confirmed
while NS exists (classic CDN/infra apex — the v2 crawler's inline rule, made
review-only); (b) high dependency in-degree — `resource_host.dependent_count` above
threshold (~100) *and* the host is itself a ranked domain (the fonts.googleapis.com
shape); (c) hostname patterns from the curated `service_domains.yml` (kept, imported
by `v6ctl`). Review via `v6ctl service-candidates list|confirm|dismiss`
(**OPEN-4: decided** — CLI-only, no admin HTTP surface exists by design). Weekly
webhook digest. The in-degree threshold stays a tuning item once phase-5 data exists.

### 4.9 Remaining tables

```sql
-- On-demand live checks (§5.3). v2's table, plus the consumer it never had.
CREATE TABLE check_job (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  host TEXT NOT NULL, requester_ip INET NOT NULL,
  status check_job_status NOT NULL DEFAULT 'pending',
  result JSONB, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), completed_at TIMESTAMPTZ
);
CREATE INDEX ON check_job (status) WHERE status = 'pending';
CREATE INDEX ON check_job (requester_ip, created_at);          -- rate limiting

CREATE TABLE asn (
  id INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  number BIGINT NOT NULL UNIQUE, name TEXT NOT NULL DEFAULT 'Unknown',
  count_total INT NOT NULL DEFAULT 0, count_v6 INT NOT NULL DEFAULT 0
);
CREATE TABLE country (
  id INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  code CHAR(2) NOT NULL UNIQUE, name TEXT NOT NULL, tld TEXT,
  sites INT NOT NULL DEFAULT 0, v6sites INT NOT NULL DEFAULT 0,
  percent NUMERIC(5,2) NOT NULL DEFAULT 0                       -- (5,2): kills the
);                                                              -- pgtype ÷10 hack
CREATE TABLE top_shame (                                        -- editorial picks
  domain_id BIGINT PRIMARY KEY REFERENCES domain(id), reason TEXT,
  added_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE tranco_import (
  id INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  list_id TEXT NOT NULL, list_date DATE NOT NULL,
  inserted INT, updated INT, delisted INT, rejected INT,
  imported_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Search: the trigram GIN index on `domain.host` serves substring search (escape `%`/`_`
in input — v2 forgot); ASN search = ILIKE over ~80k rows (fine). *Rejected — FTS/
tsvector:* domains aren't prose; trigram is the right tool and production's
table-scanning `LIKE '%x%'` (`db/query/domain.sql:65`) dies here.

GeoIP: MaxMind GeoLite2 ASN + Country mmdb (kept, `IncSW/geoip2` or the official
reader). Country attribution keeps production's rule — ccTLD wins over server
location, now PSL-correct instead of regex-on-last-label — GeoIP country as fallback
(**OPEN-5: decided** — keep ccTLD precedence).

## 5. API surface

### 5.1 Compatibility contract (must-keep, verified against both `whynoipv6/internal/rest/` and the actual frontend calls in `whynoipv6-web/src/services/`)

Ground rules extracted from code: paths mount at the **API root** (no `/api/v1`
prefix); `/metric` is **singular**; pagination is `?offset=` (+ `?limit=`, default 50
max 100 — the frontend only ever sends `offset` and hard-assumes page size 50 in its
Next-button logic); UUIDs in URLs are **shortuuid-encoded**; two endpoints wrap in a
`{"data": [...]}` envelope; campaign detail returns a `{campaign, domains}` composite;
status strings are exactly `supported|unsupported|no_record`.

| Endpoint | Behavior (new backend) |
|---|---|
| `GET /domain?offset=` | Sinner list (`classification='sinner'`) by rank — matches old semantics (plain list = shame list) |
| `GET /domain/heroes` | `classification='hero'` by rank. Hero bar is now §5.5 (old query was `mx != 'unsupported'`; the changed membership is a **deliberate, announced** break — OPEN-6: decided) |
| `GET /domain/topsinner` | `top_shame` join, still curated. Fix: return real Tranco `rank` (old code returned domain *id* as `rank`) |
| `GET /domain/{domain}` | Detail from confirmed columns. Keep field names incl. `asn` = AS **name** string, `country` = name, all `ts_*` keys (map: `ts_check`→last_checked_at, dimension `ts_*`→`<d>_since`). `v6_only` now real: the `conn` status (production served a dead column) |
| `GET /domain/{domain}/log` | Last 90 `scan` rows mapped to `{id,time,base_domain,www_domain,nameserver,mx_record}` (id = synthetic, frontend uses it as list key only) |
| `GET /domain/search/{q}` | Trigram search, **`{"data":[...]}` envelope kept** |
| `GET /country`, `/country/{code}`, `/country/{code}/sinners`, `/country/{code}/heroes` | Same shapes; percent from `NUMERIC(5,2)` cleanly; sinners ordered by rank (old: by id — minor fix) |
| `GET /changelog`, `/changelog/campaign`, `/changelog/{domain}`, `/changelog/campaign/{uuid}`, `/changelog/campaign/{uuid}/{domain}` | From structured changelog; `message` + `ipv6_status` **generated at the API layer** from `(field, old, new)` via the ported `generateChangelog` ladder (`crawl.go:416-495`) — same strings, single implementation. `domain_url` rules kept (incl. empty-string cases). v2-team dropped three of these routes; they're restored — the frontend calls all five |
| `GET /campaign` | List + `count`, `v6_ready` (v6_ready = base+www+ns supported — keep old formula for continuity; OPEN-6) |
| `GET /campaign/{uuid}?offset=` | `{campaign, domains}` composite; domain rows are the shared entity's status now |
| `GET /campaign/{uuid}/{domain}`, `.../log`, `GET /campaign/search/{q}` | Kept, incl. envelope + `campaign_uuid` field in search rows |
| `GET /metric/overview` | Array-of-one `{time, data:{domains, base_domain, www_domain, nameserver, mx_record, heroes, top_heroes, top_nameserver}}` mapped from `stats_global_daily` latest row |
| `GET /metric/asn?order=`, `GET /metric/asn/search/{q}` | Kept; `count_v4` = `count_total - count_v6` computed server-side as today |
| `GET /ip` | `{"ip":"<remote addr>"}` — the frontend's IPv4-banner calls `api.whynoipv6.com/ip` **hardcoded**; today this must be served by nginx or lost code — the new API serves it natively |
| `GET /` | health `{"message":"ok"}` |

Two contract cleanups worth making (frontend verified tolerant): empty lists return
`[]` instead of JSON `null` (frontend does `response.data \|\| []`), and proper
graceful-shutdown/timeouts on the server (production has neither). Everything else
stays bug-compatible until the frontend modernization round.

*Rejected — versioned `/v2` API now:* the frontend is frozen; a v2 surface with
cleaned-up shapes belongs to the frontend round. The OpenAPI spec documents the legacy
quirks explicitly so they're contained.

### 5.2 New endpoints

| Endpoint | Purpose |
|---|---|
| `GET /domain/almost?offset=` | Partial tier ("almost there") by rank, with `class_flags` per row |
| `GET /domain/{domain}/subdomains` | Children (entity model) with each child's own status. **Also embedded** in `GET /domain/{domain}` as `subdomains: [...]` capped at 25 with `subdomain_count` — the detail page renders the drill-down without a second request; the sub-resource endpoint exists for pagination past the cap |
| `GET /domain/{domain}/resources` | #23: linked resource hosts with per-host status, source (discovered/manual), required flag |
| `GET /resource/{host}/dependents?offset=` | Reverse dependency lookup ("who depends on this v4-only CDN") |
| `POST /check {"domain": "..."}` / `GET /check/{id}` | Live check (§5.3) |
| `GET /stats/overview`, `/stats/country/{code}`, `/stats/campaign/{uuid}`, `/stats/asn/{number}`, `/stats/domain/{domain}` | Time-series for graphs, from `stats_*` + `scan`; `?from=&to=&interval=daily\|weekly` |
| `GET /badge/{domain}.svg` (optional, cheap) | Status badge for READMEs — advocacy multiplier; flagged optional |
| `GET /datasets` (+ static files) | Dataset manifest (§5.4) |

### 5.3 Live "check any domain"

Flow: `POST /check` → validate hostname shape (LDH, punycode-normalize) → **rate
limit**: 10 requests/IP/hour via indexed `check_job(requester_ip, created_at)` count
(v2's `CountByIP` design) + a global cap (e.g. 500/h) → **dedupe**: if the host exists
and was scanned <1h ago, return the stored result immediately (`status:"done"`, zero
cost); else insert `check_job(pending)` → `202 {id}`. Client polls `GET /check/{id}`.

Consumer: a dedicated goroutine in the **crawler** binary claims jobs
(`FOR UPDATE SKIP LOCKED`, the v2 `ClaimJob` SQL — which existed with *no consumer*;
here the consumer ships in the same phase as the endpoint, §8 phase 6) and runs the full engine with a 60s
budget, writing the per-check 3-state JSON into `result`. Live-checked unknown hosts
become `domain` rows (`created_by='live_check'`, rank NULL) with a 7-day frontier
linkage, so repeat lookups are cheap and abuse doesn't permanently grow the frontier.
SSRF is already handled by the engine's pinned dialer; additionally reject literal
IPs, `.internal`/`.local`/rfc2606 TLDs at the API boundary.

*Rejected — synchronous inline check:* a full engine run can take 60–90s (SMTP/HTTP
timeouts); holding an anonymous HTTP request open that long invites trivial
resource-exhaustion abuse. Job + poll matches ready.chair6.net-class tools.

### 5.4 Datasets for researchers

Nightly `v6ctl export` (after the stats tick): writes dated snapshots to a static
directory served by nginx (`data.whynoipv6.com` or `/datasets/` path):

- Sizes: `top100k`, `top1m`, `full` (all scannable entities incl. campaign/subdomains).
- Formats: **CSV** (gzip) + **Parquet** (`parquet-go/parquet-go`), columns: host,
  rank, kind, parent, classification + flags, gold, the six confirmed statuses +
  since-timestamps, country, asn, last_checked.
- `datasets/manifest.json` (+ `GET /datasets` serving it): dates, sizes, sha256,
  schema version; `latest/` symlinks. A `DICTIONARY.md` documents every column and the
  status semantics (incl. "confirmed" meaning). Retention: dailies 90d, first-of-month
  forever.

*Rejected — API-generated exports on demand:* 1M-row × N-format generation belongs in
a batch job writing static files; the API only serves the manifest. *Rejected — S3:*
operator runs own hardware + nginx; a directory is the 20-line solution.

### 5.5 OpenAPI

**Spec-first**: `openapi/openapi.yaml` is the committed source of truth (legacy quirks
documented explicitly — envelopes, shortuuid formats, singular `/metric`);
`make generate` runs **oapi-codegen** (chi-server strict-server mode → handler
interfaces + typed models in `backend/internal/api/gen/`) and **openapi-typescript**
(TS types + client for `frontend/`). CI fails if generated output is stale (the
monorepo's single-commit sync promise from §2.5 of the brief).

*Rejected — code-first swaggo annotations:* comment-derived specs drift and can't
express the legacy response envelopes precisely; with a frozen contract, the spec is
the natural place the contract *lives*. The one-time cost of writing the legacy
surface into YAML is real but bounded (~25 paths), and it doubles as the
parity-test fixture (§8 phase 4).

---

## 6. Package & binary layout

Monorepo per brief §2.5 (backend + frontend + openapi; campaign repo stays separate).
Backend module:

```
backend/
  cmd/
    api/main.go            # HTTP server
    crawler/main.go        # frontier workers + check-job consumer + daily tick + preflight
    v6ctl/                 # cobra: tranco import|status, campaign sync, disable,
                           #   service-candidates, resource add, export, stats recalc, migrate
  internal/
    domain/                # entities, enums, classification ladder (pure; zero deps)
    checker/               # LIFTED from v6audit (see table §2.1) — engine + resolver + ssrf
    consensus/             # multi-resolver quorum wrapper implementing checker's resolver seam
    crawler/               # claim loop, worker pool, confirmed-status commit, resource sweep,
                           #   daily tick, checkpoint metrics
    ingest/                # tranco fetcher/parser/upserter
    campaign/              # YAML parse + idempotent sync (tolerates the format variance found:
                           #   0/2/4-space indents, comments, trailing spaces)
    repository/            # port interfaces (v2-team repository.go, extended)
    postgres/              # sqlc-generated queries + adapters (sqlc actually used this time —
                           #   v2 configured it and then hand-wrote everything)
    service/               # use-case layer the api handlers call
    api/                   # chi server, handlers, legacy-shape mappers, gen/ (oapi-codegen)
    geoip/                 # MaxMind mmdb
    notify/                # webhook + healthcheck ping (v2's Notifier, actually invoked)
    config/                # viper env config
  db/migrations/           # golang-migrate; 001 schema, 002 timescale, 003 seed
  db/query/                # sqlc sources
```

Layering discipline is the v2-team's (verified clean there): `domain` ← `repository`
← {`postgres`, `service`, `crawler`} ← {`api`, `cmd`}; crawler depends only on
interfaces. Tests go **where v2 had none**: dockerized Postgres+Timescale integration
tests for `postgres/` (every sqlc query + the claim/commit transaction), a fake-DNS
server for `consensus/` and the checker resolver seam, and golden-response parity
tests for `api/`. v2's test suite concentrated on handlers/services with fakes (84
test funcs, not the claimed ~258) and left SQL, crawler and resolver untested — the
risk-inverted distribution to avoid.

Binary split rationale: `api` (stateless, scale/deploy independently), `crawler`
(one per machine; owns preflight + claim loops), `v6ctl` (operator verbs, cron
targets). *Rejected — single binary with subcommands:* deploy/restart coupling between
API availability and crawler restarts is exactly what hurt production ops.

---

## 7. Campaign automation

Current reality (verified): the campaign repo has **no CI at all** and an issue-based
workflow (README says "create an issue"; maintainer hand-crafts YAML, `make fix-uuids`
generates UUIDs; production imports via a systemd timer that also **writes generated
UUIDs back into the YAML working copy** — mutating a git checkout from a daemon).

Target pipeline (PR-based per brief):

1. **PR validation (GitHub Actions in the campaign repo — new, tiny):** YAML schema
   check (title/description/domains required — the schema stays exactly today's
   four keys; tolerant parse), hostname validation (LDH/punycode), duplicate
   detection (within file + across files), size cap. UUID
   **must be absent or valid** — contributors never invent UUIDs. A bot comment posts
   the parsed summary ("32 domains, 3 subdomains → parents auto-linked").
2. **On merge to main:** repo dispatch → operator CI (Semaphore, as wired today for
   other projects) → runs `v6ctl campaign sync --repo /srv/whynoipv6-campaign` on the
   backend host (git pull + import). Alternative trigger for simplicity: the crawler's
   daily tick also runs sync (pull + import) — the webhook is latency sugar, the cron
   is the guarantee. **Both.**
3. **Idempotent import** (per file): if `uuid` present → upsert campaign by uuid
   (name/description update); if absent → generate UUID, import, and **commit the
   UUID back to the campaign repo via a bot commit** (deploy-key push, `[skip ci]`) —
   moving the write-back from daemon-mutating-a-checkout into an auditable commit.
   Then diff domains: additions → ensure `domain` entity (+ PSL parent auto-link,
   kind detection) + membership row; removals → delete membership row only (entity
   remains; orphan handling via `delisted` lifecycle). Campaign deleted = YAML file
   deleted → campaign `disabled=true` (soft, history preserved).
4. **Report:** sync summary (created/updated/added/removed/rejected + reasons) to the
   ops webhook.

*Rejected — merging the campaign repo into the monorepo:* locked out by brief (clean
contributor surface). *Rejected — backend polling GitHub API for merges:* the daily
cron + webhook covers freshness without API tokens/rate limits.

---

## 8. Phased implementation plan

Crawler-first, each phase gated on explicit verification. Phases 1–4 produce a
shippable replacement; 5–7 complete the product vision.

**Phase 0 — repo scaffolding (short).** Monorepo per §2.5: subtree-import
`whynoipv6-web` (history preserved) into `frontend/`; `backend/` skeleton with go.mod,
Makefile (`test/lint/build/generate/migrate`), `.golangci.yml`, docker-compose
(postgres18+timescale2.28, unbound ×2, api, crawler), CI. *Verify:* `make build` +
compose up green.

**Phase 1 — schema + ingestion.** Migrations (§4 DDL), sqlc setup, Tranco ingester,
campaign sync (import all 28 YAMLs; subdomain entries auto-link parents — §4.2),
GeoIP wiring. *Verify:* 1M+~30k domain rows with
correct kinds/parents/ranks; re-running import is a no-op (idempotency test); junk
Tranco entries rejected with counts; integration-test suite for every query.

**Phase 2 — crawler core (the heart).** Lift `internal/checker` + tests; consensus
resolver; Unbound deployment + tuning; frontier claim/commit with confirmed-status
machine; changelog writes; preflight; checkpoint metrics. *Verify:* (a) unit: the
commit state machine table-driven over every transition incl. error/inconsistent
sequences; (b) fake-DNS quorum tests (2/3 agree, split, timeout combinations);
(c) sample run: 10k mixed-rank domains, results diffed against production's current
statuses — investigate every divergence class (expected ones: co.uk NS fix, stricter
conn-based `v6_only`); (d) chaos: kill a worker mid-batch, batch reclaimed after
lease expiry, no double changelog.

**Phase 3 — full-scale daily crawl.** 1M daily on production hardware; Grafana
dashboards (throughput, error rates, resolver latencies, queue depth, Unbound stats);
public-resolver rate smoothing verified (~23 qps/provider measured); Cloudflare
courtesy email sent. *Verify:* 3 consecutive full passes <24h; confirmed-transition
volume plausible (~1–3k/day); zero preflight false-negative incidents; compression +
retention jobs observed running (`timescaledb_information.jobs`).

**Phase 4 — API + cutover.** Full compat surface + `/ip`; golden parity tests
(recorded production responses vs new, modulo documented deviations); OpenAPI spec +
TS client generation; **data migration** — one-time import of production's current
statuses as seed confirmed values, full changelog history (the site's credibility
archive), and the trailing **90 days** of per-scan history (**OPEN-7: decided**; the
importer takes the window as a flag and the production dump is retained, so a deeper
backfill stays possible later); dual-run with the frontend
pointed at staging; DNS cutover. *Verify:* frontend E2E (playwright) against new API
with zero visual diffs; changelog continuity (old entries render identically).

**Phase 5 — #23 resources + classification surfacing.** Resource sweep worker,
manual endpoints, `resources` dimension + gold badge, `/domain/almost`, resources +
dependents endpoints. *Verify:* known-fixture sites (e.g. a hero with v4-only fonts
CDN) classify as hero+not-gold with `resources_v4only`; resource-host dedup ratio
measured; classification counts stable across 3 days (no flap storm from the new
dimension — it only affects gold).

**Phase 6 — public features.** Live check (+consumer), stats endpoints, dataset
export, badge (optional). *Verify:* abuse test on `/check` (rate limits hold under
scripted load); datasets validate against DICTIONARY; stats endpoints match
snapshot tables.

**Phase 7 — campaign automation + ops polish.** Campaign-repo Actions, merge webhook,
bot write-back; healthcheck/webhook notifications; runbooks (Unbound, Timescale jobs,
frontier surgery). *Verify:* end-to-end: test PR → merge → domains appear scanned
within 24h with UUID committed back.

---

## 9. Risks & decision log

All round-1 open items were resolved in operator review (2026-07-06):

| # | Item | Decision |
|---|---|---|
| OPEN-1 | NS/MX `partial` mapping (§2.2) | **≥1 v6-capable host = `supported`**, as recommended |
| OPEN-2 | www NXDOMAIN vs hero bar (§4.3) | **`not_applicable` skips** — a site without www can be a Hero; www `no_record` treated the same |
| OPEN-3 | Campaign-level manual resource lists (§4.6) | **Revised (round 1.2):** no campaign `resources:` syntax — campaign YAML keeps exactly today's four keys. Endpoint intent is expressed by listing hostnames in `domains:` (auto-linked as subdomain entities of their registrable parent, §4.2). #23 resources = auto-discovery + operator `v6ctl resource add` only |
| OPEN-4 | Service-candidate review UX (§4.8) | **CLI-only** (`v6ctl service-candidates ...`); no admin HTTP surface. In-degree threshold stays a tuning item after phase-5 data |
| OPEN-5 | Country attribution (§4.9) | **Keep ccTLD precedence**, GeoIP fallback |
| OPEN-6 | Cutover behavior (§5.1) | **Serve migrated seed values immediately**; publish a "methodology v2" note for the deliberate metric shifts (hero bar, real `v6_only`, correct multi-label-TLD NS) |
| OPEN-7 | History migration scope (§8 phase 4) | **Trailing 90 days** of per-scan history (+ full changelog, seed statuses). Importer takes the window as a flag and the production dump is retained — extensible later |
| OPEN-8 | Cloudflare throttling contingency (§2.4) | **No pre-emptive standby**; upstream list stays config so a 4th resolver can be swapped in if it ever becomes a problem |
| OPEN-9 | DNS library | **miekg/dns v2** (Codeberg) from day one; v1-API revert is the mechanical escape hatch if v2 misbehaves in phase-2 verification |
| OPEN-10 | `/domain` default view (delegated to architect) | **Keep `/domain` = sinners** for compat with the frozen frontend; `/domain/almost` ships now, and the full ladder (sinner → almost-there → hero → gold) gets explicit presentation in the frontend round |
| OPEN-11 | Live-check rate budget (§5.3) | **10/IP/h + 500/h global** adopted; revisit with abuse data. CAPTCHA stays off the table (anonymous, no-JS-wall ethos) |

Remaining risks: **the confirmed-status machine is novel code** — it gets the
heaviest unit coverage in the repo (phase 2 gate); **Unbound becomes prod infra** —
mitigated by two instances, Grafana, and public-resolver fallback for bulk (config
flag) in emergencies; **adoption-number shifts at cutover** could look like
measurement bugs — mitigated by dual-run diffing (phase 4) and the methodology note;
**miekg/dns v2 is newer code in a load-bearing spot** — phase-2/3 soak is the check,
the v1-API revert the escape hatch.

---

## 10. Explicit tradeoffs (summary)

| Decision | Chosen | Rejected | Why |
|---|---|---|---|
| Scan engine | Lift v6audit `internal/checker` | Rewrite; extend production resolver | Encodes years of RFC edge-cases; production resolver has the co.uk bug and no reachability checks |
| DNS library | miekg/dns v2 (Codeberg) from day one | Ship v1, migrate later | Operator decision (OPEN-9); v2 is ~2× faster and actively developed, port is mechanical, v1 revert trivial |
| Status model | 4-value public enum + 6-value internal observation | Single shared enum | `error`/`inconsistent` must exist internally and must never leak to public output (§5.5 principles) |
| Consensus scope | apex+www AAAA only via 3 public resolvers | Consensus on all lookups | 3× public-resolver load for records that don't gate classification; ~23 qps/provider is the validated-safe budget |
| Bulk DNS | Self-hosted Unbound ×2 | Public resolvers for everything; zdns | Volume belongs on own infra; geo-diversity only matters for the AAAA verdicts; zdns duplicates an integrated resolver |
| Work distribution | Frontier columns on `domain` + SKIP LOCKED lease | River/jobs table; Redis/NATS | Uniform periodic sweep needs no queue; materializing schedulers are the proven failure (v6audit `handleScanScheduler`) |
| Fall-behind policy | Claim `ORDER BY rank` | Oldest-due-first | Brief mandate: top ranks stay freshest; tail stretch is graceful degradation (aging flag as valve) |
| Anti-flap | N-consecutive-scans (2/3) + quorum + error-excluded | Rolling-average smoothing (APNIC-style) | Binary public states need hysteresis, not averaging; matches Nagios-class prior art; bounded latency (+1–2 days) |
| Confirmed-status storage | Column groups + pending counter on `domain` | Derive from scan log in views; batch confirm job | O(1), transactional with changelog, inspectable; avoids the bolt-on wiring failure mode |
| Campaigns | Membership join onto shared `domain` | Mirrored status tables + second crawler (both predecessors) | Kills ~500 duplicated lines, dual-truth statuses, and the unbounded `campaign_domain_log` in one move |
| #23 resources | Global `resource_host` registry + link table, DNS-only check, wired end-to-end from phase 5 | Per-domain URL rows (prompts spec); full CSS-recursive discovery | Dedup makes daily re-checks ~free and gives reverse lookups; CSS recursion is a data-justified later step |
| Scan history | Slim typed hypertable (2y) + fat JSONB (90d) | One table; caggs-instead-of-raw | Retention drops chunks, not columns; slim-forever is cheap, fat-forever is 500+ GB of unread JSONB |
| Compression | `orderby='domain_id, ts DESC'`, no segmentby | v2's `segmentby=domain_id` | 1 row/domain/day → 1–7-row segments → compression collapse (documented TS guidance) |
| Product stats | Nightly snapshots of confirmed state | Continuous aggregates only | Public graphs must equal public lists; caggs aggregate observations and can't track membership joins |
| Stats DB | TimescaleDB (Community/TSL) | Plain PG18 + pg_partman + pg_cron | Compression, caggs, retention, jobs = four `CALL`s vs hand-rolled glue; TSL is free self-hosted |
| API strategy | Bug-compatible root paths + additive new endpoints | Clean `/v2` now | Frontend is frozen this round; contract quirks are contained in the OpenAPI spec |
| OpenAPI | Spec-first + oapi-codegen + openapi-typescript | swaggo code-first | The frozen contract *is* the spec; generated TS keeps the monorepo single-commit promise |
| Live check | Queue + poll, engine-backed, frontier-linked | Synchronous endpoint | 60–90s worst-case engine runs vs anonymous abuse surface |
| Datasets | Nightly static CSV+Parquet + manifest | On-demand API export; S3 | Batch job + nginx dir is the whole requirement on owned hardware |
| Campaign pipeline | PR validation Action + merge webhook + daily-cron sync + bot UUID write-back | Issue-based flow (current); daemon writing into a checkout | Idempotent, auditable, keeps contributor surface at "submit YAML" |

---

*End of round-1.1 report. All round-1 decisions are logged in §9; remaining
iteration targets are the tuning items noted there.*
