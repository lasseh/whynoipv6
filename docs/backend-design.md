# WhyNoIPv6 Backend Rewrite — Design Report

> **⚠ SUPERSEDED — historical design rationale.** The authoritative, build-ready spec is
> **`docs/spec/`** (Round 3.0). This Round 2.0 report is kept as design-rationale provenance —
> its phase plan, package layout, and engine-adaptation contract are still cited by the spec
> as "design §N" — **but its API sections (§5 API surface, §5.1, and related) are OBSOLETE:**
> the legacy/compat API they describe was removed in the Round 3.0 redesign. For the current
> API see `docs/spec/07-api.md` and `docs/api-design-research.md`.

**Status:** Round 2.0 — spec-ready. All 35 resolutions from
docs/history/spec-readiness-review.md (2026-07-07 audit) folded in.
**Input:** `docs/history/backend-research-brief.md` (authoritative), study of the production
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
   `internal/checker` (15 checks, two-phase conditional execution, SSRF-pinned dialer,
   IPv6 self-preflight, NS zone walk) with scoring deleted.
3. **Split DNS load.** The classification-critical records (apex + www AAAA) are
   resolved through **3 public resolvers concurrently (Cloudflare/Google/Quad9) with a
   2-of-3 quorum**; everything else (NS chains, MX hosts, A records, sub-resource AAAA)
   goes through **local Unbound recursors**. That's ~24 qps per public provider
   (validated safe: Google documents 1500 qps/IP, Quad9's contact threshold is
   500 qps) and ~140–190 qps on Unbound (§2.7 — an order of magnitude of headroom on
   one box).
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
(~12 domains/s sustained, ~72 concurrent domain slots on average (128 provisioned,
§2.7), ~1–2 GB/day of raw scan detail before compression). The real costs are (a) the confirmed-status state machine —
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
| `internal/checker/resolver.go` | DNS resolver: EDNS0, UDP→TCP-on-truncation, retry-once-on-next-upstream, CNAME chase ≤10 hops with loop detection. Behavior lifted 1:1 **except the in-process TTL cache, which is deleted (§2.8 A)**; API ported to miekg/dns **v2** (§2.4, §2.8 D, OPEN-9) |
| `internal/checker/ssrf.go` | `SafeDialer`: full v4+v6 blocklists (RFC1918, CGNAT, link-local/metadata, Teredo/6to4, NAT64, ULA, AWS v6 metadata), DNS-pinned dialing — resolve once, validate, dial the literal IP |
| `internal/checker/dns_aaaa_base.go`, `dns_aaaa_www.go` | AAAA lookup + CDN detection on www CNAME chain — **adapted, not verbatim**: wrapped into composite `base`/`www` observations (§2.3.1); v6audit's NXDOMAIN→`not_applicable` mapping is replaced per-dimension, and the globally-routable-AAAA filter from production (`whynoipv6/internal/resolver/resolver.go:486-514`) is ported in |
| `internal/checker/dns_ns_ipv6.go` | NS with **label walk-up zone discovery** — fixes the production `co.uk` bug without a PSL |
| `internal/checker/dns_mx_ipv6.go` | MX with RFC 5321 implicit-MX fallback and RFC 7505 null-MX → `not_applicable` |
| `internal/checker/http_ipv6.go`, `https_ipv6.go`, `tls_ipv6.go` | **pure tcp6-only reachability** (the headline check), TLS handshake w/ expiry+hostname; only the UA constant changes — plus one enumerated deviation: `http_ipv6.go` is extended during the lift with the same terminal error classification `https_ipv6.go` already has (`isTimeout` → `details.error_type = "timeout"`; keep connection-refused → `unsupported`; everything else → `error_type = "unknown"`), so the §2.2 `conn` mapping table applies identically on the http-only fallback path. This is one of the three enumerated lift deviations, alongside the UA constant and the miekg/dns v2 resolver port |
| `internal/checker/response_parity.go` | v4-vs-v6 fetch comparison (status/content-type/±10% body length) |
| `internal/checker/smtp_ipv6.go`, `dns_ptr_ipv6.go`, `dns_dnssec.go`, `spf_ipv6.go`, `latency.go` | SMTP EHLO over v6, PTR+FCrDNS, resolver-validated DNSSEC (AD flag), SPF v6 mechanics, TTFB v4/v6 |
| `cmd/v6agent/main.go:356-380` (`checkIPv6Connectivity`) | the **IPv6 self-preflight** — moved in front of every claim cycle |
| `runner.go` `runPhase`/`runCheck` | bounded-errgroup phase execution with per-check panic recovery |

**Adapt** (not verbatim): `checker.go` (drop `Category()`, drop `ScanResult.Score/Grade`,
drop `DBColumnToChecker`), `runner.go` (remove the `ComputeScore` call; keep two-phase
gating and skip-reasons; `latency_ipv4` moves to phase 2 per §2.8 C), the resolver
wiring (§2.4), and the base/www composite wrapper (§2.3.1) that combines the quorumed
AAAA with a conditional bulk-resolver A lookup. The full adaptation contract is §2.8.

> `resource_ipv6.go` → adapted into `resource_discovery`: keep the IPv6-pinned page fetch (2 MB body cap, 15 s timeout, ≤3 redirects), the streaming HTML tokenizer (script/img/link/iframe/source/video/audio/object/embed + `<base href>`), external-host dedup, and the 50-host cap. **Delete** the inline concurrent AAAA checks and the supported/partial/unsupported derivation (lines 88–147 of the v6audit file) — host AAAA status lives in the `resource_host` registry (§4.6). The check returns the **full** deduped host list (≤50, no 20-item truncation) plus `{total_hosts}` in Details, and a discovery status: `ok` (fetch succeeded; list may be empty), `not_applicable` (phase-2 gate: no AAAA on the domain), `error` (fetch/TLS failure).

**Delete**: `scoring.go`, all
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
(`supported/unsupported/no_record/not_applicable`); `error` and `inconsistent` exist
only on raw observations and never become public. The mapping is **explicit per dimension**
(the last rewrite's mistake was collapsing `partial→supported` silently in
`workers.go:341-420`):

| Dimension (public) | Engine source | `partial` maps to | Role |
|---|---|---|---|
| `base` (apex AAAA) | `dns_aaaa_base` | n/a | **core** — the sinner trigger |
| `www` (www AAAA) | `dns_aaaa_www` | n/a | **core** — hero gate |
| `ns` (nameserver v6) | `dns_ns_ipv6` | `supported` (≥1 NS with AAAA ⇒ zone resolvable over v6; per-host detail for up to 4 NS hosts kept in scan payload, §2.8 E) | **core** — hero gate |
| `mx` (mail v6) | `dns_mx_ipv6` | `supported` (≥1 MX host with AAAA ⇒ mail deliverable over v6) | **core** — hero gate when mail exists |
| `conn` (pure IPv6-only reachability) | derived — worker-side composition of `https_ipv6` + `http_ipv6` (decision table below) | n/a | **core** — hero gate; confirmed `unsupported` ⇒ `broken_v6` flag (refused / TLS failure / preflight-guarded persistent timeout, each after N=3); non-definitive errors never set the flag and never advance confirmed state |
| `resources` (#23 dependencies) | registry roll-up (§4.6) over linked hosts' **confirmed** `resource_host.aaaa_status`; link discovery via adapted `resource_discovery` + manual `v6ctl resource add` | n/a — the roll-up is defined directly in 4-valued terms, no engine `partial` exists on this path; `resources_v4only` flag set when **confirmed** resources = `unsupported` | **core for Gold badge only** — never affects hero/sinner |
| `tls` validity | `tls_ipv6` | n/a | informational (an invalid cert already fails `conn` via https) |
| `smtp` EHLO over v6 | `smtp_ipv6` | `unsupported` | informational |
| `parity` v4-vs-v6 | `response_parity` | kept as-is in payload | informational |
| `dnssec`, `ptr`, `spf`, `latency_v4/v6` | respective checks | kept as-is | informational |

**Storage rule (normative).**

> `partial` is a legal stored value ONLY for the two informational dimensions whose
> mapping above is "kept as-is": `ptr` and `parity` (columns `domain.ptr_observed`,
> `domain.parity_observed`, `scan.ptr`, `scan.parity`). Every other partial-capable
> engine check is mapped to a non-partial observation BEFORE any DB write, per the
> table above: ns partial -> supported, mx partial -> supported,
> smtp partial -> unsupported (`resources` has no engine `partial` on its path — the
> §4.6 roll-up is defined directly in 4-valued terms). The core-dimension
> `*_observed`/`*_pending` columns and the §4.3 commit algorithm therefore never see
> `partial`; the raw engine verdict is always preserved in `scan_detail.details`.

*Rejected — strict all-NS/all-MX = supported:* a zone with one v6-capable NS **is**
resolvable from an IPv6-only network, and one v6 MX **does** accept mail over v6;
requiring all hosts would shame operators who are functionally v6-ready. The per-host
detail stays visible in the scan payload for the detail page — capped at 4 NS hosts /
5 MX hosts plus the `total`/`checked`/`ipv6_count` counters (§2.8 E). (**OPEN-1:
decided** — ≥1-host rule adopted.)

*Rejected — dropping SPF/PTR/DNSSEC (not in brief §3.3's core list):* they cost 1–3
cheap local-resolver queries each, come free with the engine, and feed the detail page.
They are informational-only and never gate classification.

**Two-phase conditional execution** (lifted from `runner.go:60-128`): phase 1 always
runs `base, www, ns, mx, dnssec, spf`; phase 2 (`conn/tls/parity/
resources/latency_v4/latency_v6/ptr` gated on an AAAA existing, `smtp` gated on MX v6)
is skipped with recorded `not_applicable` results (`latency_v4` moved out of phase 1
per §2.8 C — it exists only as a v4-vs-v6 comparison and would otherwise fire ~2.5M
daily HTTPS probes against v4-only sites). Since ~72–75% of the top-1M has no AAAA,
most domains cost only DNS.

**Kind-aware checks (brief §6 entity model):** for `kind = subdomain` entities
(`nettbank.dnb.no` from campaigns), `www` is forced `not_applicable` and the MX check
**skips the implicit-MX fallback** (explicit MX → evaluate normally; no MX →
`not_applicable`, not "the AAAA accepts mail"). NS walk-up already climbs to the
authoritative zone automatically. Because `not_applicable` never counts against a
domain, a subdomain can be a Hero on host + NS + conn.

**`conn` composition rule.** `conn` is a **derived dimension with no single engine
source** — v6audit has no combiner (`https_ipv6` and `http_ipv6` are independent
phase-2 siblings in `runner.go`), so this composition function is new code in the
worker, applied after `Runner.Run` and before the §4.3 commit.

**Inputs:** the same scan's `https_ipv6` result `H` (status + `details.error_type`
+ `details.reason`) and `http_ipv6` result `P` (status). Both checks stay
registered as independent phase-2 checks and both run unconditionally whenever
phase 2 runs (gate unchanged from the lifted runner: base OR www AAAA
supported). When phase 2 is skipped, both are `not_applicable`.

**Decision table (first match wins):**

| # | Condition | conn observation | Notes |
|---|---|---|---|
| 1 | H = `supported` | `supported` | source=`https`, http_only=false |
| 2 | H = `unsupported` AND H.error_type = `connection_refused` AND P = `supported` | `supported` | source=`http`, **http_only=true** — the only fallback case |
| 3 | H = `unsupported` AND H.error_type = `certificate_error` | `unsupported` | never rescued by http: an invalid cert over v6 is broken v6 (consistent with the `tls` row: "an invalid cert already fails `conn` via https") |
| 4 | H = `unsupported` (any other case: connection_refused with P ≠ supported; or no-AAAA-on-host) | `unsupported` | ⇒ `broken_v6` flag once confirmed (§4.3/brief §5.5) |
| 5a | H = `error` with H.error_type = `timeout` AND the process preflight (§2.5) passed within the last 5 minutes | `unsupported` | a persistent connect/response timeout against a published AAAA **is** the canonical broken-v6 failure and must be definitive; the raw `error_type = "timeout"` stays in the scan payload for the detail page |
| 5b | H = `error` with H.error_type = `timeout` but preflight stale/failed | `error` | non-definitive; worker should not be claiming anyway per §2.5 |
| 5c | H = `error`, any other error_type (`unknown`, blocked address, internal) | `error` | non-definitive: touches nothing, `recheck_error` applies |
| 6 | H = `not_applicable` (phase 2 skipped: no AAAA on base or www) | `not_applicable` | |

`error` outcomes (rows 5b/5c) are **never overridden by P, even P = `supported`** —
non-definitive per §4.3 ("touch nothing"); recorded in the scan log only, never
advancing confirmed state. The N=3 anti-flap for `conn` (§2.3) is unchanged and is
the flap guard: a single slow (>10s) response never demotes a hero; only three
consecutive daily timeouts confirm `unsupported`, write the changelog entry, and
raise `broken_v6`.

"HTTP-only site" is thereby **operationally defined** as: port 443 actively
refuses (ECONNREFUSED) while port 80 serves over IPv6. A 443 that blackholes
(firewall DROP → timeout) takes rows 5a/5b: under a passing preflight it is
definitive `unsupported` (the canonical broken-v6 failure; N=3 still applies),
while under a stale/failed preflight it is non-definitive `error` — the confirmed
`conn` never advances (a previously confirmed value simply persists, and per §4.3
a NULL-confirmed `conn` blocks hero without setting a flag). Rows 2/3 cannot
conflict and row "no-AAAA + P=supported" cannot occur: both checks issue the
identical AAAA lookup on the same host through the same bulk resolver
(Unbound-cached, §2.8 A).

**Target host:** `conn` always dials **the entity's own host** — the apex for
`kind = apex`, the subdomain itself for `kind = subdomain`. This is the
verbatim §2.1 lift (each check does its own `LookupAAAA(entity_host)` and
dials only that host); there is **no www fallback for conn**. A www-only
domain (AAAA on `www` but not the apex) therefore gets
`conn = unsupported` (row 4, no-AAAA-on-host). This can never change hero
membership: hero already requires `base = supported`.

**Payload:** the worker hoists a derived object into `scan_detail.details`:
`"conn": {"status": "...", "source": "https"|"http", "http_only": bool, "error_type": "..."}`
(`error_type` copied from the https result when present; omitted on success).
`http_only` is payload-only for the detail page — it is **not** a
`class_flag` and does not alter §5.1's `v6_only` field, which serves the
confirmed `conn` status unchanged.

**Accepted skew (stated, not fixed):** the https/http checks re-resolve AAAA
via the **bulk** resolver (§2.4), not the consensus verdict used for
`base`/`www`. A persistent disagreement (e.g. region-scoped GeoDNS AAAA
visible to the 3 public anycast networks but not to our local Unbound) can
confirm `conn = unsupported` on a `base = supported` domain after N=3
consecutive scans. That outcome is accepted and semantically honest —
"publishes AAAA, unreachable over v6 from our vantage" is exactly what
`broken_v6` means — and transient skew is absorbed by the N=3 anti-flap rule.

### 2.3 Consensus (Tier 1) and anti-flap

**Quorum applies only to the two classification-critical lookups: apex AAAA and www
AAAA.** For each, query Cloudflare (`1.1.1.1`/`2606:4700:4700::1111`), Google
(`8.8.8.8`/`2001:4860:4860::8888`) and Quad9 (`9.9.9.9`/`2620:fe::fe`) **concurrently**
(2s per-resolver timeout, one retry). Each resolver's response is first classified as
a **valid answer** or a **non-answer**:

- valid answer: rcode NOERROR → `exists` (≥1 globally-routable AAAA) or `empty`
  (no AAAA after CNAME chase); rcode NXDOMAIN → `nxdomain`. These are §2.3.1's
  per-resolver symbols; the post-quorum mapping to the per-dimension observation
  (`supported`/`unsupported`/`no_record`/`not_applicable`) is §2.3.1's tables.
- non-answer (§2.3.1's `error` symbol): any other rcode (SERVFAIL, REFUSED, …),
  timeout, or transport error — after the single retry. SERVFAIL is "the resolver
  could not determine an answer" (e.g. broken DNSSEC on all three validating
  resolvers); it is never a vote.

The quorum is taken **over these reduced symbols, not record sets** (GeoDNS
legitimately returns different AAAA contents per region; what must agree is *whether
v6 exists*), and only **over valid answers**:

- ≥2 valid answers agree → that symbol is the quorum result (3-0, 2-1, or 2-0 with
  one non-answer); it maps to the dimension's observation per §2.3.1, including the
  conditional A lookup when the quorum symbol is `empty`.
- ≥2 valid answers, no two agree → observation = `inconsistent`.
- ≤1 valid answer (≥2 resolvers non-answering) → observation = `error`.

`inconsistent` and `error` are both non-definitive (never advance confirmed state,
never write changelog) but schedule differently: `inconsistent` → 2h lane, `error` →
6h lane (§2.5 recheck rules). The per-resolver tuple {resolver, rcode, reduced symbol,
answered} for both consensus lookups is recorded in `scan_detail.details.consensus`
(v6audit's `LookupAAAA` already returns the rcode string).

**Anti-flap / confirmation rule (the "layman's Byzantine generals" answer):** a
dimension's **confirmed** value changes only when a new definitive value has been
observed on **N consecutive scans spaced ≥ `anti_flap.min_confirm_spacing` apart**
(default 12h; the §4.3 counting gate) — N=2 for DNS dimensions (`base/www/ns/mx`), N=3
for the noisier `conn` and `resources`. At daily cadence that is the advertised +1/+2
days of transition latency — and never faster than (N−1) × 12h even when fast-lane
rechecks run every 2h — which is the right trade for a changelog users must trust. The
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

#### 2.3.1 Observation mapping — from engine outcomes to per-dimension observations

Each per-resolver AAAA answer (apex and www, consensus resolver) reduces to one of four symbols, and quorum (§2.3 rules unchanged) is taken over these symbols:

- `exists` — ≥1 globally-routable AAAA (loopback/link-local/ULA answers rejected)
- `empty` — NOERROR, no AAAA after CNAME chase
- `nxdomain` — NXDOMAIN
- `error` — timeout / SERVFAIL / network error

No-quorum → observation `inconsistent`; quorum on `error` → observation `error` (both handled per §2.3/§4.3: never advance confirmed state).

**Conditional A lookup:** when and only when the AAAA quorum result is `empty`, the wrapper issues ONE A query for the same name through the **bulk resolver** (no quorum; Go transport: §2.8 B's `AAAAAnswer.AOutcome`). Outcomes: `a_present` (≥1 A), `a_absent` (NOERROR-empty; an A-NXDOMAIN contradicting the AAAA NOERROR is also treated as `a_absent` — resolve contradictions in the domain's favor), `a_error`. These are the two "A×2" queries already budgeted in §2.7. The A answer is not quorumed: any single wrong answer still has to survive the N=2 confirmation gate (§4.3) before it can change confirmed state.

**`base` observation (apex; for `kind=subdomain`, the host itself):**

| AAAA quorum | A lookup | base observation |
|---|---|---|
| `exists` | not run | `supported` |
| `empty` | `a_present` | `unsupported` (sinner-eligible: A exists, AAAA definitively absent) |
| `empty` | `a_absent` | `no_record` (empty/parked zone → inactive) |
| `empty` | `a_error` | `error` |
| `nxdomain` | not run | `no_record` (domain doesn't exist → inactive; raw rcode kept in `scan_detail` so §4.8 dead-detection can require NXDOMAIN specifically) |
| `error` | not run | `error` |
| no quorum | not run | `inconsistent` |

**`www` observation** (skipped entirely — forced `not_applicable` — for `kind=subdomain`, §2.2 unchanged):

| AAAA quorum | A lookup | www observation |
|---|---|---|
| `exists` | not run | `supported` |
| `empty` | `a_present` | `unsupported` (www is v4-only → blocks hero, `www_missing` flag) |
| `empty` | `a_absent` | `not_applicable` (www node serves nothing → site doesn't use www) |
| `empty` | `a_error` | `error` |
| `nxdomain` | not run | `not_applicable` (site doesn't use www) |
| `error` | not run | `error` |
| no quorum | not run | `inconsistent` |

**`www` never produces `no_record`.** `no_record` can only ever be observed (and confirmed) for `base`.

**Remaining core dimensions** (engine status → observation; engine statuses per v6audit `checker.go`):

| Dimension | supported | partial | unsupported | not_applicable | error |
|---|---|---|---|---|---|
| `ns` | `supported` | `supported` (§2.2 ≥1-host rule) | `unsupported` | (never emitted; walk-up finding no zone yields engine `error`) | `error` |
| `mx` | `supported` | `supported` (§2.2) | `unsupported` | `not_applicable` (null-MX; and for subdomains, no explicit MX) | `error` |
| `conn` | `supported` | n/a | `unsupported` (requires preflight pass ≤5 min; full composition + timeout rule in §2.2's decision table) | `not_applicable` (phase-2 skip: no AAAA) | `error` (except preflight-guarded persistent timeouts → `unsupported`, §2.2 row 5a) |

`resources` is not engine-status-mapped: its observation is produced by the §4.6 registry roll-up (defined directly in 4-valued terms; the adapted `resource_discovery` check only feeds link discovery). `ns`, `mx`, `conn`, and `resources` never produce `no_record` either. The `observation` enum values `error`/`inconsistent` and the `ipv6_status` values are otherwise as defined in §4.1; informational dimensions (`tls/smtp/parity/dnssec/ptr/spf/latency`) have no mapping table — `ptr` and `parity` store the raw engine status verbatim (including `partial`) in `*_observed`/payload, while `smtp` maps `partial` → `unsupported` before storage (§2.2); the rest keep their raw status in the scan payload.

### 2.4 DNS resolver-load split

Two resolver instances are injected into the engine (a small adaptation of
`checker.Resolver` construction — the checks themselves don't change):

- **Consensus resolver** — used *only* by `dns_aaaa_base` and `dns_aaaa_www`. Fans out
  to the 3 public resolvers, applies quorum, returns the agreed answer + a
  disagreement annotation (seam and Go transport: §2.8 B).
- **Bulk resolver** — used by everything else (NS chain + NS-host AAAA, MX + MX-host
  AAAA, A records, PTR, TXT/SPF, DNSSEC probes, the resource-host registry sweep §4.6).
  Points at **two local Unbound instances** (round-robin, retry-on-next — the existing
  `resolver.go` behavior is unchanged apart from the §2.8 A cache deletion; different
  upstream addresses). The in-process TTL cache is deleted, not replaced: it is
  unbounded, evicts only on same-key re-access, and clamps TTL to ≤300s while the
  frontier revisits a name every 24h — at ~12–16M mostly-unique queries/day it is a
  multi-GB dead map. **Unbound is the cache**; intra-scan duplicate lookups hit Unbound
  locally at sub-ms cost (§2.8 A).

Load math at 1M/day (see §2.7 for the full table): consensus = 2 records × 3 resolvers
× 1.03M = 6.2M queries/day ≈ **71 qps total, ~24 qps per provider**. Research verdict:
Google documents **1500 qps/IP** (we'd use 1.5%); Quad9 documents a **500 qps contact
threshold** (4.6%); Cloudflare publishes no number but flags "security scanning"
patterns and SERVFAIL storms — mitigation: the consensus resolver in `consensus/`
**owns rate control**, plus a courtesy note to resolver@cloudflare.com describing the
research use (cheap insurance, recommended):

1. Per-provider token bucket, `consensus.per_provider_qps` sustained (blocking
   acquire; worker slots absorb the wait). This is the "smooth the rate" mechanism.
2. Fast-lane breaker: over a rolling `window`, if (error+inconsistent)/total
   consensus observations > `nondefinitive_rate` with ≥ `min_samples`, stop applying
   the 2h/6h pull-ins (non-definitive scans schedule at `cadence(rank)` instead), and
   alert the ops webhook. Re-enable when the rate stays below `recover_below` for one
   full window. This caps the resolver-degradation amplification (worst case
   otherwise ≈ 12× the sized 24 qps/provider).
3. Provider breaker: if a single provider's non-answer rate > `failure_rate` over the
   window with ≥ `min_samples`, drop it from the fan-out and alert; quorum degrades
   to 2-of-2 (both remaining agree → observation; disagree → `inconsistent`; ≤1 valid
   answer → `error`). A canary lookup probes the sick provider every 5 min; restore
   after `recovery_probes` consecutive successes.

"Never retry a SERVFAIL'ing domain in a tight loop" is enforced structurally by the
§2.5 recheck backoff and the §4.8 dead lifecycle.

Bulk = **~12–16M queries/day ≈ 140–190 qps** against Unbound (derivation in §2.7;
excludes the decoupled resource-host sweep, ~2–4 qps). Tuned per NLnet Labs
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
maintenance-mode; **OPEN-9: decided**; module path and version pin per §2.8 D): the
port is mechanical per the official
v1→v2 migration guide, and reverting to the v1 API is equally mechanical if v2
misbehaves during phase-2 verification.

### 2.5 Worker / frontier model

**The frontier is the `domain` table itself.** Scheduling state = `next_check_at`
(+ `rank`) with a partial index; workers claim atomically:

```sql
UPDATE domain SET claimed_at = now()
WHERE id IN (
  SELECT id FROM domain
  WHERE (NOT disabled OR disabled_reason IN ('dead', 'delisted'))
    AND next_check_at <= now()
    AND (claimed_at IS NULL OR claimed_at < now() - interval '30 minutes')
  ORDER BY rank ASC NULLS LAST, next_check_at ASC
  LIMIT $1
  FOR UPDATE SKIP LOCKED
) RETURNING id, host, kind, rank, claimed_at, disabled, disabled_reason, dead_streak,
            <all d_status/d_pending/d_pending_count/d_since column groups>;
```

> **Plan shape (load-bearing).** The inner SELECT must execute as an index scan on
> `idx_domain_due` (§4.2) bounded by `next_check_at <= now()`, followed by a top-N
> heapsort on `(rank NULLS LAST, next_check_at)`. Cost is O(due-set) per claim: a few
> hundred rows in the steady state, at worst one pass over the full backlog after
> downtime (~hundreds of ms at 1M due, shrinking as the backlog drains) — and
> rank-priority fall-behind is exact in both regimes. Do NOT "optimize" with an inner
> `ORDER BY next_check_at LIMIT k` pre-filter before the rank sort: that silently
> flips the brief's fall-behind policy from rank-priority to aging-priority precisely
> when the due set is large. The `claimed_at` lease condition is intentionally a
> residual filter, not an index column (`claimed_at` is deliberately unindexed so
> lease stamping can be a HOT update).

`service`/`manual` rows are excluded by the predicate (they "leave the frontier
entirely", §4.8). `dead`/`delisted` rows are claimable but sit on the slow lane
because their `next_check_at` is +30d (§4.8). This is the v2-team `ClaimBatch` pattern
(`internal/postgres/domain_repo.go`), hardened: a separate `claimed_at` lease (instead
of v2's "claiming sets `ts_check`") means a crashed worker's batch is reclaimed after
30 minutes rather than lost for a day. Post-commit scheduling rule:

- if the row is still/newly disabled after the commit → `next_check_at = now() +
  lifecycle.slow_lane_every` (default 720h)
- else → `next_check_at = now() + cadence(rank)` (and the recheck pull-in/backoff
  rules below for `inconsistent`/`error`).

**Claim-loop cadence.** Config, `crawler` section:

```yaml
claim:
  batch_size: 200          # $1 above
  empty_poll_interval: 10s # sleep when a claim returns zero rows
```

After a claim returning ≥1 row, the process feeds its worker pool and claims again as
soon as the batch is dispatched (the pool's slot availability is the natural
throttle). After a claim returning 0 rows, sleep `empty_poll_interval` before the next
preflight+claim cycle. With `idx_domain_due`, an empty-frontier claim is a
sub-millisecond range probe, so this cadence is safe even at 1-second intervals; 10s
is chosen to keep idle-log noise down, not for DB protection.

`ORDER BY rank ASC NULLS LAST` implements the brief's fall-behind
policy directly: when due-domains exceed capacity, top-ranked domains are refreshed
first and the tail's effective interval stretches — graceful degradation, no separate
mode. (The starvation risk this creates for the tail under *permanent* undercapacity
is accepted per brief; a config flag can flip the sort to `next_check_at ASC` as an
aging pressure valve.)

Process model: **K crawler processes × M concurrent domain slots each** (start: 2
processes × 64 slots = 128; §2.7 derives ~72 slots average, so 128 leaves headroom for
tail latency). Each process: preflight → claim batch (~200) → feed an in-process worker
pool running `Runner.Run` per domain → commit results per domain in one transaction
(§4.3); each domain's complete commit unit — fenced domain UPDATE, changelog rows,
scan row, scan_detail row — is queued as a single `pgx.Batch` inside that domain's
`pgx.Tx`, so a whole-domain commit costs one round trip (vs the v2 rebuild's 2M
single-row round-trips/day). Batching is strictly a round-trip optimization over
intact per-domain atomic units; scan rows are never split out into a separate bulk
write, and `CopyFrom` is not used (it cannot preserve per-domain atomicity) → claim
next batch.

**Cadence-per-rank-band** is config, default daily everywhere:

```yaml
cadence:
  default: 24h
  bands: []              # e.g. [{max_rank: 10000, every: 12h}, {min_rank: 1000001, every: 72h}]
recheck_inconsistent: 2h
recheck_error: 6h
recheck_backoff_max: 720h
anti_flap:
  min_confirm_spacing: 12h
consensus:
  per_provider_qps: 15          # token bucket per provider PER PROCESS (2 procs => 30 qps/provider total, vs documented limits of 500-1500)
  fastlane_breaker: { nondefinitive_rate: 0.05, window: 15m, min_samples: 500, recover_below: 0.02 }
  provider_breaker: { failure_rate: 0.50, window: 15m, min_samples: 200, recovery_probes: 3 }
```

**Fast-recheck pull-in rules:**

1. Only the two consensus dimensions trigger pull-ins: if `base` or `www` observed
   `inconsistent` → lane = `recheck_inconsistent` (2h); else if `base` or `www`
   observed `error` → lane = `recheck_error` (6h). `inconsistent` wins over `error`.
2. `error` on `ns`/`mx`/`conn`/`resources` and anything on informational dimensions
   (`dnssec`/`ptr`/`smtp`/`parity`) never changes scheduling — those retry at normal
   cadence (anti-flap already ignores non-definitive observations).
3. Rechecks are full scans (`Runner.Run` on the whole domain); there is no
   partial-scan mode.
4. Backoff: `domain.error_streak` increments on every scan where `base` or `www` is
   non-definitive, resets to 0 otherwise. `next_check_at = now() + min(lane ×
   2^(error_streak−1), recheck_backoff_max)`. Default `recheck_backoff_max`: 720h
   (the 30d slow lane). Error progression: 6h, 12h, 24h, 48h, 96h, 192h, 384h, then
   capped.
5. Scheduling on a definitive scan is unchanged: `next_check_at = now() +
   cadence(rank)`.

**Self-preflight:** before *every* claim cycle the process runs
`checkIPv6Connectivity` (AAAA + tcp6 dial to a probe host, default
`one.one.one.one:443`); on failure it claims nothing, alerts via the ops webhook, and
retries in 60s. v6audit only had this in the remote agent — the internal worker gap is
explicitly closed here, since a v6-dark crawler mass-producing false `unsupported` is
the #1 false-negative source. Belt-and-suspenders: **every** `conn = unsupported`
observation — whether from connection-refused, TLS failure, or timeout — additionally
requires the preflight to have passed within the last 5 minutes; otherwise the
observation is downgraded to `error` (§2.2 decision table, rows 5a/5b).

**User-Agent:** `WhyNoIPv6Bot/1.0 (+https://whynoipv6.com/bot)` on every HTTP fetch;
`/bot` page documents purpose, opt-out contact, and crawl behavior. SMTP EHLO name
`whynoipv6.com`.

*Rejected — River (or any job queue):* the work is a flat, uniform, periodic sweep
over a known set — a frontier column + SKIP LOCKED is the whole requirement (this
justification implicitly assumes O(due) claims; with `idx_domain_due` that assumption
actually holds, including on the §2.7 Tranco-full path — at 4.5M rows the claim cost
stays proportional to the due set, not the table). A queue
adds a jobs table that must be *filled by a scheduler* (v6audit's scheduler died
exactly there: materializing 1M due domains into memory and millions of job-row
inserts per tick, `workers.go:1067-1193`, 2-minute job timeout). The check-queue for
on-demand live checks (§5.3) is the one place queue semantics are real, and SKIP
LOCKED covers that too. *Rejected — Redis/NATS work distribution:* new stateful infra
to operate for a problem Postgres already solves at this scale.

### 2.6 Crawl pass, stats, and notifications

There is no "pass" barrier in the hot path — workers run continuously against
`next_check_at`. A **daily tick** (03:30 UTC, after most of the day's Tranco delta has
settled) runs in `crawler`'s coordinator goroutine. Canonical step order (one advisory
lock for the whole sequence — see singleton coordination below):

1. **Lifecycle sweep** (one transaction, set-based over `rank IS NULL` rows — tens of
   thousands, cheap):
   a. Compute linkage for every non-disabled `rank IS NULL` row:
      `linked := EXISTS (SELECT 1 FROM campaign_domain cd JOIN campaign c ON c.id =
      cd.campaign_id AND NOT c.disabled WHERE cd.domain_id = d.id) OR EXISTS child
      (parent_id = id) OR last_requested_at >= now() - lifecycle.live_check_linkage`.
      Campaign membership counts only while the campaign itself is enabled — without
      the `NOT c.disabled` join, a disabled campaign's kept membership rows would pin
      its rank-NULL domains in the frontier forever, contradicting §4.8's delist
      grace.
   b. Linked rows (and any row with `rank IS NOT NULL`): `orphaned_at = NULL`.
   c. Unlinked rows with `created_by = 'live_check'`: disable immediately —
      `disabled = true, disabled_reason = 'delisted', disabled_at = now(),
      next_check_at = now() + lifecycle.slow_lane_every`. (No 30-day grace: the §5.3
      contract is a 7-day frontier linkage; `last_requested_at` has already expired.
      The grace exists for Tranco rank flapping, and these rows were never publicly
      listed.)
   d. Other unlinked rows: `orphaned_at = COALESCE(orphaned_at, now())`; where
      `orphaned_at < now() - lifecycle.delist_grace` → disable as in (c).
   Grace-period rows (`orphaned_at` set, not yet disabled) keep normal-cadence
   scanning; `ORDER BY rank NULLS LAST` already deprioritizes them. This sweep is the
   **single owner of orphan detection** — Tranco import and campaign sync never set
   `orphaned_at`; they only clear state on re-entry (§3/§7). Campaign-membership
   removals and live-check expiry are therefore picked up within 24h, which satisfies
   the 30-day/7-day windows with margin.
2. Snapshot product stats from confirmed state into the `stats_*` tables (§4.7).
3. Recompute country/ASN counter columns (ported `update_country_metrics` /
   `update_asn_metrics`, fixed: v6 definition = classification-based, v4 count =
   actual v4-only count — the production proc counted *all* domains as v4). Scope:
   `rank IS NOT NULL AND NOT disabled` (the §5.1 publicly-ranked predicate), so
   `/country` and `/metric/asn` figures match the public lists exactly.
4. Service-domain candidate detection (§4.8).
5. Campaign sync (pull + import, §7), via `Run(JobCampaignSync, wait=5m, …)` — nested
   lock, waits out a concurrently webhook-triggered sync rather than silently
   skipping the daily guarantee.
6. `check_job` purge, 30d retention (§5.3).
7. Ops summary → webhook (domains scanned, confirmed transitions, error rate, queue
   depth) + healthcheck ping (healthchecks.io pattern, lifted from production's
   heartbeat, IRC dropped).

**Failure containment:** a failing step logs the error and **continues** to the next
step; step 7's ops summary lists any failed steps by name. The tick never aborts
mid-sequence on a single step error. The healthcheck ping in step 7 fires only if
steps 1–3 succeeded (stats and lifecycle are the health-critical core); otherwise the
missed ping is the alert.

**Singleton-job coordination (advisory locks).** Both crawler processes are identical
(no `--coordinator` flag, no per-instance config). Each process runs the same
coordinator goroutine; every **singleton job** is gated by a Postgres
**session-scoped advisory lock**, keyed per job. Whichever process acquires the lock
runs the job; the other skips. This also serializes v6ctl-triggered runs (webhook,
cron, operator) against the coordinator — the lock, not the trigger topology, is the
mutual exclusion.

Lock registry (pinned constants, two-int form; classid identifies the app):

```go
// internal/lock/lock.go
const ClassID int32 = 60660 // whynoipv6 advisory-lock namespace, never change

const (
    JobDailyTick    int32 = 1 // §2.6 tick, all steps, one lock for the whole sequence
    JobTrancoImport int32 = 2 // §3 import (scheduled + v6ctl tranco import)
    JobCampaignSync int32 = 3 // §7 sync (tick + webhook/Semaphore + v6ctl campaign sync)
)
```

Distinct from golang-migrate's single-bigint advisory lock (different key encoding);
no collision.

Acquisition contract (`internal/lock`, used by crawler and v6ctl):

```go
// TryRun acquires (ClassID, job) via pg_try_advisory_lock on a connection
// checked out from the pool for the job's whole duration. If the lock is
// busy it returns ErrHeld without running fn. On return (or process crash /
// connection loss) the lock is released: pg_advisory_unlock on success path,
// session teardown otherwise.
func TryRun(ctx context.Context, pool *pgxpool.Pool, job int32, fn func(ctx context.Context) error) error

// Run is the blocking variant: pg_advisory_lock with a wait deadline
// (default 5m). Used by v6ctl so an explicitly requested run always executes
// once the concurrent one finishes; deadline exceeded → exit 1 with a clear
// "another <job> is running" error.
func Run(ctx context.Context, pool *pgxpool.Pool, job int32, wait time.Duration, fn func(ctx context.Context) error) error
```

Rules:

- The connection holding the lock is dedicated to the lock for the job's duration
  (job steps may use other pool connections freely). Session lock ⇒ crash of the
  holding process drops the connection and frees the lock — no lease/expiry
  machinery.
- Crawler-scheduled invocations use `TryRun`; a skip is **not** an error: log
  `level=info msg="singleton skipped, held elsewhere" job=<name>` and increment
  counter `crawler_singleton_skipped_total{job}` (Grafana). Getting exactly one skip
  per scheduled fire is the healthy steady state with 2 processes.
- All v6ctl invocations (`tranco import`, `campaign sync`, `stats recalc`) use `Run`
  with the 5-minute wait (hardcoded; no config key).

Trigger resolution (replaces ambiguous wording):

- §3: the 23:15 UTC Tranco import is fired by the **crawler coordinator goroutine**
  (gated by `JobTrancoImport`), NOT a systemd timer. The "list ID unchanged → retry
  in 2h" loop lives in the same goroutine. `v6ctl tranco import` remains the manual
  verb calling the identical import function under the identical lock.
- §7 step 2 stays "Both": Semaphore webhook → `v6ctl campaign sync` on the backend
  host, AND the daily tick runs sync. `JobCampaignSync` covers the entire sync —
  `git pull` + YAML import + UUID generation + bot write-back push — so the shared
  `/srv/whynoipv6-campaign` checkout is never touched by two syncs concurrently and
  UUIDs/bot commits cannot double-fire.
- §2.6: at 03:30 UTC both coordinators attempt `TryRun(JobDailyTick, …)`; one wins,
  runs all steps in order under the single lock; the loser skips the whole tick.

Idempotent-write second guard (protects against operator re-runs and any future
trigger overlap, even though the lock already serializes):

- Stats snapshot (step 2): all four `stats_*` inserts use `INSERT … ON CONFLICT
  (<pk>) DO UPDATE SET <every counter> = excluded.<col>` (PKs: `day` /
  `(day,country_id)` / `(day,campaign_id)` / `(asn_id,day)`). DO UPDATE, not
  DO NOTHING — this is also what makes `v6ctl stats recalc` (already in the §6 v6ctl
  verb list) a safe same-day re-run.
- Candidate detection (step 4): `ON CONFLICT DO NOTHING` on the candidate rows.
- Sweep, recompute, purge (steps 1, 3, 6): set-based UPDATE/DELETE, inherently
  idempotent — no change.
- Tranco import step 4: staging upsert already `ON CONFLICT (host) DO UPDATE`;
  additionally the `tranco_import` provenance insert conflicts on the partial unique
  index `(list_id) WHERE NOT aborted` (§4.9) DO NOTHING, so a re-run of an
  already-successfully-recorded list is a no-op.

SKIP LOCKED consumers unchanged: the frontier claim and check_job claim loops run in
every process by design and need no singleton gating.

Checkpointed **operational metrics** stream continuously: each process writes a
`crawler_metrics` row every 1000 domains (run_id, processed/success/fail, qps,
p50/p99 durations, per-dimension counters) — the prompts-spec design
(`11-resource-checker.md`) minus its unbounded in-memory latency slices (use a
streaming histogram/t-digest). Grafana-only, never the public API.

### 2.7 Throughput math (validating daily cadence)

**Canonical sizing constants** (normative; the derivation table below is their single
source — every other section cites these values and never restates an independent
range):

| Constant | Value |
|---|---|
| Engine checks | **15** (the §2.1 inventory, defined by enumeration per §2.8 F; latency.go registers 2 of them: latency_ipv4 + latency_ipv6) |
| Scan rate | ~12 domains/s sustained (1.03M/day) |
| Worker slots | **~72 average**, **128 provisioned** (2 processes × 64) |
| Public-resolver load | 6.2M queries/day ≈ 71 qps total, **~24 qps/provider** |
| Local (Unbound) bulk load | **~12–16M queries/day ≈ 140–190 qps** |
| Resource-host sweep (separate, decoupled per §4.6) | ~100–300k lookups/day ≈ **2–4 qps** |
| HTTP(S) fetches (per §2.8 C) | ~3M/day ≈ **35/s** |

Assume 1M ranked + ~30k campaign/subdomain entities; ~25% have apex or www AAAA
(current adoption ~20–28% depending on measure — use 25%), ~70% have MX.

| Stage | Volume/day | Rate | Sizing |
|---|---|---|---|
| Domains scanned | 1.03M | **~12/s sustained** | — |
| Public-resolver queries (apex+www AAAA × 3) | 6.2M | 71 qps ÷ 3 ≈ **24 qps/provider** | Google 1.6% of limit, Quad9 4.8% of threshold |
| Local-resolver queries (A ≤2 — conditional per §2.3.1, only on NOERROR-empty AAAA quorum, fires for ~75% of names; NS walk ~2 + NS-AAAA ≤4; MX 1 + MX-AAAA ≤5·70%; DS+SOA 2; TXT 1; PTR ≤3·25%) ≈ 12–16/domain | **~12–16M** | **140–190 qps** | Unbound: 1–3% of tuned single-instance capacity |
| Resource-host sweep (registry AAAA, bulk resolver) | ~100–300k lookups/day | ~2–4 qps | negligible |
| HTTP(S) fetches (http+https+tls+parity×2+resource page ≈ 5–6, plus latency v4+v6 TTFB probes ≤3+3 ≈ 11–12 per v6 domain × 258k) | ~3M | ~35/s | ~50–80 concurrent sockets (latency probes are TTFB-only, body unread — bandwidth row unchanged) |
| Egress bandwidth (parity 2×1MB cap, resource page 2MB cap; typical pages ~200–500KB) | ~200–400 GB | ~25–45 Mbps avg | trivial on operator hardware |
| Worker concurrency: phase-1-only (pure DNS after §2.8 C) ≈ 2–4s wall (775k), full phase-2 incl. latency probes ≈ 10–25s (258k) → weighted ≈ 6s/domain | — | 12/s × 6s = **~72 slots avg** | provision 128 slots (2 procs × 64) for tail latency |
| DB writes: 1 scan + 1 detail row/domain + state UPDATE, batched | ~3.1M rows | ~36/s | nothing for PG18 |

**Consistency rule:** wherever this document (or the implementation spec) mentions
check count, slot counts, or resolver qps, use the constants above verbatim — no new
independent ranges. If a future edit changes a derivation input (adoption %, per-check
query counts), re-derive this table and update the citing sentences in §1/§2.4/§2.5 in
the same commit.

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

### 2.8 Engine adaptation contract (supersedes the "verbatim" listing in §2.1 and "zero changes" in §2.4)

The following files move from "lift verbatim" to "adapt". Every other §2.1 row stays verbatim.

**A. `resolver.go` — delete the in-process cache (bulk path).**
Remove `cacheEntry`, `dnsCacheKey`, `minTTL`, the `cache sync.Map` field, and the cache load/store in `QueryWithRetry` (v6audit resolver.go:28-31, 38, 50-94, 153-162, 177-182). `QueryWithRetry` keeps only the retry-once-on-error/SERVFAIL/REFUSED logic. Rationale (record in §2.4): the cache is unbounded, evicts only on same-key re-access, and clamps TTL to ≤300s while the frontier revisits a name every 24h — at ~12–16M mostly-unique queries/day it is a multi-GB dead map. Unbound is the cache; intra-scan duplicate lookups (apex AAAA ~3-5×/domain) hit Unbound locally at sub-ms cost. Do NOT replace with an LRU. The §2.1 table row for resolver.go drops the phrase "TTL cache (30s–300s clamp, RFC 2308 negative caching)". Everything else in resolver.go (EDNS0, UDP→TCP on truncation, round-robin upstreams, CNAME chase ≤10 hops) is unchanged.

**B. The consensus seam (`dns_aaaa_base.go`, `dns_aaaa_www.go`, new `internal/consensus`).**

In package `checker`, define (this is the seam §6's `consensus/` implements):

```go
// AAAAAnswer is the result of a (possibly quorum'd) AAAA resolution.
type AAAAAnswer struct {
    IPs        []net.IP
    CNAMEChain []string   // full chase, feeds cname_chain + CDN detection
    TTL        int        // min TTL of the returned answer set
    Rcode      string     // "NOERROR", "NXDOMAIN", ...
    Quorum     *QuorumInfo // nil when not quorum-resolved
    AOutcome   string      // "a_present" | "a_absent" | "a_error"; set only when the
                           // AAAA quorum result was NOERROR-empty — the §2.3.1
                           // conditional bulk-resolver A lookup. Empty otherwise.
}

// QuorumInfo records the per-resolver breakdown of a consensus lookup.
type QuorumInfo struct {
    PerResolver map[string]string // "cloudflare"|"google"|"quad9" → per-resolver symbol, §2.3.1:
                                  // "exists"|"empty"|"nxdomain"|"timeout"|"error"
                                  // (timeout/error both reduce to §2.3.1's `error`; kept split here for diagnostics)
    Agreement   string            // "3of3", "2of3", "2of2"
    Disagreed   bool              // true when an answering resolver's reduced status differed from the quorum
}

// ErrQuorumInconsistent is returned when no quorum per §2.3 is reached.
var ErrQuorumInconsistent = errors.New("resolver quorum inconsistent")

// AAAAResolver is the seam consumed by dns_aaaa_base and dns_aaaa_www.
type AAAAResolver interface {
    LookupAAAA(ctx context.Context, name string) (AAAAAnswer, error)
}
```

`internal/consensus` implements `AAAAResolver`: three single-upstream `checker.Resolver` instances (Cloudflare, Google, Quad9 — v4+v6 addresses per §2.3), queried concurrently, 2s per-resolver timeout, one retry, **no caching anywhere on this path** (every observation must be fresh). Each resolver's reply is reduced to a per-resolver symbol per §2.3/§2.3.1; quorum is over symbols. Outcomes:
- Quorum reached: return the **entire answer** (IPs, CNAME chain, min TTL, rcode) of the first resolver in fixed order Cloudflare → Google → Quad9 whose reduced symbol equals the quorum symbol, with `Quorum` filled (`Disagreed=true` on 2-of-3 splits). Do not merge record sets across resolvers.
- No quorum (§2.3 "otherwise"): return `AAAAAnswer{Quorum: &qi}, ErrQuorumInconsistent`.

`internal/consensus` also holds a reference to the **bulk** resolver for the §2.3.1 conditional A lookup: when and only when the AAAA quorum result is NOERROR-empty, it issues ONE un-quorumed A query for the same name through the bulk resolver and sets `AOutcome` per §2.3.1 (`a_present`/`a_absent`/`a_error`). This is the base/www composite wrapper of §2.3.1 in Go terms.

Adapt `dns_aaaa_base.go` / `dns_aaaa_www.go`: constructors become `NewDNSAAAABase(res AAAAResolver)` / `NewDNSAAAAWWW(res AAAAResolver)` (they only resolve, never dial — the SafeDialer dependency is dropped from these two checks; runner.go wiring changes accordingly). Check logic is otherwise unchanged except:

```go
ans, err := c.res.LookupAAAA(ctx, name)
if ans.Quorum != nil {
    details["quorum"] = ans.Quorum          // persists into scan_detail (the §2.4 "disagreement annotation")
}
if errors.Is(err, ErrQuorumInconsistent) {
    details["inconsistent"] = true
    return Result{Status: StatusError, Details: details, Latency: time.Since(start)}, nil
}
// ...existing err / NXDOMAIN / no-ips / supported branches, using ans.IPs, ans.CNAMEChain,
// ans.TTL, ans.Rcode, and ans.AOutcome (the §2.3.1 base/www mapping tables)
```

Engine statuses stay 5-valued (§2.2 unchanged); `inconsistent` exists only at the observation layer. The Result→observation mapper (crawler-side, new code) adds one rule for the `base`/`www` dimensions: `Status == error AND Details["inconsistent"] == true → observation 'inconsistent'`; any other `error → observation 'error'`. This drives §2.3/§2.5's 2h-vs-6h recheck split and §4.3's "touch nothing" branch.

`SafeDialer` keeps its concrete bulk `*Resolver` unchanged for all DNS-pinned dialing: `conn`/`tls`/`parity`/`resources` re-resolve via the bulk resolver when dialing. Consensus answers gate classification only; they are never used as dial targets.

**C. `runner.go` — move `latency_ipv4` to phase 2, gated on AAAA.**
Remove `"latency_ipv4"` from `phase1Names` (runner.go:60-68) and add it to the hasAAAA-gated case alongside `http_ipv6/https_ipv6/tls_ipv6/latency_ipv6/resource_ipv6` (runner.go:95), skip reason `reasonNoAAAARecord`. Rationale: latency is informational and exists only as a v4-vs-v6 comparison; up to 3 real HTTPS GETs against ~750k v4-only sites daily (~2.5M fetches) contradicts §2.2's "most domains cost only DNS" and the politeness posture. Doc corrections: §2.2 phase-1 list becomes `base, www, ns, mx, dnssec, spf`; §2.7's fetch row becomes "HTTP(S) fetches (http+https+tls+parity×2+resource page ≈ 5–6, plus latency v4+v6 TTFB probes ≤3+3) ≈ 11–12 per v6 domain × 258k ≈ **~3M/day ≈ 35/s**" (latency probes are TTFB-only, body unread — bandwidth row unchanged).

**D. DNS library pin.**
"miekg/dns v2" means module path `codeberg.org/miekg/dns`, pinned in go.mod at an exact version (v0.6.83 at time of writing; use latest v0.6.x at implementation). It is pre-1.0: any version bump is a reviewed change, never `go get -u`'d in passing. `github.com/miekg/dnsv2` is a stale dead path — never import it. (Amends §2.1 table row, §2.4, OPEN-9.)

**E. NS/MX detail caps (§2.2 wording correction + config).**
The scan payload contains per-host AAAA results for up to **4 NS hosts** (sorted alphabetically) and **5 MX hosts** (sorted by preference), not "all hosts" — plus the `total`, `checked`, and `ipv6_count` counters the checks already emit (dns_ns_ipv6.go, dns_mx_ipv6.go), which let the detail page render "checked 4 of 7". Replace §2.2's "all-NS detail kept in scan payload" / "all-hosts detail stays visible" accordingly. Expose the caps as config with current behavior as default:

```yaml
checks:
  max_ns_lookups: 4   # per-host AAAA detail cap for dns_ns_ipv6
  max_mx_lookups: 5   # per-host AAAA detail cap for dns_mx_ipv6
```

**F. Check-set enumeration rule.**
The check set is defined by enumeration, not by count. The authoritative list is
v6audit runner.go's **15** registered checkers — `dns_aaaa_base`, `dns_aaaa_www`,
`dns_ns`, `dns_mx`, `dnssec`, `http`, `https`, `tls`, `response_parity`, `resource`,
`smtp`, `spf`, `dns_ptr`, `latency_v4`, `latency_v6` — as adapted per this section
(base/www wrapped into composite observations per §2.3.1; scoring deleted). Any prose
stating a numeric count of checks must be derived from that enumeration at writing
time; never carry a stale count forward.

---

## 3. Data-source plan (Tranco-only, replacing `tldbwriter`)

All Tranco mechanics below were **verified live** (2026-07-06) against
tranco-list.eu, not just docs.

**Trigger ownership.** The `crawler` coordinator goroutine is the **sole scheduled
trigger** — no systemd timer is deployed for Tranco import. `v6ctl tranco import`
invokes the identical code path (shared `ingest/` package) for manual/break-glass
runs; §6's description of v6ctl as "cron targets" does not apply to this verb.

**Coordinator import cycle.** At `tranco.import_at` (default **23:15 UTC**, after the
daily list is generated 22:00–23:00 UTC) the coordinator starts a new import cycle. A
cycle ends when a new list is successfully imported OR the next cycle starts (retry
state resets); max ~11 attempts/day. **Reschedule** = re-attempt after
`tranco.retry_interval` (default 2h) unless the next 23:15 cycle starts first; every
non-success attempt outcome (lock busy, network/HTTP error, unchanged list,
aborted-list short-circuit, sanity-guard abort) reschedules.

Every import execution — scheduled or v6ctl — is serialized by the `JobTrancoImport`
advisory lock (§2.6 lock registry), acquired **before any download/parse** and
released after the upsert transaction commits or on any exit. Lock not acquired →
another import is running: the coordinator logs at INFO and treats the attempt as
done (reschedules); v6ctl uses the blocking `Run` variant with the 5-minute wait per
§2.6.

**Staleness alert:** on every attempt, if `now() - (SELECT max(imported_at) FROM
tranco_import WHERE aborted = false)` exceeds `tranco.stale_warn_after` (default
48h), send an ops-webhook WARNING ("no new Tranco list for `<N>`h; ranks frozen at
list `<list_id>`"), rate-limited to once per 24h. Warning, not page: the
unchanged-list_id short-circuit means staleness freezes ranks — it never delists.
The retry loop and the staleness check need no new DDL: `tranco_import.imported_at`
(§4.9) plus the `aborted`/`note` columns already carry everything.

**Import attempt** (steps of one execution):

1. `GET https://tranco-list.eu/top-1m-id` → plain-text list ID (e.g. `94VW2`).
   If it equals `list_id` of the most recent `tranco_import` row with
   `aborted = false` → no new list yet; done for this attempt (reschedule). If it
   equals `list_id` of any `tranco_import` row with `aborted = true` → do **not**
   auto-reimport an aborted list (it would abort again and spam the webhook);
   reschedule. Operator override: `v6ctl tranco import --force` (which also bypasses
   the step-4 sanity guard — it does **not** bypass the advisory lock). Network/HTTP
   error → reschedule.
2. `GET https://tranco-list.eu/top-1m.csv.zip` with **conditional GET**
   (`If-None-Match`; the endpoint serves strong ETag + Last-Modified and honors 304 —
   verified). This is the **standard list = pay-level domains** (its config is
   `filterPLD: "on"` — the eTLD+1 requirement is the default artifact, no variant
   selection needed). One inner file, always named `top-1m.csv`.
3. Parse: `rank,domain` CSV, **CRLF line endings**, no header. Each line's host goes
   through the single `Canonicalize(host)` rule (host canonicalization, end of §3) —
   this replaces any ad-hoc "lowercase + reject" logic. The live list contains
   `_wildcard_.ph`-style entries and mixed-case junk (though it is largely already
   punycode: 1,452 `xn--` entries, pure ASCII); lines failing Canonicalize are
   counted in `tranco_import.rejected_count`, logged at debug, and skipped.
4. Upsert in one transaction (staging table + set-based SQL, not 1M row-by-row).
   **Staging dedup first:** canonicalization can fold two raw lines into one host,
   and a naive `ON CONFLICT DO UPDATE` fed duplicate hosts aborts the entire import
   transaction (SQLSTATE 21000, "ON CONFLICT DO UPDATE command cannot affect row a
   second time") — so the insert's source SELECT dedupes with `DISTINCT ON (host)`,
   **lowest rank wins**. Rows present in today's list (re-entry semantics, §4.8):

   ```sql
   -- staging(rank int, host text): host already canonicalized in Go,
   -- garbage lines dropped (step 3)
   INSERT INTO domain (host, rank, next_check_at, created_by)
   SELECT DISTINCT ON (host) host, rank, /* spread over next 24h */, 'tranco'
   FROM staging
   ORDER BY host, rank ASC              -- MIN(rank) wins the fold
   ON CONFLICT (host) DO UPDATE SET
     rank = excluded.rank,
     orphaned_at = NULL,
     disabled    = CASE WHEN domain.disabled_reason = 'delisted' THEN false ELSE domain.disabled END,
     disabled_reason = CASE WHEN domain.disabled_reason = 'delisted' THEN NULL ELSE domain.disabled_reason END,
     disabled_at = CASE WHEN domain.disabled_reason = 'delisted' THEN NULL ELSE domain.disabled_at END,
     next_check_at = CASE WHEN domain.disabled_reason IN ('delisted','dead') THEN now() ELSE domain.next_check_at END,
     updated_at  = now();
   ```

   Semantics: **delisted** → re-enabled directly (confirmed state was never reset, it
   is merely ≤30d stale; the immediate rescan refreshes it — no changelog
   implications beyond real transitions). **dead** → stays disabled but
   `next_check_at = now()`, so the next claim runs a real scan and recovery goes
   through §4.3 step R only if the domain actually resolves — re-listing alone never
   resurrects a dead domain. **service/manual** → rank updated, remains disabled and
   out of the frontier. New domains get `next_check_at` **spread across the next
   24h** (production's `InitSpaceTimestamps` idea, kept — prevents a thundering
   herd); domains present yesterday but absent today → `rank = NULL` (delisting
   lifecycle §4.8) — the delisting UPDATE is unaffected by the staging dedup. Record
   provenance in `tranco_import` per run — the list ID is Tranco's requested
   citation unit — including the counters `line_count` (raw CSV lines),
   `rejected_count` (failed Canonicalize/validation), `duplicate_count`
   (`COUNT(*) - COUNT(DISTINCT host)` over staging), and `imported_count` (rows in
   the insert). A `duplicate_count > 0` run is **normal, not an error**.

   **Import sanity guard:** before applying rank changes, compute `valid_rows`
   (post-rejection) and `would_delist` (ranked yesterday, absent today). Abort the
   import when `valid_rows < tranco.min_rows` OR `would_delist >
   tranco.max_delist_pct%` of currently-ranked rows, unless
   `v6ctl tranco import --force`. On abort: keep yesterday's ranks untouched, write a
   `tranco_import` row with `aborted = true` and a reason in `note`, and fire the ops
   webhook. Config (crawler section, defaults are the doc's own numbers):

   ```yaml
   tranco:
     min_rows: 950000          # abort import below this many valid rows
     max_delist_pct: 2.0       # abort if more than this % of ranked rows would delist
     import_at: "23:15"        # UTC; daily cycle start
     retry_interval: 2h        # re-attempt spacing within a cycle
     stale_warn_after: 48h     # ops-webhook warning when no successful import for this long
   ```
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

### Host canonicalization (single rule, all ingresses)

**Invariant.** `domain.host`, `resource_host.host`, and every hostname compared
against them exist in exactly one form: lowercase punycode (ASCII/A-label) FQDN, no
trailing dot, ≤253 octets, ≥2 labels. This form is both the storage form and the API
serving form; Unicode (U-label) display conversion is a frontend concern, out of
scope for this round.

**Function.** One implementation in the backend repo, importable by `api`, `crawler`,
and `v6ctl`:

```go
// internal/domain/host.go
// Canonicalize returns the canonical form of a hostname:
// lowercase punycode FQDN, no trailing dot. It is the ONLY
// path by which a hostname may reach a DB write or DB lookup.
func Canonicalize(raw string) (string, error)

var ErrInvalidHost = errors.New("invalid host") // all failures wrap this
```

Algorithm (in order):

1. `s := strings.TrimSpace(raw)`; strip **exactly one** trailing `.` if present
   (`"dnb.no."` → `"dnb.no"`; `"dnb.no.."` keeps one dot and fails step 4 on the
   empty label).
2. Reject (`ErrInvalidHost`) if `s == ""` or contains any of `/ \ : @ ? # [ ]` or
   whitespace — callers must pass bare hostnames, never URLs.
3. `s = strings.ToLower(s)`.
4. `ascii, err := idna.Lookup.ToASCII(s)` (`golang.org/x/net/idna`, IDNA2008 lookup
   profile with UTS46 mapping). This converts Unicode → punycode and enforces strict
   LDH: rejects `_` (kills `_wildcard_.ph`), empty labels, disallowed characters, and
   bad hyphen placement. Any error → `ErrInvalidHost`.
5. Explicit post-checks (do not rely on profile internals): total length ≤253 octets;
   ≥2 labels; each label 1–63 octets; `net.ParseIP(ascii) == nil` (rejects IPv4
   literals; bracketed IPv6 already died in step 2).
6. Return `ascii`.

Unit-test vectors (must pass): `DNB.no.`→`dnb.no`; `møre.no`→`xn--mre-qla.no`;
`XN--MRE-QLA.no`→`xn--mre-qla.no`; reject: `_wildcard_.ph`, `a..b`, `1.2.3.4`,
`[::1]`, `localhost` (1 label), 254-octet input, `http://x.no`.

**Mandated call sites and failure policy:**

| Ingress | When | On Canonicalize failure |
|---|---|---|
| Tranco import (§3 step 3) | per CSV line, replaces the ad-hoc "lowercase + reject" prose | count in `tranco_import.rejected_count`, log at debug, continue |
| Campaign PR validation (§7 step 1) | per YAML domain entry | CI check fails with the offending line |
| Campaign sync (§7.3 step 1) | per YAML domain entry, **before** entity lookup/creation and membership diff | entry skipped, counted under `rejected + reasons` in the sync report |
| POST /check (§5.3) | body domain — **this is §5.3 step 1**: Canonicalize() first, then the POST /check-only policy layer (reject RFC 2606 TLDs, `.internal`, `.local`) | 400 `{"error":"invalid_host"}` per §5.3 |
| Resource discovery (§4.6) | the host inserted into `resource_host` is defined as Canonicalize() output | host skipped (not inserted), no error surfaced |
| `v6ctl` verbs taking a hostname (`domain add`, `shame add`, etc.) | on argument parse | command errors with the reason |
| API path params (§5.1 addendum) | per request | 404 — **exception:** `GET /badge/{domain}.svg` returns 400 per §5.2a |

**Cross-references.** The §5.3 live-check ingress and the §4.6 `resource_host`
inserts delegate to this definition — no contradiction; §5.3's reserved-TLD list
stays a POST /check-only policy layer on top of Canonicalize(). §4.2's comment
`-- lowercase punycode FQDN` now has an enforcing function; no CHECK constraint is
added (application-enforced, single write path per table). The Tranco staging dedup
(step 4 above) exists because Canonicalize can fold two raw lines into one host.

## 4. Proposed database schema

Base: the v2-team `001_schema.up.sql`/`002_timescaledb.up.sql` (its split write model,
enum status type, no-FK hypertables, partial indexes and trigram search are the crown
jewels), reworked for: the domain-entity model, confirmed status, the resource model,
current TimescaleDB 2.28 columnstore API, and one correction from research — **do not
`segmentby = domain_id`** (v2 did): at 1 row/domain/day a segment holds 1–7 rows and
compression collapses; the documented fix is `orderby = 'domain_id, ts DESC'` with no
(or coarse) segmentby, which co-locates each domain's rows and still gives min/max
sparse-index pruning for per-domain queries. segmentby is deliberately unset on every
columnstore table. Because `timescaledb.orderby` is set explicitly on each of them,
TimescaleDB >= 2.20 does NOT auto-select a default segmentby (PR #8033); no
`segmentby = ''` override is required.

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

-- Raw observation outcomes; internal only. 'partial', 'error' and 'inconsistent'
-- never reach public output (classification and the API read only the confirmed
-- ipv6_status columns, which remain 4-valued).
CREATE TYPE observation AS ENUM
  ('supported', 'partial', 'unsupported', 'no_record', 'not_applicable',
   'error', 'inconsistent');

CREATE TYPE domain_kind     AS ENUM ('apex', 'subdomain');
CREATE TYPE created_by      AS ENUM ('tranco', 'campaign', 'parent_link', 'live_check',
                                     'import');  -- 'import': phase-4 history import only (§8)
CREATE TYPE classification  AS ENUM ('unknown', 'inactive', 'sinner', 'partial', 'hero');
CREATE TYPE disabled_reason AS ENUM ('dead', 'service', 'manual', 'delisted');
CREATE TYPE resource_source AS ENUM ('discovered', 'manual');
CREATE TYPE check_job_status AS ENUM ('pending', 'processing', 'done', 'failed');
```

### 4.2 `domain` — entity + confirmed state + frontier (one table)

```sql
CREATE TABLE domain (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  host          TEXT NOT NULL UNIQUE,          -- lowercase punycode FQDN (Canonicalize, §3)
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

  asn_id      INT NOT NULL REFERENCES asn(id),     -- sentinel row when unknown (§4.9);
  country_id  INT NOT NULL REFERENCES country(id), --   no serializer ever handles NULL

  disabled        BOOLEAN NOT NULL DEFAULT FALSE,
  disabled_reason disabled_reason,
  disabled_at     TIMESTAMPTZ,

  -- Lifecycle bookkeeping (§4.8)
  dead_streak       SMALLINT NOT NULL DEFAULT 0, -- consecutive unresolvable scans (dead lifecycle)
  orphaned_at       TIMESTAMPTZ,                  -- when linkage was lost; starts the 30d delist grace
  last_requested_at TIMESTAMPTZ,                  -- last POST /check for this host (live-check linkage)

  -- Frontier / scheduling
  next_check_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  claimed_at      TIMESTAMPTZ,                 -- worker lease; reclaim after 30 min
  last_checked_at TIMESTAMPTZ,
  last_counted_at TIMESTAMPTZ,                 -- last scan that advanced anti-flap counters (§4.3)
  error_streak    SMALLINT NOT NULL DEFAULT 0, -- consecutive non-definitive base/www scans (backoff)

  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_domain_host_trgm ON domain USING gin (host gin_trgm_ops); -- search
-- Claim-path index: leading range column = next_check_at, so the claim scans
-- ONLY the due set. Predicate must textually match the claim query (§2.5).
CREATE INDEX idx_domain_due ON domain (next_check_at)
  WHERE NOT disabled OR disabled_reason IN ('dead', 'delisted');
CREATE INDEX idx_domain_rank      ON domain (rank) WHERE rank IS NOT NULL;
CREATE INDEX idx_domain_sinners ON domain (rank)
  WHERE classification = 'sinner' AND rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_heroes  ON domain (rank)
  WHERE classification = 'hero'   AND rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_partial ON domain (rank)
  WHERE classification = 'partial' AND rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_parent    ON domain (parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX idx_domain_country ON domain (country_id, classification, rank)
  WHERE rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_asn       ON domain (asn_id);

-- Table storage settings: the claim/commit cycle updates every active row ≥2×/day —
-- claimed_at at claim, next_check_at + status columns at commit; the commit update
-- is always non-HOT because next_check_at is indexed.
ALTER TABLE domain SET (
  fillfactor = 90,
  autovacuum_vacuum_scale_factor = 0.02,
  autovacuum_analyze_scale_factor = 0.02
);
```

The classification/country index predicates match the §5.1 publicly-ranked
predicate; queries must spell out `AND rank IS NOT NULL AND NOT disabled` verbatim
so the planner's predicate-implication check is trivial.

Do NOT create any composite `(rank, next_check_at)` index — ranked list endpoints are
served by `idx_domain_rank` and the per-classification partial indexes, and a
rank-led index usable by the claim query would hand the planner back the pathological
plan (rank-ordered full-index walk with `next_check_at <= now()` as a per-tuple
filter). Invariant for future schema work: no full (non-partial) index with leading
column `rank` may be added without re-running the Phase-2 claim-plan gate (§8).

Design points:

- **One wide table, not entity/status/frontier splits.** 1M rows × ~40 columns is
  small; every hot list query (`sinners by rank`, `heroes by rank`, country drill-down)
  hits exactly one table + the classification partial indexes. *Rejected — a 1:1
  `domain_status` side table:* saves nothing at this row count and adds a join to every
  endpoint. *Rejected — normalized `(domain_id, dimension, status…)` rows:* elegant for
  the commit algorithm, but turns every list row into a 6-way pivot; the wide-column
  layout is what sqlc/typed Go wants anyway.
- **Frontier eligibility is materialized, not computed at claim time.** The claim
  query reads only `disabled`/`disabled_reason`/`next_check_at`/`claimed_at`. Linkage
  (rank, campaign membership, children, recent live-check) is evaluated once per day
  by the lifecycle sweep (§2.6 step 1), which sets `orphaned_at` / `disabled`
  accordingly. Delisted, orphaned entities stop being scanned via
  `disabled='delisted'` after the grace period (§4.8) without being deleted.
- **Entity rules (brief §6):** Tranco contributes only `kind='apex'` rows. Campaign
  import stores each YAML entry **as given**; if PSL says it's a subdomain
  (`publicsuffix-go`, needed only at import time — the crawler's NS zone walk needs no
  PSL), it auto-ensures the registrable parent (`created_by='parent_link'`, rank NULL)
  and sets `parent_id`. Classification is per-entity; children never change a parent's
  tier. The detail API lists children with their own statuses (§5.2).

### 4.3 The confirmed-status commit (the trust machine)

Per scanned domain the worker builds ONE commit unit (worker-side, after
`Runner.Run`). Inputs:

- `L` — the lease value: `claimed_at` as stamped by the claim query (§2.5). One
  `UPDATE ... SET claimed_at = now()` stamps every row in a batch with the same
  value, so L is a single per-batch token held by the worker.
- `T` — the commit timestamp, `time.Now()` fixed once per domain and used for the
  scan row, scan_detail row, changelog rows, `*_since`, and `last_checked_at`.
- The domain's state columns as returned by the claim query (the claim's RETURNING
  list includes all `d_status`/`d_pending`/`d_pending_count` groups); the lease
  fence guarantees this snapshot is still authoritative at commit time.

**Dead-signal computation** (lifecycle, §4.8): the worker holds the raw engine
results, so it computes, per scan, before the per-dimension loop — a scan is
**unresolvable** when either:

- (a) apex A and AAAA both NXDOMAIN (quorum majority of resolver rcodes) AND the NS
  zone walk found no delegated zone for the host, or
- (b) all 3 consensus resolvers returned an explicit SERVFAIL or REFUSED rcode for
  apex AAAA after retry (timeouts do NOT count — three timeouts more likely indicate
  our own network trouble).

(Branch (a) requires the NXDOMAIN rcode, not merely base = `no_record` —
NOERROR-with-no-records is a live but inactive zone and must NOT count. This is
§4.8's "both A and AAAA absent AND NS walk finds no zone" made precise. The rule
applies to all rows regardless of kind; for subdomains whose parent zone exists the
NS walk finds a zone, so they become `inactive`, never `dead` — as intended.)

```
# All state computed client-side from the claimed snapshot.
# All equality comparisons use IS DISTINCT FROM semantics: NULL never equals anything.

counting = (last_counted_at IS NULL) OR (T - last_counted_at >= min_confirm_spacing)  # default 12h

# 1. Lifecycle: dead detection & recovery (§4.8)
if unresolvable:
    dead_streak = LEAST(dead_streak + 1, lifecycle.dead_streak)
else:
    dead_streak = 0
    if base observation is definitive AND disabled AND disabled_reason = 'dead':
        re-enable + reset (step R below), then continue as a fresh domain   # RECOVERY

# 2. Per-dimension confirm/pending loop
for each core dimension d in {base, www, ns, mx, conn, resources}:
  O = observation(d)                       # quorum already applied for base/www
  d_observed = O
  if O in {error, inconsistent}: continue  # non-definitive: touch nothing
                                           #   (status, pending, since all survive)
  if not counting: continue                # record-only scan: pending/status/changelog untouched
  if d_status IS NULL:                     # bootstrap: first definitive observation
      d_status = O; d_since = T            #   commits immediately, NO changelog row
      d_pending = NULL; d_pending_count = 0
  elif O == d_status:                      # steady state
      d_pending = NULL; d_pending_count = 0
  elif O == d_pending:
      d_pending_count += 1
      if d_pending_count >= N(d):          # N=2 dns dims, N=3 conn/resources
          changelog_rows += (domain_id, T, field=d, old=d_status, new=O)
          d_status = O; d_since = T; d_pending = NULL; d_pending_count = 0
  else:
      d_pending = O; d_pending_count = 1

if counting and at least one core dimension was definitive:
    last_counted_at = T

recompute classification + class_flags + gold from confirmed d_status values (truth table below)

# 3. Dead trigger (§4.8)
if NOT disabled AND dead_streak >= lifecycle.dead_streak:
    disabled = true; disabled_reason = 'dead'; disabled_at = T
    dead_streak = 0; error_streak = 0                # reset both streaks

# 4. Scheduling: next_check_at per the §2.5 rules — slow lane if still/newly
#    disabled; recheck pull-in + error_streak backoff if base/www non-definitive;
#    else cadence(rank).

# One pgx.Tx per domain; all statements queued as one pgx.Batch (single round trip):
BEGIN
  UPDATE domain SET <all state + lifecycle cols>, classification, class_flags, gold,
         next_check_at = <per §2.5 scheduling rules>, last_checked_at = T,
         claimed_at = NULL, updated_at = now()
   WHERE id = $domain_id AND claimed_at = $L;          -- LEASE FENCE
  INSERT INTO changelog (domain_id, ts, field, old_value, new_value) VALUES ... ;  -- 0..6 rows, ts = T
  INSERT INTO scan (..., ts) VALUES (..., T)        ON CONFLICT (domain_id, ts) DO NOTHING;
  INSERT INTO scan_detail (domain_id, ts, details, duration_ms) VALUES ($id, T, $json, $ms)
                                                    ON CONFLICT (domain_id, ts) DO NOTHING;
if RowsAffected(domain UPDATE) == 0: ROLLBACK        -- lease lost: another worker reclaimed
else: COMMIT                                          --   this domain; write NOTHING
```

**Fence semantics:** a worker that stalls past the 30-minute lease and resumes after
a reclaim finds `claimed_at` changed (or NULL), the fenced UPDATE matches 0 rows, and
the whole transaction — scan row, scan_detail, changelog, state — is discarded.
Reclaims happen ≥30 min after the original claim, so two lease values can never
collide. Count fence aborts in `crawler_metrics` (add a `lease_lost` counter to
`dim_counters` or a dedicated column). This is the mechanism behind phase 2(d)'s
"no double changelog" verification.

**Counting gate:** non-counting scans still write the scan + scan_detail rows and
update `*_observed`, informational dimensions, latency, and scheduling —
pending/status/changelog stay untouched. The first-ever definitive scan always counts
(`last_counted_at` IS NULL), so new domains still get a status after one scan. The
§2.3 guarantee, restated: a confirmed flip requires N definitive observations of the
new value on scans spaced ≥ `min_confirm_spacing` apart — at daily cadence the
advertised +1/+2 days, and never faster than (N−1) × 12h even when fast-lane rechecks
run every 2h.

**Step R — re-enable + reset** ("resets state to unknown", §4.8), executed before
applying this scan's observations:

- clear `disabled`, `disabled_reason`, `disabled_at`; `dead_streak = 0`.
- for every core dimension d: `d_status = NULL, d_observed = NULL, d_pending = NULL,
  d_pending_count = 0, d_since = NULL`.
- informational columns (`dnssec_observed`, `ptr_observed`, `smtp_observed`,
  `parity_observed`, `latency_v4_ms`, `latency_v6_ms`) → NULL.
- `classification = 'unknown'`, `class_flags = '{}'`, `gold = false`. Keep
  `asn_id`/`country_id` (refreshed by the scan anyway).
- NO changelog rows are written for the reset itself, and none for the first
  post-reset commits either: the current scan's observations then flow through the
  normal algorithm above against NULL confirmed values, so the first definitive value
  commits immediately with no changelog row (first-confirmation rule below). A domain
  returning from the dead reappears with a fresh status and a clean changelog.

While a row is disabled (`dead`/`delisted`) its slow-lane scans still commit through
the normal §4.3 machinery (confirmed state stays maintainable); public exposure is
handled purely by query filters (§4.8).

**First-confirmation rule (normative):** the NULL→value bootstrap transition NEVER
writes a changelog row — suppression happens at write time, not at read time.
Consequently `changelog.old_value` is never NULL on native rows (§4.4; nullable only
for `field='legacy'` import rows) and the §5.1 changelog endpoints apply no
first-confirmation filter. Phase-4 seed import writes production's current statuses
directly into the `d_status` columns (with `d_since` from production data where
available, else import time; `d_pending = NULL`, `d_pending_count = 0`) — seeded
values ARE confirmed values, so the anti-flap N-consecutive rule governs the first
post-cutover divergence, and a real divergence publishes an ordinary changelog entry
once confirmed.

Classification ladder (brief §5.5, deterministic, first match wins), evaluated over
**confirmed** values only. **The value sets enumerated in each rule are exhaustive**: a
dimension satisfies a rule only if its confirmed value is explicitly listed.
`not_applicable` and NULL confirmed values never *shame* a domain (they never trigger
`sinner` and never set a sub-reason flag), but they also never *satisfy* the hero bar
unless the rule lists `not_applicable` (it does for `www` and `mx`, deliberately not
for `ns` and `conn`). Consequence: a domain whose `conn` is NULL (persistent errors)
or confirmed `not_applicable` (transition window) is `partial` with **no** flag — hero
requires demonstrated IPv6-only reachability, and `broken_v6` requires demonstrated
failure; neither may be assumed.

**Classification truth table (normative).** Inputs are confirmed values; each ∈
{`supported`, `unsupported`, `no_record`, `not_applicable`, NULL}. First match wins:

| # | Condition (confirmed values) | classification |
|---|---|---|
| 1 | `base` = NULL | `unknown` |
| 2 | `base` = `no_record` | `inactive` |
| 3 | `base` = `unsupported` | `sinner` |
| 4 | `base` = `supported` AND `www` ∈ {`supported`, `not_applicable`, `no_record`} AND `ns` = `supported` AND `conn` = `supported` AND `mx` ∈ {`supported`, `not_applicable`} | `hero` |
| 5 | `base` = `supported` (hero bar not met) | `partial` |

(`base` = `not_applicable` is unreachable: the apex AAAA check always yields a concrete status or a non-definitive error.)

Flags (computed for every domain; only ever true when the named dimension is confirmed `unsupported` — NULL, `not_applicable`, and `no_record` set no flag):

| Flag | Condition |
|---|---|
| `broken_v6` | `conn` = `unsupported` |
| `www_missing` | `www` = `unsupported` |
| `ns_missing` | `ns` = `unsupported` |
| `mail_missing` | `mx` = `unsupported` |
| `resources_v4only` | `resources` = `unsupported` |

`gold` = classification `hero` AND `resources` ∈ {`supported`, `not_applicable`} (NULL resources → not gold).

Notes: (a) A `partial` domain may legitimately carry zero flags — that is the "hero bar unverified" state (conn/ns NULL or `not_applicable`), which is transient by construction: definitive first-scan observations commit immediately (§4.3), and the base-N=2 / conn-N=3 asymmetry bounds any confirmed `conn=not_applicable`-with-`base=supported` overlap to the transition window. Do not invent an extra flag for it. (b) `ns` = `not_applicable` is unreachable by construction (the NS walk-up always reaches an authoritative zone); if it ever occurs it blocks hero and sets no flag, per the table.

Note www: www NXDOMAIN and www with neither A nor AAAA both map to `not_applicable`
(§2.3.1) — a site without a working www can be a Hero (**OPEN-2: decided**). `www`
never produces `no_record`; only `base` can, and confirmed `base = no_record` is what
feeds the `inactive` tier and the §4.8 lifecycles.

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
-- No extra (domain_id, ts DESC) index: the primary key (domain_id, ts) already
-- serves backward per-domain scans; an additional index is pure write/storage overhead.

-- Fat scan payload: full engine Details JSONB (per-check evidence, record sets,
-- TLS cert info, resource host lists). Short retention; debugging + detail page.
CREATE TABLE scan_detail (
  domain_id BIGINT NOT NULL,
  ts        TIMESTAMPTZ NOT NULL,
  details   JSONB NOT NULL,
  duration_ms INT,
  PRIMARY KEY (domain_id, ts)
);
-- No result_id column: idempotency is the worker-fixed timestamp T +
-- ON CONFLICT (domain_id, ts) DO NOTHING under the PK (§4.3); no unique constraint
-- on a synthetic id is possible on a hypertable and nothing consumes it.
SELECT create_hypertable('scan_detail', by_range('ts', INTERVAL '1 day'));
ALTER TABLE scan_detail SET (timescaledb.enable_columnstore,
                             timescaledb.orderby = 'domain_id, ts DESC');
CALL add_columnstore_policy('scan_detail', after => INTERVAL '3 days');
SELECT add_retention_policy('scan_detail', drop_after => INTERVAL '90 days');

-- Structured field-level changelog. FOREVER — the credibility surface.
CREATE TABLE changelog (
  domain_id BIGINT NOT NULL,
  ts        TIMESTAMPTZ NOT NULL DEFAULT now(),
  field     TEXT NOT NULL,                     -- base|www|ns|mx|conn|resources|legacy
  old_value ipv6_status,                       -- never NULL on native rows: first
                                               --   confirmation writes no row at all (§4.3);
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
-- field='legacy' is the import escape hatch for unmappable production rows
-- (§8 phase 4); native (post-cutover) rows never set the legacy columns.
SELECT create_hypertable('changelog', by_range('ts', INTERVAL '30 days'));
ALTER TABLE changelog SET (timescaledb.enable_columnstore,
                           timescaledb.orderby = 'ts DESC, domain_id');
CALL add_columnstore_policy('changelog', after => INTERVAL '60 days');
-- no retention policy: kept forever (~1-3k rows/day)
CREATE INDEX idx_changelog_ts ON changelog (ts DESC);  -- global recent-changes feed
-- (The PK leads with domain_id and cannot serve the sitewide GET /changelog feed.)

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

A fifth hypertable, `unbound_stats` (Unbound resolver metrics, Grafana-only), lands
in the same migration-002 hypertable pass as `crawler_metrics`; its DDL and
collection mechanism are specified with the rest of the ops surface in §11.3.

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

`tls` and `spf` deliberately have NO typed columns anywhere: they are
informational-only (§2.2) and live exclusively in `scan_detail.details` JSONB. The
detail page reads the latest scan_detail row, which for any actively scanned domain
is always far younger than the 90-day scan_detail retention. This is accepted
design, not an omission.

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
  host TEXT NOT NULL UNIQUE,                   -- lowercase punycode (Canonicalize, §3)
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

**Discovery (per domain scan, phase 2, inside the §4.3 commit transaction):**
1. `resource_discovery` returns (hosts, status). If status = `error`: keep existing links untouched (a failed fetch is not evidence dependencies changed) and skip to the roll-up. If `not_applicable`: skip to the roll-up.
2. If `ok`: for each host — canonicalize via `Canonicalize(host)` (§3; failure → host skipped, not inserted, no error surfaced), then `INSERT INTO resource_host (host) VALUES (canonical) ON CONFLICT (host) DO NOTHING` (new rows get `aaaa_status NULL`, `next_check_at = now()`, so the sweep confirms them within one day); upsert `domain_resource (source='discovered', required=TRUE)` or refresh `last_seen = now()` (never downgrade `source='manual'`).
3. Prune this domain's links where `source='discovered' AND last_seen < now() - INTERVAL '30 days'`.
4. `dependent_count` is maintained in the same statements: +1 on link insert, −1 on link delete.

**Sweep worker (dedicated, daily):** claims batches `WHERE next_check_at <= now() AND dependent_count > 0`. Per host, one AAAA lookup via the **bulk resolver**, mapped: ≥1 globally-routable AAAA → `supported`; NOERROR empty → `unsupported`; NXDOMAIN → `no_record`; timeout/SERVFAIL → non-definitive. Commit mirrors §4.3: non-definitive touches nothing and sets `next_check_at = now() + 2h`; a definitive value passes the host confirmation machine — **first-ever definitive value commits immediately** (aaaa_status NULL → value), thereafter N=2 consecutive sweeps to change `aaaa_status` — and sets `next_check_at = now() + 24h`. Hosts never write changelog rows (changelog is domain-scoped).

**Roll-up (per domain scan, produces the NOT NULL `scan.resources` observation consumed by §4.3):**
```
if conn_observation in {error, inconsistent}:  resources_obs = error          # defer with conn
elif conn_observation != supported:            resources_obs = not_applicable # v6-unreachable site: deps moot
else:
  hosts = confirmed aaaa_status of linked domain_resource rows WHERE required = TRUE
  hosts = hosts minus those with status in {no_record, not_applicable}        # dead references are not
                                                                              #   evidence of v4-only dependence
  if any remaining status IS NULL:             resources_obs = error          # host not yet swept: defer,
                                                                              #   never advances pending
  elif hosts is empty:                         resources_obs = not_applicable # no (live) external deps
  elif any status = unsupported:               resources_obs = unsupported
  else:                                        resources_obs = supported
```
The observation then enters §4.3's commit machinery unchanged (N=3). **Deliberate double hysteresis:** host N=2 stacked under domain N=3 gives a worst-case ~5 days for a gold transition; this is intentional — the domain level also absorbs link-set churn (rotating ad/CDN hosts across fetches), which host-level confirmation cannot. `error` never advances `resources_pending`, so the NULL window on discovery day costs one deferred day, not a false transition.

**Phasing + config:** config key `crawler.resources.enabled` (bool, default `false`; flipped to `true` at phase-5 deploy). While `false`, the crawler: skips `resource_discovery` entirely, writes `resources = not_applicable` to every `scan` row (satisfying the NOT NULL column), and **excludes the resources dimension from the §4.3 commit loop** — domain `resources_status/pending/pending_count/since/observed` stay NULL. Consequently `gold = hero AND resources ∈ {supported, not_applicable}` evaluates false for all domains until phase 5, which is correct: no gold badges before the feature ships. At phase-5 flip, resources confirm via the normal first-observation rule (one clean scan after the first sweep pass).

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
  unchanged — locked by brief §5.5); `resources_v4only` becomes a partial-tier flag for
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
  conn_supported INT, resources_supported INT,
  top_heroes INT,        -- Tranco top-1000 with web-facing IPv6 (see formula)
  top_nameserver INT     -- Tranco top-1000 with IPv6-capable nameservers
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

**Stats visibility scope (composes with the §5.1 publicly-ranked predicate):**

- `stats_global_daily`, `stats_country_daily`, `stats_asn_daily`: every column is
  computed over `rank IS NOT NULL AND NOT disabled`, EXCEPT
  `stats_global_daily.disabled`, which counts `rank IS NOT NULL AND disabled`
  (visibility into how much of the ranked set is suppressed).
- `stats_campaign_daily`: scoped by campaign membership `AND NOT disabled`; rank is
  irrelevant (campaign members are typically unranked). The nightly job writes rows
  only for `NOT disabled` campaigns; historical rows for disabled campaigns are
  retained untouched — on re-enable the series resumes with a gap (frontends
  already tolerate missing days).
- Ported `update_country_metrics` / `update_asn_metrics` counter recomputes (§2.6
  step 3): same `rank IS NOT NULL AND NOT disabled` scope, so `/country` and
  `/metric/asn` figures match the lists exactly.
- `scan_daily_adoption` cagg is measurement-flavored (Grafana/research) and stays
  unfiltered over all scans; DICTIONARY.md must state that it counts observations
  over all scanned entities and is NOT comparable to the product stats.
- Datasets (§5.4): `top100k`/`top1m` use the publicly-ranked predicate; `full`
  includes all non-disabled scannable entities (any kind/origin).

**Top-1k columns.** `top_heroes` and `top_nameserver` are computed by the nightly
snapshot job over the same population as every other `stats_global_daily` counter
(the visibility scope above; `rank <= 1000` implies rank IS NOT NULL, so unranked
campaign domains and subdomains are excluded):

```sql
count(*) FILTER (WHERE rank <= 1000
                   AND base_status = 'supported'
                   AND www_status IS DISTINCT FROM 'unsupported') AS top_heroes,
count(*) FILTER (WHERE rank <= 1000
                   AND ns_status  = 'supported')                  AS top_nameserver
```

Semantics (part of the OPEN-6 "methodology v2" note as two more deliberate,
announced metric fixes):
- `top_heroes` = Tranco top-1000 domains whose website is reachable over IPv6:
  confirmed base = `supported`, and www does not *contradict* it. Per OPEN-2,
  www `not_applicable`, `no_record`, and NULL (never confirmed) never count
  against — only confirmed www = `unsupported` excludes. This is NOT the §4.3
  hero classification (no ns/conn/mx requirement): the metric measures
  web-facing IPv6 only, matching the production metric's intent and the
  frontend copy, which reports nameserver readiness as a separate number.
- `top_nameserver` = Tranco top-1000 domains with confirmed ns = `supported`.
- Two production quirks are fixed, not reproduced:
  (1) `rank < 1000` → `rank <= 1000` (production counted 999 domains; all
      frontend copy says "top 1000");
  (2) `base != 'unsupported'` → `base = 'supported'` (production counted
      `no_record`/inactive domains as IPv6-enabled, inflating the number).

Global/country/campaign tables are a few hundred rows/day — plain tables, no
hypertable ceremony. Per-domain graphs come straight from `scan` (already slim,
served backward by its PK `(domain_id, ts)`, 2y depth).

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
-- Stable policy API only (timescaledb_experimental.add_policies is early-access,
-- SELECT-invoked, and has no schedule-interval parameter — do not use it):
SELECT add_continuous_aggregate_policy('scan_daily_adoption',
  start_offset      => INTERVAL '3 days',
  end_offset        => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 hour');
ALTER MATERIALIZED VIEW scan_daily_adoption
  SET (timescaledb.enable_columnstore,
       timescaledb.orderby = 'day DESC, country_id');
CALL add_columnstore_policy('scan_daily_adoption', after => INTERVAL '90 days');
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
| `dead` | Crawler (§4.3): `dead_streak >= lifecycle.dead_streak` (default 7) **unresolvable scans**. A scan is unresolvable when either (a) apex A and AAAA both NXDOMAIN and the NS walk finds no delegated zone, or (b) all 3 consensus resolvers returned an explicit SERVFAIL or REFUSED rcode for apex AAAA after retry (timeouts do NOT count — three timeouts more likely indicate our own network trouble). `dead_streak` increments on an unresolvable scan, resets to 0 otherwise; at the trigger, `disabled=true, disabled_reason='dead', next_check_at=now()+30d`, both streaks reset. NXDOMAIN domains ride daily cadence → dead in 7 days; SERVFAIL domains ride the §2.5 backoff → dead in ~2.3 weeks (6+12+24+48+96+192+384h). Matches brief §6 ("dead (NXDOMAIN/SERVFAIL)"); production disabled on a single TXT SERVFAIL, which is how transient failures became permanent deletions | **Auto:** slow-lane revalidation — `next_check_at` set to +30d instead of exclusion; a successful resolution re-enables and resets state to `unknown` (§4.3 step R) |
| `delisted` | Tranco import: rank became NULL and no campaign/children/live-check linkage; 30-day grace | Auto: regains rank or campaign membership |
| `service` | Operator confirms a detection candidate, or `service_domains.yml` import (`v6ctl disable --service-list`) | Manual only |
| `manual` | `v6ctl disable <host> --reason=...` | Manual only |

Disabled domains are excluded from **all public list/detail-list/stats/changelog
queries** — every public query joins or filters `NOT disabled` (changelog endpoints
join `changelog → domain` and filter there; rows written during a disabled period
simply become visible again on re-enable, and for `dead` there are none because of
the §4.3 step-R reset). `dead`/`delisted` stay in the frontier on the slow lane
(`next_check_at = now() + lifecycle.slow_lane_every`), `service`/`manual` leave it
entirely.

**Disable semantics.** Setting `disabled = TRUE` does NOT modify `classification`,
`class_flags`, `gold`, or any confirmed status/`*_since` column — history and state
are preserved; public exclusion is achieved solely by the `NOT disabled` filter
(the §5.1 publicly-ranked predicate). The one state reset remains the existing rule
above: when a `dead` domain resolves again during slow-lane revalidation,
re-enabling resets classification to `'unknown'` (statuses re-confirm from
scratch). Live-check submissions never bypass any of this: they are `rank NULL`
rows and therefore structurally invisible to lists and stats regardless of
classification.

Lifecycle config keys (crawler config, with defaults — all values are the doc's own
numbers):

```yaml
lifecycle:
  dead_streak: 7            # consecutive unresolvable scans before disabled_reason='dead'
  slow_lane_every: 720h     # revalidation cadence for disabled dead/delisted rows (30d)
  delist_grace: 720h        # orphaned_at age before rank-NULL rows are disabled (30d)
  live_check_linkage: 168h  # frontier lifetime granted by a POST /check (7d)
```

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
-- On-demand live checks (§5.3). v2's table + the consumer it never had,
-- hardened with the same claimed_at lease as the domain frontier (§2.5).
CREATE TABLE check_job (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  host         TEXT NOT NULL,                 -- validated, lowercase punycode
  requester_ip INET NOT NULL,
  status       check_job_status NOT NULL DEFAULT 'pending',
  claimed_at   TIMESTAMPTZ,                   -- consumer lease; reclaim after 5 min
  result       JSONB,                         -- shared-mapper output (§5.3)
  error        TEXT,                          -- set when status = 'failed'
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ
);
CREATE INDEX ON check_job (created_at) WHERE status IN ('pending','processing'); -- claim + reaper
CREATE INDEX ON check_job (requester_ip, created_at);                            -- rate limiting
CREATE INDEX ON check_job (host, completed_at DESC) WHERE status = 'done';       -- host-side dedupe

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
  line_count INT, imported_count INT, delisted INT,    -- §3 provenance counters
  rejected_count INT, duplicate_count INT,             --   (duplicate_count > 0 is normal)
  aborted BOOLEAN NOT NULL DEFAULT FALSE,       -- §3 import sanity guard
  note    TEXT,                                 -- abort reason
  imported_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- One successful import per list; abort rows may repeat (a --force retry of an
-- aborted list must still be able to record its success). Target of the §2.6
-- idempotent-write guard's ON CONFLICT (list_id) WHERE NOT aborted DO NOTHING.
CREATE UNIQUE INDEX idx_tranco_import_list ON tranco_import (list_id) WHERE NOT aborted;
```

Search: the trigram GIN index on `domain.host` serves substring search (escape `%`/`_`
in input — v2 forgot); ASN search = ILIKE over ~80k rows (fine). *Rejected — FTS/
tsvector:* domains aren't prose; trigram is the right tool and production's
table-scanning `LIKE '%x%'` (`db/query/domain.sql:65`) dies here.

**GeoIP attribution.**

**Library and files.** MaxMind GeoLite2-ASN + GeoLite2-Country mmdb, read with the
official reader `github.com/oschwald/geoip2-golang/v2` (has `Close()`, netip-based;
readers are safe for concurrent use). Filenames are fixed: `GeoLite2-ASN.mmdb`,
`GeoLite2-Country.mmdb`, both in the directory given by config key `GEOIP_PATH`
(uppercase-env viper convention, same as `API_LISTEN`), default `/var/lib/GeoIP`.
Only the **crawler binary** opens them (attribution is a scan-commit concern; the
API never does GeoIP lookups). The crawler **fails fast at startup** if either file
is missing or unreadable — Phase 1 gates on GeoIP wiring.

**Attribution input IP** (replaces production `resolver.IPLookup`, which is rejected
with the rest of that resolver). Computed from the scan's own §2.3.1 base-composite
answers for the apex — no extra DNS queries:

1. If the base AAAA quorum observation is `exists`: input IP = the first
   globally-routable AAAA in the recorded answer set (the designated resolver's
   answers per §2.8's fixed order, after the bogon filter). AAAA always wins over A.
2. Else if the conditional bulk A lookup ran and returned `a_present`: input IP =
   the first A address in that answer.
3. Else (nxdomain / empty+a_absent / a_error): no input IP.

Address order within the RRset is "as returned"; cross-scan determinism is not
required (production reads Answer[0] of a round-robin RRset; attribution self-heals
every scan).

**ASN attribution** (`domain.asn_id`), production parity with geoip.go
`getNetworkProvider`:
- GeoLite2-ASN lookup of the input IP → (AS number, organization name).
- AS number ≠ 0: find `asn` row by `number`; if absent, `INSERT (number, name)`
  (`ON CONFLICT (number) DO NOTHING`, then re-read). New ASNs are auto-registered
  exactly as production does; existing names are **not** updated on later scans.
- No input IP, lookup miss, or AS number 0 → the sentinel ASN row (see Seeds).

**Country attribution** (`domain.country_id`), production parity with `getCountryID`
under OPEN-5 (ccTLD wins over server location), PSL-correct per the doc:
1. **ccTLD**: take the final label of the host's ICANN public suffix
   (`golang.org/x/net/publicsuffix`; equivalently the host's final DNS label —
   PSL replaces production's `[a-z]{2,}$` regex, which fails on IDN/punycode TLDs).
   Match it against `country.tld` (seed stores production's dot-prefixed uppercase
   form, e.g. `.NO`; normalize the probe to `"." + strings.ToUpper(label)`).
   A match wins unconditionally — no GeoIP lookup is made.
2. **GeoIP fallback**: GeoLite2-Country ISO code of the input IP, matched against
   `country.code`.
3. **Sentinel** otherwise (no input IP, lookup miss, or unmapped code).

**Timing.**
- Recomputed inside every §4.3 scan-commit transaction, for every scanned entity
  (ranked, campaign, subdomain) — matches production's per-crawl recompute and §4.3
  step R ("refreshed by the scan anyway").
- Deferred scans (base observation `error`/`inconsistent` — no commit) do **not**
  touch attribution: a transient resolver failure must not flip a domain to
  'Unknown'. (Deliberate improvement over production, which degraded to Unknown on
  any IPLookup timeout.)
- Run once more at **entity insert** (Tranco import, campaign sync, live-check row
  creation) with no input IP — yielding ccTLD-or-sentinel country and sentinel ASN —
  so the columns are never NULL (`asn_id INT NOT NULL REFERENCES asn(id)`,
  `country_id INT NOT NULL REFERENCES country(id)`, §4.2). No serializer ever
  handles NULL asn/country.
- Live checks never touch attribution on existing rows (§5.3 Rule 0 unchanged).

**Seeds** (migration ordering: sentinels land with the asn/country seed data,
before any domain row). Carry production's `02_data.up.sql` country
list + tld mappings forward, including the sentinels — production ids were
hardcoded (asn 1, country 251); the new schema uses IDENTITY, so the crawler
resolves sentinel ids **once at startup by lookup**, never by literal id:
- `asn`: `(number 0, name 'Unknown')`
- `country`: `(code 'UN', name 'Unknown', tld '.UN')`
Both appear in `/metric/asn` and `/country` listings exactly as they do today
(production parity; the frontend already renders them).

**mmdb reload.** The crawler stats the two mmdb files hourly; on mtime change it
opens new readers, swaps them via `atomic.Pointer`, and `Close()`s the old ones.
Startup and each swap log the databases' build epochs (slog, `geoip.build_epoch`).
A plain systemd restart after update is an acceptable operational substitute; the
mtime swap just avoids interrupting long crawl runs.

(**OPEN-5: decided** — keep ccTLD precedence.)

## 5. API surface

### 5.0 HTTP server baseline (applies to every endpoint in §5)

**Listen address.** Config key `API_LISTEN`, default `[::1]:8080` (viper env, same
uppercase-env convention as production's `app.env`). The API binds IPv6 loopback
**by design**: it is always fronted by nginx, which terminates TLS and is the only
process that can reach it. Keep this default and document it in the README
(brief Appendix: "Old API bound [::1]:PORT — keep intentional but document").
Override to `:8080` / `0.0.0.0:8080` only for docker-compose/dev.

**Real client IP.** Because the bind is loopback-only, every request arrives from
nginx and the peer address is useless. Apply a RealIP middleware (chi
`middleware.RealIP` or equivalent) first in the chain: set the request's remote
address from `X-Real-IP` if present, else the first entry of `X-Forwarded-For`,
else leave the peer address. This derived address is the **single source of truth**
for (a) the `GET /ip` response body and (b) `check_job.requester_ip` in the §5.3
rate limiter. Operator caveat (state in README): trusting these headers is safe
only because the default bind is unreachable except via the local proxy; if
`API_LISTEN` is opened to a non-loopback interface without a trusted proxy,
per-IP rate limits become spoofable.

Required nginx location config (document in deploy notes):

```nginx
proxy_set_header X-Real-IP        $remote_addr;
proxy_set_header X-Forwarded-For  $proxy_add_x_forwarded_for;
proxy_set_header Host             $host;
```

**CORS.** The frontend is cross-origin (whynoipv6.com → api.whynoipv6.com).
Middleware config = production's rs/cors settings plus `POST` (needed by
`POST /check`; production allowed only GET/HEAD/OPTIONS):

- AllowedOrigins: `https://*`, `http://*` (allow-all; API is public and anonymous)
- AllowedMethods: `GET`, `HEAD`, `OPTIONS`, `POST`
- AllowedHeaders: `Accept`, `Authorization`, `Content-Type`, `X-CSRF-Token`
- ExposedHeaders: `Link`
- AllowCredentials: `false`
- MaxAge: `300`

**Default headers (all responses):** `Content-Type: application/json` (default;
overridden by `/badge/{domain}.svg` → `image/svg+xml` and static datasets),
`X-Content-Type-Options: nosniff`, `X-Frame-Options: deny` (both as production).

**Cache-Control by endpoint class:**

| Class | Header |
|---|---|
| All JSON API endpoints (§5.1, §5.2, §5.3) | `Cache-Control: no-cache, no-store, no-transform, must-revalidate, private, max-age=0` (chi `middleware.NoCache`, as production) |
| `GET /badge/{domain}.svg` | `Cache-Control: public, max-age=3600` (status changes at most daily; 1h keeps README badges fresh enough) |
| `GET /datasets` (manifest) | `Cache-Control: public, max-age=300` |
| Dataset files | served statically by nginx (§5.4), not by the API |

**Server timeouts & shutdown** (production had none — §5.1 cleanup made concrete):
`http.Server{ReadHeaderTimeout: 5s, ReadTimeout: 10s, WriteTimeout: 30s,
IdleTimeout: 120s}`; per-request `middleware.Timeout(30s)`. Graceful shutdown on
SIGINT/SIGTERM: `server.Shutdown(ctx)` with a 15s drain budget. (`POST /check` is
async job+poll per §5.3, so no handler legitimately exceeds 30s.)

**Middleware order (outermost first):** RealIP → RequestID → slog request logger →
Recoverer → Timeout(30s) → CORS → security/content-type headers → per-route
Cache-Control.

**Parity tests (extend §8 phase 4 golden tests):**
1. `GET /ip` with `X-Real-IP: 2001:db8::7` returns `{"ip":"2001:db8::7"}` — not
   `::1` (guards the Notification.vue `ip.includes(":")` IPv4-banner check and the
   §5.3 per-IP bucket).
2. `OPTIONS /check` preflight with `Origin: https://whynoipv6.com` and
   `Access-Control-Request-Method: POST` returns 2xx with
   `Access-Control-Allow-Origin` and `POST` in `Access-Control-Allow-Methods`.
3. Two `POST /check` requests with different `X-Real-IP` values consume different
   rate-limit buckets.

### 5.1 Compatibility contract (must-keep, verified against both `whynoipv6/internal/rest/` and the actual frontend calls in `whynoipv6-web/src/services/`)

Ground rules extracted from code: paths mount at the **API root** (no `/api/v1`
prefix); `/metric` is **singular**; pagination is `?offset=` (+ `?limit=`, default 50
max 100 — the frontend only ever sends `offset` and hard-assumes page size 50 in its
Next-button logic); UUIDs in URLs are **shortuuid-encoded**; two endpoints wrap in a
`{"data": [...]}` envelope; campaign detail returns a `{campaign, domains}` composite;
status strings are exactly `supported|unsupported|no_record`.

| Endpoint | Behavior (new backend) |
|---|---|
| `GET /domain?offset=` | Sinner list (`classification='sinner'`) by rank. **Membership narrows vs production:** old query was `base_domain='unsupported' OR www_domain='unsupported'` (domain.sql ListDomain); new membership is base-unsupported only — domains with base supported but www unsupported leave this list and surface on `/domain/almost` as `partial`/`www_missing`. Response shape unchanged. This is a **deliberate, announced** break — OPEN-6: decided (methodology-v2 note) |
| `GET /domain/heroes` | `classification='hero'` by rank. Hero bar is now the §4.3 truth table (old query was `mx != 'unsupported'`; the changed membership is a **deliberate, announced** break — OPEN-6: decided) |
| `GET /domain/topsinner` | `top_shame` join, still curated. Fix: return real Tranco `rank` (old code returned domain *id* as `rank`). Query pinned in the visibility-scope addendum below (`classification='sinner'` auto-hide + publicly-ranked predicate) |
| `GET /domain/{domain}` | Detail from confirmed columns. Keep field names incl. `asn` = AS **name** string, `country` = name, all `ts_*` keys (full mapping: legacy-serialization rule R3 below). `v6_only` now real: the `conn` status (production served a dead column). **404 for disabled entities** (production parity: ViewDomain read the disabled=FALSE view) |
| `GET /domain/{domain}/log` | Last 90 `scan` rows mapped to `{id,time,base_domain,www_domain,nameserver,mx_record}` (id = synthetic, frontend uses it as list key only) |
| `GET /domain/search/{q}` | Trigram search, **`{"data":[...]}` envelope kept** |
| `GET /country`, `/country/{code}`, `/country/{code}/sinners`, `/country/{code}/heroes` | Same shapes; percent from `NUMERIC(5,2)` cleanly; sinners ordered by rank (old: by id — minor fix). `/country/{code}/sinners` membership narrows identically (production used the same OR-predicate, country.sql ListDomainsByCountry) to `classification='sinner'` — same deliberate OPEN-6 break as `/domain` |
| `GET /changelog`, `/changelog/campaign`, `/changelog/{domain}`, `/changelog/campaign/{uuid}`, `/changelog/campaign/{uuid}/{domain}` | From structured changelog; `message` + `ipv6_status` **generated at the API layer** from `(field, old, new)` via the ported `generateChangelog` ladder (`crawl.go:416-495`) — same strings, single implementation. `domain_url` rules kept (incl. empty-string cases). v2-team dropped three of these routes; they're restored — the frontend calls all five. Full ladder, per-endpoint scope, and synthetic-id rules: legacy-changelog addendum below |
| `GET /campaign` | List + `count`, `v6_ready`; `WHERE NOT campaign.disabled`, `ORDER BY campaign.id` (production parity). `count` counts only `NOT disabled` member domains; `v6_ready` uses the amended R4 formula (`base='supported' AND ns='supported' AND www IN ('supported','not_applicable')` — legacy-serialization rule R4 below, an OPEN-6 announced amendment) over the same member set. Full row shape: disabled-campaign addendum below |
| `GET /campaign/{uuid}?offset=` | `{campaign, domains}` composite; domain rows are the shared entity's status now |
| `GET /campaign/{uuid}/{domain}`, `.../log`, `GET /campaign/search/{q}` | Kept, incl. envelope + `campaign_uuid` field in search rows |
| `GET /metric/overview` | Array-of-one `{time, data:{domains, base_domain, www_domain, nameserver, mx_record, heroes, top_heroes, top_nameserver}}` mapped from `stats_global_daily` latest row — fully pinned in the metric-overview addendum below |
| `GET /metric/asn?order=`, `GET /metric/asn/search/{q}` | Kept; `count_v4` = `count_total - count_v6` computed server-side as today |
| `GET /ip` | `{"ip":"<remote addr>"}` — the frontend's IPv4-banner calls `api.whynoipv6.com/ip` **hardcoded**; today this must be served by nginx or lost code — the new API serves it natively |
| `GET /` | health `{"message":"ok"}` |

Two contract cleanups worth making (frontend verified tolerant): empty lists return
`[]` instead of JSON `null` (frontend does `response.data \|\| []`; the cleanup
rewrites bodies only, never status codes — see the zero-result addendum below), and
proper graceful-shutdown/timeouts on the server (production has neither, now §5.0).
Everything else stays bug-compatible until the frontend modernization round.

*Rejected — versioned `/v2` API now:* the frontend is frozen; a v2 surface with
cleaned-up shapes belongs to the frontend round. The OpenAPI spec documents the legacy
quirks explicitly so they're contained.

#### §5.1 addendum — public visibility scope

**Definition — "publicly ranked" predicate:**

```sql
rank IS NOT NULL AND NOT disabled
```

Invariant: only the Tranco importer ever writes `rank`, and Tranco is eTLD+1, so
`rank IS NOT NULL` implies `kind = 'apex'`. `created_by` is irrelevant to visibility:
a campaign-created apex that later enters Tranco gains a rank and becomes publicly
ranked — that is correct and intended.

**Endpoint scope rules (§5.1 / §5.2).** The following endpoints select ONLY rows
matching the publicly-ranked predicate, in addition to their classification filter:

- `GET /domain` (sinners), `GET /domain/heroes`, `GET /domain/almost`
- `GET /domain/topsinner` (the `top_shame` join additionally requires the predicate)
- `GET /country/{code}/sinners`, `GET /country/{code}/heroes`
- `GET /domain/search/{q}` (production parity: GetDomainsByName searched
  domain_view_list, i.e. ranked+enabled only; campaign matches already reach the
  Search page via `GET /campaign/search/{q}`, which is scoped by campaign
  membership and `NOT disabled`, rank irrelevant)

Ordering everywhere: `ORDER BY rank ASC` (no NULLS handling needed — NULLs are
excluded by the predicate).

Entity/detail endpoints are NOT rank-scoped: `GET /domain/{domain}`,
`/domain/{domain}/log`, `/domain/{domain}/subdomains`, `/domain/{domain}/resources`,
`/stats/domain/{domain}`, and the campaign detail/log endpoints serve any entity
regardless of rank (this is how campaign domains, subdomains, and live-check hosts
are viewed). `GET /domain/{domain}` returns **404 for disabled entities**
(production parity: ViewDomain read the disabled=FALSE view); the embedded and
paginated `subdomains` listings exclude disabled children.

Stats scoping lives in §4.7 (stats visibility scope); index predicates match in
§4.2; disable semantics (state never cleared) in §4.8.

**`GET /domain/topsinner` — query pin:**

```sql
SELECT d.*
FROM top_shame ts
JOIN domain d ON d.id = ts.domain_id
WHERE d.classification = 'sinner'          -- production parity: domain_shame_view
                                           --   filtered base_domain = 'unsupported';
                                           --   a shamed domain that ships IPv6
                                           --   auto-hides, its row persists
  AND d.rank IS NOT NULL AND NOT d.disabled  -- publicly-ranked predicate
ORDER BY d.rank ASC;                        -- deviation already declared in §5.1:
                                            -- real rank replaces production's id
```

No pagination (production returned the whole filtered list; it is ≤ a dozen rows).

#### §5.1 addendum — legacy serialization rules (normative, part of openapi.yaml + parity fixtures)

**R1. Status projection (all legacy endpoints).** Every field carrying an `ipv6_status`
(`base_domain`, `www_domain`, `nameserver`, `mx_record`, `v6_only` in domain/campaign
detail and list rows, campaign-domain composite rows, changelog `ipv6_status`, and log
rows) is serialized through one shared function:

```go
// legacyStatus projects the 4-value public enum + NULL onto the frozen
// 3-string wire contract. not_applicable and never-confirmed both render
// as "no_record" (frontend shows the amber "no record" marker).
func legacyStatus(s *ipv6_status) string {
    switch {
    case s == nil:                 return "no_record" // never confirmed
    case *s == NotApplicable:      return "no_record"
    default:                       return string(*s)  // supported|unsupported|no_record
    }
}
```
No legacy endpoint may ever emit `not_applicable`, `error`, `inconsistent`, `unknown`,
or empty string. New endpoints (§5.2) are exempt and serve the real 4-value enum.

**R2. `GET /domain/{domain}/log`.** Source: last 90 `scan` rows by `ts DESC` **after
filtering out non-definitive rows** — a documented exception that *enforces* §2.2
("error/inconsistent never become public") by exclusion, not remapping:

```sql
SELECT ts, base, www, ns, mx FROM scan
WHERE domain_id = $1
  AND base NOT IN ('error','inconsistent') AND www NOT IN ('error','inconsistent')
  AND ns   NOT IN ('error','inconsistent') AND mx  NOT IN ('error','inconsistent')
ORDER BY ts DESC LIMIT 90;
```
Per-field values then pass through R1 (`not_applicable` → `"no_record"`).
Response row: `{"id": <extract(epoch from ts)::bigint>, "time": <ts RFC3339>,
"base_domain": ..., "www_domain": ..., "nameserver": ..., "mx_record": ...}`.
`id` is synthetic (frontend list key only); epoch seconds is stable across requests.
Same rules apply to `GET /campaign/{uuid}/{domain}/log`.

**R3. Timestamp key mapping (`GET /domain/{domain}` and campaign detail).**

| JSON key     | Source column          |
|--------------|------------------------|
| `ts_aaaa`    | `domain.base_since`    |
| `ts_www`     | `domain.www_since`     |
| `ts_ns`      | `domain.ns_since`      |
| `ts_mx`      | `domain.mx_since`      |
| `ts_curl`    | `domain.conn_since`    |
| `ts_check`   | `domain.last_checked_at` |
| `ts_updated` | `domain.updated_at`    |

NULL source columns serialize as the Go zero time `"0001-01-01T00:00:00Z"`
(bug-compatible with production's nullable-timestamp encoding; the frontend already
tolerates it). No fallback substitution (do NOT substitute last_checked_at for a
NULL `<d>_since`).

**R4. `v6_ready` (amended formula — announced under the OPEN-6 methodology-v2 note).**
For `GET /campaign` list counts, the `{campaign}` object in the composite, and
§4.7 `stats_campaign_daily.v6_ready`:

```sql
v6_ready := base_status = 'supported'
        AND ns_status   = 'supported'
        AND www_status IN ('supported', 'not_applicable')
```
Rationale (record in doc): subdomain entities force `www = not_applicable` (§4.2);
production's strict `www = 'supported'` test would permanently pin subdomain-heavy
campaigns at 0%, violating §4.3's "not_applicable never counts against" and the
OPEN-2 decision. NULL (unconfirmed) www does NOT count as ready. `mx`/`conn` remain
excluded from v6_ready, as in production.

**R5. Legacy changelog collapse.** Changelog rows whose `(old_value, new_value)` map
to the same string under R1 (e.g. `not_applicable` → `no_record`) are omitted from
all five legacy `/changelog*` endpoints; the `generateChangelog` message ladder is fed
the R1-projected values, so it only ever sees the 3 production strings.

**Parity-test note.** Golden fixtures captured from production cannot exercise R1's
not_applicable/NULL branches or R2's filter (production never produces those values);
add synthetic fixtures for them alongside the recorded ones, keyed to this addendum.

#### §5.1 addendum — legacy changelog endpoints under the unified model

**A. Canonical message ladder (single implementation, API layer).**

`renderChangelog(field, old, new, host) -> (message, ipv6_status)` is defined ONLY for
`field IN ('base','www','ns','mx')` and `old, new IN ('supported','unsupported','no_record')`, `old IS NOT NULL`.
`ipv6_status` in the response is always `new_value` (production stored the new value of the changed field).
Exact strings (verbatim from production `crawl.go:416-495`; the campaign ladder in `campaign_crawl.go` is string-identical, so one function serves all five endpoints). `{h}` = entity host; for `field='www'` the rendered name is `www.{h}`:

| field | old → new | message |
|---|---|---|
| base | unsupported→supported OR no_record→supported | `IPv6 enabled for {h}` |
| base | supported→unsupported | `IPv6 lost for {h}` |
| base | no_record→unsupported | `IPv4-only for {h}` |
| base | any→no_record | `No DNS records found for {h}` |
| www | unsupported→supported OR no_record→supported | `IPv6 enabled for www.{h}` |
| www | supported→unsupported | `IPv6 lost for www.{h}` |
| www | no_record→unsupported | `IPv4-only for www.{h}` |
| www | any→no_record | `No DNS records found for www.{h}` |
| ns | unsupported→supported OR no_record→supported | `IPv6 enabled nameserver for {h}` |
| ns | supported→unsupported | `Nameservers degraded to IPv4-only for {h}` |
| ns | no_record→unsupported | `IPv4-only nameservers for {h}` |
| ns | any→no_record | `No NS records found for {h}` |
| mx | unsupported→supported OR no_record→supported | `IPv6 enabled MX records for {h}` |
| mx | supported→unsupported | `MX records degraded to IPv4-only for {h}` |
| mx | no_record→unsupported | `IPv4-only MX records for {h}` |
| mx | any→no_record | `No Mail records found for {h}` |

**Coverage rule:** all five legacy `/changelog*` endpoints apply this SQL filter, so the ladder is total over what they serve:

```sql
WHERE c.old_value IS NOT NULL
  AND c.field IN ('base','www','ns','mx')
  AND c.old_value IN ('supported','unsupported','no_record')
  AND c.new_value IN ('supported','unsupported','no_record')
```

`conn`/`resources` rows and any transition involving `not_applicable` ARE written to the `changelog` table (§4.3/§4.4 unchanged — they remain queryable, appear in datasets, and are available to the future v2 API) but are NOT served by the legacy endpoints. Rationale: production never emitted them, the frontend is frozen, and exposing them later is purely additive. `field='legacy'` rows (see the §4.4 escape hatch) bypass the ladder: `message = legacy_message`, `ipv6_status = legacy_status`.

**B. Feed scope and domain_url (per endpoint).**

All feeds: `ORDER BY c.ts DESC, c.domain_id DESC, c.field ASC`; `?offset=`/`?limit=` (default 50, max 100). Filter from A applies everywhere.

| Endpoint | Scope | domain_url |
|---|---|---|
| `GET /changelog` | `JOIN domain d ON … WHERE d.rank IS NOT NULL` (Tranco apexes only — reproduces production's implicitly-Tranco feed; campaign-only, live_check, and subdomain entities excluded) | `"/domain/{host}"` |
| `GET /changelog/campaign` | `JOIN campaign_domain cd ON cd.domain_id = c.domain_id JOIN campaign ON …` — all campaigns, rank irrelevant. A domain in N campaigns yields N rows per change (production duplicated these rows per campaign too — accepted) | `"/campaign/{shortuuid(campaign.uuid)}/{host}"` |
| `GET /changelog/{domain}` | entity resolved by host, any kind/rank; 404 if unknown host or zero rows (production behavior — the []-cleanup never changes status codes; see the zero-result addendum) | `""` (field present, empty string — production struct has no omitempty) |
| `GET /changelog/campaign/{uuid}` | membership join filtered to the decoded campaign; 404 on zero rows (see the zero-result addendum) | `"/campaign/{shortuuid}/{host}"` |
| `GET /changelog/campaign/{uuid}/{domain}` | membership check + host; 404 on zero rows | `""` |

Response row (unchanged from production): `{id, ts, domain, domain_url, message, ipv6_status}`.

**C. `id`.**

No identity column is added to the `changelog` hypertable. `id` is synthetic: **epoch milliseconds of `ts`** (int64) — same precedent as `GET /domain/{domain}/log`. The frontend keys rows by array index and never dereferences `id`; collisions are harmless. Pagination stability comes from the deterministic ORDER BY above.

#### §5.1 addendum — zero-result behavior (status codes are never changed by the []-cleanup)

**Rule.** The "empty lists return `[]` instead of JSON `null`" cleanup applies only to responses production already served as **HTTP 200 with a `null` body** (a serialized nil slice). It never changes a status code. Every zero-result **404** production emits is kept bug-compatibly: same status, byte-identical error JSON.

**Kept 404s on zero results** (exact production bodies; content-type application/json):

| Endpoint | Fires when | Response |
|---|---|---|
| `GET /domain/search/{q}` | trigram search matches 0 publicly-ranked domains (visibility predicate) | `404 {"error":"no domains found"}` |
| `GET /campaign/search/{q}` | 0 campaign-domain matches | `404 {"error":"No domains found"}` (note the capital N — differs from domain search) |
| `GET /campaign/{uuid}?offset=` | member-domain page is empty: unknown campaign **or** `offset >= member count` (paging past the last page 404s — bug-compatible, frontend tolerates) | `404 {"error":"Campaign not found"}` |
| `GET /campaign/{uuid}/{domain}` | single resource not found (membership or host miss) | `404 {"error":"Domain not found"}` |
| `GET /changelog/{domain}` | zero changelog rows | `404 {"error":"No changelog entries found for {domain}"}` |
| `GET /changelog/campaign/{uuid}` | zero changelog rows for the campaign — production's third zero-rows check (changelog.go:229) | `404 {"error":"No changelog entries found for campaign {uuid}"}` where `{uuid}` is the **decoded canonical 36-char UUID**, not the shortuuid from the URL |
| `GET /changelog/campaign/{uuid}/{domain}` | zero rows | `404 {"error":"No changelog entries found for campaign {uuid} and domain {domain}"}` where `{uuid}` here is the **shortuuid exactly as given in the URL** (production does not decode it for the message) |

Consequence: because both `{"data":[...]}`-enveloped endpoints (the two searches) 404 on zero matches, `{"data":[]}` never occurs on the legacy surface.

**[]-cleanup applies (production returned 200 `null`)** — zero rows → `200 []`:
`GET /domain`, `/domain/heroes`, `/domain/topsinner`, `/domain/{domain}/log`, `/country`, `/country/{code}/sinners`, `/country/{code}/heroes`, `/changelog`, `/changelog/campaign`, `/campaign`, `/campaign/{uuid}/{domain}/log`, `/metric/asn`, and — explicitly — `GET /metric/asn/search/{q}` (production metric.go:132-157 has no zero-rows check; it is the one search endpoint that returns `200 []` on zero matches).

Single-resource endpoints are untouched by this rule and 404 as already pinned (`GET /domain/{domain}` 404 for unknown/disabled).

**Parity tests (extend the golden-fixture plan):** one fixture per row of the 404 table asserting status + exact body (use a garbage query like `zzzzqqqq` for the searches; a valid campaign with `offset=10000` for the paging case), plus `GET /metric/asn/search/zzzzqqqq` → `200 []`.

#### §5.1 addendum — shortuuid codec pin (wire-frozen)

**Codec.** All campaign UUIDs crossing the API boundary are encoded with
`github.com/lithammer/shortuuid/v4` `DefaultEncoder` — base57 alphabet
`23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz`, fixed 22-character
output. Use the latest v4.x release (production runs v4.2.0). MUST be the v4
major: v3 produces different (variable-length) tokens and would 404 every
previously shared campaign link. Do not hand-roll the codec.

Helpers (mirror production `internal/rest/server.go`):

    func encodeUUID(id uuid.UUID) string {           // uuid = github.com/google/uuid
        return shortuuid.DefaultEncoder.Encode(id)   // always 22 chars
    }
    func decodeUUID(s string) (uuid.UUID, error) {
        return shortuuid.DefaultEncoder.Decode(s)
    }

**Surfaces (exhaustive).** The token — never the canonical hyphenated UUID —
appears at:
1. `uuid` field of campaign list/detail responses (`GET /campaign`, `GET /campaign/{uuid}`).
2. `campaign_uuid` field of campaign-changelog entries.
3. `domain_url` in changelog responses: `"/campaign/{token}/{host}"` (see the changelog addendum's table — every `shortuuid(...)` there means this codec).
4. Path params: `GET /campaign/{uuid}`, `GET /campaign/{uuid}/{domain}`, `GET /changelog/campaign/{uuid}`, `GET /changelog/campaign/{uuid}/{domain}`.

The database stores only the canonical `UUID` column (§4 DDL unchanged);
encode/decode happens exclusively in the HTTP layer.

**Decode-failure behavior (production parity).** For any `{uuid}` path param:
- `decodeUUID` returns an error (character outside base57 alphabet, overflow,
  etc.) → `404` body `{"error":"Invalid UUID"}` (byte-exact production body;
  campaign.go:117-122, changelog.go:196-201).
- Token decodes but no matching campaign row → `404` body
  `{"error":"Campaign not found"}` (campaign.go:132-136).
No extra length/shape validation beyond what `DefaultEncoder.Decode` performs —
parity with production is the rule.

**OpenAPI (§5.5).** Document token params/fields as:
`type: string`, `pattern: ^[2-9A-HJ-NP-Za-km-z]{22}$`,
`example: bHTMghm9txZFhwMKVCiBey`.

**Parity fixtures (§8 phase 4).** Round-trip test, both directions; encode
output must be exactly 22 chars. Vectors verified against lithammer/shortuuid/v4
v4.2.0 (first two are live campaign UUIDs from whynoipv6-campaign):

| canonical UUID                          | shortuuid token          |
|-----------------------------------------|--------------------------|
| baff94c3-c4b2-4f19-be66-3247250f7868    | bHTMghm9txZFhwMKVCiBey   |
| 9b587e73-7694-46f7-b3dc-96f6a1c15317    | VeT2mCvhzny4kAiQ9oLe2r   |
| 00000000-0000-0000-0000-000000000000    | 2222222222222222222222   |

Negative fixtures: `GET /campaign/not-a-token` and
`GET /campaign/baff94c3-c4b2-4f19-be66-3247250f7868` (raw UUID in the path is
NOT accepted — `-` is outside the alphabet) both → `404 {"error":"Invalid UUID"}`.

#### §5.1 addendum — GET /metric/overview, fully pinned

Response: JSON array with exactly one element, built from the latest
`stats_global_daily` row (max `day`):

```json
[
  {
    "time": "2026-07-06T00:00:00Z",
    "data": {
      "domains":        <domains>,
      "base_domain":    <base_supported>,
      "www_domain":     <www_supported>,
      "nameserver":     <ns_supported>,
      "mx_record":      <mx_supported>,
      "heroes":         <heroes>,
      "top_heroes":     <top_heroes>,
      "top_nameserver": <top_nameserver>
    }
  }
]
```

- `time` = the row's `day` serialized as an RFC 3339 timestamp at midnight UTC
  (production returned the metrics row's timestamptz via Go `time.Time`; the
  frontend never reads `time`, so midnight-UTC is fine — keep it a timestamp
  string, not a bare date).
- `heroes` maps from the `heroes` column (§4.3 classification count — the
  membership change vs production's `base+www supported` formula is the
  already-decided OPEN-6 break).
- All eight `data` keys are required (frontend types/Metric.ts and
  MetricCrawler.vue read every one); values are plain JSON numbers.
- If `stats_global_daily` is empty (first boot before the first nightly
  snapshot), the snapshot job must be run as part of seed migration so the
  endpoint always has a row — per OPEN-6 "serve migrated seed values
  immediately", the seed migration writes day-0 rows for all stats_* tables.

#### §5.1 addendum — disabled-campaign visibility

**Context.** `campaign.disabled = true` is the soft-delete outcome of deleting a campaign YAML (§7). Production *accidentally* keeps disabled campaigns in the public list with zeroed counts, because `campaign.disabled = FALSE` sits in the LEFT JOIN condition instead of a WHERE clause (`whynoipv6/db/query/campaign.sql` ListCampaign/GetCampaignByUUID). The new backend fixes this — an **announced fix** in the same category as the `[]`-instead-of-null cleanup in §5.1. Disabled campaigns disappear from the public API entirely; their rows, memberships, changelog, and stats history are preserved in the database.

**1. `GET /campaign`.** Add `WHERE NOT campaign.disabled`; order `ORDER BY campaign.id` (production parity). Row shape is production's CampaignListResponse, unchanged:

```json
{ "id": 7, "uuid": "<shortuuid>", "name": "...", "description": "...", "count": 42, "v6_ready": 17 }
```

`id` = campaign.id (int), `uuid` = shortuuid-encoded campaign.uuid, `count` = COUNT of campaign_domain members whose domain row is `NOT disabled` (visibility scoping), `v6_ready` = R4 formula (`base='supported' AND ns='supported' AND www IN ('supported','not_applicable')`) over the same member set. A live campaign with zero members returns `count:0, v6_ready:0` (row kept — only `campaign.disabled` removes it from the list).

**2. UUID-addressed campaign endpoints → 404 when disabled.** One shared resolver: decode shortuuid → `SELECT ... FROM campaign WHERE uuid = $1 AND NOT disabled`; no row → `404`. Applies to:
- `GET /campaign/{uuid}` (composite), `GET /campaign/{uuid}/{domain}`, `GET /campaign/{uuid}/{domain}/log`
- `GET /changelog/campaign/{uuid}`, `GET /changelog/campaign/{uuid}/{domain}`
- `GET /stats/campaign/{uuid}` (§5.2)

The composite's `{campaign}` object (only reachable for non-disabled campaigns) carries the same shape as the list row in item 1.

**3. Cross-campaign endpoints filter disabled campaigns.** `GET /campaign/search/{q}` and `GET /changelog/campaign` join through `campaign` and add `NOT campaign.disabled` (in addition to `NOT domain.disabled` on the domain side).

Related rules living elsewhere: the lifecycle sweep counts only non-disabled
campaigns as linkage (§2.6 step 1a); `stats_campaign_daily` skips disabled
campaigns (§4.7 stats visibility scope); re-add semantics for a restored YAML
are in §7.3 (steps 3 and 5).

#### §5.1 addendum — hostname path-parameter canonicalization

Every path parameter that carries a hostname — `{domain}` in `GET /domain/{domain}`,
`/domain/{domain}/log`, `/campaign/{uuid}/{domain}` and its changelog variants,
`/stats/domain/{domain}`, and `{host}` in `/resource/{host}/dependents` — is passed
through `Canonicalize()` (§3) in a shared handler helper before the DB lookup;
failure → plain **404** (it is a lookup miss, not a client contract violation). For
`GET /badge/{domain}.svg`, strip the `.svg` suffix first; the badge is the declared
exception to the 404 rule — Canonicalize failure there returns **400**
`{"error":"invalid_host"}` per §5.2a. This intentionally supersedes production's
mixed behavior (domain.go:193 and campaign.go:280 lowercase; the changelog handlers
don't): the change is strictly additive (previously-404 mixed-case URLs now resolve)
and is NOT a §5.1 bug-compat quirk — those cover response shapes, not lookup
normalization.

### 5.2 New endpoints

| Endpoint | Purpose |
|---|---|
| `GET /domain/almost?offset=` | Partial tier ("almost there") by rank, with `class_flags` per row |
| `GET /domain/{domain}/subdomains` | Children (entity model) with each child's own status. **Also embedded** in `GET /domain/{domain}` as `subdomains: [...]` capped at 25 with `subdomain_count` — the detail page renders the drill-down without a second request; the sub-resource endpoint exists for pagination past the cap |
| `GET /domain/{domain}/resources` | #23: linked resource hosts with per-host status, source (discovered/manual), required flag |
| `GET /resource/{host}/dependents?offset=` | Reverse dependency lookup ("who depends on this v4-only CDN") |
| `POST /check {"domain": "..."}` / `GET /check/{id}` | Live check (§5.3) |
| `GET /stats/overview`, `/stats/country/{code}`, `/stats/campaign/{uuid}`, `/stats/asn/{number}`, `/stats/domain/{domain}` | Time-series for graphs, from `stats_*` + `scan`; `?from=&to=&interval=daily\|weekly` |
| `GET /badge/{domain}.svg` (optional, cheap) | Status badge for READMEs — advocacy multiplier; flagged optional; normative behavior in §5.2a |
| `GET /datasets` (+ static files) | Dataset manifest (§5.4) |

### 5.2a GET /badge/{domain}.svg — normative behavior

**Route.** chi pattern `GET /badge/{domain}.svg` (chi matches a static suffix after a param in one segment; the `.svg` is part of the route, never part of the `domain` param). `{domain}` is the bare host — no scheme, no port, no trailing dot (strip one trailing dot if present).

**Input handling.** Normalize and validate with the SAME function as POST /check step 1 (§5.3): lowercase, punycode-normalize (IDNA Lookup ToASCII), LDH hostname, ≤253 octets, ≥2 labels; reject IP literals and `.internal`/`.local`/RFC 2606 TLDs. Failure → **400** `{"error":"invalid_host","message":"..."}` (standard JSON error, not an SVG; malformed hosts are not legitimate embeds).

**Lookup.** `SELECT classification, gold, disabled FROM domain WHERE host = $1`. Per the §5.1 visibility scope, entity endpoints are not rank-scoped: any kind/origin (Tranco apex, campaign domain, subdomain, live-check host) resolves. **Read-only, zero side effects**: never inserts a domain row, never enqueues a check_job, never touches `last_requested_at`.

**Badge selection (first match wins):**

| Condition | Message | Message color |
|---|---|---|
| no row, `disabled = TRUE` (any reason), or `classification = 'unknown'` | `unknown` | `#9f9f9f` (gray) |
| `classification = 'hero' AND gold` | `gold` | `#d4af37` |
| `classification = 'hero'` | `supported` | `#4c1` |
| `classification = 'partial'` | `partial` | `#dfb317` |
| `classification = 'sinner'` | `unsupported` | `#e05d44` |
| `classification = 'inactive'` | `inactive` | `#9f9f9f` |

Always **HTTP 200** for a valid host — a 404 renders as a broken image in READMEs. Disabled → gray `unknown` implements the §5.1 public-exclusion rule for this endpoint; it deliberately differs from `GET /domain/{domain}`'s 404-on-disabled, which is production parity and does not bind this new endpoint. Badge copy is public status vocabulary, NOT ladder branding: a README badge never says "sinner"/"hero" (owners won't embed self-shaming badges; the ladder wording stays on the site). The copy/color table is one Go constant table — the single place to reword.

**Rendering.** shields.io flat style, label `IPv6` (label bg `#555`, white text), six precompiled variants of one template — fixed geometry + `textLength`, so output is byte-deterministic with no font measurement and no dependencies:

```svg
<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="20" role="img" aria-label="IPv6: {MSG}"><title>IPv6: {MSG}</title><linearGradient id="s" x2="0" y2="100%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient><clipPath id="r"><rect width="{W}" height="20" rx="3" fill="#fff"/></clipPath><g clip-path="url(#r)"><rect width="37" height="20" fill="#555"/><rect x="37" width="{MW}" height="20" fill="{COLOR}"/><rect width="{W}" height="20" fill="url(#s)"/></g><g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="110" text-rendering="geometricPrecision"><text x="195" y="150" transform="scale(.1)" fill="#010101" fill-opacity=".3" textLength="270">IPv6</text><text x="195" y="140" transform="scale(.1)" textLength="270">IPv6</text><text x="{TX}" y="150" transform="scale(.1)" fill="#010101" fill-opacity=".3" textLength="{TL}">{MSG}</text><text x="{TX}" y="140" transform="scale(.1)" textLength="{TL}">{MSG}</text></g></svg>
```

Geometry table (`W = 37 + MW`, `TX = (37 + MW/2) × 10`, `TL = (MW − 10) × 10`):

| MSG | MW | W | TX | TL |
|---|---|---|---|---|
| gold | 38 | 75 | 560 | 280 |
| supported | 69 | 106 | 715 | 590 |
| partial | 53 | 90 | 635 | 430 |
| unsupported | 81 | 118 | 775 | 710 |
| inactive | 59 | 96 | 665 | 490 |
| unknown | 61 | 98 | 675 | 510 |

**Headers.** Per the §5.0 baseline (already pinned, do not redefine): `Content-Type: image/svg+xml`, `Cache-Control: public, max-age=3600`, global `X-Content-Type-Options: nosniff`. No ETag/rate-limit special-casing — one indexed PK lookup + string template is cheaper than any JSON endpoint.

**Interactions.** Gold before phase 5: `domain.gold` is false for everyone until the resources flip (`crawler.resources.enabled`, §4.6), so pre-phase-5 heroes render `supported` — correct, no special case. Frontend/README usage string for docs: `![IPv6](https://api.whynoipv6.com/badge/example.com.svg)`.

**Tests.** Golden-file test per variant (six SVGs byte-exact), plus: unknown host → 200 gray; disabled host → 200 gray; `xn--`-input and Unicode input normalize to the same badge; `.svg`-less path → 404 (route miss); invalid host → 400 JSON.

**Scope note for §8.** Stays phase 6, priority "ship-when-cheap": nothing else references it, so cutting it needs no spec change.

### 5.3 Live "check any domain"

**Rule 0 — live checks never touch confirmed state.** The live-check consumer runs
the engine and writes its result ONLY to `check_job.result`. It never inserts `scan`
or `scan_detail` rows and never updates any `domain` column except the initial row
insert for unknown hosts (below). Confirmed statuses, pending counters (`*_pending`,
`*_pending_count`), `*_observed`, `last_checked_at`, `next_check_at`,
`classification`, changelog rows, and country/ASN counters advance exclusively via
frontier scans (§4.3). The POST handler's lifecycle re-entry writes (below) —
`last_requested_at = now()` and, for `dead`/`delisted` rows, `next_check_at = now()`
/ re-enable per §4.8 — are allowed alongside the initial row insert: they schedule
frontier work, they never advance confirmed state. Rationale: §2.3's
N-consecutive-scans rule assumes daily cadence; anonymous POSTs must not be able to
accelerate a confirmed transition. `check_job` rows and results are public data;
sequential BIGINT ids are enumerable and that is accepted (no auth, nothing
sensitive).

**POST /check** — processing order:
1. Parse body `{"domain": "<host>"}`. Validate via the single `Canonicalize(host)`
   rule (§3), then apply the POST /check-only policy layer on top: reject IP literals
   and `.internal`/`.local`/RFC 2606 TLDs (`.test`, `.example`, `.invalid`,
   `.localhost`). Failure → **400** `{"error":"invalid_host","message":"..."}`.
   (SSRF is already handled by the engine's pinned dialer; these rejections are the
   API-boundary layer on top.)
2. Rate limit: count `check_job` rows for `requester_ip` with
   `created_at > now()-1h` (limit 10), then global count (limit 500). Exceeded →
   **429** `{"error":"rate_limited","scope":"ip"|"global","retry_after_s":<int>}` +
   `Retry-After` header.
3. **Dedupe, domain-side:** if a `domain` row for the host exists and
   `last_checked_at >= now() - interval '1 hour'`, load its latest `scan_detail`
   row, run the shared result mapper over `details`, and return **200** with a
   synthetic done envelope (below, `id: null`, `cached: true`). No job row is
   created. (`last_checked_at` is written only by frontier commits, so live checks
   never count as "scanned" for this window.)
4. **Dedupe, job-side:** else if a `check_job` for the same host has
   `status='done' AND completed_at >= now() - interval '1 hour'`, return **200**
   with that job's envelope (`cached: true`).
5. Else `INSERT check_job (host, requester_ip) → status 'pending'` and return
   **202** `{"id":123,"host":"...","status":"pending","created_at":"..."}`.

**GET /check/{id}** — **200** envelope or **404** `{"error":"not_found"}`:
```json
{
  "id": 123,
  "host": "example.com",
  "status": "pending|processing|done|failed",
  "cached": false,
  "created_at": "...",
  "completed_at": null,          // set when done|failed
  "error": null,                 // short string when failed
  "result": null,                // object below when done
  "confirmed": null              // object below when a domain row exists
}
```
`result` (produced by the shared mapper; statuses use the raw-observation vocabulary
`supported|unsupported|no_record|not_applicable|error` — plus `inconsistent` for
base/www when quorum split; live results are raw observations, explicitly NOT
confirmed state):
```json
{
  "checked_at": "...", "duration_ms": 4183,
  "checks": {
    "base": {"status": "supported"}, "www": {"status": "supported"},
    "ns": {"status": "supported"},   "mx": {"status": "not_applicable"},
    "conn": {"status": "supported"}, "resources": {"status": "unsupported"},
    "tls": {"status": "supported"},  "smtp": {"status": "not_applicable"},
    "parity": {"status": "supported"}, "dnssec": {"status": "unsupported"},
    "ptr": {"status": "supported"},  "spf": {"status": "supported"}
  },
  "latency": {"v4_ms": 12, "v6_ms": 14}
}
```
`confirmed` (from the `domain` row; `null` if no row or nothing confirmed yet):
`{"classification":"partial","class_flags":["mail_missing"],"gold":false,"statuses":{"base":"supported","www":"supported","ns":"supported","mx":"unsupported","conn":"supported","resources":null},"as_of":"<last_checked_at>"}`.

**Shared result mapper** (one implementation, two inputs):
`MapLiveResult(sr checker.ScanResult) → result JSON`. Applies §2.2's engine→public
dimension mapping exactly (keys are the PUBLIC dimension names, not engine check
names): `base`←dns_aaaa_base, `www`←dns_aaaa_www, `ns`←dns_ns_ipv6
(partial→supported), `mx`←dns_mx_ipv6 (partial→supported), `conn`←https_ipv6 w/
http_ipv6 fallback, `tls`/`smtp` (partial→unsupported)/`parity`/`dnssec`/`ptr`/`spf`
informational, `latency`←latency_ipv4/ipv6. `resources` is NOT engine-mapped (§2.2):
it is the §4.6 roll-up, computed **read-only** over the run's `resource_discovery`
host list against confirmed `resource_host.aaaa_status` (discovery `error` →
`error`, `not_applicable` → `not_applicable`; hosts missing or unswept in the
registry are NULL → `error`, §4.6's defer branch; while
`crawler.resources.enabled=false` → `not_applicable`) — no registry rows are
written on this path, per Rule 0. Because `scan_detail.details` stores the engine
ScanResult serialization (§4.4), the same mapper serves the domain-side dedupe path.
This mapper is also the single mapping used by the frontier worker before §4.3's
commit — one mapping, three consumers.

**Consumer** (dedicated goroutine pool in the **crawler** binary — the v2 `ClaimJob`
pattern, which existed with *no consumer*; here the consumer ships in the same phase
as the endpoint, §8 phase 6. Config `live_check.workers`, default 4; poll every 2s
when idle):
1. Claim one job:
```sql
UPDATE check_job SET status='processing', claimed_at = now()
WHERE id = (
  SELECT id FROM check_job
  WHERE status = 'pending'
     OR (status = 'processing' AND claimed_at < now() - interval '5 minutes')
  ORDER BY created_at
  LIMIT 1 FOR UPDATE SKIP LOCKED
) RETURNING id, host;
```
2. Ensure a `domain` row: `INSERT ... (host, kind, parent_id, rank=NULL,
   created_by='live_check', last_requested_at=now()) ON CONFLICT (host) DO NOTHING`.
   `kind` via the campaign-import PSL helper; `parent_id` set only if the registrable
   parent row ALREADY exists — live checks never auto-ensure parents (a `parent_link`
   row would grant permanent frontier eligibility, letting abuse grow the frontier).
3. Run the full engine with a 60s context budget (panic-recovered, as in `runPhase`).
4. On success: `UPDATE check_job SET status='done', result=$1, completed_at=now()`.
   On error/timeout: `status='failed', error=$2, completed_at=now()`. Nothing else
   is written (Rule 0).

**Reaper** (same goroutine, every 60s) — guarantees every poller terminates ≤15 min:
```sql
UPDATE check_job SET status='failed', error='timed out', completed_at=now()
WHERE status IN ('pending','processing') AND created_at < now() - interval '15 minutes';
```
**Retention** (§2.6 daily tick, step 6):
`DELETE FROM check_job WHERE created_at < now() - interval '30 days';`

**Frontier eligibility for live-check rows** (pins §4.2's "recent live-check"
linkage): eligibility is materialized by the §2.6 lifecycle sweep, never computed at
claim time — the 7-day window keys off `last_requested_at` (set at row insert and
refreshed by every later POST /check, per the lifecycle re-entry rules below),
compared against `lifecycle.live_check_linkage`. `next_check_at` defaults to
`now()`, so the frontier scans the new host promptly and its confirmed snapshot
populates via the normal §4.3 path. Once `last_requested_at` ages past the window,
the sweep delists the row (§2.6 step 1c); a later POST refreshes
`last_requested_at` and re-enables it per the §4.8 re-entry rules below.

**Lifecycle re-entry (§4.8):** every POST /check for an existing host sets
`last_requested_at = now()` — this is the "live-check origin within 7 days" linkage
evaluated by the §2.6 lifecycle sweep, and it also extends the frontier life of any
rank-NULL row a user actively watches. If the row is disabled with reason
`'delisted'` → re-enable (clear `disabled`/`disabled_reason`/`disabled_at`,
`orphaned_at = NULL`) with `next_check_at = now()`. If `'dead'` → leave disabled but
set `next_check_at = now()`: the live check itself never touches confirmed state
(the check-job consumer writes only `check_job.result`), so recovery happens via the
pulled-in frontier scan, which commits through §4.3 and runs step R if the domain
actually resolves. `'service'`/`'manual'` → the live check runs and returns its
result, but never re-enables. New hosts get `created_by = 'live_check'`, `rank NULL`,
`last_requested_at = now()`.

*Rejected — synchronous inline check:* a full engine run can take 60–90s (SMTP/HTTP
timeouts); holding an anonymous HTTP request open that long invites trivial
resource-exhaustion abuse. Job + poll matches ready.chair6.net-class tools.

Live-check config keys (crawler config, with defaults):

```yaml
live_check:
  workers: 4              # concurrent engine slots for check jobs
  job_budget: 60s         # per-job engine deadline
  reclaim_after: 5m       # processing lease reclaim
  fail_after: 15m         # pending/processing -> failed (poller termination bound)
  retention: 720h         # 30d purge, runs in the daily tick
  rate_ip_per_hour: 10    # OPEN-11
  rate_global_per_hour: 500
  dedupe_window: 1h
```

### 5.4 Datasets for researchers

Nightly `v6ctl export` (after the stats tick): writes dated snapshots to a static
directory served by nginx (`/datasets/` on the API origin — pinned in the addendum
below):

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

#### §5.4 addendum — dataset hosting, directory layout, manifest schema (resolves the `data.whynoipv6.com` vs `/datasets/` either/or)

**Decision: datasets live under the `/datasets/` path on the API origin (api.whynoipv6.com).** No separate vhost. Rationale: one cert, one DNS name, one CORS story, one nginx server block — consistent with the rejected-S3 "one nginx directory" rationale and the §5.0 baseline. All file references in the manifest are origin-relative absolute paths (`/datasets/...`), which resolve because manifest and files share an origin.

**On-disk layout** (`DATASETS_DIR`, default `/var/lib/whynoipv6/datasets`):

```
/var/lib/whynoipv6/datasets/
├── manifest.json                      # rewritten atomically after every export
├── DICTIONARY.md                      # column + status-semantics docs (§5.4)
├── latest -> 2026-07-06               # symlink to newest COMPLETE snapshot
├── 2026-07-06/                        # immutable once published
│   ├── whynoipv6-top100k.csv.gz
│   ├── whynoipv6-top100k.parquet
│   ├── whynoipv6-top1m.csv.gz
│   ├── whynoipv6-top1m.parquet
│   ├── whynoipv6-full.csv.gz
│   ├── whynoipv6-full.parquet
│   └── SHA256SUMS                     # sha256sum -c compatible, all 6 files
└── 2026-07-05/ ...
```

File naming: `whynoipv6-{size_tier}.{format}` with `size_tier ∈ {top100k, top1m, full}` and `format ∈ {csv.gz, parquet}`. Public URLs: `https://api.whynoipv6.com/datasets/{YYYY-MM-DD}/whynoipv6-top1m.csv.gz`, `.../datasets/latest/whynoipv6-top1m.csv.gz` (stable URL for scripts), `.../datasets/DICTIONARY.md`. Retention unchanged: dailies 90 d, first-of-month forever.

**manifest.json schema** (this is also the response schema of `GET /datasets` in openapi.yaml; `Cache-Control: public, max-age=300` per §5.0):

```json
{
  "schema_version": 1,
  "generated_at": "2026-07-06T04:30:00Z",
  "dictionary": "/datasets/DICTIONARY.md",
  "latest": {
    "date": "2026-07-06",
    "files": [
      {
        "size_tier": "top1m",
        "format": "csv.gz",
        "path": "/datasets/2026-07-06/whynoipv6-top1m.csv.gz",
        "bytes": 48211334,
        "sha256": "hex-encoded, 64 chars",
        "rows": 1000000
      }
    ]
  },
  "snapshots": [
    { "date": "2026-07-06", "files": [ /* same file object shape */ ] }
  ]
}
```

Field semantics: `schema_version` (int, starts at 1) is the version of the *export column schema* documented in DICTIONARY.md — bump it whenever exported columns change; `generated_at` RFC 3339 UTC; `rows` = data rows excluding the CSV header (identical for the csv.gz/parquet pair of the same tier); `path` is origin-relative and always points at the **dated** (immutable) path, never `latest/`, so `sha256` stays valid; `snapshots` is sorted newest-first and lists every snapshot currently retained on disk (≤ ~90 dailies + monthlies); `latest` duplicates the newest complete snapshot's entry for convenience. Every snapshot entry contains exactly 6 files (3 tiers × 2 formats).

**Atomic publish procedure** (nightly `v6ctl export`, after the stats tick):
1. Write all 6 files + `SHA256SUMS` into `$DATASETS_DIR/{date}.tmp/`; fsync files.
2. `rename({date}.tmp, {date})` — snapshot becomes visible complete-or-not-at-all.
3. Repoint latest atomically: `ln -sfn {date} $DATASETS_DIR/latest.tmp && mv -T latest.tmp latest` (rename(2), no window where `latest` is missing).
4. Prune per retention (delete expired daily dirs; keep first-of-month).
5. Regenerate `manifest.json` from the directory tree (source of truth = what is on disk), write `manifest.json.tmp`, rename over `manifest.json`.
On any failure before step 2, delete the `.tmp` dir and fire the ops webhook (§2.6 alert points); the previous manifest/latest remain untouched and correct.

**API side:** `GET /datasets` reads `$DATASETS_DIR/manifest.json` and returns it verbatim as `application/json` (re-read per request or with a ≤60 s in-process cache; the file is a few KB). 503 with the standard error envelope if the file is missing/unparseable. Config key: `DATASETS_DIR` (default `/var/lib/whynoipv6/datasets`), shared by the API binary and `v6ctl export`.

**nginx** (extends the §5.0 deploy notes; sibling locations in the api.whynoipv6.com server block):

```nginx
# exact match: manifest endpoint → API (§5.0 proxy_set_header block applies)
location = /datasets {
    proxy_pass http://[::1]:8080;
    proxy_set_header X-Real-IP       $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header Host            $host;
}

# dated snapshots: immutable forever
location ~ ^/datasets/\d{4}-\d{2}-\d{2}/ {
    root /var/lib/whynoipv6;
    add_header Cache-Control "public, max-age=31536000, immutable";
    add_header Access-Control-Allow-Origin "*";
    gzip off;   # payloads are pre-compressed (.csv.gz) or binary (.parquet)
}

# latest/ symlink + DICTIONARY.md: mutable, short TTL
location /datasets/ {
    root /var/lib/whynoipv6;
    autoindex off;
    add_header Cache-Control "public, max-age=3600";
    add_header Access-Control-Allow-Origin "*";
    gzip off;
}
```

(`root`, not `alias`, so `/datasets/...` maps directly under `/var/lib/whynoipv6/`; nginx follows the `latest` symlink by default.)

**Content of the exports is unchanged from §5.4** (columns: host, rank, kind, parent, classification + flags, gold, six confirmed statuses + since-timestamps, country, asn, last_checked; `top100k`/`top1m` use the publicly-ranked predicate, `full` includes all scannable entities — §9 note stands). This addendum pins only hosting, layout, manifest contract, and publish mechanics.

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
                           #   service-candidates, resource add, shame, export,
                           #   stats recalc, migrate
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

**`v6ctl shame` (the `top_shame` writer — the table otherwise has no write path):**

```
v6ctl shame add <host> [--reason "..."]
v6ctl shame remove <host>
v6ctl shame list
```

Semantics (single-maintainer editorial tool; direct DB access like the other v6ctl verbs):

- **`shame add <host>`** — canonicalize `<host>` via the single `Canonicalize` rule
  (§3; a scheme/port/path in the argument is rejected, not stripped). Look up
  `domain` by host. **Error (exit 1)** if: no row exists, `kind <> 'apex'`, `rank IS NULL`, or
  `disabled` — editorial picks must satisfy the §5.1 publicly-ranked predicate,
  otherwise the row could never render. Then:

  ```sql
  INSERT INTO top_shame (domain_id, reason)
  VALUES ($1, $2)
  ON CONFLICT (domain_id) DO UPDATE SET reason = EXCLUDED.reason;
  ```

  (idempotent; re-add updates reason, preserves added_at; `--reason` omitted ⇒
  NULL). If the domain's current `classification <> 'sinner'`, **warn but
  succeed**: "added; will not render on /domain/topsinner until classified
  sinner" — rows are durable picks, visibility is computed at read time (§5.1
  topsinner query pin), matching production where fixed domains stay in the table
  but drop out of the view.

- **`shame remove <host>`** — resolve host to domain_id (error if unknown host),
  `DELETE FROM top_shame WHERE domain_id = $1`; print "not on the shame list" if
  0 rows deleted (exit 0).

- **`shame list`** — print all rows joined to domain: `host, rank, classification,
  reason, added_at, visible` where `visible = (classification = 'sinner' AND rank
  IS NOT NULL AND NOT disabled)`, ordered by rank.

- Shame edits write **no changelog entries** (editorial action, not an observed
  status transition).

Note on migrations: the `003 seed` migration seeds static reference data only
(e.g. the country table) and **does not** populate `top_shame` — its `domain_id`
FK requires phase-1 ingestion; population is the phase-4 importer plus
`v6ctl shame add` thereafter.

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

1. **PR validation (GitHub Actions in the campaign repo — new, tiny).** Runs only on
   `pull_request`, and evaluates **only the `.yml` files changed by the PR** (git
   diff against the merge base) — never the whole repo, so pre-existing issues in
   untouched files cannot fail an unrelated PR. For each changed file, in order:

   - **YAML schema (blocking):** tolerant parse; exactly today's four keys — `title`
     (non-empty string, required), `description` (non-empty string, required),
     `uuid` (optional), `list` of hostnames (required, non-empty). Unknown keys →
     error.
   - **UUID (blocking, diff vs main — §7.2):** contributors never invent UUIDs; the
     Action compares each file's `uuid:` value against the merge-base with main and
     rejects any introduced or changed value (full rule, including the rename
     allowance, in §7.2).
   - **Hostname validation (blocking):** per entry, after normalization with the
     single `Canonicalize` rule (§3: trim, lowercase, strip trailing dot, IDN →
     punycode via golang.org/x/net/idna Lookup profile, strict LDH, ≤253 octets, no
     scheme/path/port/wildcard), plus an eTLD+1 check (PSL, ICANN section).
   - **Within-file duplicate (blocking):** two entries in the same file normalizing
     to the same host → error listing the host and both line numbers. Scope: the
     changed file only.
   - **Size cap (blocking):** ≤1000 list entries per file (config
     `campaign.max_domains_per_file`, default 1000).
   - **Cross-file duplicate (informational only — NEVER blocking):** for each host
     added by the PR that already appears in another campaign file, the bot comment
     notes "`host` is also in `<other campaign title>`". This is expected and
     legitimate: §4.5's membership model exists precisely so one domain belongs to
     several campaigns and is still checked once per day. Do not implement any code
     path that rejects, warns-as-failure, or auto-dedupes across files.
   - **Bot comment:** parsed summary per changed file ("32 domains, 3 subdomains →
     parents auto-linked"), plus the cross-file informational lines. Exit status
     reflects blocking checks only.

   One-time ops procedure (in the campaign repo, before merging the Action): commit
   `chore: remove within-file duplicate hosts` deleting the 6 existing duplicate
   entries — Dutch_Central_Goverment.yml: magazines.rijksoverheid.nl,
   magazines.werkenvoornederland.nl, parlement.nl, services.belastingdienst.nl,
   temis.nl (second occurrence of each); German_Federal_Government.yml:
   bundesarchiv.de (second occurrence). Do NOT touch cross-file duplicates — the 99
   hosts appearing in multiple files today are intentional memberships.
2. **On merge to main:** repo dispatch → operator CI (Semaphore, as wired today for
   other projects) → runs `v6ctl campaign sync --repo /srv/whynoipv6-campaign` on the
   backend host (git pull + import). Alternative trigger for simplicity: the crawler's
   daily tick also runs sync (pull + import) — the webhook is latency sugar, the cron
   is the guarantee. **Both.** (Both invocations call the single sync implementation
   and are serialized by the `JobCampaignSync` advisory lock — §7.1.)
3. **Idempotent import:** normative algorithm in **§7.3** (uuid-keyed matching,
   membership diff, deletion by uuid-set diff, re-add semantics). Generated UUIDs
   are **committed back to the campaign repo via a bot commit** (deploy-key push,
   `[skip ci]`) — moving the write-back from daemon-mutating-a-checkout into an
   auditable commit.
4. **Report:** sync summary (created/updated/renamed/re-enabled/disabled campaigns,
   membership adds/removes, rejected files + reasons, write-back status) to the ops
   webhook (§7.3 step 7).

### 7.1 Sync serialization (webhook + daily tick)

There is exactly ONE sync implementation: `internal/campaign.Sync(ctx, cfg, pool)`.
`v6ctl campaign sync` (webhook path) and the crawler daily tick (§2.6 step 5, after
the lifecycle sweep) both call it. No other code touches the campaign checkout or
the campaign tables' YAML-derived columns.

Sync serializes across processes with the `JobCampaignSync` session-level advisory
lock (§2.6 lock registry, `internal/lock`), acquired **before any git operation** —
the lock protects the shared `/srv/whynoipv6-campaign` checkout as well as the DB.
Per the §2.6 acquisition contract the lock lives on a dedicated pooled connection
held for the whole run; the import transaction itself runs on the pool as usual.
Both sync paths use the blocking `Run(JobCampaignSync, wait=5m, …)` variant (§2.6):
an explicitly requested sync (webhook/v6ctl) and the daily tick's nested step each
wait out a concurrent run rather than silently dropping the daily guarantee;
deadline exceeded → exit 1 with a clear "another campaign sync is running" error.
If a process crashes mid-sync, the connection closes and the lock releases
automatically — no stale-lock cleanup needed.

### 7.2 UUID trust rule — GitHub Action (diff vs main) + DB backstop

This replaces step 1's former "UUID must be absent or valid". The Action (no DB
access) checks out both the PR head and the merge-base with main and compares
`uuid:` values per file:

- **Added file:** `uuid` must be absent or empty — UNLESS its value equals the uuid
  of exactly one file deleted in the same PR (git rename, possibly undetected as
  such). Then it passes, and the bot comment states loudly: "rename detected:
  old.yml → new.yml (uuid preserved)".
- **Modified file:** `uuid` must be byte-identical to the value in main (absent
  stays absent — only the bot commit ever adds one).
- **Deleted file:** allowed (that is how a campaign is retired; sync disables it,
  §7.3 step 5).
- Any other introduction or change of a `uuid:` value → check fails with: "uuid
  values are assigned by the import bot; remove the uuid field".

DB backstop (covers direct pushes and force-merges): during sync, a file whose uuid
does not exist in `campaign` is REJECTED and reported ("unknown uuid — invented or
DB drift; remove the uuid field to register as a new campaign"). Exception:
`v6ctl campaign sync --adopt-unknown-uuids` inserts campaigns using the file's
uuid — used once during the production-data migration (the existing YAML files
already carry production uuids), never in cron/webhook paths.

### 7.3 Sync algorithm (normative; replaces step 3's former prose)

After acquiring the lock and running `git -C $repo pull --ff-only`:

1. **Parse** every `*.yml`/`*.yaml` at the repo root (tolerant parse per §6
   `internal/campaign`). Every domain entry goes through `Canonicalize` (§3)
   **before** entity lookup/creation and membership diff; entries failing it are
   skipped and counted under `rejected + reasons`. Then hosts are **deduped within
   each file** (first occurrence wins) — the membership PK
   `(campaign_id, domain_id)` plus `INSERT … ON CONFLICT DO NOTHING` makes duplicate
   entries harmless regardless of repo state, and the importer must never fail or
   warn on a host present in multiple campaign files: that is N legitimate
   membership rows for one domain row (§4.5). Files failing schema/hostname/size
   validation are rejected and reported; they never partially import.
2. **Duplicate-uuid guard:** if one uuid appears in >1 file, keep the file whose
   path equals the DB's `campaign.source_file` for that uuid and reject the others;
   if none matches, reject all of them. (This is what defeats a copied-uuid file
   that coexists with the original.)
3. **Files with uuid:** upsert by uuid — update `name`, `description`; if
   `source_file` differs, update it and log "campaign renamed: old.yml → new.yml";
   if `disabled=true`, set `disabled = false, updated_at = now()` and log "campaign
   re-enabled (file re-appeared)" — because campaign row, memberships, and domain
   state were all preserved on soft delete, the campaign reappears fully populated,
   with no re-import of members and no changelog noise. Then diff memberships:
   additions → ensure `domain` entity (+ PSL parent auto-link, kind detection) +
   membership row; on membership addition to an **existing** row, apply the §3/§4.8
   re-entry rule (`delisted` → re-enable + `next_check_at = now()`; `dead` → keep
   disabled, `next_check_at = now()`; `service`/`manual` → unchanged); removals →
   delete the membership row only (entity remains; the §2.6 lifecycle sweep handles
   orphaning).
4. **Files without uuid:** first check `SELECT uuid FROM campaign WHERE source_file
   = $file` — if a row exists AND its uuid appears in no repo file, REUSE that uuid
   (a previous write-back push failed; this makes write-back idempotent and prevents
   duplicate campaigns). Otherwise generate a fresh UUIDv4. Insert campaign +
   memberships inside the import transaction.
5. **Deletion (uuid-set diff, not source_file diff):** after steps 3–4, `UPDATE
   campaign SET disabled = true, updated_at = now() WHERE NOT disabled AND uuid <>
   ALL($all_uuids_seen_in_repo_including_newly_generated)` — log each. Membership
   rows are kept (soft delete, history preserved); orphaned rank-NULL domains are
   handled by the lifecycle sweep (§7.4). A restored YAML *without* a uuid is
   treated as a new campaign per step 4 (new uuid written back); the old disabled
   row stays soft-deleted. Consequence, stated for the implementer: a uuid edited
   in place = old campaign disabled by this step + new uuid rejected by §7.2's
   backstop — both loudly in the report; nothing is silently renamed.
6. **Write-back:** after the import transaction commits, write generated uuids into
   their files, make ONE bot commit (`chore: assign campaign uuids [skip ci]`), push
   via deploy key; on non-fast-forward, `git pull --rebase` and retry once; on final
   failure, alert via ops webhook and continue — step 4's reuse rule recovers on the
   next run.
7. **Report** (pipeline step 4): created/updated/renamed/re-enabled/disabled
   campaigns, membership adds/removes, rejected files with reasons (schema,
   duplicate uuid, unknown uuid), write-back status → ops webhook.

Config keys (viper, `campaign.` prefix):

```yaml
campaign:
  repo_path: /srv/whynoipv6-campaign   # shared checkout, owned by the service user
  git_remote: origin                   # push target for the bot commit (deploy key)
```

Ops (Ansible): the checkout and the GitHub deploy key (write access, campaign repo
only) are provisioned for the single service user that runs both `crawler` and
Semaphore-invoked `v6ctl`; the key lives in that user's ssh config. Public
visibility: `NOT disabled` filtering for campaign list endpoints is covered by the
§5.1 visibility addenda.

### 7.4 Lifecycle-sweep linkage (same rule as §2.6 step 1a)

In the lifecycle sweep's linkage predicate, campaign membership counts only if the
campaign is enabled:

```sql
linked := EXISTS (SELECT 1 FROM campaign_domain cd
                  JOIN campaign c ON c.id = cd.campaign_id AND NOT c.disabled
                  WHERE cd.domain_id = d.id)
          OR EXISTS child
          OR last_requested_at >= now() - lifecycle.live_check_linkage
```

Without the `NOT c.disabled` join, a disabled campaign's kept membership rows
(§7.3 step 5) would pin its rank-NULL domains in the frontier forever, contradicting
§4.8's delist grace. With it, they enter the normal `orphaned_at` → 30-day grace →
`delisted` path, and campaign re-enable (§7.3 step 3) restores linkage before the
next sweep or via the delisted re-entry rule.

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
campaign sync (import all 28 YAMLs via `v6ctl campaign sync --adopt-unknown-uuids` —
the existing files already carry production uuids, §7.2; subdomain entries auto-link
parents — §4.2), GeoIP wiring. *Verify:* 1M+~30k domain rows with
correct kinds/parents/ranks; re-running import is a no-op (idempotency test); junk
Tranco entries rejected with counts; integration-test suite for every query.

**Phase 2 — crawler core (the heart).** Lift `internal/checker` + tests; consensus
resolver; Unbound deployment + tuning; frontier claim/commit with confirmed-status
machine; changelog writes; preflight; checkpoint metrics; per-process liveness
heartbeats + idle checkpoint rule (§11.3). *Verify:* (a) unit: the
commit state machine table-driven over every transition incl. error/inconsistent
sequences; (b) fake-DNS quorum tests (2/3 agree, split, timeout combinations);
(c) sample run: 10k mixed-rank domains, results diffed against production's current
statuses — investigate every divergence class (expected ones: co.uk NS fix, stricter
conn-based `v6_only`); (d) chaos: kill a worker mid-batch, batch reclaimed after
lease expiry, no double changelog; (e) claim-plan gate: with the table loaded at ≥1M
rows and a near-empty due backlog (<1k rows due), `EXPLAIN (ANALYZE, BUFFERS)` of the
claim query must show an index scan on `idx_domain_due` with buffers/rows examined
proportional to the due set (not the table), execution <50 ms; the empty-frontier
case (<5 ms) and the full-backlog case (all 1M due — verify rank-ordered claiming and
record the O(due) cost) are both exercised; (f) liveness: kill one of the two crawler
processes — its healthchecks.io check flips to "down" within ≤45 min while the
other stays up (§11.3 heartbeats + idle checkpoint rule).

**Phase 3 — full-scale daily crawl.** 1M daily on production hardware; Grafana
dashboards (throughput, error rates, resolver latencies, queue depth, Unbound stats
via the §11.3 `unbound_stats` scrape + timer); Grafana alert rules A1–A5 (§11.3)
provisioned before the 1M crawl is declared operational; backups live from phase-3
start (§11.1); public-resolver rate smoothing verified (~24 qps/provider measured);
Cloudflare courtesy email sent. *Verify:* 3 consecutive full passes <24h;
confirmed-transition volume plausible (~1–3k/day); zero preflight false-negative
incidents; compression + retention jobs observed running
(`timescaledb_information.jobs`); alert rules exercised (A1 fires when both crawlers
are stopped; A5 fires when the unbound-stats timer is disabled); backup gate per
§11.1 (stanza created, first full backup + WAL archiving confirmed, one full restore
to a scratch instance succeeds) before the first production sweep is declared done;
claim-plan gate
re-run after real churn: after the 3 consecutive full passes (≥3M `next_check_at`
updates through `idx_domain_due`), re-run the Phase-2(e) EXPLAIN with steady-state
backlog — execution must remain <50 ms and `pgstattuple`/`pgstatindex` on
`idx_domain_due` must show autovacuum keeping bloat bounded (index size stable across
passes, not monotonically growing). Grafana dashboard gains a panel graphing
claim-query duration (from the §2.6 checkpoint metrics) — alert threshold 250 ms.

**Phase 4 — API + cutover.** Full compat surface + `/ip`; golden parity tests
(recorded production responses vs new, modulo documented deviations); OpenAPI spec +
TS client generation; **data migration** — one-time import of production's current
statuses as seed confirmed values, full changelog history (the site's credibility
archive), the trailing **90 days** of per-scan history (**OPEN-7: decided**; the
importer takes the window as a flag and the production dump is retained, so a deeper
backfill stays possible later), and the curated `top_shame` list (below); dual-run
with the frontend
pointed at staging; DNS cutover. *Verify:* frontend E2E (playwright) against new API
with zero visual diffs; changelog continuity (old entries render identically);
restore-drill cutover gate after the history import (§11.1): restore the latest
backup to a scratch instance — `SELECT count(*) FROM changelog` matches prod as of
the backup timestamp, and the API binary starts against the restored DB with
`GET /changelog` returning rows.

**Changelog history import transform.** Sources: production `changelog` (id, ts,
domain_id, message, ipv6_status) and `campaign_changelog` (adds campaign_id;
domain_id references campaign_domain.id — resolve via campaign_domain.site → new
entity id). campaign_id is dropped on import; the campaign feed re-derives
membership by join.

1. Resolve host → new `domain.id`. Rows whose host no longer resolves to an entity: create the entity (rank NULL, `created_by='import'`) — history must not be orphaned.
2. Reverse-map `message` by **prefix match, longest/www-variant first** (each pattern implies field/old/new; `{h}` must equal the row's resolved host as a suffix check):
   - `IPv6 enabled for www.` → (www, unsupported, supported); `IPv6 enabled for ` → (base, unsupported, supported)
   - `IPv6 lost for www.` → (www, supported, unsupported); `IPv6 lost for ` → (base, supported, unsupported)
   - `IPv4-only for www.` → (www, no_record, unsupported); `IPv4-only for ` → (base, no_record, unsupported)
   - `No DNS records found for www.` → (www, unsupported, no_record); `No DNS records found for ` → (base, unsupported, no_record)
   - `IPv6 enabled nameserver for ` → (ns, unsupported, supported); `Nameservers degraded to IPv4-only for ` → (ns, supported, unsupported); `IPv4-only nameservers for ` → (ns, no_record, unsupported); `No NS records found for ` → (ns, unsupported, no_record)
   - `IPv6 enabled MX records for ` → (mx, unsupported, supported); `MX records degraded to IPv4-only for ` → (mx, supported, unsupported); `IPv4-only MX records for ` → (mx, no_record, unsupported); `No Mail records found for ` → (mx, unsupported, no_record)

   **Canonical ambiguous-old rule:** production collapsed `unsupported→supported` and `no_record→supported` into one string, and any-old→`no_record` into one string; the importer canonically records `old='unsupported'` in those cases. This is render-safe by construction: the forward ladder (§5.1 changelog addendum A) emits the identical string for every old value the original row could have had.
3. **Cross-check:** derived `new_value` must equal the row's stored `ipv6_status`. Mismatch, no pattern match, or `ipv6_status` outside the three legacy statuses → legacy path: insert `(domain_id, ts, field='legacy', old_value=NULL, new_value=NULL, legacy_message=message, legacy_status=ipv6_status)`. The API filter in the changelog addendum explicitly admits `field='legacy'` rows into all feeds their entity qualifies for (add `OR c.field = 'legacy'` to the filter) and renders them verbatim — this is what makes phase 4's "old entries render identically" verification achievable unconditionally.
4. PK conflicts `(domain_id, ts, field)` (possible when both changelog tables carry the same change, or two legacy rows share a timestamp): if the colliding rows are value-identical, keep one; otherwise bump ts by +1 microsecond until unique (display truncates to seconds; ordering impact nil).
5. **Verification for phase 4:** for every imported row, `renderChangelog(field, old_value, new_value, host)` (or the legacy passthrough) must byte-equal the original production `(message, ipv6_status)`. This is the parity gate.

**Cutover note (seeded state).** Phase 4 seeds confirmed `base/www/ns/mx` statuses
(+ `*_since` from production `ts_*`) but `conn`/`resources` seed NULL. Per §4.3,
each domain's first definitive post-cutover observation of those dimensions commits
immediately with `old_value` NULL → the changelog row is suppressed from all feeds.
Consequence to document: no changelog flood at cutover, and detail pages show
conn/resources as unconfirmed for up to one crawl cycle (~1 day) after launch.

**`top_shame` import (the `v6ctl migrate` importer).** Procedure:

- Source: the `top_shame` table (`site TEXT`) in the retained **production dump** — not the hardcoded 02_data.up.sql seed (the live table is authoritative if the maintainer has edited it since). As of the audit it holds 12 hosts: twitter.com, twitch.tv, ebay.com, imgur.com, imdb.com, wordpress.com, github.com, paypal.com, stackoverflow.com, soundcloud.com, nytimes.com, w3schools.com.
- For each site, resolve to the new `domain.id` by normalized host and insert:

  ```sql
  INSERT INTO top_shame (domain_id)
  SELECT d.id FROM domain d WHERE d.host = $1
  ON CONFLICT (domain_id) DO NOTHING;      -- reason NULL: production has no reason column
  ```

- A site with no matching domain row (fell out of Tranco top-1M between dump and import) is **logged as a warning and skipped** — it must not fail the migration; the operator re-adds it later via `v6ctl shame add` if desired.
- Run order: after phase-1 Tranco ingestion has populated `domain` (the FK makes this a hard precondition), alongside the other phase-4 imports.
- Entries whose domains are no longer sinners (e.g. github.com) are imported anyway; the §5.1 topsinner read filter hides them, same as production. Do not prune on import.

**Golden parity-test scoping (refines "modulo documented deviations"):**

- For `GET /domain` and `GET /country/{code}/sinners`: assert **response shape** (field names, types, envelope/no-envelope, pagination behavior) and **ordering** (rank ascending) against production captures — do **not** assert row-set equality. Instead assert the documented direction of divergence: `new_members ⊆ old_members` on the captured pages (every domain the new backend lists was also listed by production; the reverse need not hold).
- Synthetic membership fixture (production data can't prove the negative): seed one entity with confirmed `base=supported, www=unsupported` and one with `base=unsupported`. Assert the first appears in `/domain/almost` and NOT in `/domain`; the second appears in `/domain` and NOT in `/domain/almost`. Repeat for the country-scoped pair via the fixture's country.
- All other endpoints in the §5.1 table keep full-fidelity golden parity except where a documented resolution already scoped them (legacy serialization branches, disabled/ranked predicate, `top_shame` rank fix, heroes membership).

**Phase 5 — #23 resources + classification surfacing.** Resource sweep worker,
manual endpoints, `resources` dimension + gold badge, `/domain/almost`, resources +
dependents endpoints. Flip `crawler.resources.enabled=true` (§4.6) at deploy — until
then the crawler writes `resources = not_applicable`, domain resource columns stay
NULL, and no gold badges exist. *Verify:* known-fixture sites (e.g. a hero with v4-only fonts
CDN) classify as hero+not-gold with `resources_v4only`; resource-host dedup ratio
measured; classification counts stable across 3 days (no flap storm from the new
dimension — it only affects gold).

**Phase 6 — public features.** Live check (+consumer), stats endpoints, dataset
export, badge (optional). *Verify:* abuse test on `/check` (rate limits hold under
scripted load); datasets validate against DICTIONARY; stats endpoints match
snapshot tables.

**Phase 7 — campaign automation + ops polish.** Campaign-repo Actions, merge webhook,
bot write-back; runbooks (Unbound, Timescale jobs, frontier surgery) and any
remaining notification polish (healthcheck/webhook notifications themselves ship in
phases 2–3, §11.3). *Verify:* end-to-end: test PR → merge → domains appear scanned
within 24h with UUID committed back.

---

## 9. Risks & decision log

All round-1 open items were resolved in operator review (2026-07-06). A subsequent
spec-readiness audit (2026-07-07, `docs/history/spec-readiness-review.md` — 35 confirmed
findings, all with forced resolutions) has been folded into this document in full;
that report is the change record for round 2.0.

| # | Item | Decision |
|---|---|---|
| OPEN-1 | NS/MX `partial` mapping (§2.2) | **≥1 v6-capable host = `supported`**, as recommended |
| OPEN-2 | www NXDOMAIN vs hero bar (§4.3) | **`not_applicable` skips** — a site without www can be a Hero; www `no_record` treated the same |
| OPEN-3 | Campaign-level manual resource lists (§4.6) | **Revised (round 1.2):** no campaign `resources:` syntax — campaign YAML keeps exactly today's four keys. Endpoint intent is expressed by listing hostnames in `domains:` (auto-linked as subdomain entities of their registrable parent, §4.2). #23 resources = auto-discovery + operator `v6ctl resource add` only |
| OPEN-4 | Service-candidate review UX (§4.8) | **CLI-only** (`v6ctl service-candidates ...`); no admin HTTP surface. In-degree threshold stays a tuning item after phase-5 data |
| OPEN-5 | Country attribution (§4.9) | **Keep ccTLD precedence**, GeoIP fallback |
| OPEN-6 | Cutover behavior (§5.1) | **Serve migrated seed values immediately**; publish a "methodology v2" note for the deliberate metric shifts (hero bar, real `v6_only`, correct multi-label-TLD NS, sinner-list membership: `/domain` + `/country/{code}/sinners` now list base-unsupported domains only — www-only offenders move to the new `/domain/almost` "almost there" list instead of the shame list, the amended `v6_ready` www formula (§5.1 legacy-serialization rule R4), and the `top_heroes`/`top_nameserver` value fixes (§4.7: `rank <= 1000`, `base = 'supported'`)). The methodology-v2 note is the single public changelog of all deliberate metric shifts |
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
| Status model | 4-value public enum + 7-value internal observation | Single shared enum | `partial`/`error`/`inconsistent` must exist internally (§4.1) and must never leak to public output (§2.2) |
| Consensus scope | apex+www AAAA only via 3 public resolvers | Consensus on all lookups | 3× public-resolver load for records that don't gate classification; ~24 qps/provider (§2.7) is the validated-safe budget |
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

## 11. Operations

Everything a single maintainer needs to run the system in production: backup &
restore, the GeoIP data lifecycle, liveness/alerting, packaging & deploy, and
logging conventions. Config keys referenced here are defined where they are
introduced (§2.6, §4.9, §5.3, §5.4); this section adds only its own new keys.

### 11.1 Backup & restore

The database is the only stateful component. `scan` and `scan_detail` are re-derivable by re-crawling; `changelog` (kept forever, §4.4) and `domain` confirmed state (`*_status/_since/_observed`, disabled/dead lifecycle) are NOT — they are the product's credibility surface and must survive loss of the DB host. Backups are prod infrastructure from **phase 3 onward** (they must be running before the first full-scale sweep writes confirmed state).

#### 1. Physical backups — pgBackRest (the authoritative recovery path)

- **Tool:** pgBackRest (current release), installed on the DB VM via the Ansible role. If PostgreSQL runs in Docker per the brief's compose setup, mount the data dir and socket into a pgbackrest sidecar built from the same PG18+timescaledb image family so library versions match; if PG runs natively under systemd, run pgBackRest natively. Either way the repo lives off-host.
- **Mode:** continuous WAL archiving + weekly full + daily differential. PITR is available across the whole retention window.
- **postgresql.conf (Ansible template):**
  ```
  archive_mode = on
  archive_command = 'pgbackrest --stanza=whynoipv6 archive-push %p'
  archive_timeout = 15min        # bounds worst-case loss to ≤15 min of changelog writes (~1-3k rows/day total)
  ```
- **pgbackrest.conf skeleton** (host-specific values are Ansible vars; secrets in Ansible vault):
  ```ini
  [global]
  repo1-type=sftp                      # default: second VM; alternative: s3 (any S3-compatible endpoint)
  repo1-path=/srv/pgbackrest/whynoipv6
  repo1-sftp-host={{ backup_host }}
  repo1-sftp-host-user=pgbackrest
  repo1-sftp-private-key-file=/etc/pgbackrest/id_ed25519
  repo1-retention-full=4               # 4 weekly fulls ≈ 28-day PITR window; diffs+WAL expire with their full
  repo1-cipher-type=aes-256-cbc
  repo1-cipher-pass={{ vault_pgbackrest_cipher_pass }}
  compress-type=zst
  start-fast=y

  [whynoipv6]
  pg1-path={{ pg_data_dir }}
  ```
  Off-host is mandatory: the repo must never live only on the DB host. Recommended default: sftp to a second VM; S3-compatible object storage (`repo1-type=s3` + bucket/endpoint/key options) is an equivalent drop-in.
- **Schedule (systemd timers, Ansible-managed):** `pgbackrest-full.timer` Sun 03:30 → `pgbackrest --stanza=whynoipv6 --type=full backup`; `pgbackrest-diff.timer` Mon–Sat 03:30 → `--type=diff backup`. 03:30 sits outside the daily crawl's heavy write window and the coordinator's Tranco import window (23:15 + 2h retries, §2.6/§3). Both services set `OnFailure=whynoipv6-notify@%n.service` (the same 3-line curl-to-webhook unit as §11.4 D.3, installed on the DB VM too) — the same ops webhook already used for Tranco import aborts and the fast-lane/provider breakers.
- **Sizing note:** with §4.4 retention (scan 2y compressed single-digit GB, scan_detail 90d ≈ 15–40 GB) the repo stays well under 100 GB; no exclusions needed. Do NOT exclude scan/scan_detail from physical backups — pgBackRest backs up the cluster, and partial-cluster physical backup is not a thing.

#### 2. TimescaleDB restore requirements (runbook, verbatim)

1. **Physical restore** requires the target to run the **same PostgreSQL major version** and have a timescaledb shared library **of the exact extension version** that was current at backup time. Record both continuously (see monitoring below). Keep the Ansible role's PG + timescaledb versions in lockstep with prod. **Never** upgrade the extension without immediately taking a fresh full backup after `ALTER EXTENSION timescaledb UPDATE;`.
2. **Logical restore** (only ever for the §3 artifacts or an ad-hoc pg_dump): create the matching extension version first, then `SELECT timescaledb_pre_restore();` → restore → `SELECT timescaledb_post_restore();`. Plain `pg_dump` of individual hypertables is **forbidden as a backup strategy** (it silently misses `_timescaledb_internal` chunks unless the whole database is dumped).
3. **Restore procedure (scratch or DR):** provision VM/container with matching PG18+timescaledb → install pgbackrest.conf pointing at the repo with `pg1-path` set to the empty data dir → `pgbackrest --stanza=whynoipv6 restore` (add `--type=time --target='…'` for PITR) → start PG → verify per §4.

#### 3. Belt-and-suspenders weekly logical export

The two irreplaceable tables, exported as plain CSV via COPY (COPY reads through all hypertable chunks and the restore path has zero PG/extension version coupling):

```sh
# /usr/local/bin/whynoipv6-export.sh — weekly systemd timer (Sun 04:30), on the DB VM
set -euo pipefail
d=$(date +%F); out=/var/backups/whynoipv6
psql -Atq service=whynoipv6 -c "COPY (SELECT * FROM changelog ORDER BY ts) TO STDOUT WITH (FORMAT csv, HEADER)" | zstd -q -o "$out/changelog-$d.csv.zst"
psql -Atq service=whynoipv6 -c "COPY (SELECT * FROM domain ORDER BY id) TO STDOUT WITH (FORMAT csv, HEADER)" | zstd -q -o "$out/domain-$d.csv.zst"
rsync -a "$out/" pgbackrest@{{ backup_host }}:/srv/logical-exports/whynoipv6/
```
Retention on the backup host: last 8 weeklies + first-of-month for 12 months (tmpwatch/find -mtime in the Ansible role). Failure → same ops webhook. All other tables (campaign*, tranco*, resource_host, crawler_metrics) are re-derivable from the campaign YAML repo, Tranco, or re-crawling and are covered by the physical backup anyway.

#### 4. Restore drills (a backup that has not been restore-tested is assumed broken)

- **Phase-3 verify item (§8 phase 3's gate):** pgBackRest stanza created, first full backup completed, WAL archiving confirmed (`pgbackrest check`), and one full restore to a scratch instance succeeds before the first production sweep is declared done.
- **Phase-4 cutover gate (§8 phase 4, after the history import):** restore the latest backup to a scratch instance; `SELECT count(*) FROM changelog` matches prod as of the backup timestamp; the API binary starts against the restored DB and `GET /changelog` returns rows.
- **Quarterly thereafter:** repeat the phase-4 drill (timebox 1h), plus one spot-check that a weekly CSV export loads into a fresh vanilla PG (`\copy changelog FROM ...`). Record date + result in the ops notes.

#### 5. Monitoring (Grafana + ops webhook)

Nightly systemd timer runs `pgbackrest --stanza=whynoipv6 info --output=json` and `psql -Atc "SELECT version(), (SELECT extversion FROM pg_extension WHERE extname='timescaledb')"`, appends both to `/var/log/pgbackrest/verify.log` (this is the version-of-record for §2 item 1), and alerts the ops webhook if: newest backup is older than 26 h, newest archived WAL is older than 1 h, or the last export timer failed. Optionally expose the same three as a Grafana panel via a textfile/exec collector — but the webhook alert is the required part.

### 11.2 GeoLite2 lifecycle runbook (Ansible + systemd)

Production's repo-bundled mmdb files date from January 2023 — this procedure
replaces that. (The attribution logic itself, the `GEOIP_PATH` config key, and the
crawler's hourly mtime check + atomic reader swap are specified in §4.9.)

1. **Account**: free MaxMind account; generate a license key. Store `AccountID` +
   `LicenseKey` in Ansible vault (`MAXMIND_ACCOUNT_ID`, `MAXMIND_LICENSE_KEY`).
2. **Ansible**: install the distro `geoipupdate` package; template
   `/etc/GeoIP.conf`:
   ```
   AccountID <vault>
   LicenseKey <vault>
   EditionIDs GeoLite2-ASN GeoLite2-Country
   DatabaseDirectory /var/lib/GeoIP
   ```
3. **Timer**: enable the packaged `geoipupdate.timer`, overridden to
   `OnCalendar=Wed,Sat 06:30` + `RandomizedDelaySec=4h` (GeoLite2 publishes
   Tuesdays and Fridays; twice-weekly pickup, weekly is the acceptable minimum).
4. **Monitoring**: crawler exports the loaded mmdb build epoch in
   `crawler_metrics`; Grafana alert when it is older than 30 days (catches expired
   license keys and broken timers — the exact failure mode production is in).

Config key: `GEOIP_PATH` (string, default `/var/lib/GeoIP`, crawler) — introduced
in §4.9. Reload interval and filenames are fixed, not config.

### 11.3 Crawl liveness, Unbound stats, Grafana alerting

(Amends §2.6 step 7's healthcheck ping and §8 phases 2/3/7 — the phase-plan
changes are folded into §8.)

**Observability model (pinned, restated):** Grafana reads Postgres directly (crawler_metrics, frontier queries, unbound_stats, timescaledb_information views). No Prometheus, no /metrics endpoints on any binary. External liveness via healthchecks.io pings (production pattern, toolbox.HealthCheckUpdate lift).

#### 1. Liveness heartbeats (phase 2, not phase 7)

One healthchecks.io check **per crawler process** (e.g. `wni6-crawler-1`, `wni6-crawler-2`), plus one for the daily tick (`wni6-daily-tick`).

- After every successful claim-cycle commit, the process pings its check's success URL, throttled to at most one ping per `ops.healthcheck_min_interval` (default 60s). Lifted from production semantics (crawl.go:182) at claim-cycle granularity.
- On preflight failure (§2.6 self-preflight), ping the `/fail` endpoint of the same check (production HealthFail pattern, crawl.go:84) in addition to the existing ops-webhook alert.
- healthchecks.io check config: Period = 15 min, Grace = 30 min. A dead or hung crawler process is therefore signaled within ≤45 min instead of ~23h.
- The 03:30 daily tick keeps its own separate check (Period = 24h, Grace = 2h), pinged at §2.6 step 7 as designed. §2.6 step 7's "healthcheck ping" now refers to THIS check only; per-process liveness is the mechanism above.
- Empty URL disables a heartbeat (dev/staging default).

**Idle checkpoint rule (amends §2.6 checkpoint metrics):** in addition to the per-1000-domains checkpoint, each crawler process writes a crawler_metrics row (processed=0, is_final=false, current queue_depth/active_slots) whenever no checkpoint has been written for 5 minutes. This keeps staleness alerting (A1 below) valid when the frontier is drained.

Config keys (add to the consolidated config registry):
```yaml
ops:
  webhook_url: ""                # existing (§2.6)
  healthcheck_url: ""            # THIS process's healthchecks.io ping URL; empty = disabled
  healthcheck_tick_url: ""       # coordinator only: daily-tick check
  healthcheck_min_interval: 60s
```
(Each process gets its own `healthcheck_url` via its unit's environment/config file — Ansible templates one systemd unit instance per process.)

#### 2. Unbound stats collection (phase 3; names the §8 phase-3 "Unbound stats" mechanism)

Mechanism: `v6ctl ops unbound-stats` executes `unbound-control stats` (the **resetting** variant, so every row holds per-interval deltas and Grafana rate math is a plain division by the interval), parses the key=value output, and inserts one row. Invoked by a systemd timer (`whynoipv6-unbound-stats.timer`, `OnCalendar=*:*:00` i.e. every 60s; Ansible-managed) on the Unbound host. No unbound_exporter, no Prometheus.

```sql
-- migration 002 (hypertable pass, alongside crawler_metrics)
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
SELECT create_hypertable('unbound_stats', by_range('ts', INTERVAL '7 days'));
SELECT add_retention_policy('unbound_stats', drop_after => INTERVAL '30 days');
```
Config: `unbound_stats.control: "unbound-control"` (path/args override for chroot setups). ~1,440 rows/day/host — negligible.

#### 3. Grafana alert rules (phase 3 deliverable, alongside the §8 phase-3 dashboards)

Provisioned as Grafana alert rules on the Postgres datasource (YAML provisioning, Ansible-deployed). Notification policy → the existing ops webhook. Thresholds are starting points, tunable in Grafana:

- **A1 crawler stalled:** `SELECT count(*) FROM crawler_metrics WHERE ts > now() - interval '15 minutes'` == 0 → critical. (Valid at all times thanks to the idle-checkpoint rule.)
- **A2 frontier lag:** `SELECT count(*) FROM domain WHERE next_check_at < now() - interval '6 hours'` (with the active-domain predicate from the frontier claim SQL, §2.5) > 50,000 → warning; > 200,000 → critical. Catches silent throughput collapse the heartbeats can't see.
- **A3 error ratio:** `SELECT coalesce(sum(failed)::float / nullif(sum(processed),0), 0) FROM crawler_metrics WHERE ts > now() - interval '1 hour'` > 0.20 → warning. (Complements, does not replace, the §2.4 fast-lane/provider breakers, which remain the primary error-path alerts.)
- **A4 TimescaleDB jobs:** `SELECT count(*) FROM timescaledb_information.job_stats WHERE last_run_status = 'Failed'` > 0 → warning. (Same view phase 3's verify step already names.)
- **A5 Unbound/scraper down:** `SELECT count(*) FROM unbound_stats WHERE ts > now() - interval '5 minutes'` == 0 → critical.

### 11.4 Production packaging & deploy (systemd + Ansible)

Scope: production runs on the operator's own VMs via systemd (brief: "Docker +
docker-compose; systemd for prod" — compose is dev-only). Everything below is
provisioned by the operator's existing Ansible; the backend repo ships the unit
files under `deploy/systemd/` as the source of truth, Ansible copies them verbatim.
The nginx api vhost (proxy_set_header block, [::1] rationale) is specified in
§5.0 and the dataset static split in §5.4 — not repeated here.

#### D.1 Filesystem & user layout

- System user `whynoipv6` (no shell, no home). Same user for api, crawler, v6ctl timers.
- `/opt/whynoipv6/bin/{api,crawler,v6ctl}` — the three release binaries (static linux/amd64).
- `/etc/whynoipv6/env` — root:whynoipv6 0640; env-format file holding every key from
  the consolidated config registry: `DATABASE_URL`, `API_LISTEN`
  (default `[::1]:8080`, per §5.0), ops-webhook + healthcheck URLs, `GEOIP_PATH=/var/lib/GeoIP`,
  crawler/consensus/lifecycle keys.
- `/var/lib/whynoipv6/datasets/` — owned whynoipv6, world-readable; nginx serves it read-only
  per §5.4 (`autoindex off`, manifest is the index).
- `/var/lib/GeoIP/` — written by the distro `geoipupdate` package, read by crawler.
- Migrations are **embedded in `v6ctl` via `go:embed` (golang-migrate iofs source)** —
  no migrations directory ships to the host; the deploy artifact set is exactly the
  three binaries. (`db/migrations/` in the repo stays the sqlc/dev source of truth.)
- Unbound: two local instances on the crawler host, managed as distro
  `unbound@1.service` / `unbound@2.service` (configs via Ansible, tuning per phase-2
  verification); their listen addresses are crawler config, not baked in.

#### D.2 Service units (`deploy/systemd/`)

`whynoipv6-api.service`:
```
[Unit]
Description=WhyNoIPv6 API
After=network-online.target postgresql.service
Wants=network-online.target
[Service]
User=whynoipv6
EnvironmentFile=/etc/whynoipv6/env
ExecStart=/opt/whynoipv6/bin/api
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
[Install]
WantedBy=multi-user.target
```

`whynoipv6-crawler.service`: identical shape, `ExecStart=/opt/whynoipv6/bin/crawler`,
plus `ReadWritePaths=/var/lib/whynoipv6/datasets` and read access to `/var/lib/GeoIP`.
One crawler unit per host (§6: "one per machine"); a single host meets the §2.7
throughput math — a second crawler host is resilience, not capacity, and needs no
coordination beyond the shared frontier (SKIP LOCKED). Graceful shutdown: both
binaries drain on SIGTERM within systemd's default 90s stop timeout (crawler
finishes in-flight scans, uncommitted claims simply expire back to the frontier).

#### D.3 Timer inventory

Decision: the 23:15 UTC Tranco import is owned by the **crawler coordinator
goroutine** under `JobTrancoImport` (§2.6 trigger resolution — no systemd timer;
the 2h retry loop and 48h staleness warning live in the coordinator). systemd
timers own the remaining scheduled jobs below. Campaign sync is unchanged:
Semaphore webhook + daily tick, per §7's explicit "Both".

| Timer | OnCalendar (UTC) | ExecStart | Notes |
|---|---|---|---|
| `whynoipv6-export.timer` | `04:30`, `Persistent=true` | `v6ctl export` | Satisfies §5.4 "after the stats tick" with 1h headroom over the 03:30 tick; export reads confirmed state + latest stats snapshot, so a late tick degrades to yesterday's stats row, never a failure. Also applies the §5.4 retention (dailies 90d, first-of-month kept). |
| `whynoipv6-unbound-stats.timer` | `*:*:00` (every 60s) | `v6ctl ops unbound-stats` | §11.3 mechanism, on the Unbound host. |
| `geoipupdate.timer` (distro package) | `Wed,Sat 06:30` + `RandomizedDelaySec=4h` | `geoipupdate` (`/etc/GeoIP.conf`: GeoLite2-ASN + GeoLite2-Country, account/license key) | Cadence per §11.2 (MaxMind publishes Tue/Fri); pickup by the crawler is §4.9's hourly mtime check + atomic reader swap — no tick step, no restart needed. |

Timer service units are `Type=oneshot`, `User=whynoipv6`,
`EnvironmentFile=/etc/whynoipv6/env`. Timer failures alert via the existing ops
webhook: `v6ctl` exits non-zero on failure and each oneshot unit sets
`OnFailure=whynoipv6-notify@%n.service` (a 3-line curl-to-webhook unit) — no new
alerting infrastructure.

The 03:30 tick's canonical step list is pinned in §2.6 and is not restated here.

#### D.4 Deploy procedure (Ansible playbook order)

1. CI (monorepo) builds and publishes release artifacts: `api`, `crawler`, `v6ctl`
   (static binaries, migrations embedded in `v6ctl`).
2. Ansible copies binaries to `/opt/whynoipv6/bin/` (new binaries land beside the
   still-running old processes — safe, nothing re-execs).
3. `sudo -u whynoipv6 /opt/whynoipv6/bin/v6ctl migrate up` — **forward-only; no
   down-migrations in production.** Contract: every migration shipped with release N
   must keep release N−1 binaries functional (expand/contract), because old binaries
   run between steps 3 and 4–5.
4. `systemctl restart whynoipv6-crawler` (drains gracefully per D.2).
5. `systemctl restart whynoipv6-api`.
6. Verify: `systemctl is-active` both units; `curl -6 http://[::1]:8080/` (the §5.1
   health endpoint, `{"message":"ok"}`); crawler_metrics row age < 10 min in Grafana.

Rollback = redeploy the previous release's binaries (steps 2, 4, 5 only); the
expand/contract contract makes the already-applied migration compatible. Never
migrate down.

### 11.5 Logging conventions (normative — all three binaries)

**Handler.** Each `cmd/*/main.go` installs `slog.SetDefault(slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})))` once at startup. `w` is `os.Stdout` for `api` and `crawler` (systemd units; journald captures stdout — no log files, no rotation logic in the binaries). `v6ctl` is the one deviation: slog goes to **stderr** so command output on stdout stays pipeable. JSON always — no format knob.

**Config.** One key, shared by all three binaries, following the existing viper uppercase-env convention (`API_LISTEN` precedent): `LOG_LEVEL` ∈ `debug|info|warn|error`. Default `info` for `api` and `crawler`, `warn` for `v6ctl` (CLI ergonomics). Add to the spec's config registry.

**Standard attribute keys (exact names).**
- `component` — binary name (`api`|`crawler`|`v6ctl`), stamped once on the root logger via `.With()`.
- `run_id` — the crawler run UUID, identical to the value written to `crawler_metrics.run_id`; stamped on a per-run child logger so every crawler line carries it.
- `worker` — worker identity string, identical to `crawler_metrics.worker`.
- `domain` — the eTLD+1 (or registry host) on any per-domain/per-host line.
- `duration_ms` — int64 milliseconds for timed operations.
- `err` — error text (`slog.String("err", err.Error())`).

**Level policy.**
- `debug` — per-domain scan outcomes, per-check observations, claim-batch contents, live-check job steps, resource-sweep per-host results. Off in production: per-domain results are already durable in the `scan` table and aggregated in `crawler_metrics`; debug lines exist for local troubleshooting only.
- `info` — lifecycle events only: startup (config summary with secrets redacted), graceful shutdown, run start/end (`run_id`, totals), Tranco import summary, migration/phase actions, and the API access log (below). Optionally one line per `crawler_metrics` checkpoint (every 1000 domains ≈ 1k lines/day — acceptable).
- `warn` — actionable anomalies that don't stop the process: preflight failure (in addition to the ops-webhook alert), quorum-inconsistency rate above threshold, claim starvation (empty frontier while backlog expected), lease-fence aborts (mirrors §4.3's `lease_lost` counter), Tranco import aborted by the §3 sanity guard, ops-webhook/heartbeat delivery failure.
- `error` — bugs and unexpected states only: recovered panics (chi `middleware.Recoverer` wired to slog), DB errors aborting a §4.3 commit unit, invariant violations. A domain that fails its scan is a scan observation, not an error — it goes to `debug` + the metrics counters.

**Volume rule (normative).** In steady state, nothing is emitted per-domain or per-check above `debug`. Per-domain failures during incidents (e.g. resolver outage) aggregate into `crawler_metrics` error counters — alerted via Grafana and the daily ops-webhook summary — never into per-line warn/error spam; this keeps journald's default rate limiting (`RateLimitBurst=10000`/30s) irrelevant even at 1M domains/day.

**API access log.** chi stack: `middleware.RequestID` → `middleware.RealIP` (nginx sets `X-Forwarded-For`) → a small slog access-log middleware (do not use chi's default text logger). One `info` line per request: `request_id`, `method`, `path`, `status`, `bytes`, `duration_ms`, `remote_ip`. Exclude the health endpoint from the access log.

---

*End of report. All decisions are logged in §9; the 2026-07-07 spec-readiness
audit's 35 resolutions (docs/history/spec-readiness-review.md) are folded in throughout.*
