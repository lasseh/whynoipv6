# WhyNoIPv6 Backend Design — Spec-Readiness Audit Report

**Date:** 2026-07-07
**Document under review:** `docs/backend-design.md` (Round 1.2, 1232 lines)
**Authoritative product brief:** `docs/backend-research-brief.md`
**Audit goal:** determine whether a full implementation spec can be written from the design doc such that Claude Opus 4.8 can implement it without asking questions, guessing product-relevant behavior, or hitting contradictions.
**Method:** two-pass multi-agent audit. Pass 1: 12 independent lenses (implementer role-plays for crawler/schema/API/ingest, brief traceability, source-code citation cross-check, frontend compat diff, internal consistency, external-fact web verification, ops completeness, edge-case hunt, decision stress-test) produced 134 raw findings → deduplicated to 15 canonical findings → each adversarially verified (blocker claims by 3-vote panel) → each confirmed finding resolved with ready-to-paste spec text. Pass 2 (appendix): the 36 distinct issues the dedup stage dropped were recovered from the raw output and run through the same verify→resolve pipeline.
**Combined result:** **35 findings confirmed** (6 blockers, 15 majors, 14 minors) + 16 refuted — every confirmed finding carries a high-confidence resolution, and **none requires a maintainer decision**.
**Verdict:** **ready-with-fixes** — fold the 35 resolutions into the design doc (grouping in §7.1 + appendix "Impact" section), then the spec can be written directly.

---

## 1. Verdict

**ready-with-fixes.**

The design document is architecturally sound and unusually thorough on the parts it covers — the trust model, the engine lift, the schema, and the compat surface are all real designs, not sketches. But it cannot be turned into an implementation spec as-is: six blockers sit exactly on the product's core surfaces (the observation model that decides who is publicly shamed, the domain lifecycle, the frozen legacy API contract, the hero gate, the resources/gold dimension, and one compat endpoint whose backing columns don't exist). Each of these would force the spec writer to invent product-relevant behavior or reconcile sections that actively contradict each other. The decisive fact, however, is that **every one of the 15 findings ships a verified, high-confidence resolution with ready-to-paste spec text, and none requires a maintainer decision** — each resolution is forced by constraints the doc or brief already states. That is precisely the definition of ready-with-fixes: fold the 15 resolutions into the design doc (mapping in §7 below) and the spec can be written immediately, with no open product questions remaining.

*(Second pass: after this report was drafted, the 36 findings the dedup stage had dropped were independently verified and resolved — 20 confirmed (6 majors, 14 minors, no blockers, none needing a decision), 16 refuted. The verdict is unchanged; see the appendix at the end of this document, whose "Impact on the first report" section amends §7's fold-in groups and spec outline.)*

---

## 2. Blockers

### B1. Base observation cannot express the inactive/sinner split: no A-record input, contradictory NXDOMAIN mapping, and `no_record` is never produced by the specified engine

- **Section:** §2.1 / §2.3 / §4.3 (brief §5.5 rules 1–2), §9 OPEN-2
- **Confidence:** high · **Needs user decision:** no

**Description.** The doc gives no way to compute the base observation the classification ladder requires. §2.3's quorum reduction is AAAA-only (NOERROR + no AAAA → `unsupported` unconditionally), so empty-but-existing zones (no A, no AAAA) become sinners instead of inactive, contradicting §4.3 rules 1–2 and brief §5.5. No engine component or spec text defines the apex A query those rules presuppose (the v6audit checker has no `no_record` status and no classification A query anywhere; `dns_aaaa_base.go` maps NXDOMAIN → `not_applicable`, making §4.3's inactive tier unreachable). §2.3's "`no_record`/`not_applicable` (NXDOMAIN)" slash never resolves the per-dimension mapping. The implementer would have to invent the A lookup, its resolver path, and the NXDOMAIN mapping — inventions that determine who gets publicly shamed. (Verifier correction: the OPEN-2 www-`no_record` hero-set sub-claim was overstated — the doc answers it; only the condition under which www yields `no_record` was undefined, a residue of the same missing mapping table.)

**Resolution.** Redefine `base` and `www` as composite observations in the consensus wrapper: the quorumed AAAA answer plus one conditional bulk-resolver A lookup (run only when AAAA quorum is NOERROR-empty — exactly the "A×2" already budgeted in §2.7). Apex NXDOMAIN → `no_record`; NOERROR + no AAAA + A → `unsupported`; NOERROR + no AAAA + no A → `no_record`; ≥1 AAAA → `supported`. `www` never produces `no_record` (NXDOMAIN and empty-www both → `not_applicable`), so the OPEN-2 clause is deleted as unreachable. `dns_aaaa_base.go`/`dns_aaaa_www.go` move from lift-verbatim to Adapt. The unquorumed A lookup is safe because the composite still passes the N=2 confirmation gate.

**Spec text (verbatim):**

````markdown
### Ready-to-paste spec changes (backend-design.md)

**1. §2.1 — move `dns_aaaa_base.go` / `dns_aaaa_www.go` out of the lift-verbatim table.** Replace that table row with:

> | `internal/checker/dns_aaaa_base.go`, `dns_aaaa_www.go` | AAAA lookup + CDN detection on www CNAME chain — **adapted, not verbatim**: wrapped into composite `base`/`www` observations (§2.3.1); v6audit's NXDOMAIN→`not_applicable` mapping is replaced per-dimension, and the globally-routable-AAAA filter from production (`whynoipv6/internal/resolver/resolver.go:486-514`) is ported in |

And extend the **Adapt** paragraph: "…and the base/www composite wrapper (§2.3.1) that combines the quorumed AAAA with a conditional bulk-resolver A lookup."

**2. New §2.3.1 — Observation mapping (base/www composites and the per-dimension table).** Insert after §2.3:

#### 2.3.1 Observation mapping — from engine outcomes to per-dimension observations

Each per-resolver AAAA answer (apex and www, consensus resolver) reduces to one of four symbols, and quorum (§2.3 rules unchanged) is taken over these symbols:

- `exists` — ≥1 globally-routable AAAA (loopback/link-local/ULA answers rejected)
- `empty` — NOERROR, no AAAA after CNAME chase
- `nxdomain` — NXDOMAIN
- `error` — timeout / SERVFAIL / network error

No-quorum → observation `inconsistent`; quorum on `error` → observation `error` (both handled per §2.3/§4.3: never advance confirmed state).

**Conditional A lookup:** when and only when the AAAA quorum result is `empty`, the wrapper issues ONE A query for the same name through the **bulk resolver** (no quorum). Outcomes: `a_present` (≥1 A), `a_absent` (NOERROR-empty; an A-NXDOMAIN contradicting the AAAA NOERROR is also treated as `a_absent` — resolve contradictions in the domain's favor), `a_error`. These are the two "A×2" queries already budgeted in §2.7. The A answer is not quorumed: any single wrong answer still has to survive the N=2 confirmation gate (§4.3) before it can change confirmed state.

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
| `conn` | `supported` | n/a | `unsupported` (requires preflight pass ≤5 min, §2.5) | `not_applicable` (phase-2 skip: no AAAA) | `error` |
| `resources` | `supported` | `unsupported` + `resources_v4only` (§2.2) | `unsupported` | `not_applicable` (phase-2 skip) | `error` |

`ns`, `mx`, `conn`, and `resources` never produce `no_record` either. The `observation` enum values `error`/`inconsistent` and the `ipv6_status` values are otherwise as defined in §4.1; informational dimensions (`tls/smtp/parity/dnssec/ptr/spf/latency`) store the raw engine status verbatim in `*_observed`/payload and have no mapping.

**3. §4.3 — replace the OPEN-2 note** (the paragraph beginning "Note www: engine maps www NXDOMAIN…") with:

> Note www: www NXDOMAIN and www with neither A nor AAAA both map to `not_applicable` (§2.3.1) — a site without a working www can be a Hero (**OPEN-2: decided**). `www` never produces `no_record`; only `base` can, and confirmed `base = no_record` is what feeds tier 1 (inactive) and the §4.8 lifecycles.

**4. §2.3 wording fix:** in the per-resolver reduction sentence, replace "`no_record`/`not_applicable` (NXDOMAIN)" with "`nxdomain` (NXDOMAIN) — mapped per dimension in §2.3.1".
````

---

### B2. Disabled/dead/delisted lifecycle is unimplementable: claim query and frontier index exclude disabled rows, eligibility predicates are absent from the claim SQL, and the dead-streak/delist-grace triggers have no storage or owning job

- **Section:** §2.5 / §4.2 / §4.8 (also §3, §2.6, §5.3)
- **Confidence:** high · **Needs user decision:** no

**Description.** Every mechanism that must implement §4.8's disabled/dead/delisted lifecycle contradicts or fails to support it: the §2.5 claim SQL and §4.2 frontier index both exclude `disabled` rows, making slow-lane revalidation and auto-re-enable of `dead` domains (mandated by the brief) unreachable dead code; §4.2's 4-way claim-query eligibility rule is absent from the shown claim SQL and has no supporting column for the 7-day live-check window; the 7-consecutive-NXDOMAIN dead trigger has no counter column and no owning job; the delisted 30-day grace has no rank-null timestamp and no job that flips `disabled`; and §3's Tranco upsert never clears `disabled`, so §4.8's auto re-enable on re-listing has no implementing code path. Reconciling this requires editing verbatim SQL/DDL and inventing schema columns, job ownership, and grace/re-entry semantics — and getting it wrong reproduces the exact transient-outage-becomes-permanent-removal failure the design claims to fix. (A no-count-guard robustness sub-point on §3 step 4 rides along as minor.)

**Resolution.** One end-to-end mechanism: (1) claim query and frontier index admit `dead`/`delisted` rows (the slow lane is just a far-future `next_check_at`); (2) the 4-way eligibility rule is deleted — eligibility is materialized by a new daily lifecycle sweep, backed by three new columns (`nxdomain_streak`, `orphaned_at`, `last_requested_at`); (3) dead detection/recovery live inside the §4.3 commit transaction, with re-enable defined as reset-to-NULL before applying the current scan; (4) Tranco import and campaign sync get explicit re-entry semantics (delisted → re-enable directly; dead → keep disabled but `next_check_at = now()` so recovery is verified by a real scan); (5) §3 step 4 gets a short-list/mass-delist abort guard with `--force`. All numbers are the doc's own, now config defaults.

**Spec text (verbatim):**

````markdown
RESOLUTION — disabled/dead/delisted lifecycle, end to end. Apply the following edits to the design doc / implementation spec.

============================================================
1. §4.2 — schema delta (domain table) and frontier index
============================================================
Add three columns to `domain` (after the `disabled_*` group):

```sql
  -- Lifecycle bookkeeping (§4.8)
  nxdomain_streak   SMALLINT NOT NULL DEFAULT 0,  -- consecutive dead-signal scans
  orphaned_at       TIMESTAMPTZ,                  -- when linkage was lost; starts the 30d delist grace
  last_requested_at TIMESTAMPTZ,                  -- last POST /check for this host (live-check linkage)
```

Replace the frontier index with (predicate must textually match the claim query so the partial index is used):

```sql
CREATE INDEX idx_domain_frontier ON domain (rank ASC NULLS LAST, next_check_at ASC)
  WHERE NOT disabled OR disabled_reason IN ('dead', 'delisted');
```

DELETE the §4.2 design-point bullet "Frontier eligibility (enforced in the claim query): rank IS NOT NULL OR campaign membership OR parent_id-linked children exist OR live-check origin within 7 days." Replace with:

> **Frontier eligibility is materialized, not computed at claim time.** The claim query reads only `disabled`/`disabled_reason`/`next_check_at`/`claimed_at`. Linkage (rank, campaign membership, children, recent live-check) is evaluated once per day by the lifecycle sweep (§2.6 step 1), which sets `orphaned_at` / `disabled` accordingly. Delisted, orphaned entities stop being scanned via `disabled='delisted'` after the grace period (§4.8) without being deleted.

============================================================
2. §2.5 — final claim SQL
============================================================
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
) RETURNING id, host, kind, disabled, disabled_reason, nxdomain_streak, ...;
```

`service`/`manual` rows are excluded by the predicate (they "leave the frontier entirely", §4.8). `dead`/`delisted` rows are claimable but sit on the slow lane because their `next_check_at` is +30d (below). Post-commit scheduling rule (replaces "sets next_check_at = now() + cadence(rank)"):

- if the row is still/newly disabled after the commit → `next_check_at = now() + lifecycle.slow_lane_every` (default 720h)
- else → `next_check_at = now() + cadence(rank)` (and the existing +2h/+6h recheck rules for inconsistent/error).

============================================================
3. §4.3 — commit-transaction additions (dead detection & recovery)
============================================================
The worker holds the raw engine results, so it computes, per scan, before the per-dimension loop:

```
dead_signal := base quorum was definitive
               AND base rcode was NXDOMAIN (quorum majority of resolver rcodes)
               AND the NS zone walk found no delegated zone for the host
```

(`dead_signal` requires the NXDOMAIN rcode, not merely base = no_record — NOERROR-with-no-records is a live but inactive zone and must NOT count. This is §4.8's "both A and AAAA absent AND NS walk finds no zone" made precise. The rule applies to all rows regardless of kind; for subdomains whose parent zone exists the NS walk finds a zone, so they become `inactive`, never `dead` — as intended.)

Commit algorithm, inserted around the existing §4.3 steps, all in the same transaction:

```
1. if base observation is definitive:
     if dead_signal:
         nxdomain_streak = LEAST(nxdomain_streak + 1, lifecycle.dead_streak)
     else:
         nxdomain_streak = 0
         if disabled AND disabled_reason = 'dead':        # RECOVERY
             re-enable + reset (step R below), then continue as a fresh domain
   else (error/inconsistent): nxdomain_streak unchanged, no lifecycle action.

2. run the existing per-dimension confirm/pending loop, classification, scan rows.

3. if NOT disabled AND nxdomain_streak >= lifecycle.dead_streak:   # DEAD TRIGGER
     disabled = true; disabled_reason = 'dead'; disabled_at = now()

4. next_check_at per the scheduling rule in item 2 above; claimed_at = NULL.
```

Step R — re-enable + reset ("resets state to unknown", §4.8), executed before applying this scan's observations:
- clear `disabled`, `disabled_reason`, `disabled_at`; `nxdomain_streak = 0`.
- for every core dimension d: `d_status = NULL, d_observed = NULL, d_pending = NULL, d_pending_count = 0, d_since = NULL`.
- informational columns (`dnssec_observed`, `ptr_observed`, `smtp_observed`, `parity_observed`, `latency_v4_ms`, `latency_v6_ms`) → NULL.
- `classification = 'unknown'`, `class_flags = '{}'`, `gold = false`. Keep `asn_id`/`country_id` (refreshed by the scan anyway).
- NO changelog rows are written for the reset itself, and none for the first post-reset commits either: the current scan's observations then flow through the normal §4.3 algorithm against NULL confirmed values, so the first definitive value commits immediately with `old_value` NULL → changelog suppressed (existing first-scan rule). A domain returning from the dead reappears with a fresh status and a clean changelog.

While a row is disabled (`dead`/`delisted`) its slow-lane scans still commit through the normal §4.3 machinery (confirmed state stays maintainable); public exposure is handled purely by query filters — see item 7.

============================================================
4. §2.6 — daily tick gains a lifecycle sweep (new step 1; renumber the rest)
============================================================
> 1. **Lifecycle sweep** (one transaction, set-based over `rank IS NULL` rows — tens of thousands, cheap):
>    a. Compute linkage for every non-disabled `rank IS NULL` row:
>       `linked := EXISTS campaign_domain row OR EXISTS child (parent_id = id) OR last_requested_at >= now() - lifecycle.live_check_linkage`.
>    b. Linked rows (and any row with `rank IS NOT NULL`): `orphaned_at = NULL`.
>    c. Unlinked rows with `created_by = 'live_check'`: disable immediately —
>       `disabled = true, disabled_reason = 'delisted', disabled_at = now(), next_check_at = now() + lifecycle.slow_lane_every`.
>       (No 30-day grace: the §5.3 contract is a 7-day frontier linkage; `last_requested_at` has already expired. The grace exists for Tranco rank flapping, and these rows were never publicly listed.)
>    d. Other unlinked rows: `orphaned_at = COALESCE(orphaned_at, now())`; where
>       `orphaned_at < now() - lifecycle.delist_grace` → disable as in (c).
>    Grace-period rows (orphaned_at set, not yet disabled) keep normal-cadence scanning; `ORDER BY rank NULLS LAST` already deprioritizes them.
>
> This sweep is the single owner of orphan detection — Tranco import and campaign sync never set `orphaned_at`; they only clear state on re-entry (item 5). Campaign-membership removals and live-check expiry are therefore picked up within 24h, which satisfies the 30-day/7-day windows with margin.

============================================================
5. §3 / §7 — re-entry semantics (auto re-enable)
============================================================
§3 step 4 upsert, for rows present in today's list:

```sql
INSERT ... ON CONFLICT (host) DO UPDATE SET
  rank = excluded.rank,
  orphaned_at = NULL,
  disabled    = CASE WHEN domain.disabled_reason = 'delisted' THEN false ELSE domain.disabled END,
  disabled_reason = CASE WHEN domain.disabled_reason = 'delisted' THEN NULL ELSE domain.disabled_reason END,
  disabled_at = CASE WHEN domain.disabled_reason = 'delisted' THEN NULL ELSE domain.disabled_at END,
  next_check_at = CASE WHEN domain.disabled_reason IN ('delisted','dead') THEN now() ELSE domain.next_check_at END,
  updated_at  = now();
```

Semantics: **delisted** → re-enabled directly (confirmed state was never reset, it is merely ≤30d stale; the immediate rescan refreshes it — no changelog implications beyond real transitions). **dead** → stays disabled but `next_check_at = now()`, so the next claim runs a real scan and recovery goes through §4.3 step R only if the domain actually resolves — re-listing alone never resurrects a dead domain. **service/manual** → rank updated, remains disabled and out of the frontier.

§7 campaign sync, on membership addition to an existing row: same rule (`delisted` → re-enable + `next_check_at = now()`; `dead` → keep disabled, `next_check_at = now()`; `service`/`manual` → unchanged). On membership removal: delete the membership row only; the lifecycle sweep handles orphaning.

§5.3 POST /check: every request for an existing host sets `last_requested_at = now()` (this is the "live-check origin within 7 days" linkage, and it also extends the frontier life of any rank-NULL row a user actively watches). If the row is disabled with reason `'delisted'` → re-enable as above with `next_check_at = now()`. If `'dead'` → leave disabled; the check-job consumer runs the full engine and commits through the standard §4.3 path, so step R re-enables it if it resolves. `'service'`/`'manual'` → the live check runs and returns its result, but never re-enables. New hosts get `created_by = 'live_check'`, `rank NULL`, `last_requested_at = now()` as already specified.

============================================================
6. §3 step 4 — import sanity guard
============================================================
Before applying rank changes, compute `valid_rows` (post-rejection) and `would_delist` (ranked yesterday, absent today). Abort the import when `valid_rows < tranco.min_rows` OR `would_delist > tranco.max_delist_pct%` of currently-ranked rows, unless `v6ctl tranco import --force`. On abort: keep yesterday's ranks untouched, write a `tranco_import` row with `aborted = true` and a reason in `note`, and fire the ops webhook. Add to the §4.9 `tranco_import` DDL:

```sql
  aborted BOOLEAN NOT NULL DEFAULT FALSE,
  note    TEXT,
```

============================================================
7. §4.8 — visibility rule made explicit
============================================================
Amend "Disabled domains are excluded from classification, lists, and stats" to: excluded from all public list/detail-list/stats/changelog queries — every public query joins or filters `NOT disabled` (changelog endpoints join `changelog → domain` and filter there; rows written during a disabled period simply become visible again on re-enable, and for `dead` there are none because of the step-R reset). `GET /domain/{host}` for a disabled domain remains the one exception if §5.1 compatibility requires it to resolve (it returns the row; frontends never link to it).

============================================================
8. Config keys (crawler config, with defaults — all values are the doc's own numbers)
============================================================
```yaml
lifecycle:
  dead_streak: 7            # consecutive dead-signal scans before disabled_reason='dead'
  slow_lane_every: 720h     # revalidation cadence for disabled dead/delisted rows (30d)
  delist_grace: 720h        # orphaned_at age before rank-NULL rows are disabled (30d)
  live_check_linkage: 168h  # frontier lifetime granted by a POST /check (7d)
tranco:
  min_rows: 950000          # abort import below this many valid rows
  max_delist_pct: 2.0       # abort if more than this % of ranked rows would delist
```
````

*Note: F14 (POST /check lifecycle, below) supersedes one detail here — under its Rule 0, the check-job consumer never commits through §4.3, so a `dead` domain's recovery happens via its pulled-in frontier scan rather than the live-check run itself. Fold both resolutions with F14's Rule 0 taking precedence for the consumer's write behavior.*

---

### B3. Legacy 3-string contract: no mapping for `not_applicable`/NULL confirmed values or 6-valued log observations; `ts_updated`/`ts_curl` and the kept `v6_ready` formula are unresolved

- **Section:** §5.1 (vs §4.1/§4.2/§4.4, §2.2, §9 OPEN-2)
- **Confidence:** high · **Needs user decision:** no

**Description.** §5.1 locks legacy status strings to exactly `supported|unsupported|no_record`, but never specifies how the new model's `not_applicable` (null-MX, www-NXDOMAIN per OPEN-2, forced-n/a on `kind=subdomain`) and NULL (never-confirmed) values serialize on the compat endpoints — cases production never produces, so golden parity tests give no answer, and the frozen frontend breaks visibly on any fourth string. Separately: `/domain/{domain}/log` serves raw 6-valued scan observations, contradicting §2.2/§10's "error/inconsistent never become public"; and the kept `v6_ready` formula (strict `www='supported'` per production `campaign.sql`) collides with forced `www=not_applicable` on subdomain entities, permanently pinning subdomain-heavy campaigns at 0% against the doc's own "not_applicable never counts against" principle. (The `ts_curl`/`ts_updated` key mapping is an inferable sub-point, pinned in the resolution anyway.)

**Resolution.** A normative "Legacy serialization rules" subsection in §5.1: one shared projection function (`not_applicable` and NULL → `"no_record"` on all legacy endpoints); the log endpoint excludes error/inconsistent rows by filtering (a documented exception that enforces the never-public rule) with a synthetic epoch-seconds id; the timestamp key table is pinned with Go-zero-time NULL serialization; `v6_ready` is amended to `www IN ('supported','not_applicable')` under the existing OPEN-6 methodology-v2 mechanism; legacy changelog rows that collapse to identical strings after mapping are dropped. Synthetic parity fixtures cover the branches production data cannot exercise.

**Spec text (verbatim):**

````markdown
### §5.1 addendum — Legacy serialization rules (normative, part of openapi.yaml + parity fixtures)

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
````

---

### B4. Hero gate contradicts the "NULL/not_applicable skipped" preamble, and timeout-broken IPv6 maps to `error` so `conn` can never confirm — the canonical broken-v6 case is misclassified either way

- **Section:** §4.3 (ladder) / §2.2
- **Confidence:** high · **Needs user decision:** no

**Description.** §4.3's ladder preamble ("not_applicable and NULL-confirmed dimensions are skipped, never counted against") contradicts rule 3's strict "ns = supported AND conn = supported"; `conn` genuinely reaches confirmed `not_applicable` (runner skip path + first-scan immediate commit, plus the base-N=2/conn-N=3 window) and stays NULL under persistent errors, so the two readings yield different hero lists. Simultaneously, the verbatim-lifted `https_ipv6` maps timeouts to `error`, and §4.3 error observations never advance confirmed state — so a blackholed AAAA (the dominant real broken-v6 mode) can never confirm `conn=unsupported`: a fresh blackholed domain is either hero-with-dead-v6 or permanently partial with no `broken_v6` flag, and an existing hero that blackholes stays hero forever. Both outcomes violate brief §5.5's mandatory "pure IPv6-only connection succeeds" hero bar.

**Resolution.** (1) Rule 3's enumerated sets are exhaustive and override the preamble; the preamble means only "never triggers shame/flags". Hero requires `conn` CONFIRMED `supported`. (2) Preflight-guarded persistent timeouts are definitive: engine error with `error_type=timeout` maps worker-side to observation `unsupported` iff the process preflight passed within 5 minutes; N=3 anti-flap still applies. (3) The full classification truth table goes in the spec; §2.2's "failure ⇒ broken_v6 flag" is corrected to "confirmed unsupported ⇒ broken_v6"; `http_ipv6` gains the same error_type classification as an enumerated lift deviation.

**Spec text (verbatim):**

````markdown
### Amendments to fold into the implementation spec

#### A. §4.3 — ladder preamble (REPLACE the sentence "evaluated over confirmed values only; `not_applicable` and NULL-confirmed dimensions are skipped, never counted against" with:)

Classification ladder (§5.5, deterministic, first match wins), evaluated over **confirmed** values only. **The value sets enumerated in each rule are exhaustive**: a dimension satisfies a rule only if its confirmed value is explicitly listed. `not_applicable` and NULL confirmed values never *shame* a domain (they never trigger `sinner` and never set a sub-reason flag), but they also never *satisfy* the hero bar unless the rule lists `not_applicable` (it does for `www` and `mx`, deliberately not for `ns` and `conn`). Consequence: a domain whose `conn` is NULL (persistent errors) or confirmed `not_applicable` (transition window) is `partial` with **no** flag — hero requires demonstrated IPv6-only reachability, and `broken_v6` requires demonstrated failure; neither may be assumed.

#### B. §4.3 — classification truth table (ADD; normative, replaces prose derivation)

Inputs are confirmed values; each ∈ {`supported`, `unsupported`, `no_record`, `not_applicable`, NULL}. First match wins:

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

#### C. §2.2 / §2.5 — `conn` observation mapping (ADD; this is the timeout rule)

The `conn` observation is derived **worker-side** from the engine result of `https_ipv6` (or `http_ipv6` on the http-only fallback path):

| Engine result | `conn` observation |
|---|---|
| `supported` | `supported` |
| `unsupported` (connection refused, TLS/certificate error, no AAAA*) | `unsupported` |
| `not_applicable` (phase-2 skip: no AAAA observed this scan) | `not_applicable` |
| `error` with `details.error_type = "timeout"` AND the process preflight (§2.5) passed within the last 5 minutes | `unsupported` — a persistent connect/response timeout against a published AAAA **is** the canonical broken-v6 failure and must be definitive; the raw `error_type = "timeout"` stays in the scan payload for the detail page |
| `error` with `error_type = "timeout"` but preflight stale/failed | `error` (non-definitive; worker should not be claiming anyway per §2.5) |
| `error`, any other `error_type` (`unknown`, blocked address, internal) | `error` (non-definitive: touches nothing, `recheck_error` applies) |

*The engine's "no AAAA ⇒ unsupported" branch inside the checker is unreachable in practice because phase-2 gating already skips `conn` when no AAAA exists; if it fires (AAAA disappeared mid-scan), treat as `not_applicable`, matching the skip path.

The §2.5 belt-and-suspenders rule is restated to cover this explicitly: **every** `conn = unsupported` observation — whether from connection-refused, TLS failure, or timeout — requires the preflight to have passed within the last 5 minutes; otherwise the observation is downgraded to `error`. The N=3 anti-flap for `conn` (§2.3) is unchanged and is the flap guard: a single slow (>10s) response never demotes a hero; only three consecutive daily timeouts confirm `unsupported`, write the changelog entry, and raise `broken_v6`.

#### D. §2.1 — enumerated lift deviation (ADD to the "only the UA constant changes" list)

`http_ipv6.go` is extended during the lift with the same terminal error classification `https_ipv6.go` already has (`isTimeout` → `details.error_type = "timeout"`; keep connection-refused → `unsupported`; everything else → `error_type = "unknown"`), so the §C mapping table applies identically on the http-only fallback path. This is the second enumerated deviation alongside the UA constant and the miekg/dns v2 resolver port.

#### E. §2.2 — correction to the `conn` row

Replace "failure ⇒ `broken_v6` flag" with: "confirmed `unsupported` ⇒ `broken_v6` flag (refused / TLS failure / preflight-guarded persistent timeout, each after N=3); non-definitive errors never set the flag and never advance confirmed state."
````

---

### B5. `resources` dimension has two contradictory computation paths (inline lift-verbatim check vs decoupled registry sweep), and the roll-up is undefined for NULL/no_record/not_applicable hosts

- **Section:** §4.6 vs §2.1/§2.2/§4.3/§4.4 (§8 phase 5, §10)
- **Confidence:** high · **Needs user decision:** no

**Description.** §2.1/§2.2 put `resource_ipv6` in the lift-verbatim table (inline AAAA checks, own status, partial→unsupported mapping), while §4.6/§6/§10 specify a decoupled `resource_host` registry sweep whose confirmed per-host statuses are rolled up per domain — and the doc never says which produces the NOT NULL `scan.resources` observation feeding §4.3's commit. The verbatim lift also cannot populate the registry (Details truncated to 20 hosts, no full host list returned). Under the decoupled reading, §4.6's roll-up rule has no branch for hosts with NULL `aaaa_status` (guaranteed on discovery day) or `no_record`/`not_applicable` hosts, and the crawler's value for `scan.resources` in phases 2–4 (before phase 5 ships) is unstated. This determines the gold badge and the forever-changelog.

**Resolution.** Adopt the decoupled registry path (three of four doc sections plus §2.7's dedup arithmetic already presuppose it, and the inline path is factually incapable of feeding the registry). `resource_ipv6` becomes discovery-only (`resource_discovery`, adapted); the roll-up gets explicit branches (NULL host → observation `error`, defers; dead references excluded; empty-after-exclusion → `not_applicable`); the stacked host-N=2/domain-N=3 hysteresis is kept deliberately; pre-phase-5 behavior runs behind `crawler.resources.enabled=false` (scan writes `not_applicable`, domain columns stay NULL, so gold is correctly false for everyone).

**Spec text (verbatim):**

````markdown
The following edits are folded into the design/spec. Section references are to backend-design.md.

--- 1. §2.1 — move resource_ipv6 out of "lift verbatim" ---

Delete the `internal/checker/resource_ipv6.go` row from the Lift-verbatim table. Add to the **Adapt** paragraph:

> `resource_ipv6.go` → adapted into `resource_discovery`: keep the IPv6-pinned page fetch (2 MB body cap, 15 s timeout, ≤3 redirects), the streaming HTML tokenizer (script/img/link/iframe/source/video/audio/object/embed + `<base href>`), external-host dedup, and the 50-host cap. **Delete** the inline concurrent AAAA checks and the supported/partial/unsupported derivation (lines 88–147 of the v6audit file) — host AAAA status lives in the `resource_host` registry (§4.6). The check returns the **full** deduped host list (≤50, no 20-item truncation) plus `{total_hosts}` in Details, and a discovery status: `ok` (fetch succeeded; list may be empty), `not_applicable` (phase-2 gate: no AAAA on the domain), `error` (fetch/TLS failure).

--- 2. §2.2 — replace the `resources` table row ---

| Dimension (public) | Engine source | `partial` maps to | Role |
|---|---|---|---|
| `resources` (#23 dependencies) | registry roll-up (§4.6) over linked hosts' **confirmed** `resource_host.aaaa_status`; link discovery via adapted `resource_discovery` + manual `v6ctl resource add` | n/a — the roll-up is defined directly in 4-valued terms, no engine `partial` exists on this path; `resources_v4only` flag set when **confirmed** resources = `unsupported` | **core for Gold badge only** — never affects hero/sinner |

--- 3. §2.7 — budget-table correction ---

Remove `resources ≤50·25%·(dedup ≈ ~8 effective)` from the per-crawl local-resolver row; the per-crawl cost of resources is the discovery page fetch only (already counted in the HTTP-fetch row). Add a standalone row:

| Resource-host sweep (registry AAAA, bulk resolver) | ~100–300k lookups/day | ~2–4 qps | negligible |

--- 4. §4.6 — replace the "Crawler:" bullet with the full algorithm ---

**Discovery (per domain scan, phase 2, inside the §4.3 commit transaction):**
1. `resource_discovery` returns (hosts, status). If status = `error`: keep existing links untouched (a failed fetch is not evidence dependencies changed) and skip to the roll-up. If `not_applicable`: skip to the roll-up.
2. If `ok`: for each host — `INSERT INTO resource_host (host) VALUES (lower_punycode) ON CONFLICT (host) DO NOTHING` (new rows get `aaaa_status NULL`, `next_check_at = now()`, so the sweep confirms them within one day); upsert `domain_resource (source='discovered', required=TRUE)` or refresh `last_seen = now()` (never downgrade `source='manual'`).
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

--- 5. Phasing + config (add to §4.6 and §8 phase notes) ---

Config key `crawler.resources.enabled` (bool, default `false`; flipped to `true` at phase-5 deploy). While `false`, the crawler: skips `resource_discovery` entirely, writes `resources = not_applicable` to every `scan` row (satisfying the NOT NULL column), and **excludes the resources dimension from the §4.3 commit loop** — domain `resources_status/pending/pending_count/since/observed` stay NULL. Consequently `gold = hero AND resources ∈ {supported, not_applicable}` evaluates false for all domains until phase 5, which is correct: no gold badges before the feature ships. At phase-5 flip, resources confirm via the normal first-observation rule (one clean scan after the first sweep pass).
````

---

### B6. `GET /metric/overview` requires `top_heroes` and `top_nameserver`, but `stats_global_daily` has no such columns and their semantics are defined nowhere

- **Section:** §5.1 vs §4.7
- **Confidence:** high · **Needs user decision:** no

**Description.** §5.1's `GET /metric/overview` contract requires `top_heroes` and `top_nameserver` (both actively rendered by `MetricCrawler.vue`) and claims they map from the latest `stats_global_daily` row — but the §4.7 DDL defines no such columns, and neither the design doc nor the brief defines their semantics anywhere. The only definition exists in production SQL (`stats.sql:10-11`, with `!= 'unsupported'` and `rank < 1000` quirks), forcing the implementer to invent both a schema change and a product-relevant formula choice (bug-compatible vs new-model) on a compat-locked endpoint. The remaining data-key mapping is also unstated.

**Resolution.** Add both columns to `stats_global_daily`, computed in the nightly snapshot, with pinned formulas following the doc's decided OPEN-6 pattern (value-level metric fixes deliberate and announced; response shapes bug-compatible): fix `rank < 1000` → `rank <= 1000` and `!= 'unsupported'` → `= 'supported'`; keep the metric web-facing (base+www) rather than switching to the §5.5 hero classification, since the frontend copy reports nameservers separately. Pin the full 8-key `data` mapping and the seed-migration day-0 row requirement.

**Spec text (verbatim):**

````markdown
### Amendment to §4.7 — stats_global_daily gains two top-1k columns

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
```

Computed by the nightly snapshot job over the same population as every other
stats_global_daily counter (all non-disabled domains; `rank <= 1000` implies
rank IS NOT NULL, so unranked campaign domains and subdomains are excluded):

```sql
count(*) FILTER (WHERE rank <= 1000
                   AND base_status = 'supported'
                   AND www_status IS DISTINCT FROM 'unsupported') AS top_heroes,
count(*) FILTER (WHERE rank <= 1000
                   AND ns_status  = 'supported')                  AS top_nameserver
```

Semantics (add to the OPEN-6 "methodology v2" note as two more deliberate,
announced metric fixes):
- `top_heroes` = Tranco top-1000 domains whose website is reachable over IPv6:
  confirmed base = `supported`, and www does not *contradict* it. Per OPEN-2,
  www `not_applicable`, `no_record`, and NULL (never confirmed) never count
  against — only confirmed www = `unsupported` excludes. This is NOT the §5.5
  hero classification (no ns/conn/mx requirement): the metric measures
  web-facing IPv6 only, matching the production metric's intent and the
  frontend copy, which reports nameserver readiness as a separate number.
- `top_nameserver` = Tranco top-1000 domains with confirmed ns = `supported`.
- Two production quirks are fixed, not reproduced:
  (1) `rank < 1000` → `rank <= 1000` (production counted 999 domains; all
      frontend copy says "top 1000");
  (2) `base != 'unsupported'` → `base = 'supported'` (production counted
      `no_record`/inactive domains as IPv6-enabled, inflating the number).

### Amendment to §5.1 — GET /metric/overview, fully pinned

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
- `heroes` maps from the `heroes` column (§5.5 classification count — the
  membership change vs production's `base+www supported` formula is the
  already-decided OPEN-6 break).
- All eight `data` keys are required (frontend types/Metric.ts and
  MetricCrawler.vue read every one); values are plain JSON numbers.
- If `stats_global_daily` is empty (first boot before the first nightly
  snapshot), the snapshot job must be run as part of seed migration so the
  endpoint always has a row — per OPEN-6 "serve migrated seed values
  immediately", the seed migration writes day-0 rows for all stats_* tables.
````

---

## 3. Major findings

### M1. `conn` dimension composition rule (https + http fallback) is never defined — the hero-gating dimension is invented by the implementer

- **Section:** §2.2 (conn row)
- **Confidence:** high · **Needs user decision:** no

**Description.** §2.2 defines the hero-gating `conn` dimension only as "`https_ipv6`, fallback `http_ipv6` if the site is http-only", but no combiner exists in the lifted v6audit engine (both checks are independent phase-2 siblings), so the composition function is new code the doc never specifies — on the single most product-critical dimension, in a doc that insists every dimension's mapping be explicit. Most cases are constrained by side remarks (cert-error must not fall back per the §2.2 tls-row aside; `connection_refused` is the only observable "http-only" signal), but https=error(timeout)+http=supported is genuinely open and decides hero eligibility. The target-host sub-claim was refuted: §2.1's verbatim lift pins `conn` to the entity's own host, and apex-only dialing is provably hero-neutral.

**Resolution.** A deterministic first-match decision table in §2.2: https wins outright; the http fallback applies only on `connection_refused` + http supported (with an `http_only` payload flag); certificate errors are never rescued; https `error` is never overridden by http (conservative, per §4.3's "touch nothing"); target host is always the entity's own host, no www fallback; the bulk-resolver AAAA re-resolution skew is stated as accepted.

**Spec text (verbatim):**

````markdown
### §2.2 addition — `conn` dimension: composition rule (replaces the one-liner "`https_ipv6`, fallback `http_ipv6` if the site is http-only")

`conn` is a **derived dimension with no single engine source** — v6audit has no
combiner (`https_ipv6` and `http_ipv6` are independent phase-2 siblings in
`runner.go`), so this composition function is new code in the worker, applied
after `Runner.Run` and before the §4.3 commit.

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
| 4 | H = `unsupported` (any other case: connection_refused with P ≠ supported; or no-AAAA-on-host) | `unsupported` | ⇒ `broken_v6` flag once confirmed (§4.3/§5.5) |
| 5 | H = `error` (error_type `timeout`, `unknown`, or blocked-address) | `error` | **never overridden by P, even P = supported** — non-definitive per §4.3 ("touch nothing"); recorded in scan log only, never advances confirmed state |
| 6 | H = `not_applicable` (phase 2 skipped: no AAAA on base or www) | `not_applicable` | |

"HTTP-only site" is thereby **operationally defined** as: port 443 actively
refuses (ECONNREFUSED) while port 80 serves over IPv6. A 443 that blackholes
(firewall DROP → timeout) is non-definitive: `conn = error` on every scan, the
confirmed `conn` never advances, and per §4.3 a NULL-confirmed `conn` is
skipped by the classification ladder (such a domain can still reach hero on
its other dimensions; a previously confirmed `conn` value simply persists).
Rows 2/3 cannot conflict and row "no-AAAA + P=supported" cannot occur: both
checks issue the identical AAAA lookup on the same host through the same
TTL-cached resolver.

**Target host:** `conn` always dials **the entity's own host** — the apex for
`kind = domain`, the subdomain itself for `kind = subdomain`. This is the
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
````

*Note: fold together with B4's amendment C — B4's timeout rule refines row 5 of this table (preflight-guarded persistent timeouts become definitive `unsupported`). The two resolutions were written to compose: B4 governs the timeout branch, this table governs everything else.*

---

### M2. §4.3 commit machine: contradictory first-confirmation changelog behavior, contradictory transaction boundaries, and no lease fencing against double-commit

- **Section:** §4.3 / §4.4 / §2.5
- **Confidence:** high · **Needs user decision:** no

**Description.** Three confirmed defects (the bootstrap-branch complaint was refuted — §4.3's prose answers it; it survives only as a pseudocode nit). (1) §4.3 says the first-confirmation changelog row is suppressed at write; §4.4's DDL comment says it is written with NULL `old_value` and filtered at read — materially different (0 vs ~6M bootstrap rows in the forever table, plus a filter §5.1 never mentions). (2) §2.5's "→ batch-write scan rows with pgx.Batch/CopyFrom" reads as separating scan-row writes from the per-domain transaction §4.3 mandates; CopyFrom cannot preserve per-domain atomicity, and `result_id`'s "idempotent re-submits" comment has no enforcing constraint. (3) Nothing fences an expired-lease worker from double-committing alongside the reclaimer — the exact "no double changelog" invariant phase 2(d) verifies with no mechanism specified.

**Resolution.** Suppression-at-write wins (`old_value` becomes NOT NULL); explicit bootstrap branch and IS DISTINCT FROM semantics in the pseudocode; seed-imported statuses are confirmed values; batching is one pgx.Batch per domain inside that domain's own transaction, never CopyFrom; `result_id` is dropped; the commit's domain UPDATE carries `AND claimed_at = $L` with RowsAffected=0 rolling back the whole per-domain transaction.

**Spec text (verbatim):**

````markdown
### §4.3 (replace pseudocode block and the "First-ever scan" paragraph)

Per scanned domain the worker builds ONE commit unit. Inputs:
- `L` — the lease value: `claimed_at` as stamped by the claim query (see §2.5 change below). One `UPDATE ... SET claimed_at = now()` stamps every row in a batch with the same value, so L is a single per-batch token held by the worker.
- `T` — the commit timestamp, `time.Now()` fixed once per domain and used for the scan row, scan_detail row, changelog rows, `*_since`, and `last_checked_at`.
- The domain's state columns as returned by the claim query (the claim's RETURNING list includes all `d_status`/`d_pending`/`d_pending_count` groups); the lease fence guarantees this snapshot is still authoritative at commit time.

```
# All state computed client-side from the claimed snapshot.
# All equality comparisons use IS DISTINCT FROM semantics: NULL never equals anything.

for each core dimension d in {base, www, ns, mx, conn, resources}:
  O = observation(d)                       # quorum already applied for base/www
  if O in {error, inconsistent}:           # non-definitive: touch nothing
      d_observed = O; continue             #   (status, pending, since all survive)
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
  d_observed = O

recompute classification + class_flags + gold from confirmed d_status values (§5.5 ladder)

# One pgx.Tx per domain; all statements queued as one pgx.Batch (single round trip):
BEGIN
  UPDATE domain SET <all state cols>, classification, class_flags, gold,
         next_check_at = T + cadence(rank), last_checked_at = T,
         claimed_at = NULL, updated_at = now()
   WHERE id = $domain_id AND claimed_at = $L;          -- LEASE FENCE
  INSERT INTO changelog (domain_id, ts, field, old_value, new_value) VALUES ... ;  -- 0..6 rows, ts = T
  INSERT INTO scan (..., ts) VALUES (..., T)        ON CONFLICT (domain_id, ts) DO NOTHING;
  INSERT INTO scan_detail (domain_id, ts, details, duration_ms) VALUES ($id, T, $json, $ms)
                                                    ON CONFLICT (domain_id, ts) DO NOTHING;
if RowsAffected(domain UPDATE) == 0: ROLLBACK        -- lease lost: another worker reclaimed
else: COMMIT                                          --   this domain; write NOTHING
```

Fence semantics: a worker that stalls past the 30-minute lease and resumes after a reclaim finds `claimed_at` changed (or NULL), the fenced UPDATE matches 0 rows, and the whole transaction — scan row, scan_detail, changelog, state — is discarded. Reclaims happen ≥30 min after the original claim, so two lease values can never collide. Count fence aborts in `crawler_metrics` (add a `lease_lost` counter to `dim_counters` or a dedicated column). This is the mechanism behind phase 2(d)'s "no double changelog" verification.

First-confirmation rule (normative): the NULL→value bootstrap transition NEVER writes a changelog row — suppression happens at write time, not at read time. Consequently `changelog.old_value` is NOT NULL and the §5.1 changelog endpoints apply no first-confirmation filter. Phase-4 seed import writes production's current statuses directly into the `d_status` columns (with `d_since` from production data where available, else import time; `d_pending = NULL`, `d_pending_count = 0`) — seeded values ARE confirmed values, so the anti-flap N-consecutive rule governs the first post-cutover divergence, and a real divergence publishes an ordinary changelog entry once confirmed.

### §4.4 (DDL changes)

changelog: change
  `old_value ipv6_status,  -- NULL on first confirmation (not published)`
to
  `old_value ipv6_status NOT NULL,  -- first confirmation writes no row at all (§4.3)`

scan_detail: DROP the `result_id` column entirely. Idempotency is the worker-fixed timestamp T + `ON CONFLICT (domain_id, ts) DO NOTHING` under the existing PK; no unique constraint on result_id is possible on a hypertable and nothing consumes it.

### §2.5 (replace the pipeline sentence and extend the claim query)

Claim query: append `claimed_at` (and all confirmed-state column groups) to the RETURNING list:
  `... ) RETURNING id, host, kind, rank, claimed_at, <all d_status/d_pending/d_pending_count/d_since columns>;`

Replace
  "commit results per domain in one transaction (§4.3) → batch-write scan rows with pgx.Batch/CopyFrom (the v2 rebuild's 2M single-row round-trips/day is a known wart) → claim next batch"
with
  "commit results per domain in one transaction (§4.3); each domain's complete commit unit — fenced domain UPDATE, changelog rows, scan row, scan_detail row — is queued as a single pgx.Batch inside that domain's pgx.Tx, so a whole-domain commit costs one round trip (vs the v2 rebuild's 2M single-row round-trips/day). Batching is strictly a round-trip optimization over intact per-domain atomic units; scan rows are never split out into a separate bulk write, and CopyFrom is not used (it cannot preserve per-domain atomicity) → claim next batch."
````

*Note: F9's legacy-changelog escape hatch (below) relaxes `old_value`/`new_value` nullability for `field='legacy'` import rows only — reconcile the two DDL edits by applying F9's CHECK-constraint form, which preserves this finding's invariant for all native rows.*

---

### M3. Legacy changelog endpoints under the unified model: message ladder has no strings for conn/resources/not_applicable, feed scope and synthetic id are undefined, and historical production rows have no mapping

- **Section:** §5.1 (changelog family) vs §4.4/§4.5, §8 phase 4
- **Confidence:** high · **Needs user decision:** no

**Description.** §5.1 delegates changelog rendering to the ported `generateChangelog` ladder, which covers only (base,www,ns,mx) × {3 legacy statuses} and errors on anything else — while §4.4's changelog also emits conn/resources rows and `not_applicable` transitions. The unified entity table leaves `GET /changelog`'s scope undefined (production's separate tables made the old feed Tranco-only implicitly). §8 phase 4's "import full changelog history" + "old entries render identically" lacks the production (message, ipv6_status) → (field, old, new) reverse transform and a fallback for unmappable rows. The `id` sub-point is minor (frontend keys by index).

**Resolution.** Legacy feeds filter to legacy fields/statuses (conn/resources/not_applicable rows still written, just not served on the legacy surface — additive-reversible); scope = `rank IS NOT NULL` for the global feed, membership join for campaign feeds; synthetic epoch-ms id; an exact reverse-transform table with a canonical ambiguous-old rule (provably render-identical) and a `field='legacy'` verbatim escape hatch that makes phase 4's verification achievable unconditionally.

**Spec text (verbatim):**

````markdown
### Spec addition — legacy changelog endpoints under the unified model (resolves §5.1 changelog row / §4.4 / §8 phase 4)

#### A. Canonical message ladder (single implementation, API layer)

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

`conn`/`resources` rows and any transition involving `not_applicable` ARE written to the `changelog` table (§4.3/§4.4 unchanged — they remain queryable, appear in datasets, and are available to the future v2 API) but are NOT served by the legacy endpoints. Rationale: production never emitted them, the frontend is frozen, and exposing them later is purely additive. `field='legacy'` rows (see D) bypass the ladder: `message = legacy_message`, `ipv6_status = legacy_status`.

#### B. Feed scope and domain_url (per endpoint)

All feeds: `ORDER BY c.ts DESC, c.domain_id DESC, c.field ASC`; `?offset=`/`?limit=` (default 50, max 100). Filter from A applies everywhere.

| Endpoint | Scope | domain_url |
|---|---|---|
| `GET /changelog` | `JOIN domain d ON … WHERE d.rank IS NOT NULL` (Tranco apexes only — reproduces production's implicitly-Tranco feed; campaign-only, live_check, and subdomain entities excluded) | `"/domain/{host}"` |
| `GET /changelog/campaign` | `JOIN campaign_domain cd ON cd.domain_id = c.domain_id JOIN campaign ON …` — all campaigns, rank irrelevant. A domain in N campaigns yields N rows per change (production duplicated these rows per campaign too — accepted) | `"/campaign/{shortuuid(campaign.uuid)}/{host}"` |
| `GET /changelog/{domain}` | entity resolved by host, any kind/rank; 404 if unknown host or zero rows (production behavior — the empty-list→`[]` cleanup in §5.1 does NOT apply to the two per-domain 404 cases) | `""` (field present, empty string — production struct has no omitempty) |
| `GET /changelog/campaign/{uuid}` | membership join filtered to the decoded campaign | `"/campaign/{shortuuid}/{host}"` |
| `GET /changelog/campaign/{uuid}/{domain}` | membership check + host; 404 on zero rows | `""` |

Response row (unchanged from production): `{id, ts, domain, domain_url, message, ipv6_status}`.

#### C. `id`

No identity column is added to the `changelog` hypertable. `id` is synthetic: **epoch milliseconds of `ts`** (int64) — same precedent as `GET /domain/{domain}/log`. The frontend keys rows by array index and never dereferences `id`; collisions are harmless. Pagination stability comes from the deterministic ORDER BY above.

#### D. §4.4 DDL delta (legacy escape hatch)

```sql
-- changelog: two added nullable columns + widened field domain
--   field: base|www|ns|mx|conn|resources|legacy
ALTER TABLE changelog
  ADD COLUMN legacy_message TEXT,   -- verbatim production message (field='legacy' only)
  ADD COLUMN legacy_status  TEXT,   -- verbatim production ipv6_status (field='legacy' only)
  ALTER COLUMN new_value DROP NOT NULL,
  ADD CONSTRAINT changelog_legacy_chk
    CHECK ( (field = 'legacy') = (legacy_message IS NOT NULL) ),
  ADD CONSTRAINT changelog_new_value_chk
    CHECK ( field = 'legacy' OR new_value IS NOT NULL );
```

(Fold into the CREATE TABLE in §4.4 rather than ALTERing.) Native (post-cutover) rows never set the legacy columns.

#### E. §8 phase 4 — import transform

Sources: production `changelog` (id, ts, domain_id, message, ipv6_status) and `campaign_changelog` (adds campaign_id; domain_id references campaign_domain.id — resolve via campaign_domain.site → new entity id). campaign_id is dropped on import; the campaign feed re-derives membership by join.

1. Resolve host → new `domain.id`. Rows whose host no longer resolves to an entity: create the entity (rank NULL, `created_by='import'`) — history must not be orphaned.
2. Reverse-map `message` by **prefix match, longest/www-variant first** (each pattern implies field/old/new; `{h}` must equal the row's resolved host as a suffix check):
   - `IPv6 enabled for www.` → (www, unsupported, supported); `IPv6 enabled for ` → (base, unsupported, supported)
   - `IPv6 lost for www.` → (www, supported, unsupported); `IPv6 lost for ` → (base, supported, unsupported)
   - `IPv4-only for www.` → (www, no_record, unsupported); `IPv4-only for ` → (base, no_record, unsupported)
   - `No DNS records found for www.` → (www, unsupported, no_record); `No DNS records found for ` → (base, unsupported, no_record)
   - `IPv6 enabled nameserver for ` → (ns, unsupported, supported); `Nameservers degraded to IPv4-only for ` → (ns, supported, unsupported); `IPv4-only nameservers for ` → (ns, no_record, unsupported); `No NS records found for ` → (ns, unsupported, no_record)
   - `IPv6 enabled MX records for ` → (mx, unsupported, supported); `MX records degraded to IPv4-only for ` → (mx, supported, unsupported); `IPv4-only MX records for ` → (mx, no_record, unsupported); `No Mail records found for ` → (mx, unsupported, no_record)

   **Canonical ambiguous-old rule:** production collapsed `unsupported→supported` and `no_record→supported` into one string, and any-old→`no_record` into one string; the importer canonically records `old='unsupported'` in those cases. This is render-safe by construction: the forward ladder (A) emits the identical string for every old value the original row could have had.
3. **Cross-check:** derived `new_value` must equal the row's stored `ipv6_status`. Mismatch, no pattern match, or `ipv6_status` outside the three legacy statuses → legacy path: insert `(domain_id, ts, field='legacy', old_value=NULL, new_value=NULL, legacy_message=message, legacy_status=ipv6_status)`. The API filter in A explicitly admits `field='legacy'` rows into all feeds their entity qualifies for (add `OR c.field = 'legacy'` to the filter) and renders them verbatim — this is what makes phase 4's "old entries render identically" verification achievable unconditionally.
4. PK conflicts `(domain_id, ts, field)` (possible when both changelog tables carry the same change, or two legacy rows share a timestamp): if the colliding rows are value-identical, keep one; otherwise bump ts by +1 microsecond until unique (display truncates to seconds; ordering impact nil).
5. **Verification for phase 4:** for every imported row, `renderChangelog(field, old_value, new_value, host)` (or the legacy passthrough) must byte-equal the original production `(message, ipv6_status)`. This is the parity gate.

#### F. Cutover note (seeded state)

Phase 4 seeds confirmed `base/www/ns/mx` statuses (+ `*_since` from production `ts_*`) but `conn`/`resources` seed NULL. Per §4.3, each domain's first definitive post-cutover observation of those dimensions commits immediately with `old_value` NULL → the changelog row is suppressed from all feeds. Consequence to document: no changelog flood at cutover, and detail pages show conn/resources as unconfirmed for up to one crawl cycle (~1 day) after launch.
````

---

### M4. Non-definitive observation policy under-specified: SERVFAIL/REFUSED unmapped, recheck scope/precedence undefined, accelerated rechecks defeat the N-consecutive anti-flap, no backoff or provider circuit breaker, dead trigger deviates from the brief

- **Section:** §2.3 / §2.4 / §2.5 (config) / §4.8 (brief §6)
- **Confidence:** high · **Needs user decision:** no

**Description.** Four interacting gaps: (1) §2.3's per-resolver reduction has no branch for SERVFAIL/REFUSED — common, since all three validating resolvers SERVFAIL on DNSSEC-broken zones — and the error-vs-inconsistent choice selects between the 2h and 6h lanes. (2) The recheck knobs are per-domain but §4.3's outcomes are per-dimension; scope and precedence are unstated. (3) §4.3 advances pending counts on every definitive scan regardless of spacing, so a domain pulled to 2h rechecks can confirm a flip in ~4h — contradicting §2.3's advertised "+1/+2 days" anti-flap during exactly the unstable episodes it targets. (4) No backoff or rate guard exists, and §4.8's dead trigger is NXDOMAIN-only while brief §6 says "dead (NXDOMAIN/SERVFAIL)" — permanently SERVFAIL'ing domains loop 4–12×/day forever, and a resolver-degradation episode can amplify provider load ~12× unguarded.

**Resolution.** SERVFAIL/REFUSED/timeouts are non-answers (quorum over remaining valid answers; ≤1 valid answer → `error`); pull-ins scoped to base/www with inconsistent > error precedence; a 12h `min_confirm_spacing` counting gate preserves the "+1/+2 days" guarantee; exponential backoff via `error_streak`; dead trigger extended to NXDOMAIN-or-all-resolver-SERVFAIL per the brief; per-provider token buckets plus fast-lane and per-provider circuit breakers in `consensus/`.

**Spec text (verbatim):**

````markdown
AMENDMENT A — §2.3 quorum reduction (replace the reduction sentence and bullet list):

Each resolver's response is first classified as a valid answer or a non-answer:
- valid answer: rcode NOERROR → `supported` (≥1 globally-routable AAAA) or `unsupported` (no AAAA); rcode NXDOMAIN → `no_record`/`not_applicable`.
- non-answer: any other rcode (SERVFAIL, REFUSED, …), timeout, or transport error — after the single retry. SERVFAIL is "the resolver could not determine an answer" (e.g. broken DNSSEC on all three validating resolvers); it is never a vote.

Quorum over valid answers only:
- ≥2 valid answers agree → that status is the observation (3-0, 2-1, or 2-0 with one non-answer).
- ≥2 valid answers, no two agree → observation = `inconsistent`.
- ≤1 valid answer (≥2 resolvers non-answering) → observation = `error`.

`inconsistent` and `error` are both non-definitive (never advance confirmed state, never write changelog) but schedule differently: `inconsistent` → 2h lane, `error` → 6h lane (Amendment B). The per-resolver tuple {resolver, rcode, reduced_status, answered} for both consensus lookups is recorded in `scan_detail.details.consensus` (v6audit's `LookupAAAA` already returns the rcode string).

AMENDMENT B — §2.5 recheck scheduling (add after the cadence config block):

Fast-recheck pull-in rules:
1. Only the two consensus dimensions trigger pull-ins: if base or www observed `inconsistent` → lane = recheck_inconsistent (2h); else if base or www observed `error` → lane = recheck_error (6h). `inconsistent` wins over `error`.
2. `error` on ns/mx/conn/resources and anything on informational dimensions (dnssec/ptr/smtp/parity) never changes scheduling — those retry at normal cadence (anti-flap already ignores non-definitive observations).
3. Rechecks are full scans (`Runner.Run` on the whole domain); there is no partial-scan mode.
4. Backoff: `domain.error_streak` increments on every scan where base or www is non-definitive, resets to 0 otherwise. next_check_at = now() + min(lane × 2^(error_streak−1), recheck_backoff_max). Default recheck_backoff_max: 720h (the 30d slow lane). Error progression: 6h, 12h, 24h, 48h, 96h, 192h, 384h, then capped.
5. Scheduling on a definitive scan is unchanged: next_check_at = now() + cadence(rank).

Config additions (crawler section):
```yaml
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

AMENDMENT C — §4.3 commit algorithm (counting gate; insert at top of the pseudocode and wrap the pending logic):

```
counting = (last_counted_at IS NULL) OR (now() - last_counted_at >= min_confirm_spacing)   # default 12h

for each core dimension d:
  O = observation(d)
  d_observed = O
  if O in {error, inconsistent}: continue        # non-definitive: touch nothing
  if not counting: continue                      # record-only scan: pending/status/changelog untouched
  ... existing steady-state / pending / confirm logic unchanged ...

if counting and at least one core dimension was definitive:
  last_counted_at = now()
```

Non-counting scans still write the scan + scan_detail rows, update *_observed, informational dimensions, latency, and scheduling — they exist to resolve fresh-domain NULL statuses faster is NOT their purpose; note the first-ever definitive scan always counts (last_counted_at NULL), so new domains still get a status after one scan. Restate the §2.3 guarantee as: a confirmed flip requires N definitive observations of the new value on scans spaced ≥ min_confirm_spacing apart — at daily cadence the advertised +1/+2 days, and never faster than (N−1) × 12h even when fast-lane rechecks run every 2h.

DDL additions to `domain` (§4.2, in the Frontier / scheduling group):
```sql
  last_counted_at TIMESTAMPTZ,                -- last scan that advanced anti-flap counters
  error_streak    SMALLINT NOT NULL DEFAULT 0, -- consecutive non-definitive base/www scans (backoff)
  dead_streak     SMALLINT NOT NULL DEFAULT 0, -- consecutive unresolvable scans (dead lifecycle)
```

AMENDMENT D — §4.8 dead trigger (replace the `dead` row's condition):

A scan is "unresolvable" when either:
(a) apex A and AAAA both NXDOMAIN and the NS walk finds no delegated zone (existing rule), or
(b) all 3 consensus resolvers returned an explicit SERVFAIL or REFUSED rcode for apex AAAA after retry (timeouts do NOT count — three timeouts more likely indicate our own network trouble).

`dead_streak` increments on an unresolvable scan, resets to 0 otherwise. At dead_streak ≥ 7: set disabled=true, disabled_reason='dead', next_check_at=now()+30d, reset both streaks. NXDOMAIN domains ride daily cadence → dead in 7 days; SERVFAIL domains ride the Amendment-B backoff → dead in ~2.3 weeks (6+12+24+48+96+192+384h). This matches brief §6 ("dead (NXDOMAIN/SERVFAIL)"); the existing auto re-enable on successful resolution is unchanged.

AMENDMENT E — §2.4 consensus resolver guards (replace "smooth the rate (no bursts), never retry a SERVFAIL'ing domain in a tight loop" with an owner):

The consensus resolver in `consensus/` owns rate control:
1. Per-provider token bucket, `per_provider_qps` sustained (blocking acquire; worker slots absorb the wait). This is the "smooth the rate" mechanism.
2. Fast-lane breaker: over a rolling `window`, if (error+inconsistent)/total consensus observations > `nondefinitive_rate` with ≥ `min_samples`, stop applying the 2h/6h pull-ins (non-definitive scans schedule at cadence(rank) instead), and alert the ops webhook. Re-enable when the rate stays below `recover_below` for one full window. This caps the resolver-degradation amplification (worst case otherwise ≈ 12× the sized 24 qps/provider).
3. Provider breaker: if a single provider's non-answer rate > `failure_rate` over the window with ≥ `min_samples`, drop it from the fan-out and alert; quorum degrades to 2-of-2 (both remaining agree → observation; disagree → inconsistent; ≤1 valid answer → error). A canary lookup probes the sick provider every 5 min; restore after `recovery_probes` consecutive successes.
"Never retry a SERVFAIL'ing domain in a tight loop" is now enforced structurally by Amendment B's backoff and Amendment D's dead lifecycle.
````

*Reconciliation note: this finding's `dead_streak` mechanism supersedes B2's `nxdomain_streak` column — they model the same counter; adopt M4's Amendment D form (which adds the brief-mandated SERVFAIL branch) and keep B2's trigger/recovery/slow-lane semantics. The spec writer should merge the two DDL deltas into one column set: `last_counted_at`, `error_streak`, `dead_streak`, `orphaned_at`, `last_requested_at`.*

---

### M5. Baseline HTTP contract absent: no CORS (cross-origin frontend breaks entirely), no real-client-IP handling behind nginx, and the brief's [::1] bind requirement is dropped

- **Section:** §5 (whole API section), §5.3 (brief appendix)
- **Confidence:** high · **Needs user decision:** no

**Description.** §5 specifies the full API surface but omits the HTTP server baseline: no CORS policy (the frontend at whynoipv6.com calls api.whynoipv6.com cross-origin, and production's rs/cors GET/HEAD/OPTIONS config must additionally gain POST for /check), no real-client-IP derivation (behind the nginx-fronted loopback bind, `GET /ip` returns `::1` to every visitor — silently defeating the frontend's IPv4 banner — and §5.3's 10/IP/hour rate limit collapses into one global bucket), and no listen-address spec despite the brief explicitly requiring the intentional `[::1]` bind be kept and documented. These failures appear only behind the real proxy at deploy time, not in local tests.

**Resolution.** A new §5.0 "HTTP server baseline": production's CORS + POST, RealIP middleware as the single source of truth for `/ip` and `requester_ip`, `API_LISTEN` config defaulting to `[::1]:8080` with rationale, per-endpoint-class Cache-Control, timeouts/graceful shutdown, middleware order, required nginx headers, and three parity tests.

**Spec text (verbatim):**

````markdown
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
````

---

### M6. §2.1/§2.4 "lift verbatim / zero changes" claims are false at crawler scale: unbounded resolver cache OOM, `latency_v4` fires ~2–3M phase-1 HTTPS fetches/day, and the consensus seam has no defined transport

- **Section:** §2.1 / §2.2 / §2.4 / §2.7 / §9 OPEN-9
- **Confidence:** high · **Needs user decision:** no

**Description.** Three confirmed defects behind the lift-verbatim narrative: (1) `resolver.go`'s TTL cache (unbounded sync.Map, same-key-only eviction, ≤300s TTL) becomes a multi-GB dead cache at 12–18M mostly-unique bulk queries/day in a long-lived process, and §2.1/§2.4's "Behavior lifted 1:1 / zero changes" wording forbids the fix. (2) `latency_ipv4` runs in phase 1 doing up to 3 HTTPS TTFB GETs per A-record domain (~2.5–3M fetches/day to mostly v4-only sites), contradicting §2.2's "most domains cost only DNS" and absent from §2.7's fetch math. (3) The consensus wrapper's quorum outcome has no defined transport: `CheckStatus` lacks `inconsistent`, and `LookupAAAA` can only signal via `err` which the checker collapses to `StatusError` — yet §4.1/§4.3 require `inconsistent` distinct from `error`, and §2.4's "disagreement annotation" has no path into Details. (Refuted: the miekg/dns "v2" identity concern — the Codeberg reference resolves uniquely; a pin note suffices. Minor rider: NS/MX per-host detail caps.)

**Resolution.** A new §2.8 engine-adaptation contract: delete the resolver cache for the bulk path (Unbound is the cache, no LRU replacement); a one-method `AAAAResolver` seam with `AAAAAnswer`/`QuorumInfo`/`ErrQuorumInconsistent` implemented by `internal/consensus` (no caching, fixed resolver order, no record-set merging); `latency_ipv4` moves to phase 2 gated on hasAAAA with §2.7's fetch row corrected; the DNS library pinned as `codeberg.org/miekg/dns` at an exact v0.6.x; NS/MX caps stated and exposed as config.

**Spec text (verbatim):**

````markdown
### §2.8 Engine adaptation contract (supersedes the "verbatim" listing in §2.1 and "zero changes" in §2.4)

The following files move from "lift verbatim" to "adapt". Every other §2.1 row stays verbatim.

**A. `resolver.go` — delete the in-process cache (bulk path).**
Remove `cacheEntry`, `dnsCacheKey`, `minTTL`, the `cache sync.Map` field, and the cache load/store in `QueryWithRetry` (v6audit resolver.go:28-31, 38, 50-94, 153-162, 177-182). `QueryWithRetry` keeps only the retry-once-on-error/SERVFAIL/REFUSED logic. Rationale (record in §2.4): the cache is unbounded, evicts only on same-key re-access, and clamps TTL to ≤300s while the frontier revisits a name every 24h — at 12-18M mostly-unique queries/day it is a multi-GB dead map. Unbound is the cache; intra-scan duplicate lookups (apex AAAA ~3-5×/domain) hit Unbound locally at sub-ms cost. Do NOT replace with an LRU. The §2.1 table row for resolver.go drops the phrase "TTL cache (30s–300s clamp, RFC 2308 negative caching)". Everything else in resolver.go (EDNS0, UDP→TCP on truncation, round-robin upstreams, CNAME chase ≤10 hops) is unchanged.

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
}

// QuorumInfo records the per-resolver breakdown of a consensus lookup.
type QuorumInfo struct {
    PerResolver map[string]string // "cloudflare"|"google"|"quad9" → "supported"|"unsupported"|"no_record"|"timeout"|"error"
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

`internal/consensus` implements `AAAAResolver`: three single-upstream `checker.Resolver` instances (Cloudflare, Google, Quad9 — v4+v6 addresses per §2.3), queried concurrently, 2s per-resolver timeout, one retry, **no caching anywhere on this path** (every observation must be fresh). Each resolver's reply is reduced to a status per §2.3; quorum is over statuses. Outcomes:
- Quorum reached: return the **entire answer** (IPs, CNAME chain, min TTL, rcode) of the first resolver in fixed order Cloudflare → Google → Quad9 whose reduced status equals the quorum status, with `Quorum` filled (`Disagreed=true` on 2-of-3 splits). Do not merge record sets across resolvers.
- No quorum (§2.3 "otherwise"): return `AAAAAnswer{Quorum: &qi}, ErrQuorumInconsistent`.

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
// ...existing err / NXDOMAIN / no-ips / supported branches, using ans.IPs, ans.CNAMEChain, ans.TTL, ans.Rcode
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
````

*Reconciliation note: B1's §2.3.1 composite wrapper and this finding's `AAAAResolver` seam describe the same component at two levels — B1 defines the observation semantics (including the conditional A lookup), M6 defines the Go transport. Fold them as one §2.3.1 + §2.8 pair; the consensus wrapper's `AAAAAnswer` gains the conditional bulk A-lookup step from B1.*

---

### M7. rank-NULL and disabled entities leak into public ranked lists and stats: no `rank IS NOT NULL` / `NOT disabled` filters anywhere

- **Section:** §5.1 / §5.2 / §4.2 / §4.8 / §2.6
- **Confidence:** high · **Needs user decision:** no

**Description.** The doc never states which domain rows are eligible for the public ranked lists and aggregate stats. In the shared-entity model, ~30k+ unranked rows (campaign-only apexes, auto-created parents, campaign subdomains) plus every abuse-submitted live-check host carry classifications and would silently join the shame/hero lists and country/global counts — behavior production structurally prevented via its RIGHT JOIN on the ranked sites table. (Narrowed: §4.8 already answers disabled-row exclusion at the product level, and the partial-index-predicate concern was refuted — Postgres uses partial indexes under predicate implication; amending them is polish.)

**Resolution.** An explicit "publicly ranked" predicate (`rank IS NOT NULL AND NOT disabled`) applied to all public ranked lists, search, and global/country/ASN stats — production parity by construction; campaign stats scoped by membership; detail endpoints not rank-scoped (404 for disabled, production parity); index predicates amended to match; disabling never clears state columns.

**Spec text (verbatim):**

````markdown
## Public visibility scope (fold into §5.1 preamble, §4.2, §4.7, §2.6, §4.8)

**Definition — "publicly ranked" predicate:**

```sql
rank IS NOT NULL AND NOT disabled
```

Invariant: only the Tranco importer ever writes `rank`, and Tranco is eTLD+1, so
`rank IS NOT NULL` implies `kind = 'apex'`. `created_by` is irrelevant to visibility:
a campaign-created apex that later enters Tranco gains a rank and becomes publicly
ranked — that is correct and intended.

**1. Endpoint scope rules (§5.1 / §5.2).** The following endpoints select ONLY rows
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

**2. Stats scope rules (§4.7 snapshots, §2.6 step 2).**

- `stats_global_daily`, `stats_country_daily`, `stats_asn_daily`: every column is
  computed over `rank IS NOT NULL AND NOT disabled`, EXCEPT
  `stats_global_daily.disabled`, which counts `rank IS NOT NULL AND disabled`
  (visibility into how much of the ranked set is suppressed).
- `stats_campaign_daily`: scoped by campaign membership `AND NOT disabled`; rank is
  irrelevant (campaign members are typically unranked).
- Ported `update_country_metrics` / `update_asn_metrics` counter recomputes (§2.6
  step 2): same `rank IS NOT NULL AND NOT disabled` scope, so `/country` and
  `/metric/asn` figures match the lists exactly.
- `scan_daily_adoption` cagg is measurement-flavored (Grafana/research) and stays
  unfiltered over all scans; DICTIONARY.md must state that it counts observations
  over all scanned entities and is NOT comparable to the product stats.
- Datasets (§5.4): `top100k`/`top1m` use the publicly-ranked predicate; `full`
  includes all non-disabled scannable entities (any kind/origin).

**3. Index amendments (§4.2 DDL).** Replace the three classification partial
indexes and the country index with:

```sql
CREATE INDEX idx_domain_sinners ON domain (rank)
  WHERE classification = 'sinner' AND rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_heroes  ON domain (rank)
  WHERE classification = 'hero'   AND rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_partial ON domain (rank)
  WHERE classification = 'partial' AND rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_country ON domain (country_id, classification, rank)
  WHERE rank IS NOT NULL AND NOT disabled;
```

Queries must spell out `AND rank IS NOT NULL AND NOT disabled` verbatim so the
planner's predicate-implication check is trivial.

**4. Disable semantics clarification (§4.8).** Setting `disabled = TRUE` does NOT
modify `classification`, `class_flags`, `gold`, or any confirmed status/`*_since`
column — history and state are preserved; public exclusion is achieved solely by the
`NOT disabled` filter above. The one state reset remains the existing §4.8 rule:
when a `dead` domain resolves again during slow-lane revalidation, re-enabling resets
classification to `'unknown'` (statuses re-confirm from scratch). Live-check
submissions never bypass any of this: they are `rank NULL` rows and therefore
structurally invisible to lists and stats regardless of classification.
````

*Reconciliation note: this finding says `GET /domain/{domain}` returns 404 for disabled entities (production parity); B2's item 7 tentatively allowed it to resolve. Adopt the 404 rule — it is the verified production behavior.*

---

### M8. POST /check / GET /check/{id} lifecycle underspecified: whether live checks feed the confirmed-state machine, dedupe source, lease/reaper, retention

- **Section:** §5.3 / §4.9
- **Confidence:** high · **Needs user decision:** no

**Description.** Three confirmed gaps: (1) the doc never states whether a live-check engine run of an existing domain feeds the §4.3 confirmed-state machine — the product-relevant choice, since feeding it makes the N-consecutive-scans anti-flap rule user-accelerable by anonymous POSTs; (2) the <1h dedupe path's "stored result" for crawler-scanned hosts has no check_job row, so the scan→result mapping is undefined; (3) the check_job DDL lacks the claimed_at lease §2.5 explicitly adds to the identical SKIP-LOCKED pattern, so a crawler restart strands `processing` jobs forever. (Refuted sub-points: envelope field names/status codes are a new surface pinnable at OpenAPI time; sequential-id enumerability and retention are minor — both pinned in the resolution anyway.)

**Resolution.** Rule 0: live checks never touch confirmed state — the consumer writes only `check_job.result`; confirmed state advances exclusively via frontier scans; `GET /check/{id}` returns the confirmed snapshot alongside raw live observations so they can never silently contradict. One shared result mapper serves fresh runs, the scan_detail dedupe path, and the frontier worker. check_job gains claimed_at + 5-min reclaim, a 15-min reaper, a 30-day purge, and dedupe/rate-limit indexes; full envelopes, status codes, consumer algorithm, and the live_check frontier-eligibility predicate are pinned.

**Spec text (verbatim):**

````markdown
### §5.3 (replaces the two paragraphs after "Flow:") — Live-check lifecycle, fully specified

**Rule 0 — live checks never touch confirmed state.** The live-check consumer runs the engine and writes its result ONLY to `check_job.result`. It never inserts `scan` or `scan_detail` rows and never updates any `domain` column except the initial row insert for unknown hosts (below). Confirmed statuses, pending counters (`*_pending`, `*_pending_count`), `*_observed`, `last_checked_at`, `next_check_at`, `classification`, changelog rows, and country/ASN counters advance exclusively via frontier scans (§4.3). Rationale: §2.3's N-consecutive-scans rule assumes daily cadence; anonymous POSTs must not be able to accelerate a confirmed transition. `check_job` rows and results are public data; sequential BIGINT ids are enumerable and that is accepted (no auth, nothing sensitive).

**POST /check** — processing order:
1. Parse body `{"domain": "<host>"}`. Validate: LDH hostname, punycode-normalize to lowercase, ≤253 octets, ≥2 labels; reject IP literals and `.internal`/`.local`/RFC 2606 TLDs (`.test`, `.example`, `.invalid`, `.localhost`). Failure → **400** `{"error":"invalid_host","message":"..."}`.
2. Rate limit: count `check_job` rows for `requester_ip` with `created_at > now()-1h` (limit 10), then global count (limit 500). Exceeded → **429** `{"error":"rate_limited","scope":"ip"|"global","retry_after_s":<int>}` + `Retry-After` header.
3. **Dedupe, domain-side:** if a `domain` row for the host exists and `last_checked_at >= now() - interval '1 hour'`, load its latest `scan_detail` row, run the shared result mapper over `details`, and return **200** with a synthetic done envelope (below, `id: null`, `cached: true`). No job row is created. (`last_checked_at` is written only by frontier commits, so live checks never count as "scanned" for this window.)
4. **Dedupe, job-side:** else if a `check_job` for the same host has `status='done' AND completed_at >= now() - interval '1 hour'`, return **202→no — 200** with that job's envelope (`cached: true`).
5. Else `INSERT check_job (host, requester_ip) → status 'pending'` and return **202** `{"id":123,"host":"...","status":"pending","created_at":"..."}`.

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
`result` (produced by the shared mapper; statuses use the raw-observation vocabulary `supported|unsupported|no_record|not_applicable|error` — plus `inconsistent` for base/www when quorum split; live results are raw observations, explicitly NOT confirmed state):
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
`confirmed` (from the `domain` row; `null` if no row or nothing confirmed yet): `{"classification":"partial","class_flags":["mail_missing"],"gold":false,"statuses":{"base":"supported","www":"supported","ns":"supported","mx":"unsupported","conn":"supported","resources":null},"as_of":"<last_checked_at>"}`.

**Shared result mapper** (one implementation, two inputs): `MapLiveResult(sr checker.ScanResult) → result JSON`. Applies §2.2's engine→public dimension mapping exactly (keys are the PUBLIC dimension names, not engine check names): `base`←dns_aaaa_base, `www`←dns_aaaa_www, `ns`←dns_ns_ipv6 (partial→supported), `mx`←dns_mx_ipv6 (partial→supported), `conn`←https_ipv6 w/ http_ipv6 fallback, `resources`←resource_ipv6 (partial→unsupported), `tls`/`smtp`(partial→unsupported)/`parity`/`dnssec`/`ptr`/`spf` informational, `latency`←latency_ipv4/ipv6. Because `scan_detail.details` stores the engine ScanResult serialization (§4.4), the same mapper serves the domain-side dedupe path. This mapper is also the single mapping used by the frontier worker before §4.3's commit — one mapping, three consumers.

**Consumer** (dedicated goroutine pool in the crawler binary; config `live_check.workers`, default 4; poll every 2s when idle):
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
2. Ensure a `domain` row: `INSERT ... (host, kind, parent_id, rank=NULL, created_by='live_check') ON CONFLICT (host) DO NOTHING`. `kind` via the campaign-import PSL helper; `parent_id` set only if the registrable parent row ALREADY exists — live checks never auto-ensure parents (a `parent_link` row would grant permanent frontier eligibility, letting abuse grow the frontier).
3. Run the full engine with a 60s context budget (panic-recovered, as in `runPhase`).
4. On success: `UPDATE check_job SET status='done', result=$1, completed_at=now()`. On error/timeout: `status='failed', error=$2, completed_at=now()`. Nothing else is written (Rule 0).

**Reaper** (same goroutine, every 60s) — guarantees every poller terminates ≤15 min:
```sql
UPDATE check_job SET status='failed', error='timed out', completed_at=now()
WHERE status IN ('pending','processing') AND created_at < now() - interval '15 minutes';
```
**Retention** (daily 03:30 tick): `DELETE FROM check_job WHERE created_at < now() - interval '30 days';`

**Frontier eligibility for live-check rows** (pins §4.2's "live-check origin within 7 days"): the claim query's eligibility predicate term is `(created_by = 'live_check' AND created_at > now() - interval '7 days')`. `next_check_at` defaults to `now()`, so the frontier scans the new host promptly and its confirmed snapshot populates via the normal §4.3 path. Re-checking a host after its 7-day window does NOT re-enter it into the frontier (`created_at` is never refreshed); later POSTs simply run on-demand under the rate limits.

### §4.9 — replace the `check_job` DDL block with:
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
```

### Config keys (crawler):
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
````

*Reconciliation notes: (a) Rule 0 supersedes B2 item 5's sentence that the check-job consumer "commits through the standard §4.3 path" — under the merged design, POST /check on a `dead` host sets `next_check_at = now()` so recovery happens via the pulled-in frontier scan, and `last_requested_at = now()` (B2) is an allowed domain write alongside the initial row insert. (b) The frontier-eligibility predicate paragraph here predates B2's materialized-eligibility model; under the merge, B2's lifecycle sweep + `last_requested_at` implement the 7-day linkage and this paragraph's claim-query predicate term is dropped. Note the eligibility window keys off `last_requested_at` (refreshed by later POSTs) rather than `created_at`.*

*(One transcription note: the check_job dedupe index comment says "host-side dedupe"; the original spec_text reads "job-side dedupe" in prose — semantics identical.)*

---

### M9. §4 DDL defects: `observation` enum cannot store parity/ptr `partial`, and the §4.7 cagg policy call is not valid TimescaleDB API

- **Section:** §4.1 / §4.2 / §4.4 / §4.7 (vs §2.2)
- **Confidence:** high · **Needs user decision:** no

**Description.** Two confirmed defects: (1) §4.1's `observation` enum has no `partial`, but the engine's `response_parity` AND `dns_ptr` checks both return partial, and §2.2 maps these "kept as-is" — so the typed columns `parity_observed`/`ptr_observed` and `scan.parity`/`scan.ptr` cannot store a common real value; the first partial observation is a runtime insert error or forces invented mapping. (2) §4.7's `CALL add_policies(..., refresh_schedule_interval => ...)` is invalid TimescaleDB API (experimental, SELECT-invoked, no schedule-interval parameter). (Refuted: the segmentby auto-selection risk — TimescaleDB ≥2.20 skips default segmentby when orderby is explicit, which the DDL sets everywhere; and tls/spf living only in scan_detail JSONB is stated design intent. Riders: drop the redundant scan index, add a changelog (ts DESC) index.)

**Resolution.** Add `partial` to the internal-only observation enum (legal only in ptr/parity columns; public ipv6_status untouched); replace the invalid policy call with the stable `add_continuous_aggregate_policy` + `enable_columnstore` + `add_columnstore_policy` trio; apply the two index riders; add the two clarifying sentences.

**Spec text (verbatim):**

````markdown
## Spec amendments (fold into implementation spec)

### 1. §4.1 — observation enum gains 'partial' (internal only)

Replace the observation enum DDL with:

```sql
-- Raw observation outcomes; internal only. 'partial', 'error' and 'inconsistent'
-- never reach public output (classification and the API read only the confirmed
-- ipv6_status columns, which remain 4-valued).
CREATE TYPE observation AS ENUM
  ('supported', 'partial', 'unsupported', 'no_record', 'not_applicable',
   'error', 'inconsistent');
```

Storage rule (add as normative prose next to §2.2's mapping table):

> `partial` is a legal stored value ONLY for the two informational dimensions whose
> §2.2 mapping is "kept as-is": `ptr` and `parity` (columns `domain.ptr_observed`,
> `domain.parity_observed`, `scan.ptr`, `scan.parity`). Every other partial-capable
> engine check is mapped to a non-partial observation BEFORE any DB write, per the
> §2.2 table: ns partial -> supported, mx partial -> supported,
> resources partial -> unsupported, smtp partial -> unsupported. The core-dimension
> `*_observed`/`*_pending` columns and the §4.3 commit algorithm therefore never see
> `partial`; the raw engine verdict is always preserved in `scan_detail.details`.

### 2. §4.7 — replace the invalid policy call

Delete the `CALL add_policies('scan_daily_adoption', ...)` statement
(timescaledb_experimental.add_policies is early-access, SELECT-invoked, and has no
schedule-interval parameter). Replace with the stable API:

```sql
SELECT add_continuous_aggregate_policy('scan_daily_adoption',
  start_offset      => INTERVAL '3 days',
  end_offset        => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 hour');
ALTER MATERIALIZED VIEW scan_daily_adoption
  SET (timescaledb.enable_columnstore,
       timescaledb.orderby = 'day DESC, country_id');
CALL add_columnstore_policy('scan_daily_adoption', after => INTERVAL '90 days');
```

The existing ordering-rule comment stays valid (cagg start_offset 3d < scan retention 2y).

### 3. §4.4 riders

a. Delete `CREATE INDEX ON scan (domain_id, ts DESC);` — the primary key
   `(domain_id, ts)` already serves backward per-domain scans; the extra index is
   pure write/storage overhead.

b. After the changelog DDL add:

```sql
CREATE INDEX idx_changelog_ts ON changelog (ts DESC);  -- global recent-changes feed
```

   (The PK leads with domain_id and cannot serve the sitewide GET /changelog feed.)

### 4. Clarifying sentences (no DDL change)

a. §2.2/§4.4: "tls and spf deliberately have NO typed columns anywhere: they are
   informational-only and live exclusively in `scan_detail.details` JSONB. The detail
   page reads the latest scan_detail row, which for any actively scanned domain is
   always far younger than the 90-day scan_detail retention. This is accepted design,
   not an omission."

b. §4/§4.4: "segmentby is deliberately unset on every columnstore table. Because
   `timescaledb.orderby` is set explicitly on each of them, TimescaleDB >= 2.20 does
   NOT auto-select a default segmentby (PR #8033); no `segmentby = ''` override is
   required."
````

*Reconciliation note: B1's §2.3.1 table says informational dimensions "store the raw engine status verbatim" — M9's storage rule refines this: raw-verbatim (including `partial`) applies to ptr/parity only; smtp partial maps to `unsupported` before storage. M9's rule is the more precise one; adopt it.*

---

## 4. Minor findings

No standalone minor-severity findings were confirmed (the canonical count is 0). The following minor riders are already folded into the resolutions above — listed here so the spec writer doesn't lose them; they are **not counted separately**:

| Rider | Section | One-line resolution | Carried by |
|---|---|---|---|
| Tranco import short-list / mass-delist guard | §3 step 4 | Abort below `tranco.min_rows` or above `tranco.max_delist_pct`, `--force` override, `aborted`/`note` columns on `tranco_import` | B2 item 6 |
| Accepted bulk-vs-consensus AAAA skew for `conn` | §2.4 | State as accepted and semantically honest; absorbed by N=3 | M1 |
| Synthetic `id` for `/domain/{domain}/log` and changelog feeds | §5.1 | Epoch seconds (log) / epoch milliseconds (changelog) of `ts`; frontend keys by index | B3 R2, M3 C |
| `ts_curl`/`ts_updated` key mapping + NULL-timestamp emission | §5.1 | Pinned table; Go zero time for NULLs, no fallback substitution | B3 R3 |
| `scan_detail.result_id` "idempotent re-submits" comment | §4.4 | Column dropped; idempotency via fixed T + ON CONFLICT | M2 |
| Redundant `scan (domain_id, ts DESC)` index | §4.4 | Deleted (PK serves it) | M9 rider 3a |
| Missing `changelog (ts DESC)` index for the global feed | §4.4 | Added | M9 rider 3b |
| tls/spf scan_detail-only placement | §2.2/§4.4 | One clarifying sentence: deliberate, accepted | M9 rider 4a |
| segmentby omission on columnstore tables | §4.4/§4.7 | One clarifying comment: safe on TS ≥ 2.20 with explicit orderby | M9 rider 4b |
| NS/MX "all-hosts detail" wording vs 4/5-host caps | §2.2 | Correct wording; expose caps as config | M6 E |
| miekg/dns "v2" module-identity note | §2.1/§2.4/OPEN-9 | Pin `codeberg.org/miekg/dns` at exact v0.6.x; warn off `github.com/miekg/dnsv2` | M6 D |
| check_job retention + id enumerability | §4.9 | 30-day purge in daily tick; enumeration explicitly accepted | M8 |
| "Does disabling reset classification?" wording | §4.8 | No — exclusion is purely the filter; only dead-recovery resets | M7 item 4 |

---

## 5. Decisions needing the maintainer

**None.** All 15 resolutions carry `needs_user_decision: false` at high confidence. Every judgment call was found to be forced by a constraint the design doc or brief already states — the locked 3-state model and §5.5 ladder, the frozen §5.1 frontend contract, the "not_applicable never counts against" principle, the OPEN-2/OPEN-6 decisions, the brief's dead-lifecycle and hero-bar requirements, and verified production behavior for compat surfaces. The closest calls, each resolved by an existing decision rather than a new one, were:

- **`v6_ready` www amendment (B3 R4)** — resolved by the doc's own OPEN-6 announced-methodology-shift mechanism; the strict alternative provably pins subdomain-heavy campaigns at 0%, contradicting the doc's principles. Recommended default: as written.
- **`top_heroes`/`top_nameserver` formulas (B6)** — resolved by the same decided OPEN-6 pattern (fix value-level bugs, keep shapes); keeping the metric web-facing preserves its public meaning. Recommended default: as written.
- **Timeout-as-definitive for `conn` (B4)** — forced by the brief's `broken_v6` definition; triple-mitigated by existing design decisions (preflight guard, multi-IP attempts, N=3). Recommended default: as written.
- **Live checks never touch confirmed state (M8 Rule 0)** — effectively dictated by the locked anti-flap trust model. Recommended default: as written.

If the maintainer wants to overrule any of these, the affected spec_text sections are self-contained and swappable; absent input, all recommended defaults stand.

---

## 6. Refuted-findings summary

**No canonical findings were refuted** — all 15 verified findings survived adversarial verification, though several were narrowed: individual sub-claims were checked and cleared during verification. These narrowings are verification scope-reduction, not separate refuted findings; they matter because they tell the spec writer what does **not** need fixing:

| Cleared sub-claim | Why it was cleared |
|---|---|
| www `no_record` hero treatment contradicts OPEN-2 (B1) | The §4.3 note explicitly answers it; only the producing condition was undefined |
| §4.3 missing bootstrap branch is a blocker (M2) | §4.3 prose directly below the pseudocode specifies first-scan behavior; seed-status question answered by §8 phase 4 + OPEN-6 |
| `conn` target host undefined (M1) | Answered by §2.1's verbatim lift (entity's own host); apex-only dialing is provably hero-neutral |
| Partial-index predicates defeat the classification indexes (M7) | Factually wrong — Postgres uses partial indexes under predicate implication; amending them is polish |
| segmentby omission recreates the compression collapse (M9) | False on TimescaleDB ≥ 2.20 (PR #8033): explicit orderby suppresses default segmentby, and the DDL sets orderby everywhere |
| tls/spf missing typed columns is a defect (M9) | Stated design intent (informational, scan_detail-only); needs one clarifying sentence, not columns |
| miekg/dns "v2" module identity unresolvable (M6) | The doc's Codeberg reference uniquely resolves to `codeberg.org/miekg/dns` (v0.6.x); an exact go.mod pin suffices |
| POST /check envelope/status-code gaps block the spec (M8) | New surface with no frozen contract; §5.5's spec-first OpenAPI process is where these get pinned (they are now pinned in M8's spec_text anyway) |
| check_job sequential-id enumerability is a risk (M8) | Anonymous public tool, nothing sensitive; explicitly accepted |
| Disabled-row exclusion from lists is undecided (M7) | Answered at the product level by §4.8; only the mechanical filter placement needed spelling out |
| latency_v4 "~70% of web at 6h" smtp-adjacent figures, Quad9-blocks-via-SERVFAIL (M4 periphery) | Peripheral inaccuracies in the original finding; did not carry the finding and do not affect its resolutions |

---

## 7. Recommended path to the spec

### 7.1 Fold the resolutions into the design doc first

Apply the 15 resolutions to `backend-design.md` **before** writing the implementation spec, grouped by theme (each group is internally interdependent and should be merged in one editing pass):

1. **Engine → observation → confirmed-state pipeline** (B1, M1, B4, B5, M6): new §2.3.1 observation-mapping tables + composite base/www wrapper (B1), new §2.8 engine-adaptation contract with the `AAAAResolver` seam (M6), the `conn` composition table (M1) refined by B4's timeout rule, the truth-table replacement of the §4.3 ladder prose (B4), and the decoupled resources path (B5). Reconcile as noted inline: B1 defines the consensus wrapper's semantics, M6 its Go transport; B4 amendment C refines M1 row 5; M9's partial-storage rule refines B1's informational-dimension sentence.
2. **Lifecycle, scheduling, and the commit machine** (B2, M2, M4): merge the three DDL deltas into one column set on `domain` (`orphaned_at`, `last_requested_at`, `last_counted_at`, `error_streak`, `dead_streak` — M4's `dead_streak` supersedes B2's `nxdomain_streak`); merge M4's counting gate and M2's lease fence into one canonical §4.3 pseudocode block; B2's lifecycle sweep becomes §2.6 step 1; M4's amendments A/B/E rewrite §2.3/§2.5/§2.4.
3. **Legacy API serialization and visibility** (B3, B6, M3, M5, M7): new §5.0 HTTP baseline (M5), §5.1 legacy-serialization addendum (B3), pinned /metric/overview + stats columns (B6), the changelog ladder/scope/import-transform package (M3, using its CHECK-constraint DDL form over M2's plain NOT NULL), and the publicly-ranked predicate (M7, adopting its 404-for-disabled rule over B2 item 7's tentative exception).
4. **Schema/DDL corrections** (M9 plus the DDL fragments of every other group): apply as one migration-affecting pass so the final `CREATE TABLE` statements in §4 are internally consistent.
5. **Live check** (M8): §5.3 rewrite + §4.9 check_job DDL, with Rule 0 taking precedence over B2 item 5's consumer-commit sentence and B2's `last_requested_at` linkage replacing M8's claim-predicate paragraph.

Three explicit cross-finding reconciliations are flagged inline above (B2↔M8, B2↔M4, M2↔M3); the spec writer should resolve them exactly as noted — each has a designated winner.

### 7.2 What the spec document must contain beyond the amended design doc

- **Consolidated config-key registry.** The resolutions introduce `lifecycle`, `tranco`, `recheck_*`, `anti_flap`, `consensus`, `checks`, `live_check`, `crawler.resources.enabled`, and `API_LISTEN`. Collect every key, type, default, and owning component in one table; the scattered YAML fragments above are the source of truth for defaults.
- **Deploy/ops notes:** the nginx `proxy_set_header` block and the loopback-bind rationale (M5), dataset static-serving split (§5.4), and the ops-webhook alert points (import abort, fast-lane breaker, provider breaker).
- **Parity-test plan:** golden fixtures from production **plus** the synthetic fixtures for branches production can never produce (B3's not_applicable/NULL projections and log filter; M5's three proxy/CORS tests; M3's byte-equal changelog-import gate as the phase-4 verification).
- **Migration ordering:** enums → asn/country → domain → campaign → resource → remaining tables in migration 001; hypertables/caggs/policies in 002; the M9-corrected policy calls; seed migration writes day-0 `stats_*` rows (B6).
- **Phase gating:** `crawler.resources.enabled=false` until phase 5 (B5), seed-import-as-confirmed semantics at phase-4 cutover (M2), and the no-changelog-flood cutover note (M3 F).
- **The three enumerated lift deviations** (UA constant, miekg/dns port, http_ipv6 error_type classification per B4 D) plus the §2.8 adapt list — so "verbatim" is auditable.

### 7.3 Proposed spec-document outline

1. Scope, locked decisions, and non-goals (restate the hard constraints)
2. Engine contract: lift-verbatim inventory, §2.8 adaptation contract, the three enumerated deviations
3. Observation model: §2.3.1 mapping tables, consensus quorum rules (M4 A), the `AAAAResolver` seam, `conn` composition, resources roll-up
4. Confirmed-state machine: canonical §4.3 pseudocode (bootstrap + counting gate + lease fence), N-values, changelog write rules, classification truth table and flags
5. Lifecycle: frontier claim SQL, cadence/recheck/backoff, lifecycle sweep, dead/delisted/recovery, Tranco/campaign/live-check re-entry
6. Schema: full final DDL (all merged deltas), indexes, hypertable/cagg/retention policies, migration ordering
7. Ingest: Tranco import (with sanity guard), campaign sync, resource-host sweep, daily tick step list
8. API: §5.0 HTTP baseline, legacy endpoint-by-endpoint contract (serialization rules, changelog ladder, visibility predicate), new endpoints, live-check lifecycle, badge/datasets
9. Migration & cutover: seed import, changelog history transform, parity gates, phase plan with per-phase verification criteria
10. Ops & config: config registry, nginx/deploy notes, alerting, crawler_metrics
11. Test plan: golden + synthetic parity fixtures, chaos tests (lease fence), quorum/anti-flap unit vectors

With the 15 resolutions folded in, nothing on this outline requires further product input — the spec can be written directly.

---

# Appendix: second-pass findings (recovered from the dedup stage)

The main report's pipeline collapsed 134 raw findings to 15 canonical ones before verification; that dedup stage also **dropped 36 findings without verifying them**. This appendix is the result of a dedicated second pass that verified and resolved those 36 dropped findings under the same rules as the main report (same refutation grounds, same severity bar, same "compose with the 15 prior resolutions, never contradict them" constraint). Outcome: **20 confirmed (0 blockers, 6 majors, 14 minors), 16 refuted** — several of the refutations because the first report's resolutions already cover them. Every confirmed finding ships a verified, high-confidence resolution with ready-to-paste spec text, and **none requires a maintainer decision**. Findings below are numbered A1–A20 (A = appendix); first-pass findings are referenced by their B*/M* ids.

---

## Confirmed findings

### Cross-finding reconciliation (read first)

Five overlaps exist **within this pass** where two resolutions specify the same mechanism differently. Each has a designated winner; apply the spec_text blocks below with these substitutions:

1. **Advisory-lock key registry.** Three schemes appear: A1 uses `(ClassID 60660, JobDailyTick=1, JobTrancoImport=2, JobCampaignSync=3)`, A2 uses `hashtext('campaign_sync')`, A7 uses `(4200, 1..3)`. **A1's registry is canonical** (it is the dedicated singleton-coordination resolution and defines the shared `internal/lock` package). A2's lock acquisition mechanics (dedicated pooled connection held for the whole run, session-scoped, auto-release on crash, lock taken *before* any git operation) and A7's job semantics are preserved unchanged — only the key constants map onto A1's registry. Register the constants in one Go file (`internal/lock/lock.go`); A7's "never reuse classid 4200" note applies to 60660 instead.
2. **Tranco 23:15 trigger ownership.** A1 and A7 both resolve §3's "systemd timer / crawler coordinator" slash to **the crawler coordinator goroutine, no systemd timer** — that is canonical. A19's deploy appendix (written independently) instead gave Tranco to a systemd timer and removed it from the coordinator; **drop the `whynoipv6-tranco.timer` row from A19's D.3 timer inventory** and keep A7's coordinator retry cycle (23:15 start, 2h re-attempts, 48h staleness warning). A19's export and geoipupdate timers stand.
3. **Tick-nested campaign sync blocking.** A1's contract is canonical: coordinator-scheduled singletons use non-blocking `TryRun` (skip = healthy), v6ctl invocations and the daily tick's nested campaign-sync step use blocking `Run` with a 5-minute wait. A2's "loser exits 0" description applies to the try-lock paths and is consistent with this.
4. **GeoIP config key and cadence.** A17's `GEOIP_PATH` (default `/var/lib/GeoIP`) supersedes A19's `GEOIP_DIR`; A17's `geoipupdate` `OnCalendar=Wed,Sat 06:30` supersedes A19's `05:41` (same twice-weekly intent). A17's hourly mtime check + atomic reader swap subsumes A19's "re-open mmdb readers in the daily tick" step — delete that tick step.
5. **Badge invalid-host response.** A15 pins `400 {"error":"invalid_host"}` for syntactically invalid badge hosts; A5's general rule for hostname path params is 404-on-Canonicalize-failure. **The badge keeps its dedicated 400** — record it as the declared exception in A5's call-site table (the badge row already notes `.svg` stripping; add "failure → 400 per §5.2a").

The daily-tick step list also appears in both A1 (canonical 7-step order) and A19 (abbreviated); **A1's list is normative**. Where A2's §7.3 sync algorithm and A1's tick step 5 meet, A2 defines *what* sync does, A1 defines *when and under which lock*.

---

### Blockers

None. The second pass produced no findings that block spec-writing or force invented product behavior — consistent with the dedup stage's implicit triage, though six of the dropped findings turned out to be majors it should not have discarded silently.

---

### Majors

### A1. Singleton coordination across crawler processes unspecified

**Section:** §2.5, §2.6 · **Severity:** major · **Confidence:** high · **needs_user_decision:** false

**Description.** §2.5 mandates 2 identical crawler processes, but §2.6's daily tick (stats snapshot, country/ASN recompute, candidate detection, ops summary — plus the first report's added lifecycle sweep and check_job purge), §3's 23:15 Tranco import trigger, and §7's campaign sync all run "in `crawler`'s coordinator goroutine" with no singleton coordination specified anywhere. With two processes both coordinators fire: the stats snapshot collides on `stats_global_daily`'s PRIMARY KEY (day) and can abort the tick mid-sequence, concurrent campaign syncs race on the shared git checkout and can double-generate UUIDs and double-push bot commits to the public campaign repo, and the Tranco staging upsert runs twice. The SKIP LOCKED consumers (frontier claim, check-job) are safe; the tick/import/sync jobs are not. The first report has no coverage — and its resolutions add *more* singleton work to the tick (B2's sweep, M8's purge, M4's recheck machinery).

**Resolution.** Per-job Postgres session advisory locks are the single coordination mechanism: every singleton job acquires a pinned two-int lock on a dedicated pooled connection before running. Crawler-scheduled invocations use `pg_try_advisory_lock` and skip if busy (the skip is the failover, not an error); v6ctl invocations block up to 5 minutes. No `--coordinator` flag — mandatory rather than optional, because §7's campaign sync is multi-trigger by design ("the webhook is latency sugar, the cron is the guarantee. Both."), so even a designated coordinator process could not prevent a webhook-triggered v6ctl sync racing the tick's sync. The §3 trigger ambiguity resolves to the coordinator (see reconciliation note 2). Idempotent writes (`ON CONFLICT ... DO UPDATE` on stats, `DO NOTHING` on candidates) are the second guard and also make `v6ctl stats recalc` safe. The canonical tick step order and per-step failure containment are pinned, composing with B2 (sweep as step 1) and M8 (purge).

**spec_text:**

````
### Amendment to §2.5/§2.6/§3/§7 — Singleton-job coordination (advisory locks)

**Decision.** Both crawler processes are identical (no `--coordinator` flag, no
per-instance config). Each process runs the same coordinator goroutine; every
**singleton job** is gated by a Postgres **session-scoped advisory lock**, keyed
per job. Whichever process acquires the lock runs the job; the other skips. This
also serializes v6ctl-triggered runs (webhook, cron, operator) against the
coordinator — the lock, not the trigger topology, is the mutual exclusion.

**Lock registry** (pinned constants, two-int form; classid identifies the app):

```go
// internal/lock/lock.go
const ClassID int32 = 60660 // whynoipv6 advisory-lock namespace, never change

const (
    JobDailyTick    int32 = 1 // §2.6 tick, all steps, one lock for the whole sequence
    JobTrancoImport int32 = 2 // §3 import (scheduled + v6ctl tranco import)
    JobCampaignSync int32 = 3 // §7 sync (tick + webhook/Semaphore + v6ctl campaign sync)
)
```

Distinct from golang-migrate's single-bigint advisory lock (different key
encoding); no collision.

**Acquisition contract** (`internal/lock`, used by crawler and v6ctl):

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
- The connection holding the lock is dedicated to the lock for the job's
  duration (job steps may use other pool connections freely). Session lock ⇒
  crash of the holding process drops the connection and frees the lock — no
  lease/expiry machinery.
- Crawler-scheduled invocations use `TryRun`; a skip is **not** an error: log
  `level=info msg="singleton skipped, held elsewhere" job=<name>` and increment
  counter `crawler_singleton_skipped_total{job}` (Grafana). Getting exactly one
  skip per scheduled fire is the healthy steady state with 2 processes.
- All v6ctl invocations (`tranco import`, `campaign sync`, `stats recalc`) use
  `Run` with the 5-minute wait (hardcoded; no config key).

**Trigger resolution (replaces ambiguous wording):**
- §3: the 23:15 UTC Tranco import is fired by the **crawler coordinator
  goroutine** (gated by `JobTrancoImport`), NOT a systemd timer — delete the
  "daily systemd timer /" alternative. The "list ID unchanged → retry in 2h"
  loop lives in the same goroutine. `v6ctl tranco import` remains the manual
  verb calling the identical import function under the identical lock.
- §7 step 2 stays "Both": Semaphore webhook → `v6ctl campaign sync` on the
  backend host, AND the daily tick runs sync. `JobCampaignSync` covers the
  entire sync — `git pull` + YAML import + UUID generation + bot write-back
  push — so the shared `/srv/whynoipv6-campaign` checkout is never touched by
  two syncs concurrently and UUIDs/bot commits cannot double-fire.
- §2.6: at 03:30 UTC both coordinators attempt `TryRun(JobDailyTick, …)`; one
  wins, runs all steps in order under the single lock; the loser skips the
  whole tick.

**Canonical daily-tick step order** (merging this doc's §2.6 with prior-report
B2 step 1 and M8's purge; one lock for the whole sequence):

1. Lifecycle sweep (B2 resolution — eligibility materialization).
2. Stats snapshot into `stats_*` tables (§4.7, incl. B6's two top-1k columns).
3. Country/ASN counter recompute.
4. Service-domain candidate detection (§4.8).
5. Campaign sync (pull + import), via `Run(JobCampaignSync, wait=5m, …)` —
   nested lock, waits out a concurrently webhook-triggered sync rather than
   silently skipping the daily guarantee.
6. `check_job` purge, 30d retention (M8 resolution).
7. Ops summary → webhook + healthcheck ping.

**Failure containment:** a failing step logs the error and **continues** to the
next step; step 7's ops summary lists any failed steps by name. The tick never
aborts mid-sequence on a single step error. The healthcheck ping in step 7
fires only if steps 1–3 succeeded (stats and lifecycle are the health-critical
core); otherwise the missed ping is the alert.

**Idempotent-write second guard** (protects against operator re-runs and any
future trigger overlap, even though the lock already serializes):
- Stats snapshot (step 2): all four `stats_*` inserts use
  `INSERT … ON CONFLICT (<pk>) DO UPDATE SET <every counter> = excluded.<col>`
  (PKs: `day` / `(day,country_id)` / `(day,campaign_id)` / `(asn_id,day)`).
  DO UPDATE, not DO NOTHING — this is also what makes `v6ctl stats recalc`
  (already in the §6 v6ctl verb list) a safe same-day re-run.
- Candidate detection (step 4): `ON CONFLICT DO NOTHING` on the candidate rows.
- Sweep, recompute, purge (steps 1, 3, 6): set-based UPDATE/DELETE, inherently
  idempotent — no change.
- Tranco import step 4: staging upsert already `ON CONFLICT (host) DO UPDATE`;
  additionally `tranco_import` provenance insert conflicts on `(list_id)`
  DO NOTHING so a re-run of an already-recorded list is a no-op.

**SKIP LOCKED consumers unchanged:** frontier claim and check_job claim loops
run in every process by design and need no singleton gating.
````

---

### A2. Campaign sync concurrency, UUID-trust rule, and file-deletion/rename matching underspecified

**Section:** §7 · **Severity:** major · **Confidence:** high · **needs_user_decision:** false

**Description.** Three §7 gaps, verified with weights. (a) **Confirmed:** sync is mandated from both the Semaphore merge webhook (`v6ctl` CLI) and the crawler daemon's daily tick ("Both.", §7 step 2) — two separate processes sharing the `/srv/whynoipv6-campaign` checkout; concurrent runs against a uuid-less file each generate a *different* UUID (`campaign.uuid UNIQUE` does not help — the UUIDs differ), creating duplicate campaigns and racing the bot write-back push. No serialization is specified anywhere. (b) **Confirmed, understated:** "UUID must be absent or valid" cannot enforce "contributors never invent UUIDs" (the Action has no DB access); worse, a contributor copying an *existing* campaign's uuid into a new/edited file causes step 3's upsert-by-uuid + membership diff to silently rename that campaign and delete its memberships — a trust/integrity break on the public advocacy surface. The rule must be diff-based vs main. (c) **Partially refuted:** "upsert campaign by uuid" already pins matching by uuid, so rename and uuid-edit-in-place behavior mostly fall out; the residual gap is deletion detection (uuid-set diff, not source_file diff), re-enable on file re-appearance, duplicate-uuid arbitration, and write-back idempotency after a failed push (which would otherwise mint a second uuid → duplicate campaign). First-report B2 item 5 covers only delisted/dead re-entry on membership changes; nothing in the 15 touches sync concurrency, UUID enforcement, or deletion matching.

**Resolution.** One shared routine (`internal/campaign.Sync`) invoked by both v6ctl and the crawler tick, serialized by a session-level advisory lock acquired *before any git operation* (key: A1's `JobCampaignSync`, per reconciliation note 1). The Action's UUID rule becomes diff-vs-main (PRs may not introduce or change `uuid:` values, with an explicit rename allowance), backstopped DB-side by rejecting file uuids unknown to the DB (`--adopt-unknown-uuids` escape hatch for the production migration). uuid-keyed matching semantics are pinned: rename = source_file update; deletion = uuid-set diff → `disabled=true`; re-appearance = re-enable; in-place uuid edit = old disabled + new rejected, both loudly; duplicate uuid across files = keep the file matching stored source_file. Write-back is idempotent against push failure (reuse the uuid of a source_file-matching campaign whose uuid appears in no repo file). One B2 extension: the lifecycle sweep's campaign linkage counts only non-disabled campaigns (same amendment as A13 item 4 — state it once).

**spec_text:**

````
RESOLUTION — §7 campaign sync: serialization, uuid trust rule, and matching semantics. Fold into the implementation spec as §7.1–§7.4; references B2 (prior report) items 4–5.

============================================================
§7.1 Sync serialization (webhook + daily tick)
============================================================
There is exactly ONE sync implementation: `internal/campaign.Sync(ctx, cfg, pool)`. `v6ctl campaign sync` (webhook path) and the crawler daily tick (§2.6, runs after the lifecycle sweep) both call it. No other code touches the campaign checkout or campaign tables' YAML-derived columns.

Sync serializes across processes with a Postgres session-level advisory lock, acquired BEFORE any git operation (the lock protects the shared /srv/whynoipv6-campaign checkout as well as the DB):

```
conn := pool.Acquire(ctx)                    // dedicated connection, held for the whole run
row  := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('campaign_sync'))`)
if !locked { log.Info("campaign sync already running elsewhere, skipping"); return nil }  // exit 0
defer conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext('campaign_sync'))`); defer conn.Release()
```

Non-blocking by design: the loser exits cleanly because the winner is doing the identical work (webhook and cron are deliberately redundant per §7 step 2). If a process crashes mid-sync, the connection closes and the lock releases automatically — no stale-lock cleanup needed. The import transaction itself runs on the pool as usual; only the lock lives on the dedicated connection.

============================================================
§7.2 UUID trust rule — GitHub Action (diff vs main) + DB backstop
============================================================
Replace §7 step 1's "UUID must be absent or valid" with a diff-based rule. The Action (no DB access) checks out both the PR head and the merge-base with main and compares `uuid:` values per file:

- **Added file:** `uuid` must be absent or empty — UNLESS its value equals the uuid of exactly one file deleted in the same PR (git rename, possibly undetected as such). Then it passes, and the bot comment states loudly: "rename detected: old.yml → new.yml (uuid preserved)".
- **Modified file:** `uuid` must be byte-identical to the value in main (absent stays absent — only the bot commit ever adds one).
- **Deleted file:** allowed (that is how a campaign is retired; sync disables it, §7.3 step 5).
- Any other introduction or change of a `uuid:` value → check fails with: "uuid values are assigned by the import bot; remove the uuid field".

DB backstop (covers direct pushes and force-merges): during sync, a file whose uuid does not exist in `campaign` is REJECTED and reported ("unknown uuid — invented or DB drift; remove the uuid field to register as a new campaign"). Exception: `v6ctl campaign sync --adopt-unknown-uuids` inserts campaigns using the file's uuid — used once during the production-data migration (the existing YAML files already carry production uuids), never in cron/webhook paths.

============================================================
§7.3 Sync algorithm (normative; replaces §7 step 3's prose)
============================================================
After acquiring the lock and running `git -C $repo pull --ff-only`:

1. **Parse** every `*.yml`/`*.yaml` at the repo root (tolerant parse per §6 `internal/campaign`). Files failing schema/hostname/size validation are rejected and reported; they never partially import.
2. **Duplicate-uuid guard:** if one uuid appears in >1 file, keep the file whose path equals the DB's `campaign.source_file` for that uuid and reject the others; if none matches, reject all of them. (This is what defeats a copied-uuid file that coexists with the original.)
3. **Files with uuid:** upsert by uuid — update `name`, `description`; if `source_file` differs, update it and log "campaign renamed: old.yml → new.yml"; if `disabled=true`, set `disabled=false` and log "campaign re-enabled (file re-appeared)". Then diff memberships: additions/removals exactly per §7 + B2 item 5 (delisted → re-enable + next_check_at=now(); dead → keep disabled, next_check_at=now(); service/manual → unchanged; removals delete the membership row only).
4. **Files without uuid:** first check `SELECT uuid FROM campaign WHERE source_file = $file` — if a row exists AND its uuid appears in no repo file, REUSE that uuid (a previous write-back push failed; this makes write-back idempotent and prevents duplicate campaigns). Otherwise generate a fresh UUIDv4. Insert campaign + memberships inside the import transaction.
5. **Deletion (uuid-set diff, not source_file diff):** after steps 3–4, `UPDATE campaign SET disabled = true, updated_at = now() WHERE NOT disabled AND uuid <> ALL($all_uuids_seen_in_repo_including_newly_generated)` — log each. Membership rows are kept (soft delete, history preserved); orphaned rank-NULL domains are handled by the lifecycle sweep (see §7.4 amendment). Consequence, stated for the implementer: a uuid edited in place = old campaign disabled by this step + new uuid rejected by §7.2's backstop — both loudly in the report; nothing is silently renamed.
6. **Write-back:** after the import transaction commits, write generated uuids into their files, make ONE bot commit (`chore: assign campaign uuids [skip ci]`), push via deploy key; on non-fast-forward, `git pull --rebase` and retry once; on final failure, alert via ops webhook and continue — step 4's reuse rule recovers on the next run.
7. **Report** (§7 step 4): created/updated/renamed/re-enabled/disabled campaigns, membership adds/removes, rejected files with reasons (schema, duplicate uuid, unknown uuid), write-back status → ops webhook.

Config keys (viper, `campaign.` prefix):
```yaml
campaign:
  repo_path: /srv/whynoipv6-campaign   # shared checkout, owned by the service user
  git_remote: origin                   # push target for the bot commit (deploy key)
```

Ops (Ansible): the checkout and the GitHub deploy key (write access, campaign repo only) are provisioned for the single service user that runs both `crawler` and Semaphore-invoked `v6ctl`; the key lives in that user's ssh config. Public visibility: `NOT disabled` filtering for campaign list endpoints is already covered by M7's visibility scope.

============================================================
§7.4 Amendment to B2 item 4 (lifecycle sweep linkage)
============================================================
In the sweep's linkage predicate, campaign membership counts only if the campaign is enabled:
`linked := EXISTS (SELECT 1 FROM campaign_domain cd JOIN campaign c ON c.id = cd.campaign_id AND NOT c.disabled WHERE cd.domain_id = d.id) OR EXISTS child OR last_requested_at >= now() - lifecycle.live_check_linkage`.
Without this, a disabled campaign's kept membership rows would pin its rank-NULL domains in the frontier forever, contradicting §4.8's delist grace.
````

---

### A3. PR-validation "duplicate detection across files" contradicts the multi-campaign membership model

**Section:** §7 (step 1), §4.5 · **Severity:** major · **Confidence:** high · **needs_user_decision:** false

**Description.** §4.5's core design is that a domain is checked once per day no matter how many lists it is on — the membership join exists precisely so one domain can be in several campaigns, and the first report's changelog resolution already accepts N rows per change for N campaigns. §7 step 1 has the GitHub Action do "duplicate detection (within file + across files)" in a list of otherwise-blocking validation checks, which reads as rejection and would forbid legitimate multi-campaign membership. Verified against the campaign repo: **99 hosts already appear in 2+ YAML files today** (altinn.no in 3 files; danskebank.no, dnb.no in 2), so a blocking cross-file check fails the first PR touching most existing files — or, worse, induces a YAML dedup "fix" that silently deletes legitimate memberships. Also verified: **6 within-file duplicates exist today** (5 in Dutch_Central_Goverment.yml, 1 in German_Federal_Government.yml), so even the within-file check must be scoped to PR-changed files plus a one-time cleanup.

**Resolution.** Within-file duplicates → blocking validation error, evaluated only on files changed by the PR, after host normalization. Cross-file duplicates → never blocking, surfaced only as an informational line in the bot comment. One-time cleanup commit removes the 6 existing within-file duplicates. The importer independently dedupes within a file (membership PK + `ON CONFLICT DO NOTHING`) so unclean history can never break sync. Does not touch the first report's re-entry semantics (B2 item 5), which apply downstream in step 3 unchanged.

**spec_text:**

````
REPLACE §7 step 1 with:

1. **PR validation (GitHub Actions in the campaign repo — new, tiny).** Runs only on `pull_request`, and evaluates **only the `.yml` files changed by the PR** (git diff against the merge base) — never the whole repo, so pre-existing issues in untouched files cannot fail an unrelated PR. For each changed file, in order:

   - **YAML schema (blocking):** tolerant parse; exactly today's four keys — `title` (non-empty string, required), `description` (non-empty string, required), `uuid` (optional), `list` of hostnames (required, non-empty). Unknown keys → error.
   - **UUID (blocking):** must be absent or a valid UUIDv4. Contributors never invent UUIDs; a *new* file containing a `uuid` key is an error ("leave uuid out — the importer assigns it").
   - **Hostname validation (blocking):** per entry, after normalization (trim, lowercase, strip trailing dot, IDN → punycode via golang.org/x/net/idna Lookup profile): LDH-valid, has an eTLD+1 (PSL, ICANN section), no scheme/path/port/wildcard, ≤253 octets.
   - **Within-file duplicate (blocking):** two entries in the same file normalizing to the same host → error listing the host and both line numbers. Scope: the changed file only.
   - **Size cap (blocking):** ≤1000 list entries per file (config `campaign.max_domains_per_file`, default 1000).
   - **Cross-file duplicate (informational only — NEVER blocking):** for each host added by the PR that already appears in another campaign file, the bot comment notes "`host` is also in <other campaign title>". This is expected and legitimate: §4.5's membership model exists precisely so one domain belongs to several campaigns and is still checked once per day. Do not implement any code path that rejects, warns-as-failure, or auto-dedupes across files.
   - **Bot comment:** parsed summary per changed file ("32 domains, 3 subdomains → parents auto-linked"), plus the cross-file informational lines. Exit status reflects blocking checks only.

ADD to §7 step 3 (idempotent import), first sentence of the domain-diff paragraph:

   Before diffing, normalize and **dedupe hosts within each file** (first occurrence wins); the membership table's primary key `(campaign_id, domain_id)` plus `INSERT … ON CONFLICT DO NOTHING` makes duplicate entries harmless regardless of repo state. The importer must never fail or warn on a host present in multiple campaign files — that is N legitimate membership rows for one domain row.

ADD ops procedure (one-time, in the campaign repo, before merging the Action):

   Commit `chore: remove within-file duplicate hosts` deleting the 6 existing duplicate entries — Dutch_Central_Goverment.yml: magazines.rijksoverheid.nl, magazines.werkenvoornederland.nl, parlement.nl, services.belastingdienst.nl, temis.nl (second occurrence of each); German_Federal_Government.yml: bundesarchiv.de (second occurrence). Do NOT touch cross-file duplicates — the 99 hosts appearing in multiple files are intentional memberships.
````

*(Note: the UUID bullet above is further tightened by A2's §7.2 diff-vs-main rule — apply A2's rule as the final form of that check.)*

---

### A4. Claim query shape defeats its own index — full-index scan per claim in the steady state

**Section:** §2.5 vs §4.2 · **Severity:** major · **Confidence:** high · **needs_user_decision:** false

**Description.** Post-B2, the authoritative claim SQL still orders by `(rank ASC NULLS LAST, next_check_at ASC)` over `idx_domain_frontier (rank, next_check_at)`: `next_check_at <= now()` on the second index column is a per-tuple filter, not a range bound. When workers keep up (due backlog ≈ a few hundred rows spread uniformly across rank space — the steady state per §2.7's own math) each claim walks a large fraction of the ~1M-entry index, and any claim returning fewer than LIMIT rows (including every poll of an empty frontier, at a loop cadence the doc never specifies) scans **all** of it — against an index churned by ~1M non-HOT `next_check_at` updates/day. Realistic cost is tens–hundreds of ms per claim recurring every ~16–35s forever, worse under bloat, scaling linearly with the doc's 4.5M Tranco-full growth path. B2's resolution pins this shape into the spec verbatim (its partial-index predicate must "textually match"), so the defect ships unless amended. The predicate-contradiction half of the original raw finding is already resolved by B2 and is **not** re-reported. Caution adopted from verification: the originally proposed inner `ORDER BY next_check_at LIMIT k` pre-filter is rejected — it silently flips the brief's rank-priority fall-behind policy to aging-priority exactly when the due set is large.

**Resolution.** Replace the frontier index with a partial index on `(next_check_at)` carrying B2's exact eligibility predicate; keep the claim SQL text identical — the ORDER BY becomes an O(due) top-N sort over the due set, preserving exact rank-priority in both regimes. Forbid any full index led by `rank` (it would hand the planner back the pathological plan). Pin the previously unspecified empty-claim poll cadence (10s default), add autovacuum/fillfactor settings for the 2M-updates/day churn, and add §8 phase-2/3 EXPLAIN gates (<50ms with near-empty backlog, re-measured after churn). Amends B2 items 1–2 (index shape and commentary only); all predicates, lease, slow-lane, and lifecycle semantics unchanged.

**spec_text:**

````
RESOLUTION — frontier claim must be O(due), not O(index). Amends prior-report B2 items 1–2 (index shape and claim-SQL commentary only; all predicates, lease, slow-lane, and lifecycle semantics from B2 are unchanged).

============================================================
1. §4.2 — replace the frontier index (supersedes B2 item 1's index DDL)
============================================================
Delete `idx_domain_frontier (rank ASC NULLS LAST, next_check_at ASC)` in every form (both the original §4.2 DDL and B2's re-statement). Replace with:

```sql
-- Claim-path index: leading range column = next_check_at, so the claim scans
-- ONLY the due set. Predicate must textually match the claim query (B2).
CREATE INDEX idx_domain_due ON domain (next_check_at)
  WHERE NOT disabled OR disabled_reason IN ('dead', 'delisted');
```

Do NOT create any composite `(rank, next_check_at)` index — ranked list endpoints are served by `idx_domain_rank` and the per-classification partial indexes, and a rank-led index usable by the claim query would hand the planner back the pathological plan (rank-ordered full-index walk with `next_check_at <= now()` as a per-tuple filter). Invariant for future schema work: no full (non-partial) index with leading column `rank` may be added without re-running the Phase-2 claim-plan gate below.

Table storage settings (the claim/commit cycle updates every active row ≥2×/day — `claimed_at` at claim, `next_check_at` + status columns at commit; the commit update is always non-HOT because `next_check_at` is indexed):

```sql
ALTER TABLE domain SET (
  fillfactor = 90,
  autovacuum_vacuum_scale_factor = 0.02,
  autovacuum_analyze_scale_factor = 0.02
);
```

============================================================
2. §2.5 — claim SQL: text unchanged from B2 item 2; add the plan-shape note
============================================================
The authoritative claim SQL remains exactly B2's:

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
) RETURNING id, host, kind, disabled, disabled_reason, dead_streak, ...;
```

Add after it:

> **Plan shape (load-bearing).** The inner SELECT must execute as an index scan on `idx_domain_due` bounded by `next_check_at <= now()`, followed by a top-N heapsort on `(rank NULLS LAST, next_check_at)`. Cost is O(due-set) per claim: a few hundred rows in the steady state, at worst one pass over the full backlog after downtime (~hundreds of ms at 1M due, shrinking as the backlog drains) — and rank-priority fall-behind is exact in both regimes. Do NOT "optimize" with an inner `ORDER BY next_check_at LIMIT k` pre-filter before the rank sort: that silently flips the brief's fall-behind policy from rank-priority to aging-priority precisely when the due set is large. The `claimed_at` lease condition is intentionally a residual filter, not an index column (`claimed_at` is deliberately unindexed so lease stamping can be a HOT update).

**Claim-loop cadence (previously unspecified).** Config, `crawler` section:

```yaml
claim:
  batch_size: 200          # $1 above
  empty_poll_interval: 10s # sleep when a claim returns zero rows
```

After a claim returning ≥1 row, the process feeds its worker pool and claims again as soon as the batch is dispatched (the pool's slot availability is the natural throttle). After a claim returning 0 rows, sleep `empty_poll_interval` before the next preflight+claim cycle. With `idx_domain_due`, an empty-frontier claim is a sub-millisecond range probe, so this cadence is safe even at 1-second intervals; 10s is chosen to keep idle-log noise down, not for DB protection.

============================================================
3. §8 — phase gates (additions)
============================================================
Append to **Phase 2 Verify**:

> (e) claim-plan gate: with the table loaded at ≥1M rows and a near-empty due backlog (<1k rows due), `EXPLAIN (ANALYZE, BUFFERS)` of the claim query must show an index scan on `idx_domain_due` with buffers/rows examined proportional to the due set (not the table), execution <50 ms; the empty-frontier case (<5 ms) and the full-backlog case (all 1M due — verify rank-ordered claiming and record the O(due) cost) are both exercised.

Append to **Phase 3 Verify** (churn re-measurement):

> claim-plan gate re-run after real churn: after the 3 consecutive full passes (≥3M `next_check_at` updates through `idx_domain_due`), re-run the Phase-2(e) EXPLAIN with steady-state backlog; execution must remain <50 ms and `pgstattuple`/`pgstatindex` on `idx_domain_due` must show autovacuum keeping bloat bounded (index size stable across passes, not monotonically growing). Grafana dashboard gains a panel graphing claim-query duration (from the §2.6 checkpoint metrics) — alert threshold 250 ms.

============================================================
4. Doc correction (§2.5 rejection paragraph)
============================================================
The "a frontier column + SKIP LOCKED is the whole requirement" justification implicitly assumes O(due) claims; with `idx_domain_due` that assumption now actually holds, including on the §2.7 Tranco-full path (4.5M rows: claim cost stays proportional to the due set, not the table). No other change to the queue-rejection rationale.
````

---

### A5. No single host-canonicalization rule; Tranco post-normalization duplicates abort the batch upsert

**Section:** §3 (also §4.2, §5.1, §5.3, §7) · **Severity:** major · **Confidence:** high · **needs_user_decision:** false

**Description.** `domain.host` is UNIQUE with a stated canonical form (§4.2 "lowercase punycode FQDN"), but no single `canonicalize(host)` rule is mandated at the ingresses. M8 already pins POST /check normalization and B5 pins `resource_host` inserts, but three gaps remain: (1) **Tranco ingest** — §3 itself observes "mixed-case junk" in the live list, so two raw lines can normalize to the same host, and the doc's verbatim staging upsert (`INSERT ... ON CONFLICT (host) DO UPDATE`) then aborts the entire nightly import transaction with PostgreSQL's "ON CONFLICT DO UPDATE command cannot affect row a second time" (SQLSTATE 21000); no dedup step or which-rank-wins rule is given. (2) **Campaign import** — §7 says only "hostname validation (LDH/punycode)"; lowercasing and trailing-dot handling unstated, so a mixed-case YAML entry creates a duplicate entity past the UNIQUE constraint. (3) **API path params** — normalization unstated and production is internally inconsistent (domain.go:193 and campaign.go:280 lowercase; the changelog handlers do not), so bug-compat gives no answer and mixed-case or IDN URLs 404 on some endpoints. Trailing-dot input ("dnb.no.") is unaddressed for every ingress.

**Resolution.** One shared `Canonicalize(host)` in `internal/domain` (trim, strip exactly one trailing dot, lowercase, IDNA2008 `Lookup.ToASCII`, explicit LDH/label/253-octet/IP-literal post-checks), mandated at every ingress and at the API read path (404 on failure — badge excepted per reconciliation note 5). Tranco staging gets a `DISTINCT ON (host)` dedup with MIN(rank) winning and provenance counters. `host` is stored and served as punycode; Unicode display is a frontend-round concern. M8's step 1 becomes Canonicalize() plus a POST /check-only policy layer; B5's `lower_punycode` is defined as Canonicalize() output.

**spec_text:**

````
### Host canonicalization (single rule, all ingresses) — folds into §3, §4.2, §5.1, §5.3, §7

**Invariant.** `domain.host`, `resource_host.host`, and every hostname compared against them exist in exactly one form: lowercase punycode (ASCII/A-label) FQDN, no trailing dot, ≤253 octets, ≥2 labels. This form is both the storage form and the API serving form; Unicode (U-label) display conversion is a frontend concern, out of scope for this round.

**Function.** One implementation in the backend repo, importable by `api`, `crawler`, and `v6ctl`:

```go
// internal/domain/host.go
// Canonicalize returns the canonical form of a hostname:
// lowercase punycode FQDN, no trailing dot. It is the ONLY
// path by which a hostname may reach a DB write or DB lookup.
func Canonicalize(raw string) (string, error)

var ErrInvalidHost = errors.New("invalid host") // all failures wrap this
```

Algorithm (in order):
1. `s := strings.TrimSpace(raw)`; strip **exactly one** trailing `.` if present (`"dnb.no."` → `"dnb.no"`; `"dnb.no.."` keeps one dot and fails step 4 on the empty label).
2. Reject (`ErrInvalidHost`) if `s == ""` or contains any of `/ \ : @ ? # [ ]` or whitespace — callers must pass bare hostnames, never URLs.
3. `s = strings.ToLower(s)`.
4. `ascii, err := idna.Lookup.ToASCII(s)` (`golang.org/x/net/idna`, IDNA2008 lookup profile with UTS46 mapping). This converts Unicode → punycode and enforces strict LDH: rejects `_` (kills `_wildcard_.ph`), empty labels, disallowed characters, and bad hyphen placement. Any error → `ErrInvalidHost`.
5. Explicit post-checks (do not rely on profile internals): total length ≤253 octets; ≥2 labels; each label 1–63 octets; `net.ParseIP(ascii) == nil` (rejects IPv4 literals; bracketed IPv6 already died in step 2).
6. Return `ascii`.

Unit-test vectors (must pass): `DNB.no.`→`dnb.no`; `møre.no`→`xn--mre-qla.no`; `XN--MRE-QLA.no`→`xn--mre-qla.no`; reject: `_wildcard_.ph`, `a..b`, `1.2.3.4`, `[::1]`, `localhost` (1 label), 254-octet input, `http://x.no`.

**Mandated call sites and failure policy:**

| Ingress | When | On Canonicalize failure |
|---|---|---|
| Tranco import (§3 step 3) | per CSV line, replaces the ad-hoc "lowercase + reject" prose | count in `tranco_import.rejected_count`, log at debug, continue |
| Campaign PR validation (§7 step 1) | per YAML domain entry | CI check fails with the offending line |
| Campaign sync (§7 step 3) | per YAML domain entry, **before** entity lookup/creation and membership diff | entry skipped, counted under `rejected + reasons` in the sync report |
| POST /check (§5.3) | body domain — **this is M8 step 1**: Canonicalize() first, then M8's policy layer (reject RFC 2606 TLDs, `.internal`, `.local`) which applies to POST /check only | 400 `{"error":"invalid_host"}` per M8 |
| Resource discovery (B5) | B5's `lower_punycode` in `INSERT INTO resource_host` is defined as Canonicalize() output | host skipped (not inserted), no error surfaced |
| `v6ctl` verbs taking a hostname (`domain add`, etc.) | on argument parse | command errors with the reason |
| API path params (below) | per request | 404 |

**API read path (§5.1).** Every path parameter that carries a hostname — `{domain}` in `GET /domain/{domain}`, `/domain/{domain}/log`, `/campaign/{uuid}/{domain}` and its changelog variants, `/stats/domain/{domain}`, and `{host}` in `/resource/{host}/dependents` — is passed through Canonicalize() in a shared handler helper before the DB lookup; failure → plain 404 (it is a lookup miss, not a client contract violation). For `GET /badge/{domain}.svg`, strip the `.svg` suffix first. This intentionally supersedes production's mixed behavior (domain.go:193 and campaign.go:280 lowercase; the changelog handlers don't): the change is strictly additive (previously-404 mixed-case URLs now resolve) and is NOT a §5.1 bug-compat quirk — those cover response shapes, not lookup normalization.

**Tranco staging dedup (§3 step 4 — replaces the quoted upsert).** Canonicalization can fold two raw lines into one host; the naive `ON CONFLICT DO UPDATE` then aborts the transaction (SQLSTATE 21000, "ON CONFLICT DO UPDATE command cannot affect row a second time"). Dedup in the SELECT feeding the insert; **lowest rank wins**:

```sql
-- staging(rank int, host text): host already canonicalized in Go, garbage lines dropped
INSERT INTO domain (host, rank, next_check_at, created_by)
SELECT DISTINCT ON (host) host, rank, /* spread over next 24h */, 'tranco'
FROM staging
ORDER BY host, rank ASC        -- MIN(rank) wins the fold
ON CONFLICT (host) DO UPDATE SET rank = excluded.rank;
```

The delisting UPDATE (`rank = NULL` for hosts absent today) is unaffected. Provenance counters recorded per run in `tranco_import`: `line_count` (raw CSV lines), `rejected_count` (failed Canonicalize/validation), `duplicate_count` (`COUNT(*) - COUNT(DISTINCT host)` over staging), `imported_count` (rows in the insert). A `duplicate_count > 0` run is normal, not an error.

**Cross-references.** M8 (live check) and B5 (resource_host) in the prior report already pin their ingresses; this section is the definition they delegate to — no contradiction, M8's reserved-TLD list stays a POST /check-only policy on top of Canonicalize(). §4.2's comment `-- lowercase punycode FQDN` now has an enforcing function; no CHECK constraint is added (application-enforced, single write path per table).
````

---

### A6. No backup/restore strategy for the forever-changelog and confirmed state

**Section:** §4.4 (ops) · **Severity:** major · **Confidence:** high · **needs_user_decision:** false

**Description.** §4.4 declares the changelog "FOREVER — the credibility surface" (no retention, kept forever) and §8 phase 4 imports the site's credibility archive, yet the design doc contains no backup/restore content anywhere — no tool, schedule, retention, off-host copy, restore verification, or ops section (full-doc grep confirms; the only "dump" is the retained legacy production dump feeding the phase-4 importer, which covers pre-cutover history only). `scan`/`scan_detail` are re-derivable by re-crawling; `changelog` rows and confirmed `domain` state are not — losing this database permanently destroys the trust proposition the whole design is built around. TimescaleDB makes naive approaches actively dangerous: logical dumps of hypertables require the `timescaledb_pre_restore()`/`timescaledb_post_restore()` dance and a matching extension version at restore time, and plain `pg_dump` of individual hypertables silently misses chunks. For a single-maintainer operation the spec is the runbook. First report: nothing backup-related in B1–B6/M1–M9; M5 established that ops-load-bearing gaps are in scope.

**Resolution.** New "Backup & restore" subsection under the spec's Ops section (outline item 10): pgBackRest physical backups (weekly full + daily differential + continuous WAL archiving, ~28-day PITR), mandatory off-host repo (sftp to a second VM by default; S3-compatible drop-in), the TimescaleDB version-matching runbook rules, a weekly COPY-to-CSV logical export of `changelog`+`domain` as a version-decoupled belt-and-suspenders artifact, freshness monitoring on the existing ops webhook, and restore drills wired into the phase-3 verify list and the phase-4 cutover gate, then quarterly. Backups must be live from phase-3 start (when irreplaceable confirmed state begins accumulating).

**spec_text:**

````
### Ops: Backup & restore (add as a subsection of the spec's "Ops & config" section, alongside the M5 nginx/deploy notes)

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
- **Schedule (systemd timers, Ansible-managed):** `pgbackrest-full.timer` Sun 03:30 → `pgbackrest --stanza=whynoipv6 --type=full backup`; `pgbackrest-diff.timer` Mon–Sat 03:30 → `--type=diff backup`. 03:30 sits outside the daily crawl's heavy write window and the Tranco import timer. Both services set `OnFailure=ops-webhook-alert@%n.service` — the same ops webhook already used for Tranco import aborts and the fast-lane/provider breakers.
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

- **Phase-3 verify item (add to §8 phase 3's gate):** pgBackRest stanza created, first full backup completed, WAL archiving confirmed (`pgbackrest check`), and one full restore to a scratch instance succeeds before the first production sweep is declared done.
- **Phase-4 cutover gate (add to §8 phase 4, after the history import):** restore the latest backup to a scratch instance; `SELECT count(*) FROM changelog` matches prod as of the backup timestamp; the API binary starts against the restored DB and `GET /changelog` returns rows.
- **Quarterly thereafter:** repeat the phase-4 drill (timebox 1h), plus one spot-check that a weekly CSV export loads into a fresh vanilla PG (`\copy changelog FROM ...`). Record date + result in the ops notes.

#### 5. Monitoring (Grafana + ops webhook)

Nightly systemd timer runs `pgbackrest --stanza=whynoipv6 info --output=json` and `psql -Atc "SELECT version(), (SELECT extversion FROM pg_extension WHERE extname='timescaledb')"`, appends both to `/var/log/pgbackrest/verify.log` (this is the version-of-record for §2 item 1), and alerts the ops webhook if: newest backup is older than 26 h, newest archived WAL is older than 1 h, or the last export timer failed. Optionally expose the same three as a Grafana panel via a textfile/exec collector — but the webhook alert is the required part.
````

---

### Minors

### A7. Tranco import trigger ownership and the 2h retry loop are ambiguous

**Section:** §3 · **Confidence:** high · **needs_user_decision:** false

§3 names two possible owners for the 23:15 UTC trigger ("daily systemd timer / `crawler` coordinator") without choosing; "retry in 2h" has no owner and is only implementable in the coordinator; there is no give-up/alert policy when Tranco publishes nothing for days; and concurrent first-runs from two triggers are unguarded. **Resolution:** the coordinator is the sole scheduled owner (23:15 UTC cycle, 2h re-attempts); `v6ctl tranco import` is the same code path for manual use; every execution is advisory-lock-serialized (map the `(4200,x)` keys below onto A1's registry per reconciliation note 1); unchanged-list_id compares against the last *successful* import; aborted lists are not auto-reimported (`--force` override); 48h staleness → rate-limited ops-webhook warning. Config: `tranco.import_at`, `tranco.retry_interval`, `tranco.stale_warn_after`. Composes with B2 item 6's sanity guard unchanged.

**spec_text:**

````
### §3 amendment — Tranco import trigger ownership and retry loop

Replace §3's "(also invoked by a daily systemd timer / `crawler` coordinator at 23:15 UTC)" and the parenthetical "(retry in 2h)" with the following.

**Owner.** The `crawler` coordinator goroutine is the sole scheduled trigger. No systemd timer is deployed for Tranco import. `v6ctl tranco import` invokes the identical code path (shared `ingest/` package) for manual/break-glass runs; §6's description of v6ctl as "cron targets" does not apply to this verb.

**Coordinator import cycle:**
1. At `tranco.import_at` (default 23:15 UTC, after the 22:00–23:00 UTC list generation) start a new import cycle. A cycle ends when a new list is successfully imported OR the next cycle starts (retry state resets); max ~11 attempts/day.
2. Each attempt:
   a. Acquire the advisory lock (below). Not acquired → another import is running; treat this attempt as done, reschedule per (e).
   b. `GET https://tranco-list.eu/top-1m-id`. On network/HTTP error → release lock, reschedule per (e).
   c. If the returned id equals `list_id` of the most recent `tranco_import` row with `aborted = false` → no new list yet; release lock, reschedule per (e).
   d. If the returned id equals `list_id` of any `tranco_import` row with `aborted = true` → do NOT auto-reimport an aborted list (it would abort again and spam the webhook); release lock, reschedule per (e). Operator override: `v6ctl tranco import --force` (which, per the prior report's B2 item 6, bypasses the min_rows/max_delist_pct guard — it does not bypass the advisory lock).
   e. Otherwise run §3 steps 2–5 unchanged (conditional GET, parse/reject, sanity-guarded upsert, provenance row). Success → cycle complete. Sanity-guard abort or error → reschedule.
   Reschedule = re-attempt after `tranco.retry_interval` (default 2h) unless the next 23:15 cycle starts first.
3. Staleness alert: on every attempt, if `now() - (SELECT max(imported_at) FROM tranco_import WHERE aborted = false)` > `tranco.stale_warn_after` (default 48h), send an ops-webhook WARNING ("no new Tranco list for <N>h; ranks frozen at list <list_id>"), rate-limited to once per 24h. Warning, not page: the unchanged-list_id short-circuit means staleness freezes ranks — it never delists.

**Concurrency guard (advisory-lock singleton pattern).** Every import execution takes a session-level `pg_try_advisory_lock(4200, 1)` before step 2b (i.e., before download/parse, released after the upsert transaction commits or on any exit). Lock not acquired: v6ctl exits non-zero with "tranco import already in progress"; the coordinator logs at INFO and skips the attempt. The same pattern, distinct keys, guards the other coordinator singletons so that running two crawler processes (§6: one per machine) never duplicates them — each process's coordinator fires on schedule, only the lock winner executes, the loser skips silently:

| key (classid, objid) | job |
|---|---|
| (4200, 1) | tranco import |
| (4200, 2) | daily tick (§2.6 steps 1–4, incl. the prior report's lifecycle sweep) |
| (4200, 3) | campaign sync (§7 — applies to both the merge-webhook path and the daily-tick path) |

Register these constants in one Go file (`internal/ingest/locks.go`); never reuse classid 4200 elsewhere.

**Config (extends the prior report's `tranco:` block — min_rows/max_delist_pct unchanged):**
```yaml
tranco:
  import_at: "23:15"        # UTC; daily cycle start
  retry_interval: 2h        # re-attempt spacing within a cycle
  stale_warn_after: 48h     # ops-webhook warning when no successful import for this long
```

**DDL:** no changes. `tranco_import.imported_at` (§4.9) plus the prior report's `aborted`/`note` columns already carry everything the loop and the staleness check need.
````

---

### A8. Numeric drift: worker slots 60 vs 72, three bulk-resolver load ranges, 16 vs 15 checks

**Section:** §1/§2.4/§2.5/§2.7 · **Confidence:** high · **needs_user_decision:** false

Three verified drifts: §2.5 cites "§2.7 shows ~60 average concurrency" while §2.7 derives ~72; bulk-resolver load appears as three different ranges; §1 says "16 checks" while the v6audit engine registers exactly 15 (matching the brief and §2.2). **Resolution:** one canonical constants block, re-derived *after* the first report's B1/B5/M6 resolutions (which change the fetch and local-resolver rows): 15 checks; ~72 slots avg / 128 provisioned; local Unbound ~12–16M/day ≈ 140–190 qps; separate ~2–4 qps resource sweep; ~3M fetches/day. §2.7's table is the sole normative home; §1/§2.4/§2.5 cite it.

**spec_text:**

````
## Resolution: canonical sizing constants + numeric normalization (§1/§2.4/§2.5/§2.7)

### 0. Canonical constants (normative; §2.7's table is their single source — all other sections cite, never restate)

| Constant | Value |
|---|---|
| Engine checks | **15** (the §2.1 inventory; latency.go registers 2 of them: latency_ipv4 + latency_ipv6) |
| Scan rate | ~12 domains/s sustained (1.03M/day) |
| Worker slots | **~72 average**, **128 provisioned** (2 processes × 64) |
| Public-resolver load | 6.2M queries/day ≈ 71 qps total, **~24 qps/provider** |
| Local (Unbound) bulk load | **~12–16M queries/day ≈ 140–190 qps** |
| Resource-host sweep (separate, per B5) | ~100–300k lookups/day ≈ **2–4 qps** |
| HTTP(S) fetches (per M6) | ~3M/day ≈ **35/s** |

### 1. §1 edits (three)
- Item 2: "`internal/checker` (16 checks, …)" → "`internal/checker` (**15 checks**, …)".
- Item 3: "~150–200 qps on Unbound" → "**~140–190 qps on Unbound (§2.7)**".
- Cost-honesty paragraph: "~12 domains/s sustained, ~60–130 concurrent domain slots" → "~12 domains/s sustained, **~72 concurrent domain slots on average (128 provisioned, §2.7)**".

### 2. §2.4 edit
"Bulk = ~12–18M queries/day ≈ **140–210 qps** against Unbound." → "Bulk = **~12–16M queries/day ≈ 140–190 qps** against Unbound (derivation in §2.7; excludes the decoupled resource-host sweep, ~2–4 qps)."

### 3. §2.5 edit
"(start: 2 processes × 64 slots; §2.7 shows ~60 average concurrency suffices, headroom for tail latency)" → "(start: 2 processes × 64 slots = 128; §2.7 derives **~72 slots average**, so 128 leaves headroom for tail latency)".

### 4. §2.7 table — replace the local-resolver and worker-concurrency rows (composes with B5's row removal/addition and M6's fetch-row rewrite; apply after those)

| Stage | Volume/day | Rate | Sizing |
|---|---|---|---|
| Local-resolver queries (A ≤2 — conditional per B1, only on NOERROR-empty AAAA quorum, fires for ~75% of names; NS walk ~2 + NS-AAAA ≤4; MX 1 + MX-AAAA ≤5·70%; DS+SOA 2; TXT 1; PTR ≤3·25%) ≈ 12–16/domain | **~12–16M** | **140–190 qps** | Unbound: 1–3% of tuned single-instance capacity |
| Resource-host sweep (registry AAAA, bulk resolver — per B5) | ~100–300k | ~2–4 qps | negligible |
| Worker concurrency: phase-1-only (pure DNS after M6) ≈ 2–4s wall (775k), full phase-2 incl. latency probes ≈ 10–25s (258k) → weighted ≈ 6s/domain | — | 12/s × 6s = **~72 slots avg** | provision 128 slots (2 procs × 64) for tail latency |

(The HTTP-fetch row is M6's: "≈ 11–12 per v6 domain × 258k ≈ ~3M/day ≈ 35/s"; egress-bandwidth row unchanged.)

### 5. Global consistency rule for the spec writer
Wherever the implementation spec mentions check count, slot counts, or resolver qps, use the §0 constants verbatim. Do not introduce new independent ranges; if a future edit changes a §2.7 input (adoption %, per-check query counts), re-derive the table and update the citing sentences in §1/§2.4/§2.5 in the same commit.
````

---

### A9. Citation staleness: "16 checks" in the executive summary (28-YAML and scheduler claims verified correct)

**Section:** §8/§2.1/§2.5 · **Confidence:** high · **needs_user_decision:** false

Only one of the raw finding's three claims survived: line 35's "16 checks" is off by one (runner.go registers exactly 15). The other two proposed edits are **rejected as factually wrong**: the campaign repo has exactly 28 `.yml` files (the "29" counted an eza header line), and §2.5's scheduler rejection is accurate (workers.go cursor-paginates queries but still materializes all due domains into one slice). Overlaps A8's check-count fix; the durable form is enumeration, not a count.

**spec_text:**

````
EDIT backend-design.md line 35 (executive summary, bullet 2), one word:

  OLD: `internal/checker` (16 checks, two-phase conditional execution, SSRF-pinned dialer,
  NEW: `internal/checker` (15 checks, two-phase conditional execution, SSRF-pinned dialer,

SPEC-AUTHORING RULE (fold into the implementation spec's engine section, alongside prior-report M6's §2.8 Engine adaptation contract): the check set is defined by enumeration, not by count. The authoritative list is v6audit runner.go's 15 registered checkers — dns_aaaa_base, dns_aaaa_www, dns_ns, dns_mx, dnssec, http, https, tls, response_parity, resource, smtp, spf, dns_ptr, latency_v4, latency_v6 — as adapted per M6 (base/www wrapped into composite observations; scoring deleted). Any prose stating a numeric count of checks must be derived from that enumeration at spec-writing time; do not carry "16" (or any stale count) forward.

NO-CHANGE confirmations (record so later passes don't re-open them):
- §8 "import all 28 YAMLs" stays as written; 28 is the actual file count in whynoipv6-campaign. Optionally phrase as "all *.yml files in the repo root (28 today)" for future-proofing — cosmetic, not required.
- §2.5's rejection of the v6audit scheduler stays as written; its three cited failure modes (in-memory materialization of all due domains, per-domain job-row insert volume, 2-minute job timeout) are all true of workers.go:1067-1131.
````

---

### A10. shortuuid encoding not pinned to a concrete alphabet/library

**Section:** §5.1 · **Confidence:** high · **needs_user_decision:** false

§5.1 says "UUIDs in URLs are shortuuid-encoded" but never names the codec, and "shortuuid" is implementation-defined (alphabets/lengths differ across libraries and lithammer majors). Production uses `lithammer/shortuuid/v4` DefaultEncoder (base57, fixed 22 chars); the token is part of the frozen wire contract (bookmarkable `/campaign/:uuid` URLs, changelog `domain_url`). A different codec 404s every shared campaign link at cutover. Decode-failure body is production's `404 {"error":"Invalid UUID"}` (not "campaign not found" as the raw finding guessed). Verified test vectors included.

**spec_text:**

````
### §5.1 addendum — shortuuid codec pin (wire-frozen)

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
3. `domain_url` in changelog responses: `"/campaign/{token}/{host}"` (see M3's table — every `shortuuid(...)` there means this codec).
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
````

---

### A11. 404-on-empty vs empty-list cleanup: the boundary is unpinned for search (and one changelog endpoint M3 missed)

**Section:** §5.1 · **Confidence:** high · **needs_user_decision:** false

Production 404s on zero search matches; §5.1's "empty lists return []" cleanup never says whether zero-match searches count as empty lists — the two readings give opposite status codes on frozen-frontend endpoints. M3 pinned this boundary only for two changelog endpoints. **Resolution:** one mechanical rule — the []-cleanup rewrites bodies only, never status codes; all seven of production's zero-rows 404s are kept byte-exact (including the *third* changelog 404 at changelog.go:229 that M3's "two per-domain cases" under-counted — a completion, not a contradiction); `GET /metric/asn/search/{q}` is the one search endpoint with no production 404 check, so it gets `200 []`.

**spec_text:**

````
### §5.1 addendum — zero-result behavior (status codes are never changed by the []-cleanup)

**Rule.** The "empty lists return `[]` instead of JSON `null`" cleanup applies only to responses production already served as **HTTP 200 with a `null` body** (a serialized nil slice). It never changes a status code. Every zero-result **404** production emits is kept bug-compatibly: same status, byte-identical error JSON.

**Kept 404s on zero results** (exact production bodies; content-type application/json):

| Endpoint | Fires when | Response |
|---|---|---|
| `GET /domain/search/{q}` | trigram search matches 0 publicly-ranked domains (M7 predicate) | `404 {"error":"no domains found"}` |
| `GET /campaign/search/{q}` | 0 campaign-domain matches | `404 {"error":"No domains found"}` (note the capital N — differs from domain search) |
| `GET /campaign/{uuid}?offset=` | member-domain page is empty: unknown campaign **or** `offset >= member count` (paging past the last page 404s — bug-compatible, frontend tolerates) | `404 {"error":"Campaign not found"}` |
| `GET /campaign/{uuid}/{domain}` | single resource not found (membership or host miss) | `404 {"error":"Domain not found"}` |
| `GET /changelog/{domain}` | zero changelog rows (per M3, unchanged) | `404 {"error":"No changelog entries found for {domain}"}` |
| `GET /changelog/campaign/{uuid}` | zero changelog rows for the campaign — **completes M3's inventory**: production's third zero-rows check (changelog.go:229), missed by M3's "two per-domain cases" phrasing | `404 {"error":"No changelog entries found for campaign {uuid}"}` where `{uuid}` is the **decoded canonical 36-char UUID**, not the shortuuid from the URL |
| `GET /changelog/campaign/{uuid}/{domain}` | zero rows (per M3, unchanged) | `404 {"error":"No changelog entries found for campaign {uuid} and domain {domain}"}` where `{uuid}` here is the **shortuuid exactly as given in the URL** (production does not decode it for the message) |

Consequence: because both `{"data":[...]}`-enveloped endpoints (the two searches) 404 on zero matches, `{"data":[]}` never occurs on the legacy surface.

**[]-cleanup applies (production returned 200 `null`)** — zero rows → `200 []`:
`GET /domain`, `/domain/heroes`, `/domain/topsinner`, `/domain/{domain}/log`, `/country`, `/country/{code}/sinners`, `/country/{code}/heroes`, `/changelog`, `/changelog/campaign`, `/campaign`, `/campaign/{uuid}/{domain}/log`, `/metric/asn`, and — explicitly — `GET /metric/asn/search/{q}` (production metric.go:132-157 has no zero-rows check; it is the one search endpoint that returns `200 []` on zero matches).

Single-resource endpoints are untouched by this rule and 404 as already pinned (M7: `GET /domain/{domain}` 404 for unknown/disabled).

**Parity tests (extend the golden-fixture plan):** one fixture per row of the 404 table asserting status + exact body (use a garbage query like `zzzzqqqq` for the searches; a valid campaign with `offset=10000` for the paging case), plus `GET /metric/asn/search/zzzzqqqq` → `200 []`.
````

---

### A12. §5.1 claims the /domain sinner list "matches old semantics" — it doesn't (membership narrowing is undeclared)

**Section:** §5.1 (GET /domain) · **Confidence:** high · **needs_user_decision:** false

Production membership is OR-based (`base_domain='unsupported' OR www_domain='unsupported'`, same for `/country/{code}/sinners`), while the new sinner class is base-unsupported only (locked ladder). Line 933's "matches old semantics" is therefore a false parity claim that would make phase-4 membership-parity tests fail — and unlike the adjacent heroes row, the shift is not declared under OPEN-6. **Resolution:** declare it as a fourth deliberate, announced break in the OPEN-6 methodology-v2 note (alongside B3 R4 and B6, which already extend that note) and scope parity tests for the two endpoints to shape/ordering plus a subset assertion and synthetic fixtures.

**spec_text:**

````
### §5.1 compat-table corrections (sinner-list membership narrowing — deliberate, announced)

**1. Replace the `GET /domain` row (design doc line 933) with:**

| `GET /domain?offset=` | Sinner list (`classification='sinner'`) by rank. **Membership narrows vs production:** old query was `base_domain='unsupported' OR www_domain='unsupported'` (domain.sql ListDomain); new membership is base-unsupported only — domains with base supported but www unsupported leave this list and surface on `/domain/almost` as `partial`/`www_missing`. Response shape unchanged. This is a **deliberate, announced** break — OPEN-6: decided (methodology-v2 note) |

**2. Amend the `GET /country/...` row (line 939): append to its cell:**

> `/country/{code}/sinners` membership narrows identically (production used the same OR-predicate, country.sql ListDomainsByCountry) to `classification='sinner'` — same deliberate OPEN-6 break as `/domain`; ordered by rank (old: by id — minor fix, as already noted).

**3. Amend OPEN-6's decision cell (line 1187) to read:**

> **Serve migrated seed values immediately**; publish a "methodology v2" note for the deliberate metric shifts (hero bar, real `v6_only`, correct multi-label-TLD NS, **and sinner-list membership: `/domain` + `/country/{code}/sinners` now list base-unsupported domains only — www-only offenders move to the new `/domain/almost` "almost there" list instead of the shame list**).

(This composes with the prior report's amendments that hang off the same note: the `v6_ready` www formula (B3 R4) and the stats-metric value fixes (B6). The methodology-v2 note is the single public changelog of all deliberate metric shifts.)

**4. Phase-4 golden parity test scoping (extends line 1147's "modulo documented deviations"):**

- For `GET /domain` and `GET /country/{code}/sinners`: assert **response shape** (field names, types, envelope/no-envelope, pagination behavior) and **ordering** (rank ascending) against production captures — do **not** assert row-set equality. Instead assert the documented direction of divergence: `new_members ⊆ old_members` on the captured pages (every domain the new backend lists was also listed by production; the reverse need not hold).
- Synthetic membership fixture (production data can't prove the negative): seed one entity with confirmed `base=supported, www=unsupported` and one with `base=unsupported`. Assert the first appears in `/domain/almost` and NOT in `/domain`; the second appears in `/domain` and NOT in `/domain/almost`. Repeat for the country-scoped pair via the fixture's country.
- All other endpoints in the §5.1 table keep full-fidelity golden parity except where a resolution in this report already scoped them (legacy serialization branches, disabled/ranked predicate, `top_shame` rank fix, heroes membership).
````

---

### A13. Disabled-campaign visibility unspecified for GET /campaign and uuid-addressed endpoints

**Section:** §5.1 (GET /campaign) · **Confidence:** high · **needs_user_decision:** false

Production *accidentally* lists disabled campaigns with zeroed counts (the `disabled=FALSE` predicate sits in a LEFT JOIN condition), while §7 makes `campaign.disabled=true` the deliberate soft-delete outcome of deleting a campaign YAML — the bug-compat rule and §7's intent contradict on whether deleted campaigns stay publicly listed. M7 scopes `NOT disabled` only for domain rows. The response-shape half of the raw finding is covered (§5.1 bug-compat + B3 R4 pin the CampaignListResponse shape). **Resolution:** exclude disabled campaigns from `GET /campaign`; 404 every uuid-addressed campaign endpoint for them (announced fix); filter them from cross-campaign endpoints; extend B2's sweep linkage to count only non-disabled campaigns (same amendment as A2 §7.4); skip them in stats snapshots; state re-add semantics.

**spec_text:**

````
## Disabled-campaign visibility (fold into §5.1 campaign rows and §7)

**Context.** `campaign.disabled = true` is the soft-delete outcome of deleting a campaign YAML (§7). Production *accidentally* keeps disabled campaigns in the public list with zeroed counts, because `campaign.disabled = FALSE` sits in the LEFT JOIN condition instead of a WHERE clause (`whynoipv6/db/query/campaign.sql` ListCampaign/GetCampaignByUUID). The new backend fixes this — an **announced fix** in the same category as the `[]`-instead-of-null cleanup in §5.1. Disabled campaigns disappear from the public API entirely; their rows, memberships, changelog, and stats history are preserved in the database.

**1. `GET /campaign`.** Add `WHERE NOT campaign.disabled`; order `ORDER BY campaign.id` (production parity). Row shape is production's CampaignListResponse, unchanged:

```json
{ "id": 7, "uuid": "<shortuuid>", "name": "...", "description": "...", "count": 42, "v6_ready": 17 }
```

`id` = campaign.id (int), `uuid` = shortuuid-encoded campaign.uuid, `count` = COUNT of campaign_domain members whose domain row is `NOT disabled` (M7 scoping), `v6_ready` = B3 R4 formula (`base='supported' AND ns='supported' AND www IN ('supported','not_applicable')`) over the same member set. A live campaign with zero members returns `count:0, v6_ready:0` (row kept — only `campaign.disabled` removes it from the list).

**2. UUID-addressed campaign endpoints → 404 when disabled.** One shared resolver: decode shortuuid → `SELECT ... FROM campaign WHERE uuid = $1 AND NOT disabled`; no row → `404`. Applies to:
- `GET /campaign/{uuid}` (composite), `GET /campaign/{uuid}/{domain}`, `GET /campaign/{uuid}/{domain}/log`
- `GET /changelog/campaign/{uuid}`, `GET /changelog/campaign/{uuid}/{domain}`
- `GET /stats/campaign/{uuid}` (§5.2)

The composite's `{campaign}` object (only reachable for non-disabled campaigns) carries the same shape as the list row in item 1.

**3. Cross-campaign endpoints filter disabled campaigns.** `GET /campaign/search/{q}` and `GET /changelog/campaign` join through `campaign` and add `NOT campaign.disabled` (in addition to M7's `NOT domain.disabled` on the domain side).

**4. Lifecycle-sweep amendment (extends B2, sweep step 1a).** Campaign linkage counts only non-disabled campaigns:

```sql
linked_campaign := EXISTS (
  SELECT 1 FROM campaign_domain cd
  JOIN campaign c ON c.id = cd.campaign_id
  WHERE cd.domain_id = d.id AND NOT c.disabled
)
```

Rationale: membership rows are preserved on soft delete (§7 history preservation); without this join, a deleted campaign's rank-NULL members would stay frontier-eligible forever. With it, they enter the normal `orphaned_at` → 30-day grace → `delisted` path, and campaign re-enable (item 6) restores linkage before the next sweep or via the delisted re-entry rule (B2 item 5).

**5. Stats snapshots (§4.7).** The nightly `stats_campaign_daily` job writes rows only for `NOT disabled` campaigns. Historical rows for disabled campaigns are retained untouched; on re-enable the series resumes with a gap (frontends already tolerate missing days).

**6. Re-add semantics (§7, explicit).** Restoring a deleted YAML file (same `uuid`) → the idempotent sync upserts by uuid and sets `disabled = false, updated_at = now()`. Because campaign row, memberships, and domain state were all preserved, the campaign reappears fully populated on the next sync — no re-import of members, no changelog noise. A restored YAML *without* a uuid is treated as a new campaign per §7 step 3 (new uuid written back); the old disabled row stays soft-deleted.
````

---

### A14. top_shame (editorial picks) has no writer — no v6ctl verb, API, or import path

**Section:** §4.9 / §6 / §8 phase 4 · **Confidence:** high · **needs_user_decision:** false

`top_shame` has a read path (`GET /domain/topsinner`) but no write path anywhere: §6's v6ctl list has no shame verb, OPEN-4 forbids an admin HTTP surface, the "003 seed" migration structurally cannot populate it (FK on `domain_id`; domain rows exist only after phase-1 ingestion), and §8 phase 4's migration list omits the curated list — shipped verbatim, the homepage HomeSinners section renders blank. **Resolution:** `v6ctl shame add|remove|list`; phase-4 host-resolved import of production's 12 curated rows (from the live dump, not the seed file); topsinner read query pinned to compose with M7 *and* restore production's auto-hide (`classification='sinner'` filter), which also dictates warn-don't-block add semantics.

**spec_text:**

````
# Resolution: top_shame writer, import path, and read-query pin

## 1. §6 — add v6ctl verbs (fold into the cmd/v6ctl verb list: `... disable, service-candidates, resource add, shame, export, stats recalc, migrate`)

```
v6ctl shame add <host> [--reason "..."]
v6ctl shame remove <host>
v6ctl shame list
```

Semantics (single-maintainer editorial tool; direct DB access like the other v6ctl verbs):

- **`shame add <host>`** — normalize `<host>` with the same normalization used everywhere else (lowercase, strip scheme/port/trailing dot). Look up `domain` by host. **Error (exit 1)** if: no row exists, `kind <> 'apex'`, `rank IS NULL`, or `disabled` — editorial picks must satisfy the M7 publicly-ranked predicate, otherwise the row could never render. Then:

  ```sql
  INSERT INTO top_shame (domain_id, reason)
  VALUES ($1, $2)
  ON CONFLICT (domain_id) DO UPDATE SET reason = EXCLUDED.reason;
  ```

  (idempotent; re-add updates reason, preserves added_at; `--reason` omitted ⇒ NULL). If the domain's current `classification <> 'sinner'`, **warn but succeed**: "added; will not render on /domain/topsinner until classified sinner" — rows are durable picks, visibility is computed at read time (§5.1 below), matching production where fixed domains stay in the table but drop out of the view.

- **`shame remove <host>`** — resolve host to domain_id (error if unknown host), `DELETE FROM top_shame WHERE domain_id = $1`; print "not on the shame list" if 0 rows deleted (exit 0).

- **`shame list`** — print all rows joined to domain: `host, rank, classification, reason, added_at, visible` where `visible = (classification = 'sinner' AND rank IS NOT NULL AND NOT disabled)`, ordered by rank.

- Shame edits write **no changelog entries** (editorial action, not an observed status transition).

## 2. §5.1 — GET /domain/topsinner query pin (extends prior-report M7, which already adds the publicly-ranked predicate to this join)

```sql
SELECT d.*
FROM top_shame ts
JOIN domain d ON d.id = ts.domain_id
WHERE d.classification = 'sinner'          -- production parity: domain_shame_view
                                           --   filtered base_domain = 'unsupported';
                                           --   a shamed domain that ships IPv6
                                           --   auto-hides, its row persists
  AND d.rank IS NOT NULL AND NOT d.disabled  -- M7 publicly-ranked predicate
ORDER BY d.rank ASC;                        -- deviation already declared in §5.1:
                                            -- real rank replaces production's id
```

No pagination (production returned the whole filtered list; it is ≤ a dozen rows).

## 3. §8 phase 4 — add to the one-time data-migration items (the `v6ctl migrate` importer)

Add to the enumerated import list ("current statuses as seed confirmed values, full changelog history, trailing 90 days of scan history"): **the curated `top_shame` list**. Procedure:

- Source: the `top_shame` table (`site TEXT`) in the retained **production dump** — not the hardcoded 02_data.up.sql seed (the live table is authoritative if the maintainer has edited it since). As of the audit it holds 12 hosts: twitter.com, twitch.tv, ebay.com, imgur.com, imdb.com, wordpress.com, github.com, paypal.com, stackoverflow.com, soundcloud.com, nytimes.com, w3schools.com.
- For each site, resolve to the new `domain.id` by normalized host and insert:

  ```sql
  INSERT INTO top_shame (domain_id)
  SELECT d.id FROM domain d WHERE d.host = $1
  ON CONFLICT (domain_id) DO NOTHING;      -- reason NULL: production has no reason column
  ```

- A site with no matching domain row (fell out of Tranco top-1M between dump and import) is **logged as a warning and skipped** — it must not fail the migration; the operator re-adds it later via `v6ctl shame add` if desired.
- Run order: after phase-1 Tranco ingestion has populated `domain` (the FK makes this a hard precondition), alongside the other phase-4 imports.
- Entries whose domains are no longer sinners (e.g. github.com) are imported anyway; the §5.1 read filter hides them, same as production. Do not prune on import.

## 4. §6 clarifying note (one line, next to the migrations list)

The `003 seed` migration seeds static reference data only (e.g. the country table) and **does not** populate `top_shame` — its `domain_id` FK requires phase-1 ingestion; population is the phase-4 importer plus `v6ctl shame add` thereafter.
````

---

### A15. Badge endpoint has no behavioral spec (SVG states, unknown domain, caching)

**Section:** §5.2 (badge row) · **Confidence:** high · **needs_user_decision:** false

`GET /badge/{domain}.svg` is one table cell ("optional, cheap") with no prior art in any repo, yet §8 ships it in phase 6. M5's §5.0 baseline already pins Content-Type/Cache-Control (referenced, not redefined); everything else — rendered value, copy, unknown/disabled behavior — was unpinned. **Resolution:** classification-driven badge with a fixed copy table using *status* vocabulary (a README badge never says "sinner"; the ladder branding stays site-side), 200 gray "unknown" for unknown/disabled/unknown-classification hosts (404 = broken image in READMEs; deliberately differs from `/domain/{domain}`'s production-parity 404), M8-shared validation (invalid host → 400 JSON — the declared exception to A5's path-param-404 rule), six precomputed deterministic shields.io-flat SVG variants, golden-file tests. Droppable from phase 6 with no ripple.

**spec_text:**

````
### §5.2a GET /badge/{domain}.svg — normative behavior (replaces the "optional, cheap" row's blank spec)

**Route.** chi pattern `GET /badge/{domain}.svg` (chi matches a static suffix after a param in one segment; the `.svg` is part of the route, never part of the `domain` param). `{domain}` is the bare host — no scheme, no port, no trailing dot (strip one trailing dot if present).

**Input handling.** Normalize and validate with the SAME function as POST /check step 1 (M8 §5.3): lowercase, punycode-normalize (IDNA Lookup ToASCII), LDH hostname, ≤253 octets, ≥2 labels; reject IP literals and `.internal`/`.local`/RFC 2606 TLDs. Failure → **400** `{"error":"invalid_host","message":"..."}` (standard JSON error, not an SVG; malformed hosts are not legitimate embeds).

**Lookup.** `SELECT classification, gold, disabled FROM domain WHERE host = $1`. Per M7, entity endpoints are not rank-scoped: any kind/origin (Tranco apex, campaign domain, subdomain, live-check host) resolves. **Read-only, zero side effects**: never inserts a domain row, never enqueues a check_job, never touches `last_requested_at`.

**Badge selection (first match wins):**

| Condition | Message | Message color |
|---|---|---|
| no row, `disabled = TRUE` (any reason), or `classification = 'unknown'` | `unknown` | `#9f9f9f` (gray) |
| `classification = 'hero' AND gold` | `gold` | `#d4af37` |
| `classification = 'hero'` | `supported` | `#4c1` |
| `classification = 'partial'` | `partial` | `#dfb317` |
| `classification = 'sinner'` | `unsupported` | `#e05d44` |
| `classification = 'inactive'` | `inactive` | `#9f9f9f` |

Always **HTTP 200** for a valid host — a 404 renders as a broken image in READMEs. Disabled → gray `unknown` implements M7's public-exclusion rule for this endpoint; it deliberately differs from `GET /domain/{domain}`'s 404-on-disabled, which is production parity and does not bind this new endpoint. Badge copy is public status vocabulary, NOT ladder branding: a README badge never says "sinner"/"hero" (owners won't embed self-shaming badges; the ladder wording stays on the site). The copy/color table is one Go constant table — the single place to reword.

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

**Headers.** Per the M5 §5.0 baseline (already pinned, do not redefine): `Content-Type: image/svg+xml`, `Cache-Control: public, max-age=3600`, global `X-Content-Type-Options: nosniff`. No ETag/rate-limit special-casing — one indexed PK lookup + string template is cheaper than any JSON endpoint.

**Interactions.** Gold before phase 5: `domain.gold` is false for everyone until the resources flip (B5 resolution), so pre-phase-5 heroes render `supported` — correct, no special case. Frontend/README usage string for docs: `![IPv6](https://api.whynoipv6.com/badge/example.com.svg)`.

**Tests.** Golden-file test per variant (six SVGs byte-exact), plus: unknown host → 200 gray; disabled host → 200 gray; `xn--`-input and Unicode input normalize to the same badge; `.svg`-less path → 404 (route miss); invalid host → 400 JSON.

**Scope note for §8.** Stays phase 6, priority "ship-when-cheap": nothing else references it, so cutting it needs no spec change.
````

---

### A16. Datasets hosting/URL scheme left as an unresolved either/or

**Section:** §5.4 · **Confidence:** high · **needs_user_decision:** false

§5.4 leaves hosting as "`data.whynoipv6.com` or `/datasets/` path" although the brief asked the design to recommend hosting; the decision gates the openapi.yaml path, the manifest schema (relative file references require a shared origin), and the directory layout. **Resolution:** `/datasets/` on the API origin (one cert, one CORS story, one nginx server block — consistent with the rejected-S3 rationale and M5); pinned directory layout (dated immutable snapshot dirs + `latest` symlink + manifest.json + DICTIONARY.md + SHA256SUMS), exact manifest JSON schema (= the `GET /datasets` response schema), atomic publish procedure, nginx location blocks, `DATASETS_DIR` config key.

**spec_text:**

````
### §5.4 addendum — dataset hosting, directory layout, manifest schema (resolves the `data.whynoipv6.com` vs `/datasets/` either/or)

**Decision: datasets live under the `/datasets/` path on the API origin (api.whynoipv6.com).** No separate vhost. Rationale: one cert, one DNS name, one CORS story, one nginx server block — consistent with the rejected-S3 "one nginx directory" rationale and the §5.0 (M5) baseline. All file references in the manifest are origin-relative absolute paths (`/datasets/...`), which resolve because manifest and files share an origin.

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
On any failure before step 2, delete the `.tmp` dir and fire the ops webhook (§7.2 alert points); the previous manifest/latest remain untouched and correct.

**API side:** `GET /datasets` reads `$DATASETS_DIR/manifest.json` and returns it verbatim as `application/json` (re-read per request or with a ≤60 s in-process cache; the file is a few KB). 503 with the standard error envelope if the file is missing/unparseable. Config key: `DATASETS_DIR` (default `/var/lib/whynoipv6/datasets`), shared by the API binary and `v6ctl export`.

**nginx** (extends the §5.0/M5 deploy notes; sibling locations in the api.whynoipv6.com server block):

```nginx
# exact match: manifest endpoint → API (M5 proxy_set_header block applies)
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
````

---

### A17. ASN/GeoIP attribution inputs and GeoLite2 lifecycle unspecified

**Section:** §4.9, §4.2 · **Confidence:** high · **needs_user_decision:** false

§4.9 keeps GeoLite2 "by reference", but the reference chain is broken for the input-IP rule: production's attribution feeds off `resolver.IPLookup` (first apex AAAA, else first A) and that resolver is explicitly *rejected* by the new design — so the rule must be restated against the new engine's §2.3.1 answer sets (which conveniently produce exactly the needed inputs, AAAA-first, at zero extra queries). Also absent: the mmdb config key, geoipupdate acquisition/cadence, and reader reload for the long-running crawler (production's checked-in mmdb files date from January 2023 — concrete evidence the procedure belongs in the spec). Partially refuted in verification: the country ccTLD-precedence rule *is* pinned by reference (OPEN-5), and per-scan recompute timing was already implied by B2 step R.

**spec_text:**

````
### §4.9 amendment — GeoIP attribution (replaces the two-sentence GeoIP paragraph)

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
  (ranked, campaign, subdomain) — matches production's per-crawl recompute and B2
  step R ("refreshed by the scan anyway").
- Deferred scans (base observation `error`/`inconsistent` — no commit) do **not**
  touch attribution: a transient resolver failure must not flip a domain to
  'Unknown'. (Deliberate improvement over production, which degraded to Unknown on
  any IPLookup timeout.)
- Run once more at **entity insert** (Tranco import, campaign sync, live-check row
  creation) with no input IP — yielding ccTLD-or-sentinel country and sentinel ASN —
  so the columns are never NULL. DDL amendment: `asn_id INT NOT NULL REFERENCES
  asn(id)`, `country_id INT NOT NULL REFERENCES country(id)`. No serializer ever
  handles NULL asn/country.
- Live checks never touch attribution on existing rows (M8 Rule 0 unchanged).

**Seeds** (extends §7.2 migration ordering: sentinels land with the asn/country
seed data, before any domain row). Carry production's `02_data.up.sql` country
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

### Ops runbook addition — GeoLite2 lifecycle (Ansible + systemd; extends §7.2 config registry)

Production's repo-bundled mmdb files date from January 2023 — this procedure
replaces that.

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

Config-registry rows to add (§7.2 table): `GEOIP_PATH` | string |
`/var/lib/GeoIP` | crawler. (Reload interval and filenames are fixed, not config.)
````

---

### A18. No crawl-stall alerting between daily ticks; Unbound stats collection undefined

**Section:** §2.6, §8 phase 3 · **Confidence:** high · **needs_user_decision:** false

The design's only liveness signal is the once-daily 03:30 healthcheck ping — *weaker* than production, which pings after every crawl loop — leaving a ~23h blind spot for a dead crawler process (and with K=2, the non-coordinator never pings at all). Phase 3's "Unbound stats" dashboard names no collection mechanism in a stack with no Prometheus, and no alert rules exist before phase 7 despite phase 3 running the full 1M crawl. Substantially narrowed in verification: the observability model itself (Grafana reads Postgres) and error-condition alerting (§2.6 preflight, M4 breakers) are already covered. **Resolution:** per-process healthchecks.io heartbeats at claim-cycle granularity + idle checkpoints; `v6ctl ops unbound-stats` scrape into a small hypertable via systemd timer; five concrete Grafana alert rules landing in phase 3; /metrics endpoints and unbound_exporter explicitly rejected.

**spec_text:**

````
### Ops spec addendum — crawl liveness, Unbound stats, Grafana alerting
(Folds into spec outline item 10 "Ops & config"; amends design §2.6 step 4 and §8 phases 2/3/7.)

**Observability model (pinned, restated):** Grafana reads Postgres directly (crawler_metrics, frontier queries, unbound_stats, timescaledb_information views). No Prometheus, no /metrics endpoints on any binary. External liveness via healthchecks.io pings (production pattern, toolbox.HealthCheckUpdate lift).

#### 1. Liveness heartbeats (phase 2, not phase 7)

One healthchecks.io check **per crawler process** (e.g. `wni6-crawler-1`, `wni6-crawler-2`), plus one for the daily tick (`wni6-daily-tick`).

- After every successful claim-cycle commit, the process pings its check's success URL, throttled to at most one ping per `ops.healthcheck_min_interval` (default 60s). Lifted from production semantics (crawl.go:182) at claim-cycle granularity.
- On preflight failure (§2.6 self-preflight), ping the `/fail` endpoint of the same check (production HealthFail pattern, crawl.go:84) in addition to the existing ops-webhook alert.
- healthchecks.io check config: Period = 15 min, Grace = 30 min. A dead or hung crawler process is therefore signaled within ≤45 min instead of ~23h.
- The 03:30 daily tick keeps its own separate check (Period = 24h, Grace = 2h), pinged at §2.6 step 4 as designed. §2.6 step 4's "healthcheck ping" now refers to THIS check only; per-process liveness is the mechanism above.
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
- **A2 frontier lag:** `SELECT count(*) FROM domain WHERE next_check_at < now() - interval '6 hours'` (with the active-domain predicate from the frontier claim SQL, spec section 5) > 50,000 → warning; > 200,000 → critical. Catches silent throughput collapse the heartbeats can't see.
- **A3 error ratio:** `SELECT coalesce(sum(failed)::float / nullif(sum(processed),0), 0) FROM crawler_metrics WHERE ts > now() - interval '1 hour'` > 0.20 → warning. (Complements, does not replace, the M4 fast-lane/provider breakers, which remain the primary error-path alerts.)
- **A4 TimescaleDB jobs:** `SELECT count(*) FROM timescaledb_information.job_stats WHERE last_run_status = 'Failed'` > 0 → warning. (Same view phase 3's verify step already names.)
- **A5 Unbound/scraper down:** `SELECT count(*) FROM unbound_stats WHERE ts > now() - interval '5 minutes'` == 0 → critical.

#### 4. Phase-plan amendments (§8)

- Phase 2 gains: per-process heartbeat pings + idle checkpoint rule (verify: kill one of the two crawler processes → its healthchecks.io check flips to "down" within 45 min while the other stays up).
- Phase 3 gains: unbound_stats scrape + timer, and alert rules A1–A5 provisioned before the 1M crawl is declared operational (verify: A1 fires when both crawlers are stopped; A5 fires when the timer is disabled).
- Phase 7 shrinks accordingly: "healthcheck/webhook notifications" is deleted from phase 7 (now phases 2-3); phase 7 keeps runbooks (Unbound, Timescale jobs, frontier surgery) and any remaining notification polish.
````

---

### A19. Production packaging and deploy pipeline undefined (systemd units, timers, migration point)

**Section:** §6, §8 phase 0, §3 · **Confidence:** high · **needs_user_decision:** false

The brief locks "Docker + docker-compose; systemd for prod" and the design doc never discharges it — no unit/timer inventory, no deploy order, no artifact path. The nginx half of the raw finding is refuted as covered (M5 + §5.4). **Resolution:** a one-page deploy appendix (slots into outline item 10): filesystem/user layout, two service units, timer inventory, migrate-before-restart forward-only deploy order via the operator's existing Ansible, with migrations embedded in `v6ctl` via `go:embed` so the artifact set is exactly three binaries. **Apply with reconciliation notes 2 and 4:** drop the `whynoipv6-tranco.timer` row (Tranco is coordinator-owned per A1/A7), use `GEOIP_PATH` and A17's geoipupdate cadence, and delete the tick's mmdb-reload step (subsumed by A17's hourly swap).

**spec_text:**

````
## Appendix D — Production packaging & deploy (systemd + Ansible)

Scope: production runs on the operator's own VMs via systemd (brief: "Docker +
docker-compose; systemd for prod" — compose is dev-only). Everything below is
provisioned by the operator's existing Ansible; the backend repo ships the unit
files under `deploy/systemd/` as the source of truth, Ansible copies them verbatim.
The nginx api vhost (proxy_set_header block, [::1] rationale) is specified in
§5.0/M5 and the dataset static split in §5.4 — not repeated here.

### D.1 Filesystem & user layout

- System user `whynoipv6` (no shell, no home). Same user for api, crawler, v6ctl timers.
- `/opt/whynoipv6/bin/{api,crawler,v6ctl}` — the three release binaries (static linux/amd64).
- `/etc/whynoipv6/env` — root:whynoipv6 0640; env-format file holding every key from
  the consolidated config registry (outline item 10): `DATABASE_URL`, `API_LISTEN`
  (default `[::1]:8080`, per M5), ops-webhook + healthcheck URLs, `GEOIP_DIR=/var/lib/GeoIP`,
  crawler/consensus/lifecycle keys.
- `/srv/whynoipv6/datasets/` — owned whynoipv6, world-readable; nginx serves it read-only
  per §5.4 (`autoindex off`, manifest is the index).
- `/var/lib/GeoIP/` — written by the distro `geoipupdate` package, read by crawler.
- Migrations are **embedded in `v6ctl` via `go:embed` (golang-migrate iofs source)** —
  no migrations directory ships to the host; the deploy artifact set is exactly the
  three binaries. (`db/migrations/` in the repo stays the sqlc/dev source of truth.)
- Unbound: two local instances on the crawler host, managed as distro
  `unbound@1.service` / `unbound@2.service` (configs via Ansible, tuning per phase-2
  verification); their listen addresses are crawler config, not baked in.

### D.2 Service units (`deploy/systemd/`)

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
plus `ReadWritePaths=/srv/whynoipv6/datasets` and read access to `/var/lib/GeoIP`.
One crawler unit per host (§6: "one per machine"); a single host meets the §2.7
throughput math — a second crawler host is resilience, not capacity, and needs no
coordination beyond the shared frontier (SKIP LOCKED). Graceful shutdown: both
binaries drain on SIGTERM within systemd's default 90s stop timeout (crawler
finishes in-flight scans, uncommitted claims simply expire back to the frontier).

### D.3 Timer inventory (resolves the §3 timer-vs-coordinator ambiguity)

Decision: **systemd timers own v6ctl verbs; the crawler's 03:30 UTC coordinator tick
does NOT run tranco import.** Both triggers are idempotent-safe, but a single owner
removes the ambiguity; timers are chosen because §6 already labels v6ctl verbs "cron
targets" and a timer keeps list ingestion alive while the crawler is down for deploys.
Campaign sync is unchanged: Semaphore webhook + daily tick, per §7's explicit "Both".

| Timer | OnCalendar (UTC) | ExecStart | Notes |
|---|---|---|---|
| `whynoipv6-tranco.timer` | `23:15`, `01:15`, `03:15` (three OnCalendar= lines), `Persistent=true` | `v6ctl tranco import` | List generated 22:00–23:00 UTC (§3); the 01:15/03:15 firings ARE §3's "retry in 2h" — each run no-ops on unchanged list-id / 304 (§3 steps 1–2). Third attempt lands before the 03:30 stats tick. |
| `whynoipv6-export.timer` | `04:30`, `Persistent=true` | `v6ctl export` | Satisfies §5.4 "after the stats tick" with 1h headroom over the 03:30 tick; export reads confirmed state + latest stats snapshot, so a late tick degrades to yesterday's stats row, never a failure. Also applies the §5.4 retention (dailies 90d, first-of-month kept). |
| `geoipupdate.timer` (distro package) | `Wed,Sat 05:41` | `geoipupdate` (`/etc/GeoIP.conf`: GeoLite2-ASN + GeoLite2-Country, account/license key) | Matches MaxMind's Tue/Fri publish; off-hour minute per their guidance. |

Timer service units are `Type=oneshot`, `User=whynoipv6`,
`EnvironmentFile=/etc/whynoipv6/env`. Timer failures alert via the existing ops
webhook: `v6ctl` exits non-zero on failure and each oneshot unit sets
`OnFailure=whynoipv6-notify@%n.service` (a 3-line curl-to-webhook unit) — no new
alerting infrastructure.

Final 03:30 tick contents (§2.6, amended): stats snapshot, country/ASN recompute,
service-candidate detection, campaign sync (§7), ops summary + healthcheck ping,
plus one new step: **re-open the GeoIP mmdb readers if file mtime changed** (so
geoipupdate takes effect without a crawler restart).

### D.4 Deploy procedure (Ansible playbook order)

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
6. Verify: `systemctl is-active` both units; `curl -6 http://[::1]:8080/healthz`;
   crawler_metrics row age < 10 min in Grafana.

Rollback = redeploy the previous release's binaries (steps 2, 4, 5 only); the
expand/contract contract makes the already-applied migration compatible. Never
migrate down.
````

*(Apply with the cross-finding reconciliation: D.3's `whynoipv6-tranco.timer` row and its decision paragraph are superseded by A1/A7 — Tranco stays coordinator-owned and no tranco timer is deployed; the tick's mmdb-reload step is superseded by A17's hourly swap; `GEOIP_DIR` in D.1 reads `GEOIP_PATH`. The export and geoipupdate timers, units, layout, and deploy order stand as written.)*

---

### A20. No structured-logging conventions (levels, format, per-domain volume at 1M/day)

**Section:** §6 · **Confidence:** high · **needs_user_decision:** false

slog is in the locked stack but the doc states no handler/format, level policy, or correlation attributes; three binaries would improvise different formats, and a naive per-domain info line (or bursty per-check error lines during resolver incidents) is redundant with the scan table/crawler_metrics and can trip journald rate limits. A conventions gap, not an observability gap — the load-bearing observability is already designed. **Resolution:** one normative conventions block: JSON handler to stdout (stderr for v6ctl), `LOG_LEVEL` config key, standard attribute keys matching existing DDL columns (`run_id`, `worker`), a level policy whose one load-bearing sentence is the volume rule (nothing per-domain above debug), and a slog-based chi access log.

**spec_text:**

````
### Logging conventions (normative — all three binaries)

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
- `warn` — actionable anomalies that don't stop the process: preflight failure (in addition to the ops-webhook alert), quorum-inconsistency rate above threshold, claim starvation (empty frontier while backlog expected), lease-fence aborts (mirrors M2's `lease_lost` counter), Tranco import aborted by the F5 sanity gates, ops-webhook/heartbeat delivery failure.
- `error` — bugs and unexpected states only: recovered panics (chi `middleware.Recoverer` wired to slog), DB errors aborting a §4.3 commit unit, invariant violations. A domain that fails its scan is a scan observation, not an error — it goes to `debug` + the metrics counters.

**Volume rule (normative).** In steady state, nothing is emitted per-domain or per-check above `debug`. Per-domain failures during incidents (e.g. resolver outage) aggregate into `crawler_metrics` error counters — alerted via Grafana and the daily ops-webhook summary — never into per-line warn/error spam; this keeps journald's default rate limiting (`RateLimitBurst=10000`/30s) irrelevant even at 1M domains/day.

**API access log.** chi stack: `middleware.RequestID` → `middleware.RealIP` (nginx sets `X-Forwarded-For`) → a small slog access-log middleware (do not use chi's default text logger). One `info` line per request: `request_id`, `method`, `path`, `status`, `bytes`, `duration_ms`, `remote_ip`. Exclude the health endpoint from the access log.
````

---

## Decisions needing the maintainer

**None.** All 20 confirmed resolutions carry `needs_user_decision: false` at high confidence — every judgment call was forced by a constraint the doc, brief, production behavior, or a first-pass resolution already states. Three maintainer-*visible* (but not maintainer-*blocking*) calls are worth a skim, each with its recommended default already applied:

- **Badge copy vocabulary (A15):** README badges say `supported`/`unsupported`, never `hero`/`sinner` — owners won't embed self-shaming badges; the ladder branding stays site-side. Isolated in one Go constant table if the maintainer wants to reword.
- **Announced fixes to accidental production behavior:** disabled campaigns disappear from `GET /campaign` and 404 on detail endpoints (A13), and hostname path params are normalized API-wide where production was internally inconsistent (A5). Both are strictly-additive/intent-restoring changes in the same declared category as the `[]`-cleanup.
- **Sinner-list membership narrowing goes into the public methodology-v2 note (A12)** — the shift itself is forced by the locked ladder; only its public announcement wording is maintainer-editable.

---

## Refuted

16 findings refuted (grounds in parentheses; first-pass finding named where refuted-as-covered):

| Dropped finding | Why refuted |
|---|---|
| Service-domain "CNAME in-degree" silently substituted with page-dependency in-degree (§4.8/§4.6) | §4.8(b) deliberately and accurately names dependency in-degree; brief §6 listed CNAME in-degree only as a heuristic "to evaluate" for a review-only flagger; residue is a 3-word schema-comment gloss with no behavioral force (B5's rewrite already re-specifies `dependent_count`). |
| Phase-0 docker-compose omits the frontend service (§8 p0) | Brief's compose sketch is labeled "validate/refine" and §8 deliberately re-enumerates services; the frozen Vue app runs under its own Vite dev server for E2E; pure dev ergonomics, no bar met. |
| Migration ordering / ON DELETE / updated_at left implicit (§4.2/§4.9) | Covered: first-pass §7.2 pins the exact migration order; the doc's no-hard-delete design makes default NO ACTION inert; M2/B2/B3 already demonstrate application-side `updated_at` in both authoritative write paths. |
| §2.6 daily tick omits campaign sync (§2.6 vs §7) | §7 step 2 answers it unambiguously ("the cron is the guarantee. Both."); first-pass §7.3 item 7 already mandates the consolidated tick step list (now pinned by A1). |
| v6ctl verb inventory missing `resource remove` (§6 vs §4.6) | §4.6 line 739 specifies the verb; §6's tree comment is demonstrably a non-exhaustive abbreviation; one-word cosmetic edit at most. |
| New §5.2 endpoints lack response schemas/pagination/sort (§5.2/§5.4) | Covered by the first pass's cleared M8 sub-claim principle: new surfaces with no frozen contract are pinned by §5.5's spec-first OpenAPI process; every shape is a mechanical projection of fully-specified DDL + stated conventions; the §5.4 evidence claim was factually wrong. |
| No config surface or secrets handling (§6/§2.5) | Covered: every value is pinned in the doc; M5/M8/M4/B2/M6/B5 introduce the load-bearing keys; first-pass §7.2 mandates the consolidated config-key registry; the residue (env-var spellings, one secrets sentence) is plumbing with production's `app.env` as the template. |
| Unbound deployment ownership/placement/failure behavior (§2.4/§8 p2) | §8 phase 0 pins "unbound ×2" in compose, §2.4 pins tuning; "wasteful and silent" is factually wrong (dead-localhost queries fail fast; M4's breakers alert on the resulting error spike); errors never advance confirmed state; residue is standard infra. |
| Phase-4 data-migration mapping unspecified (§8 p4/OPEN-7) | Covered by M2's normative seed rule + M3 sections A–F (mapping, `field='legacy'` escape hatch, byte-equality gate, cutover note); the "weeks below the hero bar" claim is factually wrong under the resolved bootstrap rule (~1 crawl cycle); the importer verb is answered by §7 + OPEN-7. |
| Monorepo CI pipeline undefined (§8 p0/§5.5) | The only product-weight CI behavior (stale-generated-output gate) is specified in §5.5; vendor/job-split/service-container choices are developer tooling, freely revisable, no bar met. |
| Crawler graceful-shutdown / claims stranded 30 min (§2.5/§5.1) | Covered (check_job half) by M8's lease+reaper; the frontier half is designed-in crash-safety (30-min lease, per-domain tx, M2 fence, phase-2(d) chaos test); worst case ~0.04% of the frontier rescanned ≤30 min late once per deploy. |
| Dataset export directory operations under-specified (§5.4) | §5.4 already specifies sha256 manifest, `latest/` symlink, and retention; M5 + first-pass §7.2 route the serving split into the spec; tmp-dir/rename/prune-in-exporter is textbook practice — and A16 now pins it anyway. |
| TimescaleDB DDL in golang-migrate needs execution constraints (§4.4/§6) | §8 phase 0 pins the timescale image (preloaded library); `v6ctl migrate` + Makefile target answer the execution path; first-pass §7.2's migration-ordering section (M9-corrected calls) is the slot for authoring details; failure mode is loud, pre-traffic, and mechanical to fix. |
| Update amplification: non-HOT commits across 9 indexes (§4.2 vs §2.5) | Math materially overstated (claim UPDATE is HOT-eligible — `claimed_at` is unindexed; partial predicates mean ~7–8 entries, not 9); §2.7 sized the write path correctly; autovacuum at this rate is routine. A4 adds fillfactor/autovacuum settings regardless. |
| Per-provider v4/v6 resolver address ambiguity (§2.3) | §2.3's quorum structure + §2.4's explicit arithmetic pin exactly one query per provider per record; which transport family to dial is a pure transport detail (anycast providers answer identically); M4's per-provider token buckets are family-agnostic. |
| Confirmation thresholds / quorum set / timeouts absent from config schema (§2.5/§2.3/OPEN-8) | Every value is pinned in prose (§2.3, §2.5, §4.3); OPEN-8 itself answers config-vs-hardcode ("upstream list stays config"); M4/M6/M8 add the relevant config blocks; first-pass §7.2's registry mandate covers the rest. Exposing N as config would arguably harm the trust model. |

---

## Impact on the first report

**Verdict: unchanged — ready-with-fixes.** The second pass adds no blockers, and all 20 confirmed findings ship forced, high-confidence resolutions with zero maintainer decisions — the same property that carried the original verdict. The combined totals across both passes are now **6 blockers, 15 majors, 14 minors confirmed; 16 refuted** (the first pass refuted none at canonical level). What changes is the *size and shape* of the fix set, not its nature: the second pass is dominated by operations, ingestion-pipeline, and cross-cutting-convention gaps the dedup stage wrongly discarded — including two findings that amend first-pass resolutions themselves (A4 amends B2's index; A2/A13 amend B2's sweep linkage).

### §7.1 fold-in groups — concrete additions

- **Group 2 (Lifecycle, scheduling, commit machine — B2, M2, M4)** gains **A1, A4, A7**: A1's lock registry + canonical tick order wraps B2's sweep and M8's purge; **A4 replaces B2 item 1's `idx_domain_frontier` with `idx_domain_due`** (semantics untouched — this is a designated-winner amendment in the same sense as the report's three flagged reconciliations); A7 pins the Tranco trigger the group's §2.6/§3 text left ambiguous.
- **Group 3 (Legacy API serialization and visibility — B3, B6, M3, M5, M7)** gains **A10, A11, A12, A13**, plus A5's API-path-param normalization rule: A10 supplies the codec definition M3's `shortuuid()` notation assumes; **A11 completes M3's 404 inventory** (a third changelog zero-rows 404 M3's "two per-domain cases" missed — completion, not contradiction); A12 adds the fourth deliberate break to the OPEN-6 methodology-v2 note B3 R4/B6 already extend; A13 extends M7's `NOT disabled` scoping to the campaign entity itself.
- **Group 4 (Schema/DDL corrections — M9 + fragments)** gains A4's index/storage DDL, A17's `NOT NULL asn_id/country_id` + sentinel seeds, and A18's `unbound_stats` hypertable (migration 002).
- **New group 6 — Ingest & contribution pipeline (A2, A3, A5, A7):** the §3/§7 rewrites (sync algorithm §7.1–§7.4, PR validation, Canonicalize + Tranco dedup, import cycle). Internally interdependent; merge in one pass, honoring the five reconciliation notes at the top of this appendix.
- **New group 7 — Ops & packaging (A6, A15, A16, A17 ops half, A18, A19, A20, A14 verb):** everything that lands in spec outline item 10 plus the two new-surface pins (badge, datasets).

### §7.3 spec-document outline — additions per item

- **Item 2 (Engine contract):** A8's canonical constants block; A9's enumeration-not-count rule (15 checks).
- **Item 5 (Lifecycle):** A4's plan-shape note + claim-loop cadence; A1's lock registry and tick order.
- **Item 6 (Schema):** `idx_domain_due` + `domain` storage settings (A4); `asn_id`/`country_id` NOT NULL + sentinels (A17); `unbound_stats` (A18); `tranco_import` provenance counters (A5/A7).
- **Item 7 (Ingest):** A7's import cycle; A2's §7.1–§7.4 sync spec; A3's PR-validation step; A5's Canonicalize section + staging dedup.
- **Item 8 (API):** A15's §5.2a badge spec; A16's §5.4 addendum; A10's shortuuid pin; A11's zero-result map; A12's compat-table corrections; A13's disabled-campaign visibility; A14's topsinner query pin.
- **Item 9 (Migration & cutover):** top_shame import (A14), shortuuid parity vectors (A10), `--adopt-unknown-uuids` migration step (A2), restore-drill gates in phases 3/4 (A6), claim-plan gates in phases 2/3 (A4), parity-test rescoping for the two sinner lists (A12), the one-time campaign-repo dedup commit (A3).
- **Item 10 (Ops & config)** grows the most: backup & restore (A6), liveness + Unbound stats + Grafana alert rules (A18), Appendix D packaging/deploy (A19, as reconciled), GeoLite2 lifecycle (A17), logging conventions (A20).
- **Item 11 (Test plan):** badge golden files (A15), Canonicalize unit vectors (A5), zero-result fixtures (A11), sinner-membership synthetic fixtures (A12), shortuuid round-trip + negative fixtures (A10), heartbeat/alert-rule verification steps (A18).

### Consolidated config-key registry — new rows (extends first-pass §7.2 item 1)

| Key | Default | Owner | From |
|---|---|---|---|
| `campaign.repo_path` | `/srv/whynoipv6-campaign` | crawler + v6ctl | A2 |
| `campaign.git_remote` | `origin` | crawler + v6ctl | A2 |
| `campaign.max_domains_per_file` | 1000 | campaign-repo Action | A3 |
| `tranco.import_at` | `"23:15"` UTC | crawler | A7 |
| `tranco.retry_interval` | `2h` | crawler | A7 |
| `tranco.stale_warn_after` | `48h` | crawler | A7 |
| `claim.batch_size` | 200 | crawler | A4 |
| `claim.empty_poll_interval` | `10s` | crawler | A4 |
| `ops.healthcheck_url` | `""` (disabled) | per crawler process | A18 |
| `ops.healthcheck_tick_url` | `""` | coordinator | A18 |
| `ops.healthcheck_min_interval` | `60s` | crawler | A18 |
| `unbound_stats.control` | `"unbound-control"` | v6ctl | A18 |
| `GEOIP_PATH` | `/var/lib/GeoIP` | crawler | A17 |
| `DATASETS_DIR` | `/var/lib/whynoipv6/datasets` | api + v6ctl | A16 |
| `LOG_LEVEL` | `info` (api/crawler), `warn` (v6ctl) | all three | A20 |

### Amendments to first-pass resolutions (designated winners, same mechanism as the report's three flagged reconciliations)

1. **A4 supersedes B2 items 1–2's index DDL** (`idx_domain_due` replaces `idx_domain_frontier`); B2's claim SQL text, predicates, lease, and sweep are untouched.
2. **A2 §7.4 / A13 item 4 amend B2 item 4's sweep linkage** (campaign membership counts only when the campaign is not disabled) — the same amendment stated in two resolutions; apply once.
3. **A11 completes M3's zero-rows 404 inventory** with the third changelog endpoint; M3's two pinned 404s stand unchanged.
4. **A5 is the definition M8 step 1 and B5's `lower_punycode` delegate to**; M8's reserved-TLD list remains a POST /check-only policy layer.
5. **A8 re-derives §2.7's sizing rows *after* B1/B5/M6** — apply those first, then A8's table.

Nothing in this appendix reopens a first-pass resolution's semantics, touches a locked constraint, or introduces an open product question. With the five intra-pass reconciliation notes applied, the combined 35 resolutions fold into the design doc and the spec can be written directly against the (unchanged) §7.3 outline.
