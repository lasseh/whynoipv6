# 04 — Lifecycle & Scheduling

**Purpose:** Defines the crawl frontier (the `domain` table itself), the atomic claim query, the cadence and recheck/backoff scheduling rules applied after every scan commit, the dead/delisted lifecycles with their re-entry semantics, the daily lifecycle sweep and daily-tick step order, singleton coordination via Postgres advisory locks, and the crawler process model (worker pool, preflight, graceful shutdown, operational metrics). Everything in this file is normative; the scan-commit algorithm that *consumes* the scheduling decision is in 03-state-machine.md.

**Deliverables:**

- `internal/crawler/frontier.go` — claim query, claim loop, worker pool
- `internal/crawler/schedule.go` — `cadence(rank)`, post-commit scheduling decision
- `internal/crawler/sweep.go` — daily lifecycle sweep (tick step 1)
- `internal/crawler/tick.go` — daily-tick coordinator (step order, failure containment)
- preflight wiring in the claim loop (`frontier.go`, §11) — this file consumes the `checker.Preflight` type defined in 01-engine.md and owns only its claim-loop integration and failure/retry behavior (it does **not** define a preflight type)
- `internal/crawler/metrics.go` — checkpointed `crawler_metrics` writer + latency histogram
- `internal/lock/lock.go` — advisory-lock singleton coordination (`TryRun`/`Run`, lock registry)
- `cmd/crawler/main.go` — process wiring, goroutine topology, signal handling

**Companion files:** 03-state-machine.md (commit algorithm, step R, dead-signal inputs), 05-schema.md (all DDL: `domain`, `idx_domain_due`, `crawler_metrics`, `service_candidate`, `check_job`), 06-ingest.md (Tranco import + campaign sync internals invoked by the tick/coordinator), 02-observation-model.md (fast-lane breaker state consumed by scheduling), 01-engine.md (`Runner.Run`, per-domain engine budget), 07-api.md (live-check consumer contract; POST /check re-entry writes), 09-ops.md (config-key registry, systemd units, healthchecks), 00-overview.md (canonical sizing constants), 10-testing.md (test fixtures for everything below).

Hard constraints restated where relevant: the crawler is fully autonomous (no accounts, no admin HTTP surface); the public 3-state model and hero/partial/sinner ladder are computed in 03-state-machine.md — nothing in this file computes classification; the stack is current Go, PG18 + TimescaleDB, pgx/v5, slog, viper.

---

## 1. Model summary

**The frontier is the `domain` table itself.** There is no jobs table, no queue, no external scheduler, and no "pass" barrier. Scheduling state is exactly three columns on `domain` (DDL in 05-schema.md — `domain`):

- `next_check_at TIMESTAMPTZ NOT NULL` — when the row is next due,
- `claimed_at TIMESTAMPTZ` — worker lease; a claim older than the lease-reclaim window is stale and reclaimable,
- `rank INT NULL` — priority (Tranco rank; NULL = unranked, sorts last).

Workers claim due rows atomically with `FOR UPDATE SKIP LOCKED`, run the engine, and commit per 03-state-machine.md; the commit's fenced UPDATE writes the next `next_check_at` per §5 below and clears the lease. Rows leave the frontier only by materialized state (`disabled` + `disabled_reason`), never by computed linkage: linkage (rank, campaign membership, children, recent live-check) is evaluated **once per day** by the lifecycle sweep (§8), which is the **single owner of orphan detection**. Tranco import and campaign sync never set `orphaned_at`; they only clear state on re-entry (§7).

Rejected alternatives (locked by the design): River or any job queue (a frontier column + SKIP LOCKED is the whole requirement; a queue adds a jobs table that must be filled by a scheduler — v6audit's scheduler died exactly there, materializing 1M due domains into memory and millions of job-row inserts per tick); Redis/NATS work distribution (new stateful infra for a problem Postgres solves at this scale). The one place queue semantics are real is on-demand live checks (`check_job`), and SKIP LOCKED covers that too (07-api.md).

Constants used throughout this file (canonical table: 00-overview.md — sizing constants): scan rate ~12 domains/s sustained (1.03M/day), worker slots ~72 average / **128 provisioned = 2 processes × 64 slots**, weighted mean scan duration ≈ 6 s.

## 2. Frontier state & eligibility

A `domain` row is **frontier-eligible** iff:

```
(NOT disabled OR disabled_reason IN ('dead', 'delisted')) AND next_check_at <= now()
```

and its lease is free:

```
claimed_at IS NULL OR claimed_at < now() - interval '30 minutes'
```

Interplay with `disabled_reason` (full lifecycle table in §6/§7):

| `disabled_reason` | Frontier | Cadence while disabled |
|---|---|---|
| *(not disabled)* | yes | `cadence(rank)` / recheck lanes (§5) |
| `dead` | yes — slow lane | `lifecycle.slow_lane_every` (default 720h) |
| `delisted` | yes — slow lane | `lifecycle.slow_lane_every` (default 720h) |
| `service` | **no** — leaves the frontier entirely | — |
| `manual` | **no** — leaves the frontier entirely | — |

The eligibility predicate is materialized, not computed at claim time: the claim query reads only `disabled`/`disabled_reason`/`next_check_at`/`claimed_at`. It must **textually match** the partial-index predicate of `idx_domain_due` (05-schema.md — `idx_domain_due`: `CREATE INDEX ... ON domain (next_check_at) WHERE NOT disabled OR disabled_reason IN ('dead', 'delisted')`) so the planner's predicate-implication check is trivial.

**Lease-reclaim window** is a compile-time constant, not config:

```go
// internal/crawler/frontier.go
const LeaseReclaim = 30 * time.Minute // embedded in the claim SQL; never configurable
```

A crashed or stalled worker's batch is reclaimed after 30 minutes rather than lost for a day. Reclaims happen ≥30 min after the original claim, so two lease values can never collide; the commit's lease fence (03-state-machine.md — fence semantics) discards the loser's whole transaction.

## 3. The claim query

One statement per claim cycle; `$1` = `claim.batch_size` (default 200). All rows in a batch get the **same** `claimed_at` value (one `now()` per statement) — that value is the lease token `L` consumed by 03-state-machine.md's fenced UPDATE.

```sql
-- internal/crawler/frontier.go :: ClaimBatch
UPDATE domain SET claimed_at = now()
WHERE id IN (
  SELECT id FROM domain
  WHERE (NOT disabled OR disabled_reason IN ('dead', 'delisted'))
    AND next_check_at <= now()
    AND (claimed_at IS NULL OR claimed_at < now() - interval '30 minutes')
  ORDER BY rank ASC NULLS LAST, next_check_at ASC
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
RETURNING
  id, host, kind, rank, claimed_at,
  disabled, disabled_reason, disabled_at,
  dead_streak, error_streak, last_counted_at,
  asn_id, country_id,
  base_status,      base_observed,      base_pending,      base_pending_count,      base_since,
  www_status,       www_observed,       www_pending,       www_pending_count,       www_since,
  ns_status,        ns_observed,        ns_pending,        ns_pending_count,        ns_since,
  mx_status,        mx_observed,        mx_pending,        mx_pending_count,        mx_since,
  conn_status,      conn_observed,      conn_pending,      conn_pending_count,      conn_since,
  resources_status, resources_observed, resources_pending, resources_pending_count, resources_since;
```

**Decision:** the design doc's RETURNING list named `id, host, kind, rank, claimed_at, disabled, disabled_reason, dead_streak` plus the status/pending column groups; the list above additionally returns `error_streak`, `last_counted_at`, `disabled_at`, `asn_id`, `country_id`, and the six `*_observed` columns, because 03-state-machine.md's algorithm reads all of them (backoff computes from `error_streak`; the counting gate reads `last_counted_at`; the fenced UPDATE writes back `disabled_at`, attribution columns, and `resources_observed` unchanged on deferred/disabled branches). This is the complete set of columns the commit algorithm may read; the commit never re-SELECTs the row — the lease fence guarantees the claimed snapshot is still authoritative at commit time.

**Plan shape (load-bearing, verified by a test gate — 10-testing.md):** the inner SELECT must execute as an index scan on `idx_domain_due` bounded by `next_check_at <= now()`, followed by a top-N heapsort on `(rank NULLS LAST, next_check_at)`. Cost is O(due-set) per claim: a few hundred rows in the steady state, at worst one pass over the full backlog after downtime (~hundreds of ms at 1M due, shrinking as the backlog drains) — and rank-priority fall-behind is exact in both regimes. Do **NOT** "optimize" with an inner `ORDER BY next_check_at LIMIT k` pre-filter before the rank sort: that silently flips the fall-behind policy from rank-priority to aging-priority precisely when the due set is large. The `claimed_at` lease condition is intentionally a **residual filter, not an index column** — `claimed_at` is deliberately unindexed so lease stamping is a HOT update. Corollary invariant (05-schema.md restates it): no full (non-partial) index with leading column `rank` may ever be added without re-running the claim-plan gate.

`ORDER BY rank ASC NULLS LAST` implements the fall-behind policy directly: when due domains exceed capacity, top-ranked domains are refreshed first and the tail's effective interval stretches — graceful degradation, no separate mode. The starvation risk this creates for the tail under *permanent* undercapacity is accepted. **Decision:** the design's "a config flag can flip the sort to `next_check_at ASC` as an aging pressure valve" is realized as config key `claim.order` ∈ `rank|age` (default `rank`; registry: 09-ops.md). `age` replaces the ORDER BY with `next_check_at ASC` (same query otherwise, still served by `idx_domain_due` with no sort at all). Implement as two sqlc queries selected at startup; the key is read once — no runtime flipping.

**Claim-loop cadence:** after a claim returning ≥1 row, the process feeds its worker pool and claims again as soon as the batch is dispatched (the pool's slot availability is the natural throttle). After a claim returning 0 rows, sleep `claim.empty_poll_interval` (default 10s) before the next preflight+claim cycle. With `idx_domain_due`, an empty-frontier claim is a sub-millisecond range probe; 10s is chosen to keep idle-log noise down, not for DB protection.

## 4. cadence(rank)

Cadence is per-rank-band config, default daily everywhere:

```yaml
# config (registry: 09-ops.md); keys are top-level, not nested under a `crawler:` map
cadence:
  default: 24h
  bands: []     # each: {min_rank: <int, optional>, max_rank: <int, optional>, every: <duration>}
```

Normative definition:

```go
// internal/crawler/schedule.go

type Band struct {
    MinRank int32         // 0 = no lower bound
    MaxRank int32         // 0 = no upper bound
    Every   time.Duration // > 0
}

// cadence returns the base re-check interval for a domain.
// rank == nil (unranked: campaign-only, live-check, parent_link rows) always
// uses Default — bands never match a NULL rank.
// Bands are evaluated in config order; the FIRST matching band wins.
func cadence(rank *int32, def time.Duration, bands []Band) time.Duration {
    if rank != nil {
        for _, b := range bands {
            if (b.MinRank == 0 || *rank >= b.MinRank) &&
                (b.MaxRank == 0 || *rank <= b.MaxRank) {
                return b.Every
            }
        }
    }
    return def
}
```

**Decision:** the design gives band examples but no matching rule; pinned here: config order, first match wins, both bounds inclusive, absent bound = open, NULL rank never matches a band. Startup validation (fail fast): every band must set at least one bound, `Every > 0`, `MinRank <= MaxRank` when both set. No jitter is added — spread comes from the Tranco import's 24h `next_check_at` spread on insert (06-ingest.md) and stays self-sustaining because each commit schedules relative to its own completion time.

The path to Tranco-full (~4.5M rows) is band config only, e.g. `[{max_rank: 1000000, every: 24h}, {min_rank: 1000001, every: 72h}]` — no schema or code change, by construction.

## 5. Post-commit scheduling

Executed once per scanned domain, worker-side, as part of building the single commit unit (03-state-machine.md step 4 — the value lands in the fenced UPDATE's `next_check_at`). Inputs: the claimed snapshot (§3), this scan's per-dimension observations (mapped per 02-observation-model.md / 03-state-machine.md), the dead-lifecycle outcome (§6), and the consensus fast-lane breaker state.

The breaker seam is the consensus resolver's `FastLaneSuppressed()` method (defined in `internal/consensus`; see 02-observation-model.md — Fast-lane breaker):

```go
// FastLaneSuppressed reports whether the fast-lane breaker is open:
// over its rolling window, (error+inconsistent)/total consensus observations
// exceeded consensus.fastlane_breaker.nondefinitive_rate. While open, the
// 2h/6h recheck pull-ins are not applied (scheduling falls back to cadence).
// It has NO effect on error_streak bookkeeping (§5.1 step 1).
func (r *Resolver) FastLaneSuppressed() bool
```

Definitions: an observation is **non-definitive** iff it is `error` or `inconsistent`. Only the two consensus dimensions (`base`, `www`) ever affect scheduling; `error` on `ns`/`mx`/`conn`/`resources` and anything on informational dimensions (`dnssec`/`ptr`/`smtp`/`parity`) never changes scheduling — those retry at normal cadence. Rechecks are always **full scans** (`Runner.Run` on the whole domain); there is no partial-scan mode.

### 5.1 Decision procedure (normative, exact order)

```
baseND = O[base] ∈ {error, inconsistent}
wwwND  = O[www]  ∈ {error, inconsistent}
nonDef = baseND OR wwwND
susp   = consensus.FastLaneSuppressed()
T      = the commit timestamp (03-state-machine.md — fixed once per domain; all
         scheduling arithmetic uses T, never a fresh now(), for determinism)

# 1. error_streak bookkeeping (always runs, every commit, disabled rows included).
#    Observation-based and UNCONDITIONAL — the breaker never changes it (03-state-machine.md
#    owns streak maintenance; this restates its rule so the scheduler is self-contained).
if nonDef:  error_streak = error_streak + 1
else:       error_streak = 0

# 2. dead lifecycle (§6): dead_streak update, dead trigger, or step-R recovery
#    (the trigger resets BOTH streaks to 0 and sets disabled/'dead')

# 3. lane selection — first match wins
if disabled (still disabled after step 2, or newly disabled by it):
    next_check_at = T + lifecycle.slow_lane_every               # slow lane, 720h
elif nonDef AND NOT susp:
    lane = recheck_inconsistent  if (O[base] == inconsistent OR O[www] == inconsistent)
           else recheck_error                            # inconsistent wins over error
    next_check_at = T + backoff(lane, error_streak)             # §5.2
else:                                                            # definitive base+www, OR breaker open
    next_check_at = T + cadence(rank)                            # §4

backoff(lane, streak):                        # streak is the post-step-1 value, >= 1 here
    if streak >= 10: return recheck_backoff_max          # overflow guard (03 parity)
    return min(lane * 2^(streak - 1), recheck_backoff_max)
```

Notes:

- The backoff exponent uses the **already-incremented** `error_streak` from step 1 (streak 1 ⇒ multiplier 1).
- `backoff`'s `streak >= 10` overflow guard prevents a `time.Duration` shift overflow; it is a no-op on the output because `min(...)` already caps at `recheck_backoff_max` well before streak 10 (the error lane caps at streak 8, the inconsistent lane at streak 10 — §5.2). `error_streak` is stored `SMALLINT`; clamp the stored value at 32767.
- **Decision (aligned with 03-state-machine.md — Scheduling):** while the fast-lane breaker is open, `error_streak` **still increments** — step 1 is observation-based and unconditional, matching the literal design rule ("increments on every scan where base or www is non-definitive, resets to 0 otherwise") and 03's ownership of streak maintenance. Only the *scheduling* effect is suspended: non-definitive base/www scans schedule at `cadence(rank)` instead of the backoff lane, and when the breaker closes the accumulated streak resumes governing the backoff. (An earlier draft froze the streak while the breaker was open; that contradicted both the design rule and 03 and is superseded here.)
- Step 1's reset branch also covers recovery (step R): a domain re-enabled by step R commits with a definitive base observation, so `error_streak = 0` and lane selection falls through to cadence.
- `dead`/`delisted` slow-lane scans run the full commit machinery (confirmed state stays maintainable — 03-state-machine.md); step 3's first branch keeps them on the slow lane until a lifecycle event re-enables them.

### 5.2 Backoff schedules (derived, for reference and tests)

`recheck_inconsistent` = 2h, `recheck_error` = 6h, `recheck_backoff_max` = 720h (the 30d slow lane). Consecutive non-definitive scans:

| `error_streak` | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | ≥10 |
|---|---|---|---|---|---|---|---|---|---|---|
| error lane (6h) | 6h | 12h | 24h | 48h | 96h | 192h | 384h | 720h | 720h | 720h |
| inconsistent lane (2h) | 2h | 4h | 8h | 16h | 32h | 64h | 128h | 256h | 512h | 720h |

The lane is re-chosen per scan from that scan's observations (a domain can alternate lanes); the streak is shared. The confirmed-flip guarantee is unaffected by fast rechecks: a confirmed transition still requires N definitive observations spaced ≥ `anti_flap.min_confirm_spacing` (12h) apart — the counting gate lives in 03-state-machine.md, key introduced there (registry: 09-ops.md).

## 6. Dead lifecycle

### 6.1 Unresolvable scans and `dead_streak`

The worker holds the raw engine results and computes, per scan, before the per-dimension commit loop: a scan is **unresolvable** when either

- **(a) NXDOMAIN branch:** the apex AAAA consensus quorum symbol is `nxdomain` (quorum majority of resolver rcodes — NXDOMAIN is name-level, so it covers A as well) **AND** the NS zone walk found no delegated zone for the host; or
- **(b) all-SERVFAIL branch:** all 3 consensus resolvers returned an explicit SERVFAIL or REFUSED rcode for apex AAAA after retry. **Timeouts do NOT count** — three timeouts more likely indicate our own network trouble.

Branch (a) requires the NXDOMAIN **rcode**, not merely a `base = no_record` observation: NOERROR-with-no-records is a live but inactive zone and must NOT count (it feeds the `inactive` classification, never `dead`). The raw rcode is available because `scan_detail.details.consensus` records the per-resolver tuples (02-observation-model.md). The rule applies to all rows regardless of `kind`; for subdomains whose parent zone exists the NS walk finds a zone, so they can become `inactive`, never `dead` — as intended.

Streak update (03-state-machine.md executes this as commit step 1):

```
if unresolvable: dead_streak = LEAST(dead_streak + 1, lifecycle.dead_streak)
else:            dead_streak = 0
```

### 6.2 Dead trigger

```
if NOT disabled AND dead_streak >= lifecycle.dead_streak:      # default 7
    disabled = true; disabled_reason = 'dead'; disabled_at = T
    dead_streak = 0; error_streak = 0                          # reset both streaks
```

Scheduling then lands on the slow lane via §5.1 step 3 (`next_check_at = now() + lifecycle.slow_lane_every`). Timelines at defaults:

- **NXDOMAIN domains** produce definitive `base = no_record` observations, so they ride **daily cadence** → dead in 7 days.
- **All-SERVFAIL domains** produce `base = error` (non-definitive), so they ride the §5 error-lane backoff: the 7th consecutive unresolvable scan lands after 6+12+24+48+96+192 = 378h of accumulated spacing → dead in **~2.3 weeks**.

Setting `disabled = TRUE` does **NOT** modify `classification`, `class_flags`, `gold`, or any confirmed status/`*_since` column — history and state are preserved; public exclusion is achieved solely by the `NOT disabled` filter in every public query (07-api.md).

### 6.3 Recovery (auto, slow-lane revalidation)

Dead rows stay claimable on the slow lane. On any subsequent scan (03-state-machine.md, before the per-dimension loop):

```
if NOT unresolvable AND base observation is definitive
   AND disabled AND disabled_reason = 'dead':
    execute step R (re-enable + full state reset to unknown — 03-state-machine.md, step R),
    then continue as a fresh domain
```

Recovery is exclusively this path: re-listing by Tranco or a POST /check pulls the scan in (`next_check_at = now()`, §7) but **never re-enables a dead row directly** — the domain must actually resolve. After step R the row is no longer disabled, so §5.1 schedules it at cadence (or a recheck lane); its first definitive observations bootstrap-commit immediately with no changelog rows (03-state-machine.md — first-confirmation rule).

## 7. Delist lifecycle & re-entry semantics

`delisted` means "lost all linkage": rank became NULL and no enabled-campaign membership, no children, no live-check request within `lifecycle.live_check_linkage`. Orphan detection and delisting are owned solely by the daily sweep (§8); re-entry is owned by the ingress that restores linkage. The **unified delisted re-entry action** (identical wherever it fires) is:

```
disabled = false; disabled_reason = NULL; disabled_at = NULL;
orphaned_at = NULL; next_check_at = now()
```

(Confirmed state was never reset — it is merely ≤30d stale; the immediate rescan refreshes it. No changelog implications beyond real transitions.)

Normative re-entry matrix — what each ingress does to an **existing** row, by current state:

| Current state | Tranco import: host in today's list (06-ingest.md) | Campaign sync: membership added (06-ingest.md) | POST /check for the host (07-api.md) | Daily sweep: linkage/rank present (§8) |
|---|---|---|---|---|
| active | `rank = excluded.rank`, `orphaned_at = NULL` | membership row added; nothing else | `last_requested_at = now()` | `orphaned_at = NULL` |
| `delisted` | unified re-entry action + `rank = excluded.rank` | unified re-entry action | `last_requested_at = now()` + unified re-entry action | unified re-entry action (**Decision**, below) |
| `dead` | stays disabled; `rank = excluded.rank`, `orphaned_at = NULL`, `next_check_at = now()` (recovery only via §6.3) | stays disabled; `next_check_at = now()` | `last_requested_at = now()`; stays disabled; `next_check_at = now()` | no action |
| `service` / `manual` | `rank = excluded.rank`, `orphaned_at = NULL`; remains disabled, out of the frontier | membership row added; remains disabled | check runs and returns its result; `last_requested_at = now()`; never re-enables | no action |

**Decision:** the sweep also applies the unified re-entry action to `delisted` rows that have regained rank or linkage. The design pins re-enable-on-rank (Tranco upsert) and re-enable-on-membership-addition (campaign sync step 3) but leaves one gap: a *campaign re-enable* (file re-appears, §7.3 step 3 of the design) restores linkage without adding membership rows, so its previously-delisted member domains would otherwise stay delisted forever. Making the sweep's linkage evaluation symmetric (linked ⇒ re-enable delisted) closes the gap with the same rule that opened it, is idempotent, and cannot fight the other ingresses (they set the same target state).

New rows (for completeness; owned by their ingress specs): Tranco inserts `created_by='tranco'` with `next_check_at` spread over the next 24h; campaign sync inserts `created_by='campaign'` (+ `parent_link` parents); the live-check consumer inserts `created_by='live_check'`, `rank NULL`, `last_requested_at = now()`, `next_check_at` default `now()`. All go through `Canonicalize` at their ingress.

## 8. The daily lifecycle sweep (tick step 1)

One transaction, set-based, over tens of thousands of `rank IS NULL` rows — cheap. Runs as step 1 of the daily tick (§9), under the tick's advisory lock. Parameters: `$1` = `lifecycle.live_check_linkage` (interval, default 168h), `$2` = `lifecycle.delist_grace` (interval, default 720h), `$3` = `lifecycle.slow_lane_every` (interval, default 720h).

The **linkage predicate**, spelled identically in every statement below (sqlc duplicates it per query):

```sql
-- linked(d):
     EXISTS (SELECT 1 FROM campaign_domain cd
             JOIN campaign c ON c.id = cd.campaign_id AND NOT c.disabled
             WHERE cd.domain_id = d.id)
  OR EXISTS (SELECT 1 FROM domain ch WHERE ch.parent_id = d.id)
  OR d.last_requested_at >= now() - $1
```

Campaign membership counts **only while the campaign itself is enabled** — without the `AND NOT c.disabled` join condition, a disabled campaign's kept membership rows would pin its rank-NULL domains in the frontier forever, contradicting the delist grace.

Statements, in order, one transaction:

```sql
BEGIN;

-- S1: clear orphan marks on every row that is ranked or linked.
UPDATE domain d
SET orphaned_at = NULL, updated_at = now()
WHERE d.orphaned_at IS NOT NULL
  AND (d.rank IS NOT NULL OR linked(d));

-- S2 (Decision, §7): symmetric re-entry — re-enable delisted rows that are
-- ranked or linked again.
UPDATE domain d
SET disabled = false, disabled_reason = NULL, disabled_at = NULL,
    orphaned_at = NULL, next_check_at = now(), updated_at = now()
WHERE d.disabled AND d.disabled_reason = 'delisted'
  AND (d.rank IS NOT NULL OR linked(d));

-- S3: live_check rows lose the frontier immediately when unlinked — no grace.
-- (The POST /check contract is a 7-day frontier linkage; last_requested_at has
-- already expired, and these rows were never publicly listed. The grace exists
-- for Tranco rank flapping.)
UPDATE domain d
SET disabled = true, disabled_reason = 'delisted', disabled_at = now(),
    next_check_at = now() + $3, updated_at = now()
WHERE NOT d.disabled AND d.rank IS NULL AND d.created_by = 'live_check'
  AND NOT (linked(d));

-- S4: other unlinked rank-NULL rows enter (or keep) the delist grace.
UPDATE domain d
SET orphaned_at = now(), updated_at = now()
WHERE NOT d.disabled AND d.rank IS NULL AND d.created_by <> 'live_check'
  AND NOT (linked(d))
  AND d.orphaned_at IS NULL;          -- only stamp fresh orphans; never move an existing mark

-- S5: grace expired -> delist.
UPDATE domain d
SET disabled = true, disabled_reason = 'delisted', disabled_at = now(),
    next_check_at = now() + $3, updated_at = now()
WHERE NOT d.disabled AND d.rank IS NULL AND d.created_by <> 'live_check'
  AND NOT (linked(d))
  AND d.orphaned_at IS NOT NULL AND d.orphaned_at < now() - $2;

COMMIT;
```

Properties the implementer must preserve:

- Grace-period rows (`orphaned_at` set, not yet disabled) keep normal-cadence scanning; `ORDER BY rank NULLS LAST` already deprioritizes them.
- The sweep is idempotent (pure set-based UPDATEs); re-running it in the same day is a no-op.
- Campaign-membership removals and live-check expiry are picked up within 24h, which satisfies the 30-day/7-day windows with margin.
- **Race with in-flight commits is tolerated by design:** a §5 commit whose claim snapshot predates the sweep can overwrite `disabled`/`next_check_at` set by S3/S5 (the fenced UPDATE writes back the snapshot's lifecycle columns). The row is then re-delisted by the next day's sweep — S4's "never move an existing mark" makes the grace clock monotonic, so the self-heal never extends the grace. No locking is added for this.
- The sweep never touches `dead`, `service`, or `manual` rows (S2 filters on `'delisted'`; S3–S5 filter `NOT disabled`), and never resets confirmed state.

## 9. The daily tick — canonical step order

There is no pass barrier in the hot path — workers run continuously against `next_check_at`. A **daily tick** runs in the crawler's coordinator goroutine at **03:30 UTC** (after most of the day's Tranco delta has settled). **Decision:** the tick time is a compile-time constant (`tickAt = "03:30"` UTC in `internal/crawler/tick.go`), not a config key — the design fixes the time and no requirement varies it; tests invoke the tick function directly (10-testing.md).

At 03:30 UTC **both** crawler processes' coordinators attempt `lock.TryRun(JobDailyTick, ...)` (§10); one wins and runs all steps in order under the single lock held for the whole sequence; the loser skips the whole tick (logged, counted — that is the healthy steady state). **Decision:** there is no catch-up for a missed tick (both processes down across 03:30): the next tick is the next day's; the miss is alerted by the tick's healthchecks.io check (24h period, 2h grace — 09-ops.md) and Grafana staleness alerts. Simplest rule; a manual `v6ctl stats recalc` / `v6ctl campaign sync` covers break-glass.

Steps (canonical order, one advisory lock for the whole sequence):

1. **Lifecycle sweep** — §8, exactly as specified.
2. **Stats snapshot** — snapshot product stats from confirmed state into the four `stats_*` tables (DDL: 05-schema.md — stats tables). The four snapshot upsert bodies live in `db/query/stats.sql` (query contents owned by 06-ingest.md — Daily stats snapshot and counter recompute; read endpoints 07-api.md). The tick-level contract pinned here: all four inserts are `INSERT ... ON CONFLICT (<pk>) DO UPDATE SET <every counter> = excluded.<col>` (PKs: `day` / `(day, country_id)` / `(day, campaign_id)` / `(asn_id, day)`). **DO UPDATE, not DO NOTHING** — this is also what makes `v6ctl stats recalc` a safe same-day re-run.
3. **Country/ASN counter recompute** — recompute the counter columns on `country` and `asn` (ported `update_country_metrics`/`update_asn_metrics`, corrected: v6 definition = classification-based; v4 count = actual v4-only count). Scope predicate, verbatim: `rank IS NOT NULL AND NOT disabled` (the publicly-ranked predicate), so `/country` and `/metric/asn` figures match the public lists exactly. Statement bodies live in `db/query/country.sql` / `db/query/asn.sql` (query contents owned by 06-ingest.md — Daily stats snapshot and counter recompute); inherently idempotent (set-based UPDATE).
4. **Service-domain candidate detection** — candidates only, never auto-disable. Inserts into `service_candidate` (DDL: 05-schema.md) with `ON CONFLICT (domain_id) DO NOTHING` (dismissed rows are never re-flagged; reasons are not merged into existing rows — per the idempotent-write guard). Two heuristics run in the tick:

   ```sql
   -- (a) classic CDN/infra apex: apex serves nothing, www absent, zone delegated.
   INSERT INTO service_candidate (domain_id, reasons)
   SELECT d.id, ARRAY['apex_www_no_record']
   FROM domain d
   WHERE d.rank IS NOT NULL AND NOT d.disabled
     AND d.base_status = 'no_record'
     AND d.www_status  = 'not_applicable'
     AND d.ns_status IN ('supported', 'unsupported')
   ON CONFLICT (domain_id) DO NOTHING;

   -- (b) high dependency in-degree: the fonts.googleapis.com shape.
   INSERT INTO service_candidate (domain_id, reasons)
   SELECT d.id, ARRAY['high_dependency_indegree']
   FROM resource_host rh
   JOIN domain d ON d.host = rh.host
   WHERE rh.dependent_count >= $1          -- service_detect.indegree_threshold, default 100
     AND d.rank IS NOT NULL AND NOT d.disabled
   ON CONFLICT (domain_id) DO NOTHING;
   ```

   **Decision:** the design phrases heuristic (a) as "apex+www both `no_record` confirmed while NS exists", but `www` can never be `no_record` (the observation mapping forbids it — 02-observation-model.md); the intended shape is pinned as `base_status='no_record' AND www_status='not_applicable' AND ns_status IN ('supported','unsupported')` (a confirmed NS status of either polarity proves the walk found a live delegated zone), scoped to publicly-ranked rows. **Decision:** the in-degree threshold is config `service_detect.indegree_threshold` (int, default 100; registry: 09-ops.md) — the design calls it "a tuning item once phase-5 data exists". **Decision:** heuristic (c) (hostname patterns from the curated `service_domains.yml`) is NOT a tick step: that list is applied at `v6ctl disable --service-list` import time (v6ctl spec); the tick computes only (a) and (b). Heuristic (b) returns zero rows until phase 5 populates `resource_host` — correct and harmless. Review is CLI-only: `v6ctl service-candidates list|confirm|dismiss`; a weekly webhook digest reports open candidates (09-ops.md).

5. **Campaign sync** — `lock.Run(JobCampaignSync, wait=5*time.Minute, campaign.Sync(...))` — a **nested blocking** lock acquisition: the tick waits out a concurrently webhook-triggered sync rather than silently skipping the daily guarantee. Sync internals: 06-ingest.md.
6. **check_job purge** — `DELETE FROM check_job WHERE created_at < now() - $1;` with `$1` = `live_check.retention` (interval, default 720h = 30d; key owned by 07-api.md; registry: 09-ops.md). Changing the config key changes the purge window — no literal.
7. **Ops summary + heartbeat** — send the ops-webhook summary: domains scanned in the last 24h, confirmed transitions, error rate, queue depth (§15's `queue_depth` probe), plus **any failed steps by name**. Then ping the daily-tick healthchecks.io check (`ops.healthcheck_tick_url`) — **only if steps 1–3 all succeeded** (lifecycle and stats are the health-critical core); otherwise the missed ping is the alert.

**Failure containment:** a failing step logs the error (`level=error`) and **continues** to the next step; the tick never aborts mid-sequence on a single step error. Idempotent-write second guards (listed per step above, plus the Tranco provenance guard in 06-ingest.md) protect against operator re-runs and any future trigger overlap, even though the lock already serializes.

**Tranco import is NOT a tick step.** The 23:15 UTC Tranco import cycle runs in the same coordinator goroutine as an independent schedule, gated by `JobTrancoImport` — see §11 and 06-ingest.md (import steps, 2h retry loop, 48h staleness warning, sanity guard).

## 10. Singleton coordination — advisory locks

Both crawler processes are **identical**: no `--coordinator` flag, no per-instance config. Each process runs the same coordinator goroutine; every **singleton job** is gated by a Postgres **session-scoped advisory lock**, keyed per job. Whichever process acquires the lock runs the job; the other skips. The lock — not the trigger topology — is the mutual exclusion, and it also serializes v6ctl-triggered runs (webhook, timer, operator) against the coordinator.

Lock registry (pinned constants, two-int form; `classid` identifies the app). This is the **complete** registry — adding a lock key is a spec change to this file:

```go
// internal/lock/lock.go
const ClassID int32 = 60660 // whynoipv6 advisory-lock namespace, never change

const (
    JobDailyTick    int32 = 1 // §9 tick, all steps, one lock for the whole sequence
    JobTrancoImport int32 = 2 // Tranco import (coordinator cycle + `v6ctl tranco import`)
    JobCampaignSync int32 = 3 // campaign sync (tick step 5 + webhook/Semaphore + `v6ctl campaign sync`)
)
```

(Two-int form: `pg_try_advisory_lock(ClassID, job)` / `pg_advisory_lock(ClassID, job)` / `pg_advisory_unlock(ClassID, job)`. This is distinct from golang-migrate's single-bigint advisory lock — different key encoding, no collision.)

Acquisition contract (`internal/lock`, used by crawler and v6ctl):

```go
// ErrHeld is returned by TryRun when the lock is busy.
var ErrHeld = errors.New("singleton lock held elsewhere")

// TryRun acquires (ClassID, job) via pg_try_advisory_lock on a connection
// checked out from the pool for the job's WHOLE duration. If the lock is
// busy it returns ErrHeld without running fn. On return (or process crash /
// connection loss) the lock is released: pg_advisory_unlock on the success
// path, session teardown otherwise.
func TryRun(ctx context.Context, pool *pgxpool.Pool, job int32, fn func(ctx context.Context) error) error

// Run is the blocking variant: pg_advisory_lock with a wait deadline.
// Used by v6ctl (and the tick's nested campaign-sync step) so an explicitly
// requested run always executes once the concurrent one finishes; deadline
// exceeded -> error "another <job> is running" (v6ctl exits 1 with it).
func Run(ctx context.Context, pool *pgxpool.Pool, job int32, wait time.Duration, fn func(ctx context.Context) error) error
```

Rules (all normative):

- The connection holding the lock is dedicated to the lock for the job's duration; job steps may use other pool connections freely. Session lock ⇒ crash of the holding process drops the connection and frees the lock — **no lease/expiry machinery**.
- Crawler-scheduled invocations use `TryRun`; a skip is **not** an error: log `level=info msg="singleton skipped, held elsewhere" job=<name>` and count it in the metrics checkpoint (§15, `dim_counters.singleton_skipped`). Exactly one skip per scheduled fire is the healthy steady state with 2 processes.
- All v6ctl invocations (`tranco import`, `campaign sync`, `stats recalc`) use `Run` with a **5-minute wait, hardcoded** — no config key. The tick's nested campaign-sync step uses the same `Run(…, wait=5m, …)`.
- Trigger resolution (pinned): the 23:15 UTC Tranco import is fired by the **crawler coordinator goroutine** under `JobTrancoImport` — NOT a systemd timer; `v6ctl tranco import` is the manual verb calling the identical import function under the identical lock. Campaign sync fires from **both** the Semaphore webhook (`v6ctl campaign sync`) and tick step 5, serialized by `JobCampaignSync`, which covers the entire sync including the git checkout operations. The daily tick is attempted by both coordinators; one wins.
- SKIP LOCKED consumers are **not** singletons and need no gating: the frontier claim loop (§3) and the `check_job` claim loop (07-api.md) run in every process by design.

## 11. Self-preflight (wiring)

Before **every** frontier claim cycle the process verifies its own IPv6 connectivity — a v6-dark crawler mass-producing false `unsupported` is the #1 false-negative source (v6audit only had this check in its remote agent; the internal-worker gap is deliberately closed here).

The `Preflight` **type is owned by 01-engine.md** (`internal/checker`, lifted from v6audit's `checkIPv6Connectivity`); this file owns only its wiring into the claim loop. The consumed surface (see 01-engine.md — self-preflight):

```go
// Owned by 01-engine.md, package checker. Restated here for the wiring contract only:
const PreflightFreshness = 5 * time.Minute   // 01's constant; every conn=unsupported
                                             // observation requires PassedWithin(PreflightFreshness),
                                             // else it is downgraded to error
                                             // (02-observation-model.md — conn table rows 5a/5b)
func NewPreflight(res *checker.Resolver, probeHost string, logger *slog.Logger) *checker.Preflight
func (p *checker.Preflight) Run(ctx context.Context) bool   // one probe; true on success, false on any failure
func (p *checker.Preflight) PassedWithin(d time.Duration) bool
```

`cmd/crawler/main.go` constructs one shared `*checker.Preflight` (probe host from config `preflight.probe_host` — key owned by 01-engine.md) and passes it to the claim loop and, by reference, to the live-check consumer and resource sweep.

Claim-loop behavior when `Run` returns `false` (probe failed): the process **claims nothing**, sends an ops-webhook alert, pings the `/fail` endpoint of its healthchecks.io check (§15), logs `level=warn` (the probe-failure detail is logged at Error by `Run` itself, 01), and retries after `preflight.retry_interval`. Config (registry: 09-ops.md):

```yaml
preflight:
  retry_interval: 60s   # sleep between failed preflights (this file owns this key)
```

**Decision:** the retry interval is pinned as config key `preflight.retry_interval` (default 60s, the design's prose value), sharing 01's top-level `preflight.` namespace with `preflight.probe_host`. The freshness window stays 01's constant — it is part of the observation-correctness contract, not tuning.

Run the probe unconditionally at the top of every claim cycle — it is cheap (one AAAA + one dial), so there is no benefit to skipping it while the last pass is still fresh. The live-check consumer and resource sweep do **not** run their own probe; they consult `PassedWithin` through the same shared `*checker.Preflight` instance (the claim loop keeps it fresh; if the frontier is idle, the empty-poll cycle still runs the probe every `claim.empty_poll_interval`).

## 12. Worker pool & claim loop

Process model: **2 crawler processes × `worker_slots` (default 64) concurrent domain slots each = 128 provisioned** (WORKER_SLOTS, canonical table: 00-overview.md; ~72 slots are busy on average, the rest is tail-latency headroom). **Decision:** the slot count is config key `worker_slots` (int, default 64; registry: 09-ops.md) — the design states the topology but names no key.

The claim loop (one goroutine per process; slots are a fixed pool of worker goroutines fed by a channel):

```
loop:
  1. if !preflight.Run(ctx):                    # §11; false = probe failed
       alert + heartbeat-fail + sleep preflight.retry_interval; goto 1
  2. rows := ClaimBatch(claim.batch_size)   # §3; one UPDATE, lease token L = rows[i].claimed_at
  3. if len(rows) == 0:
       maybeWriteIdleCheckpoint()               # §15 idle rule
       sleep claim.empty_poll_interval  # default 10s
       goto 1
  4. for each row: dispatch to the slot pool    # blocks until a slot is free
  5. goto 1                                     # claim again as soon as the batch is dispatched
```

Each slot, per domain:

```
  a. res := Runner.Run(ctx, domain)             # full engine, 01-engine.md (panic-recovered)
  b. obs := MapObservations(res)                # engine→dimension mapping, 02-observation-model.md/03-state-machine.md
  c. sched := Schedule(snapshot, obs, dead, consensus.FastLaneSuppressed())   # §5
  d. Commit(tx, snapshot, obs, sched)           # 03-state-machine.md: one pgx.Tx, one pgx.Batch,
                                                #   fenced UPDATE + changelog + scan + scan_detail
  e. metrics.Record(domain, outcome, duration)  # §15
  f. heartbeat.OK()                             # throttled, §15
```

Load-bearing properties:

- `claim.batch_size` (200) is deliberately larger than the slot count (64): claims amortize to ~3 claim queries per 200 domains. Queue-wait inside the process is covered by the lease: a full batch drains in ≈ 200 × 6s ÷ 64 ≈ 19s ≪ `LeaseReclaim` (30 min).
- Per-domain commits are strictly per-domain atomic units — one `pgx.Tx` per domain with all statements queued as a single `pgx.Batch` (one round trip). Batching is a round-trip optimization only; scan rows are never split into a separate bulk write and `CopyFrom` is never used (it cannot preserve per-domain atomicity).
- A lease-fence abort (fenced UPDATE matched 0 rows) rolls back the whole domain transaction, writes nothing, and increments the `lease_lost` counter (§15) + a `level=warn` log line. It is not retried — the reclaiming worker owns the domain now.
- Worker slots absorb consensus rate-limiter waits (the per-provider token bucket blocks inside the engine — 02-observation-model.md); no additional pacing exists in the claim loop.

## 13. Crawler process topology (`cmd/crawler`)

One binary, both processes identical. Deployment: 2 processes, one per host (`whynoipv6-crawler.service`, one unit per host — units and Ansible layout in 09-ops.md); a single host meets the throughput math — the second process is resilience and deploy hygiene, not capacity, and needs no coordination beyond the shared frontier (SKIP LOCKED) and the §10 locks.

Goroutine inventory per process (started by `cmd/crawler/main.go`, all tied to one root context):

| # | Goroutine | Count | What it does | Spec |
|---|---|---|---|---|
| 1 | Frontier claim loop | 1 | §12 loop: preflight → claim → dispatch | this file |
| 2 | Worker slots | `worker_slots` (64) | engine run + commit per domain | this file, 01, 03 |
| 3 | Live-check consumer | `live_check.workers` (4) | claims `check_job` rows (SKIP LOCKED), runs the engine, writes `check_job.result` only | 07-api.md |
| 4 | Live-check reaper | 1 (60s ticker) | fails jobs older than `live_check.fail_after` | 07-api.md |
| 5 | Coordinator | 1 | fires the 03:30 daily tick (`TryRun(JobDailyTick)`) and the 23:15 Tranco import cycle incl. its 2h retry + staleness warning (`TryRun(JobTrancoImport)`) | §9, §10, 06-ingest.md |
| 6 | Resource sweep worker | 1 | daily `resource_host` sweep; started only when `crawler.resources.enabled=true` | 06-ingest.md — Sweep worker |
| 7 | Metrics checkpointer | 1 | §15: per-1000-domains + 5-min-idle `crawler_metrics` rows, heartbeat throttle | this file |
| 8 | GeoIP mmdb reloader | 1 (hourly ticker) | stat + atomic reader swap of the two mmdb files | 06-ingest.md — mmdb hot reload |

Startup order (fail fast — any failure exits non-zero before any goroutine starts): load config (viper env; startup log line with secrets redacted) → open pgx pool → resolve sentinel asn/country ids by lookup → open GeoIP readers (missing/unreadable mmdb = fatal) → construct engine + consensus + preflight → generate `run_id` (UUIDv4) → start goroutines 1–8.

Both processes run goroutines 3/4 (SKIP LOCKED consumer, no singleton gating — 2×4 live-check workers total) and 5 (singleton jobs deduplicate via §10 locks) and 6 (the sweep's claim query is also SKIP LOCKED-safe; see 06-ingest.md).

## 14. Graceful shutdown

On SIGTERM or SIGINT (systemd's stop signal; default 90s stop timeout — unit in 09-ops.md):

1. Cancel the **claiming** contexts immediately: frontier claim loop (no new `ClaimBatch`), live-check consumer (no new `check_job` claims), resource sweep, coordinator (no new singleton jobs; a singleton job already running — tick, import, sync — has its context cancelled too: every step is either transactional or idempotent, the advisory lock releases on pool close, and the next scheduled fire or manual v6ctl run completes the work).
2. **Drain in-flight domain scans:** worker slots finish their current domain — engine run and commit — under a drain deadline. **Decision:** the drain deadline is 80 seconds from the signal (constant `drainBudget = 80 * time.Second`), inside systemd's 90s window with margin; the per-domain engine budget (01-engine.md) bounds each in-flight scan well under it. At the deadline the root context is cancelled: unfinished scans abort without committing, and their claims simply expire back to the frontier via the 30-minute lease — nothing is lost, nothing double-commits (lease fence).
3. Write the final `crawler_metrics` checkpoint row (`is_final = true`) with the tallies accumulated since the previous checkpoint.
4. No heartbeat `/fail` ping on clean shutdown (a deploy restart must not page); the success-ping simply stops and the healthchecks.io grace window (09-ops.md) absorbs the restart.
5. Close the pgx pool (releases any advisory locks and leases nothing — leases are timestamps, not sessions), flush slog, exit 0.

The API binary's shutdown (HTTP server drain) is specified in 07-api.md; v6ctl commands are synchronous and need only context cancellation.

## 15. Operational metrics — `crawler_metrics` — and heartbeats

Grafana reads Postgres directly; there is **no Prometheus and no /metrics endpoint on any binary**. DDL for `crawler_metrics` lives in 05-schema.md (hypertable, 90d retention). This section defines the writer semantics.

### 15.1 Checkpoint rows

Each crawler process writes one `crawler_metrics` row:

- after every **1000 processed domains** (constant `checkpointEvery = 1000`), and
- whenever **no checkpoint has been written for 5 minutes** (constant `idleCheckpointAfter = 5 * time.Minute`) — an idle checkpoint with `processed = 0` and current `queue_depth`/`active_slots`. This keeps the Grafana staleness alert ("no crawler_metrics row in 15 min" → critical) valid when the frontier is drained.

**Decision:** all counters in a row are **per-interval deltas** (since the previous checkpoint row of this process), not cumulative — Grafana alerting sums rows over windows (`sum(failed)/sum(processed)`), which requires deltas. `qps = processed / interval_seconds` for the row's interval.

Column semantics (names/types per 05-schema.md):

| Column | Value |
|---|---|
| `ts` | row write time |
| `run_id` | UUIDv4 generated once at process start; also stamped on the per-run child logger (`run_id` attr) |
| `worker` | process identity string; **Decision:** `"<os.Hostname()>:<pid>"` — identical to the slog `worker` attribute |
| `processed` | domains whose engine run completed and whose commit was attempted, this interval |
| `succeeded` | commits that COMMITted (including record-only/non-counting scans — those are successes) |
| `failed` | engine panic/error preventing a commit attempt, DB error aborting the commit unit, or lease-fence rollback |
| `qps` | `processed / interval_seconds` |
| `p50_ms`, `p99_ms` | per-domain wall duration percentiles, this interval (histogram below) |
| `active_slots` | busy worker slots at write time |
| `queue_depth` | due-set size at write time (probe below) |
| `dim_counters` | JSONB, schema below |
| `is_final` | `true` only on the shutdown row (§14 step 3) |

**Decision:** `succeeded + failed = processed` exactly; a lease-fence rollback counts in `failed` *and* in `dim_counters.lease_lost` (the design left the counter's placement open — "dim_counters or a dedicated column"; pinned: `dim_counters` key, no schema change).

Latency percentiles come from a **fixed-bucket log-scale histogram, reset at every checkpoint** — never unbounded in-memory latency slices (the failure mode of the previous rebuild). **Decision:** buckets are powers of two from 1 ms to 2^19 ms (~524 s) plus an overflow bucket (21 counters); p50/p99 are estimated by linear interpolation within the bucket. This replaces the design's "streaming histogram/t-digest" with the simplest structure that satisfies it.

`queue_depth` probe (sampled at most once per checkpoint; O(due-set) via `idx_domain_due`, same predicate as the claim query):

```sql
SELECT count(*) FROM domain
WHERE (NOT disabled OR disabled_reason IN ('dead', 'delisted'))
  AND next_check_at <= now()
  AND (claimed_at IS NULL OR claimed_at < now() - interval '30 minutes');
```

### 15.2 `dim_counters` JSONB schema

**Decision** (pinned key set; keys with zero counts are omitted to keep rows small):

```json
{
  "base":      {"supported": 0, "unsupported": 0, "no_record": 0, "not_applicable": 0, "error": 0, "inconsistent": 0},
  "www":       { "...same keys..." },
  "ns":        { "...same keys..." },
  "mx":        { "...same keys..." },
  "conn":      { "...same keys..." },
  "resources": { "...same keys..." },
  "lease_lost": 0,
  "unresolvable": 0,
  "dead_triggered": 0,
  "recovered": 0,
  "singleton_skipped": {"daily_tick": 0, "tranco_import": 0, "campaign_sync": 0}
}
```

Per-dimension objects tally this interval's **observations** (post-mapping, pre-confirmation). `unresolvable` counts §6.1 scans; `dead_triggered` counts §6.2 firings; `recovered` counts §6.3 step-R executions; `singleton_skipped` counts `TryRun` `ErrHeld` skips by job name (this realizes the design's `crawler_singleton_skipped_total{job}` counter — **Decision:** as a `dim_counters` key, since there is no Prometheus).

### 15.3 Liveness heartbeats

One healthchecks.io check **per crawler process** plus one for the daily tick (URLs via `ops.healthcheck_url` / `ops.healthcheck_tick_url`; empty string disables — keys and check configuration (Period 15 min / Grace 30 min; tick 24h/2h) in 09-ops.md):

- After every successful domain commit the worker calls `heartbeat.OK()`; the throttler sends at most one success ping per `ops.healthcheck_min_interval` (default 60s).
- On preflight failure (§11) the claim loop pings the same check's `/fail` endpoint (in addition to the ops-webhook alert).
- The daily tick pings its own separate check at step 7 only (§9).

A dead or hung crawler process is therefore signaled within ≤45 min instead of ~23h.

## 16. Config keys introduced by this file

All keys follow the viper uppercase-env convention; the consolidated registry with env-var names is **09-ops.md** — this table introduces the keys this file owns (type, default, meaning). Durations are Go `time.Duration` strings.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `claim.batch_size` | int | `200` | `$1` of the claim query (§3) |
| `claim.empty_poll_interval` | duration | `10s` | sleep after a zero-row claim (§3) |
| `claim.order` | string enum `rank\|age` | `rank` | claim ORDER BY policy; `age` = aging pressure valve (§3, Decision) |
| `worker_slots` | int | `64` | concurrent domain slots per process (§12, Decision) |
| `cadence.default` | duration | `24h` | base cadence (§4) |
| `cadence.bands` | list of `{min_rank, max_rank, every}` | `[]` | per-rank-band cadence (§4) |
| `recheck_inconsistent` | duration | `2h` | pull-in lane for `inconsistent` base/www (§5) |
| `recheck_error` | duration | `6h` | pull-in lane for `error` base/www (§5) |
| `recheck_backoff_max` | duration | `720h` | backoff cap (§5) |
| `preflight.retry_interval` | duration | `60s` | sleep after a failed preflight before re-probing (§11, Decision) |
| `service_detect.indegree_threshold` | int | `100` | heuristic (b) threshold (§9 step 4, Decision) |
| `lifecycle.dead_streak` | int | `7` | consecutive unresolvable scans before `'dead'` (§6) |
| `lifecycle.slow_lane_every` | duration | `720h` | revalidation cadence for disabled dead/delisted rows (§§5–8) |
| `lifecycle.delist_grace` | duration | `720h` | `orphaned_at` age before rank-NULL rows are delisted (§8) |
| `lifecycle.live_check_linkage` | duration | `168h` | frontier lifetime granted by a POST /check (§8) |

Keys referenced but owned elsewhere: `anti_flap.min_confirm_spacing` (03-state-machine.md), `consensus.*` incl. breakers (02-observation-model.md), `crawler.resources.enabled` (06-ingest.md), `preflight.probe_host` (01-engine.md), `live_check.*` (07-api.md), `tranco.*` and `campaign.*` (06-ingest.md), `ops.webhook_url` / `ops.healthcheck_url` / `ops.healthcheck_tick_url` / `ops.healthcheck_min_interval` (09-ops.md), `LOG_LEVEL`, `DATABASE_URL`, `GEOIP_PATH` (09-ops.md).

Compile-time constants owned by this file (never config): `LeaseReclaim = 30m` (§2), tick time `03:30 UTC` (§9), v6ctl lock wait `5m` (§10), `checkpointEvery = 1000`, `idleCheckpointAfter = 5m`, `drainBudget = 80s`, histogram buckets (§15).

## 17. Acceptance criteria

Fixture tables and harness details live in 10-testing.md; an implementation of this file is done when:

1. **Claim-plan gate:** `EXPLAIN (FORMAT JSON)` of the claim query's inner SELECT on a seeded 1M-row table shows an Index Scan on `idx_domain_due` with `next_check_at <= now()` as the index condition and a top-N sort on `(rank NULLS LAST, next_check_at)`; the gate re-runs whenever an index is added to `domain`.
2. **Claim atomicity:** two concurrent processes claiming from an overlapping due set never return the same row twice within a lease window; a row whose `claimed_at` is >30 min old is re-claimed.
3. **Scheduling:** table-driven tests reproduce §5.2's two backoff progressions exactly, the inconsistent-beats-error lane choice, the breaker-open behavior (cadence lane, `error_streak` still increments), the slow-lane override for disabled rows, and cadence band matching incl. NULL rank.
4. **Dead lifecycle:** an NXDOMAIN-scripted domain dies on the 7th daily scan; an all-SERVFAIL domain dies on the 7th backoff-spaced scan; three timeouts never increment `dead_streak`; a NOERROR-empty apex never increments it; recovery runs step R exactly once and produces no changelog rows.
5. **Sweep:** each of S1–S5 verified in isolation and as a sequence: grace stamping is monotonic, live-check rows skip the grace, disabled campaigns don't pin members, S2 re-enables a delisted member when its campaign is re-enabled, and a second same-day run changes zero rows.
6. **Re-entry matrix:** every cell of §7's matrix has a test at its owning ingress (06/07 tests reference this section).
7. **Singleton:** with two pools, exactly one of two simultaneous `TryRun(JobDailyTick)` calls runs `fn`; the other returns `ErrHeld`; killing the winner's connection mid-job frees the lock; `Run` waits and then executes.
8. **Shutdown:** SIGTERM during a full batch commits all in-flight domains ≤80s, writes an `is_final` row, and leaves no row with a fresh lease; the expired leases are reclaimed by a restarted process.
9. **Metrics:** checkpoint rows appear every 1000 domains and within 5 min of idleness; `succeeded + failed = processed` on every row; a forced lease-fence abort shows up in `failed` and `dim_counters.lease_lost`.
