# 03 — Confirmed-Status State Machine (the trust core)

_Status: Round 3.0 — API redesign folded in (docs/history/api-design-research.md, decisions 2026-07-09): clean root API, keyset pagination, RFC 9457, no legacy compat, no history import._

**Purpose:** This file specifies the confirmed-status commit machine — the single code path that turns per-scan observations into public, confirmed IPv6 state, changelog entries, and classification. It is the trust core of the whole system: every public status, every changelog row, and every hero/partial/sinner verdict flows through the algorithm defined here, exactly once per scanned domain, in one atomic per-domain transaction protected by a lease fence.

**Deliverables:**

- `internal/crawler` — the commit unit: dead-signal computation, the per-dimension confirm/pending loop, streak maintenance, scheduling computation, the one-`pgx.Batch`-in-one-`pgx.Tx` write unit, lease-fence handling, and the resource-link persistence statements (files: `commit.go`, `commit_sql.go` or the sqlc-generated equivalents under `internal/postgres` + `db/query/commit.sql`).
- `internal/domain` — `classify.go`: the pure classification ladder, flags, and saint computation (zero dependencies), plus the `Dimension`, `IPv6Status`, `Observation`, `Classification` Go types mirroring the DB enums.

**Companion files:** 02 (observation production: engine adaptation, consensus quorum, conn composition, resources roll-up, and the answer-set order the attribution input IP is drawn from — everything that produces this file's per-dimension inputs), 06-ingest.md (§6 GeoIP/ASN attribution — the sole owner of the ASN + ccTLD attribution algorithm that resolves input `A`), 04 (claim query, lease stamping, frontier — everything that produces the snapshot this file consumes), 05-schema.md (all DDL: `domain`, `scan`, `scan_detail`, `changelog`, `resource_host`, `domain_resource`), 09-ops.md (config-key registry), 00-overview.md (canonical sizing constants), 10-testing.md (fixtures and contract tests for everything asserted here).

---

## 1. Vocabulary and invariants

- **Core dimensions** — the six confirmed dimensions: `base`, `www`, `ns`, `mx`, `conn`, `resources`. Each has a 5-column group on `domain` (see 05-schema.md — `domain`): `<d>_status` (confirmed, public), `<d>_observed` (last raw observation, telemetry only), `<d>_pending` (candidate awaiting confirmation), `<d>_pending_count`, `<d>_since` (when the confirmed value last changed).
- **Informational dimensions** — `dnssec`, `ptr`, `smtp`, `parity`, plus `latency_v4_ms`/`latency_v6_ms`. Latest observation only, no confirmation machinery, never gate classification.
- **Observation** — a per-dimension, per-scan value of the DB enum `observation`: `supported | partial | unsupported | no_record | not_applicable | error | inconsistent`. Produced entirely by 02 (quorum, mapping tables, conn composition, resources roll-up) before the commit runs.
- **Confirmed status** — a value of the 4-valued public enum `ipv6_status`: `supported | unsupported | no_record | not_applicable`. `error`, `inconsistent`, and `partial` never become confirmed and never reach public output.
- **Definitive observation** — an observation NOT in `{error, inconsistent}`. Only definitive observations can advance confirmed state. Non-definitive observations **touch nothing**: status, pending, pending_count, and since all survive unchanged.
- **`partial` never reaches this machine on a core dimension.** 02's storage rule maps every partial-capable engine check to a non-partial observation before any DB write (`ns`/`mx` partial → `supported`, `smtp` partial → `unsupported`); `partial` is a legal stored value only for the informational `ptr` and `parity`. The committer MUST validate this: a `partial` observation on a core dimension is a programming defect — abort the domain's commit with an error (counted as `commit_error`, section 15), write nothing.
- **IS DISTINCT FROM semantics everywhere.** All equality comparisons in this file use SQL `IS DISTINCT FROM` semantics: NULL never equals anything, including NULL. In Go, model status/pending as pointers (or a valid-flag struct) and compare accordingly.
- **One writer.** Only the frontier-scan commit path runs this machine. The live-check (`POST /check`) consumer NEVER commits through it (its Rule 0 — it writes only `check_job` rows). The resource-host sweep runs a separate, simplified mirror of this machine over `resource_host` rows (N=2, immediate first commit, no changelog — see 06-ingest.md §5.4 — sweep worker host confirmation machine); it never touches `domain`.

## 2. Constants

Confirmation thresholds (fixed constants, not config):

| Dimension | N(d) — consecutive counted definitive observations required to change a confirmed value |
|---|---|
| `base` | 2 |
| `www` | 2 |
| `ns` | 2 |
| `mx` | 2 |
| `conn` | 3 |
| `resources` | 3 |

Config keys introduced here by name (types, defaults, and the consolidated registry: 09-ops.md):

- `anti_flap.min_confirm_spacing` — duration, default `12h`. The counting gate (section 7).
- `lifecycle.dead_streak` — int, default `7`. Consecutive unresolvable scans before `disabled_reason='dead'`.
- `lifecycle.slow_lane_every` — duration, default `720h`. Post-commit cadence for disabled `dead`/`delisted` rows.
- `cadence.default` / `cadence.bands` — normal-cadence resolution by rank (section 9).
- `recheck_inconsistent` (default `2h`), `recheck_error` (default `6h`), `recheck_backoff_max` (default `720h`) — fast-recheck lanes (section 9).
- `crawler.resources.enabled` — bool, default `false`. While `false` the `resources` dimension is excluded from the loop entirely (section 5, step 2 preamble).

Guarantee (restated from the design, normative): a confirmed flip requires N definitive observations of the new value on scans spaced ≥ `min_confirm_spacing` apart — at daily cadence the advertised +1/+2 days of transition latency, and never faster than (N−1) × 12h even when fast-lane rechecks run every 2h.

## 3. Commit-unit inputs (contracts with 02 and 04)

Per scanned domain the worker builds ONE commit unit, worker-side, after `Runner.Run` and after 02's observation mapping. Inputs:

- **`L` — the lease token**: the `claimed_at` value stamped by the claim query (see 04 — claiming). One `UPDATE … SET claimed_at = now()` stamps every row in a batch with the same value, so L is a single per-batch token held by the worker.
- **`T` — the commit timestamp**: `time.Now()` fixed once per domain and used, unmodified, for the scan row, scan_detail row, changelog rows, every `*_since` set this commit, `last_checked_at`, `last_counted_at`, `disabled_at` (when set this commit), and resource-link `first_seen`/`last_seen`. Never call `time.Now()` twice inside one commit unit.
- **`S` — the claimed snapshot**: the domain's state columns as returned by the claim query's RETURNING list. The lease fence (section 13) guarantees this snapshot is still authoritative at commit time — the committer never re-reads the row. **Decision:** the snapshot contract with 04 is the following exact field set, which 04's claim RETURNING list MUST cover: `id, host, kind, rank, claimed_at, disabled, disabled_reason, disabled_at, dead_streak, error_streak, last_counted_at, asn_id, country_id`, and for each core dimension `d`: `d_status, d_pending, d_pending_count, d_since` (the `d_observed` columns are write-only for the commit and need not be claimed).
- **`O[d]`** — one observation per core dimension, post-mapping (02). For `kind='subdomain'`, `O[www]` is always `not_applicable` (definitive). While `crawler.resources.enabled=false`, `O[resources]` is `not_applicable` for the scan row but the dimension is excluded from the loop (step 2).
- **`I`** — informational observations: `dnssec, ptr, smtp, parity` (observation values; `ptr`/`parity` may be `partial`) and `latency_v4_ms, latency_v6_ms` (`*int32`, nil when not measured).
- **`U` — the dead signal** (section 4), computed by the worker from raw engine/consensus evidence before the loop.
- **`A` — attribution**: `(asn_id, country_id)` computed per 06-ingest.md §6 (GeoIP/ASN attribution — IPinfo Lite ASN + the ccTLD-wins-over-GeoIP country rule; the input IP is drawn from this scan's base-composite answer set in the provider order pinned by 02), or *deferred* (nil) when `O[base]` is non-definitive. **Decision:** ASN/country *registry row resolution* (the insert-if-absent of a new `asn` row) runs on the pool BEFORE the commit transaction is opened (idempotent `INSERT … ON CONFLICT (number) DO NOTHING` + re-read); only the resolved integer ids are written inside the fenced transaction. This keeps the batch linear (no conditional statements) while the column write stays atomic with the state commit.
- **`D` — resource discovery output** (only when `crawler.resources.enabled=true`): the canonicalized, deduplicated host list from the adapted `resource_discovery` check plus its status (`ok | error | not_applicable`). Production of the list, canonicalization, and the roll-up that yields `O[resources]` are 02's; this file owns only the persistence statements (section 12.3).
- **`BreakerOpen`** — the fast-lane breaker state from the consensus package (02): when open, the 2h/6h pull-ins are suspended (section 9).
- **`details` / `duration_ms`** — the assembled scan_detail JSON payload (section 14.2) and total scan duration.

## 4. Dead-signal computation

The worker holds the raw engine results, so it computes, per scan, before the per-dimension loop: a scan is **unresolvable** (`U = true`) when either

- **(a)** the apex AAAA consensus quorum symbol is `nxdomain` AND the NS zone walk found no delegated zone for the host, or
- **(b)** all 3 consensus resolvers returned an explicit SERVFAIL or REFUSED rcode for the apex AAAA after retry, **AND the conditional CD=1 re-query (02-observation-model.md §2.7b) also returned no usable answer (`CDOutcome = cd_fail`)** — the authoritative servers give nothing even with DNSSEC validation disabled, so the name is genuinely lame/dead. Timeouts do NOT count — three timeouts more likely indicate our own network trouble. **Decision (grilling round, 2026-07-10 — supersedes the design's "broken-DNSSEC is dead" reading):** a domain with **broken DNSSEC that still publishes AAAA** does **not** hit this branch — its CD=1 re-query returns `cd_present`, its base observation is `supported`, so it is credited for IPv6 and stays publicly visible (hero-eligible); the broken chain is recorded only as the informational `dnssec` dimension, which never gates classification (§10). DNSSEC is nice-to-have, not a shame/hide trigger — research showed ~⅓ of users sit behind validating resolvers, so hiding such a domain would be wrong *and* falsely marking it dead would be wrong. Only genuine non-existence (branch a) or genuinely-lame authoritative servers (branch b, `cd_fail`) mark a domain `dead`.

Operational definitions:

- Branch (a) requires the NXDOMAIN **rcode** (delivered as the quorum symbol `nxdomain`), not merely `O[base] = no_record` — NOERROR-with-no-records is a live but inactive zone and must NOT count. **Decision:** the design's "apex A and AAAA both NXDOMAIN" is satisfied by the AAAA quorum symbol alone: NXDOMAIN is name-level (it entails the absence of every rrtype including A), and no A query exists on the `nxdomain` path (02's conditional A lookup fires only on the `empty` symbol). The raw per-resolver rcodes are preserved in `scan_detail.details.consensus` precisely so this rule is auditable.
- "NS zone walk found no delegated zone" := the (lifted) `dns_ns_ipv6` walk-up found zero NS records at every label up to the TLD. The worker holds the raw engine result and reads this directly from the check's details (01-engine.md §11.3): the walk-up records `details["zone"]` when it found a delegated zone above the input, and returns `StatusError` with `details["error"] = "no NS records found"` and **no** `zone` key when it found none. **Decision:** the evidence source is the presence of the `zone` key on the raw result, NOT a mapped-observation field — 02's `Observations` struct carries no NS-zone boolean, and its `ns` observation collapses a resolver `error` and a genuine no-zone `error` to the same value (02-observation-model.md §7.2, §7.3), so it cannot distinguish them. Compute it worker-side: `NSZoneFound := sr.Results["dns_ns_ipv6"].Details["zone"]` is present and non-empty. Branch (a) is `quorum(base AAAA) == nxdomain AND !NSZoneFound`.
- Branch (b) := every one of the three configured providers **answered** (was not a timeout/transport error) and its rcode ∈ {`SERVFAIL`, `REFUSED`}, after the single retry, **AND** the conditional CD=1 re-query returned `cd_fail` (02-observation-model.md §2.7b). **Decision:** when the provider breaker (02) has degraded the fan-out to 2-of-2, branch (b) cannot be satisfied — dead detection is deliberately conservative and requires all 3 providers; the domain rides the error backoff (section 9) until the provider recovers. **Decision (grilling round):** the CD=1 `cd_present`/`cd_empty` outcomes are exactly what keep a broken-DNSSEC-but-live domain out of this branch — it is not unresolvable, so `dead_streak` never accrues for it and it commits its real (`supported`/`unsupported`/`no_record`) base observation.
- The rule applies to all rows regardless of `kind`; for subdomains whose parent zone exists, the NS walk finds a zone, so they can become `inactive` (confirmed `base = no_record`) but never `dead` — as intended.

## 5. The commit algorithm (normative)

All state is computed client-side from the claimed snapshot `S`; the database sees only the final values (section 12). All equality comparisons use IS DISTINCT FROM semantics. Numbered pseudocode; every step is normative:

```
# Inputs: S, O[d], I, U, A, T, L, BreakerOpen, D  (section 3)

# Step 0 — counting gate (section 7)
counting = (S.last_counted_at IS NULL) OR (T - S.last_counted_at >= anti_flap.min_confirm_spacing)

# Step 0b — dimension set
dims = [base, www, ns, mx, conn]                    # fixed order
if crawler.resources.enabled: dims += [resources]   # while false, the resources
                                                    # columns on domain stay NULL forever

# Step 1 — lifecycle: dead detection & recovery (§4.8 semantics)
if U:                                                        # unresolvable scan (section 4)
    dead_streak = min(S.dead_streak + 1, lifecycle.dead_streak)
else:
    dead_streak = 0
    if O[base] is definitive AND S.disabled AND S.disabled_reason == 'dead':
        apply Step R (section 6)                             # RECOVERY: re-enable + reset,
                                                             # then continue as a fresh domain —
                                                             # every d_status is now NULL, so this
                                                             # scan's definitive observations
                                                             # bootstrap-commit in step 2.

# Step 2 — per-dimension confirm/pending loop
changelog_rows = []
for d in dims:
    O = O[d]
    assert O != partial                        # defect guard (section 1); abort commit on violation
    d_observed = O                             # ALWAYS recorded, even error/inconsistent
    if O in {error, inconsistent}: continue    # non-definitive: touch nothing —
                                               #   status, pending, pending_count, since all survive
    if not counting: continue                  # record-only scan: pending/status/changelog untouched
    if d_status IS NULL:                       # BOOTSTRAP: first definitive observation
        d_status = O; d_since = T              #   commits immediately, NO changelog row
        d_pending = NULL; d_pending_count = 0
    elif O == d_status:                        # steady state: re-observation of the confirmed value
        d_pending = NULL; d_pending_count = 0  #   cancels any pending candidate
    elif O == d_pending:                       # pending candidate re-observed
        d_pending_count += 1
        if d_pending_count >= N(d):            # N per section 2: base/www/ns/mx=2, conn/resources=3
            changelog_rows += (S.id, T, field=d, old_value=d_status, new_value=O)
            d_status = O; d_since = T
            d_pending = NULL; d_pending_count = 0
    else:                                      # new candidate (or the candidate changed)
        d_pending = O; d_pending_count = 1

if counting AND (exists d in dims with O[d] definitive):
    last_counted_at = T
# else last_counted_at keeps S.last_counted_at

# Step 3 — classification (section 10; pure function over the post-step-2 CONFIRMED values)
classification, class_flags, saint = domain.Classify({d: d_status for d in
                                                     [base, www, ns, mx, conn, resources]})

# Step 4 — dead trigger
if NOT disabled AND dead_streak >= lifecycle.dead_streak:
    disabled = true; disabled_reason = 'dead'; disabled_at = T
    dead_streak = 0; error_streak = 0          # reset both streaks (error_streak set after step 5
                                               #   is overridden by this reset)

# Step 5 — error_streak maintenance (section 8)
if O[base] in {error, inconsistent} OR O[www] in {error, inconsistent}:
    error_streak = S.error_streak + 1
else:
    error_streak = 0
# (step 4's reset wins if both fired this scan)

# Step 6 — scheduling (section 9)
next_check_at = schedule(disabled, O[base], O[www], error_streak, rank, BreakerOpen, T)

# Step 7 — attribution
if O[base] is definitive: asn_id, country_id = A         # recomputed every committed scan
else:                     asn_id, country_id = S.asn_id, S.country_id   # deferred: a transient
                                                          # resolver failure must not flip
                                                          # a domain to 'Unknown'

# Step 8 — informational columns: overwritten verbatim from this scan, every commit
dnssec_observed = I.dnssec; ptr_observed = I.ptr
smtp_observed   = I.smtp;   parity_observed = I.parity
latency_v4_ms   = I.latency_v4_ms; latency_v6_ms = I.latency_v6_ms   # NULL when not measured
# Decision: NULL latency/skip values overwrite previous values (a domain that lost its AAAA
# gets NULL latency, honestly), matching step R's reset-to-NULL semantics.

# Step 9 — the write unit (section 12): one pgx.Batch in one pgx.Tx, fenced on L.
```

Notes binding the steps together:

- **Disabled rows still commit.** While a row is disabled (`dead`/`delisted`) its slow-lane scans still commit through this exact machinery — confirmed state stays maintainable; public exposure is handled purely by read-side query filters (the `NOT disabled` predicate; see 06/API spec). Setting `disabled = TRUE` never modifies `classification`, `class_flags`, `saint`, or any confirmed status/`*_since` column; the one state reset is Step R.
- **Recovery is `dead`-only.** `delisted` rows are re-enabled by Tranco import / campaign sync / the lifecycle sweep, never by this commit. `service`/`manual` rows never reach the commit (they are not claimable; see 04).
- **An unresolvable scan still runs the loop.** Under branch (a) the base observation is `no_record` (definitive), so `dead_streak` accrues while `base` confirms `no_record` and classification goes `inactive` — both are correct simultaneously. Under branch (b) the base observation is `error` (non-definitive): the loop touches nothing while `dead_streak` accrues.

## 6. Step R — re-enable + reset

Executed inside step 1, **before** applying this scan's observations, only for `disabled_reason='dead'` rows whose base observation is definitive:

1. `disabled = false`, `disabled_reason = NULL`, `disabled_at = NULL`, `dead_streak = 0`.
2. For every core dimension `d` (all six, regardless of `crawler.resources.enabled`): `d_status = NULL`, `d_observed = NULL`, `d_pending = NULL`, `d_pending_count = 0`, `d_since = NULL`. (Step 2 then immediately re-populates `d_observed` and bootstrap-commits definitive observations for the in-loop dimensions.)
3. Informational columns → NULL: `dnssec_observed`, `ptr_observed`, `smtp_observed`, `parity_observed`, `latency_v4_ms`, `latency_v6_ms`. (Step 8 then re-populates them from this scan.)
4. `classification = 'unknown'`, `class_flags = '{}'`, `saint = false`. (Step 3 recomputes them from the post-step-2 values anyway.)
5. Keep `asn_id`/`country_id` — refreshed by the scan in step 7.
6. **NO changelog rows** are written for the reset itself, and none for the first post-reset commits either: the current scan's observations flow through the normal step-2 algorithm against NULL confirmed values, so the first definitive value commits immediately with no changelog row (first-confirmation rule, section 11). A domain returning from the dead reappears with a fresh status and a clean changelog.

`last_counted_at` is deliberately NOT reset: a recovering dead row last counted ≥ `lifecycle.slow_lane_every` (30d) ago, so `counting` is already true and the fresh bootstrap commits this same scan.

## 7. Counting gate semantics

`counting = (last_counted_at IS NULL) OR (T − last_counted_at ≥ anti_flap.min_confirm_spacing)` (default 12h).

- **Non-counting scans are record-only for the confirmation machinery**: they still write the `scan` + `scan_detail` rows and update every `*_observed` column, the informational dimensions, latency, attribution, streaks, and scheduling — pending/status/changelog stay untouched.
- The first-ever definitive scan always counts (`last_counted_at IS NULL`), so new domains get a public status after one scan.
- `last_counted_at = T` is set only when the scan counted AND at least one in-loop dimension was definitive; an all-non-definitive scan does not consume the counting window.

## 8. Streak maintenance

- **`dead_streak`** (SMALLINT): incremented (saturating at `lifecycle.dead_streak`) on every unresolvable scan; reset to 0 on every resolvable scan and at the dead trigger itself. The saturation cap means an already-disabled dead row holds `dead_streak = lifecycle.dead_streak` while it stays unresolvable on the slow lane; the trigger checks `NOT disabled`, so it cannot re-fire.
- **`error_streak`** (SMALLINT): incremented on every scan where the `base` **or** `www` observation is non-definitive (`error`/`inconsistent`); reset to 0 otherwise; reset to 0 by the dead trigger. Non-definitive observations on `ns`/`mx`/`conn`/`resources` or informational dimensions never touch it. For `kind='subdomain'`, `www` is `not_applicable` (definitive), so only `base` drives the streak.

## 9. Scheduling — `next_check_at`

Computed client-side in the commit; the value lands in the fenced UPDATE. Rules (first match wins), all intervals added to `T`:

```
1. disabled (still or newly, after step 4)     → next_check_at = T + lifecycle.slow_lane_every   # 720h
2. O[base] or O[www] == inconsistent           → lane = recheck_inconsistent                      # 2h; inconsistent wins over error
3. O[base] or O[www] == error                  → lane = recheck_error                            # 6h
4. otherwise (definitive base and www)         → next_check_at = T + cadence(rank)

For rules 2–3:
  if BreakerOpen: next_check_at = T + cadence(rank)     # fast-lane breaker: pull-ins suspended
  else:           next_check_at = T + backoff(lane, error_streak)

backoff(lane, streak):                                   # streak is the post-step-5 value, ≥ 1 here
  if streak >= 10: return recheck_backoff_max            # Decision: overflow guard — 2h·2⁹ and 6h·2⁷
                                                         # both already exceed the 720h cap
  return min(lane * 2^(streak-1), recheck_backoff_max)
```

- Error-lane progression: 6h, 12h, 24h, 48h, 96h, 192h, 384h, then capped at 720h. Inconsistent-lane progression: 2h, 4h, 8h, … capped identically.
- `cadence(rank)`: `rank IS NULL` → `cadence.default` (24h); else the first entry of `cadence.bands` (config order) whose `min_rank`/`max_rank` bounds contain the rank → its `every`; no match → `cadence.default`. (Claim-side use of `next_check_at` and the frontier index: 04.)
- **Decision:** `error_streak` still increments while the fast-lane breaker is open (its definition in section 8 is observation-based); only the *scheduling* effect is suspended. When the breaker closes, the accumulated streak resumes governing the backoff.
- **Decision:** all scheduling arithmetic uses `T` as the base (not a fresh `now()`), making the commit unit fully deterministic and testable; T is at most seconds older than wall clock at write time.
- Rechecks are full scans (`Runner.Run` on the whole domain); there is no partial-scan mode.

## 10. Classification: ladder, flags, saint

Implemented as a pure function in `internal/domain` (`Classify`), recomputed on every commit (step 3) from the six post-step-2 **confirmed** values. Restated verbatim from the design (normative):

The ladder is deterministic, first match wins, evaluated over **confirmed** values only. **The value sets enumerated in each rule are exhaustive**: a dimension satisfies a rule only if its confirmed value is explicitly listed. `not_applicable` and NULL confirmed values never *shame* a domain (they never trigger `sinner` and never set a sub-reason flag), but they also never *satisfy* the hero bar unless the rule lists `not_applicable` (it does for `www` and `mx`, deliberately not for `ns` and `conn`). Consequence: a domain whose `conn` is NULL (persistent errors) or confirmed `not_applicable` (transition window) is `partial` with **no** flag — hero requires demonstrated IPv6-only reachability, and `broken_v6` requires demonstrated failure; neither may be assumed.

**Classification truth table (normative).** Inputs are confirmed values; each ∈ {`supported`, `unsupported`, `no_record`, `not_applicable`, NULL}. First match wins:

| # | Condition (confirmed values) | classification |
|---|---|---|
| 1 | `base` = NULL | `unknown` |
| 2 | `base` = `no_record` | `inactive` |
| 3 | `base` = `unsupported` | `sinner` |
| 4 | `base` = `supported` AND `www` ∈ {`supported`, `not_applicable`, `no_record`} AND `ns` = `supported` AND `conn` = `supported` AND `mx` ∈ {`supported`, `not_applicable`} | `hero` |
| 5 | `base` = `supported` (hero bar not met) | `partial` |

(`base` = `not_applicable` is unreachable: the apex AAAA check always yields a concrete status or a non-definitive error. Likewise `www` = `no_record` in rule 4 is defensive-only: the www mapper never emits `no_record` — only `base` can (02-observation-model.md — §4) — so the arm is unreachable in production; it stays in the ladder so `Classify` remains total and non-contradicting over the full enum cross-product the §17 vectors exercise.)

**Flags** (computed for every domain; only ever true when the named dimension is confirmed `unsupported` — NULL, `not_applicable`, and `no_record` set no flag):

| Flag | Condition |
|---|---|
| `broken_v6` | `conn` = `unsupported` |
| `www_missing` | `www` = `unsupported` |
| `ns_missing` | `ns` = `unsupported` |
| `mail_missing` | `mx` = `unsupported` |
| `resources_v4only` | `resources` = `unsupported` |

**Saint rule:** `saint` = classification `hero` AND `resources` ∈ {`supported`, `not_applicable`} (NULL resources → not saint).

**`ipv6_only` fold (derived, ADR):** `domain.IPv6Only(conn, resources)` is the classification-ungated conn+resources fold serialized by the API (07 §4.2) — "does the site present the same over an IPv6-only connection". `supported` iff `conn = supported` AND `resources` ∈ {`supported`, `not_applicable`}; `unsupported` iff `conn = unsupported` (first match — broken_v6 wins) or `resources = unsupported`; `not_applicable` iff `conn = not_applicable` (no AAAA to assess); NULL otherwise — **strict**: `conn = supported` with NULL `resources` claims nothing, and the impossible `no_record` inputs claim nothing. It is derived at API render time, never stored.

Notes: (a) A `partial` domain may legitimately carry zero flags — that is the "hero bar unverified" state (conn/ns NULL or `not_applicable`), which is transient by construction: definitive first-scan observations commit immediately (section 5), and the base-N=2 / conn-N=3 asymmetry bounds any confirmed `conn=not_applicable`-with-`base=supported` overlap to the transition window. Do not invent an extra flag for it. (b) `ns` = `not_applicable` is unreachable by construction (the NS walk-up always reaches an authoritative zone); if it ever occurs it blocks hero and sets no flag, per the table. (c) www NXDOMAIN and www with neither A nor AAAA both map to `not_applicable` (02's mapping tables) — a site without a working www can be a Hero. `www` never produces `no_record`; only `base` can, and confirmed `base = no_record` is what feeds the `inactive` tier and the dead/delist lifecycles.

While `crawler.resources.enabled=false`, `resources_status` is NULL for every domain, so `saint` evaluates false everywhere — correct: no saint badges before the resources feature ships. Flags are stored in `domain.class_flags TEXT[]`, sorted in the fixed order `broken_v6, www_missing, ns_missing, mail_missing, resources_v4only` (**Decision:** fixed order makes the column value deterministic and diff-friendly; the API never re-orders).

## 11. Changelog write rules

- **One row per confirmed transition**, written in the same transaction as the transition itself (step 2's `changelog_rows`). Columns: `(domain_id, ts, field, old_value, new_value)` with `ts = T` — the same T as the scan row, so a changelog entry always joins exactly one scan row.
- `field` ∈ `base | www | ns | mx | conn | resources` — the six core dimensions, the exact `changelog.field` domain (see 05-schema.md — `changelog`). The crawler is the sole writer of changelog rows; there is no history import and no `'legacy'` field.
- `old_value` and `new_value` are `ipv6_status` values, both always NOT NULL and (by construction of step 2) always distinct on native rows.
- **First-confirmation rule (normative): the NULL→value bootstrap transition NEVER writes a changelog row — suppression happens at write time, not at read time.** Consequently `old_value` is never NULL and the changelog endpoints (07-api.md — change feeds) apply no first-confirmation filter (see 05-schema.md — `changelog` for the constraints enforcing this shape).
- **Shadow-transition rule (normative, ADR): confirmed `conn → not_applicable` and `resources → not_applicable` flips NEVER write a changelog row — suppression at write time, same as the bootstrap rule.** They are deterministic shadows of rows the feed already carries: `conn` only reaches `not_applicable` when base/www lose their AAAA (which writes its own row in the same confirmation window), and `resources` only reaches `not_applicable` when `conn` leaves `supported`. The flip itself still commits (status/`*_since` move; the Transition is reported to telemetry) — only the feed row is suppressed. Transitions **out of** `not_applicable` are genuine news and keep their rows.
- Step R writes no changelog rows, and the first post-reset commits write none (bootstrap rule again). A resurrected domain has a clean changelog.
- **Cold classification start — no seed import.** Start-fresh cutover (OPEN-9; 08-migration-cutover.md — §1): no production statuses are seeded. Every domain begins with all `d_status = NULL` (`unknown`, `d_pending = NULL`, `d_pending_count = 0`) and is confirmed dimension-by-dimension by the fresh crawl over N consecutive scans; each first confirmation is the NULL→value bootstrap that writes **no** changelog row (the first-confirmation rule above). The changelog therefore records only genuine post-cutover transitions, and the day-1 dashboard's confirmed counts rise from the cold-start baseline to their true values over the first ~N days.
- Rows written while a domain is disabled simply become visible again on re-enable (public filtering is read-side); for `dead` recoveries there are none, because of Step R.
- 0–6 rows per commit (at most one per dimension per scan; multiple dimensions can transition in the same scan — e.g. a domain dropping its AAAA can flip `base`, `www`, and `conn` in one commit once each pending count matures).

## 12. The write unit — one `pgx.Batch` in one `pgx.Tx`

One `pgx.Tx` per domain; ALL statements queued as one `pgx.Batch` and sent in a single round trip. Batching is strictly a round-trip optimization over intact per-domain atomic units; scan rows are never split out into a separate bulk write, and `CopyFrom` is not used (it cannot preserve per-domain atomicity). Statement order is fixed (**Decision:** normative order below; the fenced UPDATE is first so its command tag is the first batch result read):

### 12.1 Statement list

**Statement 1 — the fenced domain UPDATE** (sqlc named-parameter style; every value computed client-side in section 5; unchanged columns are written back with their snapshot values — one static statement, no dynamic SQL):

```sql
-- name: CommitDomain :execrows
UPDATE domain SET
  base_status = @base_status, base_observed = @base_observed,
  base_pending = @base_pending, base_pending_count = @base_pending_count, base_since = @base_since,
  www_status = @www_status, www_observed = @www_observed,
  www_pending = @www_pending, www_pending_count = @www_pending_count, www_since = @www_since,
  ns_status = @ns_status, ns_observed = @ns_observed,
  ns_pending = @ns_pending, ns_pending_count = @ns_pending_count, ns_since = @ns_since,
  mx_status = @mx_status, mx_observed = @mx_observed,
  mx_pending = @mx_pending, mx_pending_count = @mx_pending_count, mx_since = @mx_since,
  conn_status = @conn_status, conn_observed = @conn_observed,
  conn_pending = @conn_pending, conn_pending_count = @conn_pending_count, conn_since = @conn_since,
  resources_status = @resources_status, resources_observed = @resources_observed,
  resources_pending = @resources_pending, resources_pending_count = @resources_pending_count,
  resources_since = @resources_since,
  dnssec_observed = @dnssec_observed, ptr_observed = @ptr_observed,
  smtp_observed = @smtp_observed, parity_observed = @parity_observed,
  latency_v4_ms = @latency_v4_ms, latency_v6_ms = @latency_v6_ms,
  classification = @classification, class_flags = @class_flags, saint = @saint,
  asn_id = @asn_id, country_id = @country_id,
  disabled = @disabled, disabled_reason = @disabled_reason, disabled_at = @disabled_at,
  dead_streak = @dead_streak, error_streak = @error_streak,
  next_check_at = @next_check_at, last_checked_at = @t, last_counted_at = @last_counted_at,
  claimed_at = NULL, updated_at = now()
WHERE id = @domain_id AND claimed_at = @lease;          -- LEASE FENCE
```

Columns deliberately NOT touched by the commit: `host, kind, parent_id, rank, created_by, orphaned_at, last_requested_at, created_at` (owned by ingest, the lifecycle sweep, and the API). While `crawler.resources.enabled=false` the `resources` dimension is excluded from the step-2 loop, so its parameters are not produced by the loop. `@resources_status`, `@resources_pending`, `@resources_pending_count`, `@resources_since` are bound from the claimed `DimState` (`Status`/`Pending`/`Since` = NULL, `PendingCount` = 0). **Decision:** `@resources_observed` is bound to NULL directly — the `DimState` snapshot (section 16) carries no `Observed` field, and `resources_observed` is provably always NULL while the flag is `false` (it is written only by the step-2 loop, which never runs for `resources` here), so `ComputeCommit` needs no snapshot `Observed` source. Net effect: all six `resources_*` columns stay NULL. (For every in-loop dimension `d_observed` is always set from `O[d]` in step 2 — even on `error`/`inconsistent` — so no in-loop `*_observed` param is ever written back from the snapshot.)

**Statements 2..(1+k) — changelog rows** (k = `len(changelog_rows)`, 0–6; the single-row statement is queued once per transition — **Decision:** per-row queuing over multi-row VALUES, so the statement is a static sqlc query):

```sql
-- name: InsertChangelog :exec
INSERT INTO changelog (domain_id, ts, field, old_value, new_value)
VALUES (@domain_id, @t, @field, @old_value, @new_value);
```

No ON CONFLICT: within one transaction the (domain_id, ts, field) key cannot repeat, and a lease-lost duplicate commit is impossible by construction (section 13) — a conflict here is a genuine defect and must surface as an error.

**Statement 2+k — the scan row** (idempotent under the worker-fixed T):

```sql
-- name: InsertScan :exec
INSERT INTO scan (domain_id, ts, base, www, ns, mx, conn, resources,
                  dnssec, ptr, smtp, parity, latency_v4_ms, latency_v6_ms,
                  classification, country_id, asn_id)
VALUES (@domain_id, @t, @base, @www, @ns, @mx, @conn, @resources,
        @dnssec, @ptr, @smtp, @parity, @latency_v4_ms, @latency_v6_ms,
        @classification, @country_id, @asn_id)
ON CONFLICT (domain_id, ts) DO NOTHING;
```

**Statement 3+k — the scan_detail row:**

```sql
-- name: InsertScanDetail :exec
INSERT INTO scan_detail (domain_id, ts, details, duration_ms)
VALUES (@domain_id, @t, @details, @duration_ms)
ON CONFLICT (domain_id, ts) DO NOTHING;
```

**Statements 4+k onward — resource-link persistence** (section 12.3; only when `crawler.resources.enabled=true` AND the discovery status was `ok`).

### 12.2 Transaction mechanics (Go shape)

```go
func (c *Committer) flush(ctx context.Context, u *commitUnit) error {
    tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
    if err != nil {
        return fmt.Errorf("begin: %w", err)
    }
    defer tx.Rollback(ctx) // no-op after a successful Commit

    br := tx.SendBatch(ctx, u.batch)
    tag, err := br.Exec() // result of statement 1: the fenced domain UPDATE
    leaseLost := err == nil && tag.RowsAffected() == 0
    var firstErr error = err
    for i := 1; i < u.batch.Len(); i++ { // drain every remaining result
        if _, e := br.Exec(); e != nil && firstErr == nil {
            firstErr = e
        }
    }
    if e := br.Close(); e != nil && firstErr == nil {
        firstErr = e
    }
    if leaseLost {
        c.metrics.LeaseLost.Add(1)
        c.log.Warn("lease lost, commit discarded", "domain_id", u.domainID)
        return nil // deferred Rollback discards EVERYTHING; write nothing
    }
    if firstErr != nil {
        return fmt.Errorf("commit batch: %w", firstErr) // rolled back; see section 15
    }
    return tx.Commit(ctx)
}
```

Rules: (1) the batch results MUST be fully drained and closed before Commit/Rollback (pgx requirement); (2) a lease-lost detection suppresses any errors from the trailing statements — the transaction is discarded regardless; (3) `RowsAffected() == 0` on statement 1 is the ONLY lease-lost signal; it is not an error condition from pgx's perspective, so it must be checked explicitly.

### 12.3 Resource-link persistence statements

Queued into the same batch, after scan_detail, only when `crawler.resources.enabled=true` AND discovery status = `ok`. If discovery status = `error`, existing links are kept untouched (a failed fetch is not evidence dependencies changed); if `not_applicable`, likewise no link statements. Hosts arrive already canonicalized and deduplicated (02; canonicalization failures were skipped there). Per discovered host, two statements:

```sql
-- name: EnsureResourceHost :exec
INSERT INTO resource_host (host) VALUES (@host)
ON CONFLICT (host) DO NOTHING;
-- new rows get aaaa_status NULL and next_check_at = now() (column defaults),
-- so the sweep confirms them within one day
```

```sql
-- name: UpsertDomainResource :exec
WITH rh AS (
  SELECT id FROM resource_host WHERE host = @host
), ins AS (
  INSERT INTO domain_resource (domain_id, resource_host_id, source, required, first_seen, last_seen)
  SELECT @domain_id, rh.id, 'discovered', TRUE, @t, @t FROM rh
  ON CONFLICT (domain_id, resource_host_id) DO NOTHING
  RETURNING resource_host_id
), bump AS (
  UPDATE resource_host SET dependent_count = dependent_count + 1
  WHERE id IN (SELECT resource_host_id FROM ins)
)
UPDATE domain_resource SET last_seen = @t
WHERE domain_id = @domain_id
  AND resource_host_id IN (SELECT id FROM rh)
  AND NOT EXISTS (SELECT 1 FROM ins);
```

The upsert never touches `source` — an existing `source='manual'` link is never downgraded; only its `last_seen` is refreshed. `dependent_count` is bumped +1 only on an actual link insert (the `ins` CTE returns rows only for genuinely new links).

Then one prune statement per domain:

```sql
-- name: PruneDomainResources :exec
WITH del AS (
  DELETE FROM domain_resource
  WHERE domain_id = @domain_id
    AND source = 'discovered'
    AND last_seen < @t - INTERVAL '30 days'
  RETURNING resource_host_id
)
UPDATE resource_host SET dependent_count = dependent_count - 1
WHERE id IN (SELECT resource_host_id FROM del);
```

(Each `resource_host_id` appears at most once in `del` — `(domain_id, resource_host_id)` is the PK — so a flat −1 is exact.) Manual links (`source='manual'`) are never pruned. The 30-day horizon is a fixed constant, not config.

All resource statements ride the same lease fence: a lease-lost rollback discards them along with everything else.

## 13. The lease fence

- The claim query stamps `claimed_at = now()` on every row in a claim batch (04); that value is the lease token L. The commit's domain UPDATE carries `WHERE id = @domain_id AND claimed_at = @lease`.
- **Fence semantics:** a worker that stalls past the 30-minute lease and resumes after a reclaim finds `claimed_at` changed (or NULL), the fenced UPDATE matches 0 rows, and the whole transaction — scan row, scan_detail, changelog, state — is discarded. Reclaims happen ≥ 30 minutes after the original claim, so two lease values can never collide (both are `now()` at claim time, ≥ 30 minutes apart).
- The claimed snapshot is therefore authoritative for the entire commit: no re-read, no optimistic-concurrency columns, no advisory locks. Exactly one worker's commit can ever land for a given claim of a given domain — this is the mechanism behind the "no double changelog" verification (10-testing.md).
- Fence aborts are counted (`lease_lost`, section 15) and logged at `warn` per the logging conventions; they are expected during worker crashes/redeployes and pathological stalls, and their steady-state rate should be ~0.

## 14. What `scan` and `scan_detail` rows contain

Every commit writes exactly one row into each (including non-counting scans and all-non-definitive scans; only a lease-lost rollback discards them). DDL: 05-schema.md — `scan`, `scan_detail`.

### 14.1 `scan` — the slim typed row

| Column | Value written |
|---|---|
| `domain_id`, `ts` | `S.id`, `T` |
| `base, www, ns, mx, conn, resources` | `O[d]` exactly as fed to step 2 — the post-mapping observation, INCLUDING `error`/`inconsistent` (the `observation` enum). `www = not_applicable` for subdomains; `resources = not_applicable` while `crawler.resources.enabled=false`. All NOT NULL. |
| `dnssec, ptr, smtp, parity` | `I.*` — the informational observations post-02-mapping (`ptr`/`parity` may be `partial`; `smtp` never is). Set on every scan (a phase-2 skip records `not_applicable`). |
| `latency_v4_ms, latency_v6_ms` | `I.latency_*`; NULL when not measured. |
| `classification` | The **post-commit confirmed** classification computed in step 3 — the confirmed class stamped at scan time, not a per-scan reclassification of observations. |
| `country_id, asn_id` | The values written to `domain` this commit (step 7): fresh attribution on definitive-base scans, carried snapshot values otherwise. |

`tls` and `spf` deliberately have NO typed columns anywhere — informational-only, they live exclusively in `scan_detail.details`.

### 14.2 `scan_detail` — the fat payload

`duration_ms` = wall-clock duration of the whole domain scan, in milliseconds. `details` (JSONB) is the adapted engine `ScanResult` serialization with the scoring fields removed, plus two hoisted derived objects. **Decision:** exact top-level shape (assembly is 02's job; the shape is pinned here because this file owns row content):

```json
{
  "domain": "<canonical host>",
  "scanned_at": "<RFC3339, = T>",
  "duration": "<int, nanoseconds>",
  "results": {
    "<check name>": { "status": "<engine 5-valued status>",
                      "details": { "...": "check-specific evidence, verbatim from the engine" },
                      "latency": "<int, nanoseconds>" }
  },
  "conn":      { "status": "<observation>", "source": "https|http",
                 "http_only": false, "error_type": "<https error_type; omitted on success>" },
  "consensus": { "base": { "resolvers": [
                             { "resolver": "cloudflare", "rcode": "NOERROR",
                               "symbol": "exists", "answered": true },
                             { "resolver": "google",  "rcode": "...", "symbol": "...", "answered": true },
                             { "resolver": "quad9",   "rcode": "...", "symbol": "...", "answered": true } ],
                           "agreement": "3of3|2of3|2of2",
                           "disagreed": false,
                           "a_outcome": "a_present|a_absent|a_error|\"\"",
                           "cd_outcome": "cd_present|cd_empty|cd_fail|\"\"" },
                 "www":  { "«same shape»": "omitted entirely for kind=subdomain" } }
}
```

- `results` keys are the engine check names, all 15 registered checks (`dns_aaaa_base, dns_aaaa_www, dns_ns_ipv6, dns_mx_ipv6, dns_dnssec, http_ipv6, https_ipv6, tls_ipv6, http_response_parity, resource_discovery, smtp_ipv6, spf_ipv6, dns_ptr_ipv6, latency_ipv4, latency_ipv6`); phase-2-skipped checks are present with status `not_applicable` and the runner's skip-reason detail. The raw engine verdict (including `partial`) is always preserved here even where the observation mapping rewrote it.
- The per-check `details` for `dns_aaaa_base`/`dns_aaaa_www` include the `quorum` object the adapted checks record (02) — the `consensus` top-level object is the flattened per-resolver tuple mandated for dead-detection auditability (section 4) and the detail page; `symbol` values are the per-resolver reduced symbols (`exists|empty|nxdomain|timeout|error`).
- The NS/MX per-host AAAA detail inside `results` is capped at 4 NS hosts / 5 MX hosts plus the `total`/`checked`/`ipv6_count` counters (02's `checks.max_ns_lookups`/`max_mx_lookups`).
- `conn` is the derived composition object; `http_only=true` only in the connection-refused-plus-HTTP-works fallback case. It is payload-only — NOT a class flag.

The detail page and the live-check dedupe path both read the latest `scan_detail` row through the shared `MapLiveResult` mapper (02), so this serialization is a stable contract: changing a key is a breaking change gated by the OpenAPI contract tests in 10-testing.md.

## 15. Failure handling & metrics

- **Lease lost** (`RowsAffected == 0` on statement 1): rollback, write nothing, count `lease_lost`, log `warn`. Not an error — the reclaimer owns the domain now.
- **Any other batch/commit error** (constraint violation, connection loss, etc.): rollback, count `commit_error`, log `error` with `domain_id` and the wrapped cause; the defect guard for a `partial` core observation lands here too. **Decision:** no compensating "unclaim" UPDATE is attempted — the row stays leased until the 30-minute expiry and is then reclaimed; this scan's observations are simply lost and reproduced by the next scan. (A compensating write outside the fence could race the reclaimer; the lease expiry is the designed recovery path.)
- **Metrics** (checkpointed into `crawler_metrics.dim_counters` every 1000 domains; Grafana-only): **Decision:** `lease_lost` and `commit_error` are keys inside the `dim_counters` JSONB (`{"lease_lost": n, "commit_error": n, ...}`) — the design offered "counter in dim_counters or a dedicated column"; the JSONB key needs no DDL and nothing queries it relationally. Also counted per checkpoint: `confirmed_transitions` (total changelog rows written), `bootstrap_commits`, and the per-dimension supported/unsupported tallies owned by the metrics spec (09-ops.md).

## 16. Go package shape

```go
// internal/domain — pure, zero deps.
type Dimension string      // "base" "www" "ns" "mx" "conn" "resources"
type IPv6Status string     // "supported" "unsupported" "no_record" "not_applicable"
type Observation string    // IPv6Status values + "partial" "error" "inconsistent"
type Classification string // "unknown" "inactive" "sinner" "partial" "hero"

// ConfirmN returns the anti-flap threshold for a dimension (section 2).
func ConfirmN(d Dimension) int16 // base/www/ns/mx → 2; conn/resources → 3

// Classify implements section 10 exactly. nil = never confirmed (NULL).
// Returned flags are in the fixed order of section 10; saint per the saint rule.
func Classify(confirmed map[Dimension]*IPv6Status) (Classification, []string, bool)
```

```go
// internal/crawler
// ClaimedDomain is the snapshot contract with 04's claim query (section 3).
type ClaimedDomain struct {
    ID             int64
    Host           string
    Kind           domain.Kind
    Rank           *int32
    ClaimedAt      time.Time // L — the lease token
    Disabled       bool
    DisabledReason *domain.DisabledReason
    DisabledAt     *time.Time
    DeadStreak     int16
    ErrorStreak    int16
    LastCountedAt  *time.Time
    AsnID          int32
    CountryID      int32
    Dims           map[domain.Dimension]DimState // all six groups
}

type DimState struct {
    Status       *domain.IPv6Status
    Pending      *domain.IPv6Status
    PendingCount int16
    Since        *time.Time
}

// CommitInput carries sections 3's inputs; produced by the worker after Runner.Run
// and 02's observation mapping. T is fixed before construction.
type CommitInput struct {
    Snapshot     ClaimedDomain
    Obs          map[domain.Dimension]domain.Observation // core dims (resources only when enabled)
    Info         Informational                           // dnssec/ptr/smtp/parity/latency
    Unresolvable bool                                    // section 4
    Attribution  *Attribution                            // nil = deferred (base non-definitive)
    Discovered   []string                                // canonical resource hosts; nil unless ok
    DiscoveryOK  bool
    BreakerOpen  bool
    Details      []byte // scan_detail JSON (section 14.2)
    DurationMS   int32
    T            time.Time
}

// Commit runs sections 5–12 for one domain: computes the post-scan state
// client-side (pure, unit-testable via ComputeCommit), then flushes the write
// unit under the lease fence. LeaseLost=true means nothing was written.
func (c *Committer) Commit(ctx context.Context, in CommitInput) (CommitResult, error)

type CommitResult struct {
    LeaseLost   bool
    Transitions []Transition // the changelog rows written (dim, old, new)
}
```

The state computation (steps 0–8) MUST be factored as a pure function (`ComputeCommit(in CommitInput) commitUnit`) with no I/O, so 10-testing.md can drive it table-style; the flush (`flush`, section 12.2) is exercised by the dockerized Postgres integration tests.

## 17. Acceptance criteria

(Fixture tables and parity data: 10-testing.md. These are the properties any implementation must satisfy.)

1. A confirmed value never changes on fewer than N(d) definitive observations of the new value, on scans spaced ≥ `anti_flap.min_confirm_spacing` apart — even when 2h fast-lane rechecks produce many more scans in between.
2. The first definitive observation on a NULL-confirmed dimension commits immediately and writes NO changelog row; the same holds for the first commits after Step R.
3. `error`/`inconsistent` observations never modify `d_status`, `d_pending`, `d_pending_count`, `d_since`, `classification`, `class_flags`, `saint`, or attribution — yet the scan and scan_detail rows are still written and `d_observed` is updated.
4. A lease-lost worker writes NOTHING: no domain state, no changelog, no scan, no scan_detail, no resource links; the `lease_lost` counter increments.
5. `changelog.old_value`/`new_value` are never NULL and never equal on native rows; `ts` always joins a scan row with the same `(domain_id, ts)`.
6. Re-running an identical commit unit (same T) after a mid-flight retry produces no duplicate scan/scan_detail rows (ON CONFLICT DO NOTHING) and cannot double-write changelog rows (the fence forbids a second successful domain UPDATE for the same lease).
7. Seven consecutive unresolvable scans (and never fewer) disable a domain with `disabled_reason='dead'`; a later definitive base observation on that row re-enables it with all confirmed state, informational state, and classification reset, and a clean changelog. A domain that is all-SERVFAIL but whose CD=1 re-query returns AAAA (`cd_present`) is **not** unresolvable — it commits `base=supported`, is never marked dead, and its broken DNSSEC is recorded only as the informational `dnssec` dimension (02-observation-model.md §2.7b).
8. `Classify` reproduces the section 10 truth table for the full cross-product of confirmed values, including: NULL conn → `partial` with no flag; `www=no_record` counts toward hero; `base=no_record` → `inactive` regardless of other dimensions; saint false whenever `resources` is NULL.
9. While `crawler.resources.enabled=false`: `scan.resources = 'not_applicable'` on every row, all `domain.resources_*` columns stay NULL, and no domain is saint.
10. `dependent_count` on `resource_host` equals the exact number of `domain_resource` links at all times (+1 only on genuine link insert, −1 only on prune delete).
