# 06 — Ingest & Contribution Pipeline

_Status: Round 3.0 — API redesign folded in (decisions 2026-07-09): clean root API, keyset pagination, RFC 9457, no legacy compat, no history import._

_Frozen 2026-08 — historical design record. The shipped code is the implementation; where it differs, the code and [`docs/adr/`](../adr/) win._

**Purpose:** This file specifies every path by which hostnames and their metadata enter the system: the single host-canonicalization rule, the daily Tranco top-1M import cycle, the campaign-repo sync pipeline (PR validation, UUID trust, idempotent import, bot write-back), the resource-host registry (discovery, sweep, link maintenance), and GeoIP/ASN attribution. It additionally owns the query bodies for the daily tick's product-stats snapshot and the country/ASN counter recompute (§10) — the aggregation logic behind every `/stats` endpoint. It is the authority for all ingest-side SQL (SELECT/INSERT/UPDATE/DELETE only — DDL lives in 05-schema.md) and for the v6ctl verbs `tranco import|status`, `campaign sync|validate`, `resource add|remove`, `shame add|remove|list`, `provider add|remove|list` (the `dns_provider` mapping seed), and `stats recalc`.

**Deliverables:**
- `internal/domain/host.go` — `Canonicalize(host)` (the one canonicalization function)
- `internal/ingest/` — Tranco fetcher, parser, staging upserter, sanity guard, retry cycle
- `internal/campaign/` — YAML parse + validation + idempotent `Sync`, PR-validation logic
- `internal/geoip/` — IPinfo Lite mmdb reader, attribution algorithm, hot reload
- `internal/crawler/resourcesweep.go` — resource-host sweep worker (runs inside `cmd/crawler`)
- resource discovery/prune/counter statements executed inside the per-domain commit transaction (transaction machinery owned by 03-state-machine.md)
- `db/query/stats.sql`, `db/query/country.sql`, `db/query/asn.sql` — the four `stats_*` snapshot upserts and the ported `update_country_metrics`/`update_asn_metrics` counter recomputes (§10), called by the daily tick steps 2–3 (04-lifecycle-scheduling.md — The daily tick) and by `v6ctl stats recalc`
- `cmd/v6ctl` verbs: `tranco import`, `tranco status`, `campaign sync`, `campaign validate`, `resource add`, `resource remove`, `shame add|remove|list`, `provider add|remove|list`, `stats recalc`
- `.github/workflows/validate.yml` in the campaign repository (PR validation Action)

**Companion files:** 05-schema.md (all DDL: `domain`, `campaign`, `campaign_domain`, `resource_host`, `domain_resource`, `tranco_import`, `asn`, `country`, `top_shame`, the Tranco staging table), 02-observation-model.md (bulk-resolver seam and bogon filter used by the resource sweep, the attribution answer-set ordering, and the resources roll-up algorithm that turns confirmed host statuses into the `resources` observation), 03-state-machine.md (the per-domain commit transaction the discovery statements run inside; it consumes the roll-up's observation), 04-lifecycle-scheduling.md (daily tick step order, lifecycle sweep, advisory-lock package `internal/lock`), 07-api.md (the `/stats/*`, `/stats/overview`, `/asns`, `/countries` serializers that read the §10 snapshot/counter columns, and the `v6_ready`/`top_heroes`/`top_nameserver`/`count_v4` read-side formulas), 09-ops.md (config-key registry, systemd timers, ops webhook), 00-overview.md (canonical sizing constants), 10-testing.md (all fixtures and test vectors named here).

Hard constraints restated where relevant: this system is public/anonymous (no accounts/auth); Tranco is the ONLY ranked list source, top-1M eTLD+1 (pay-level domains); newest stack — current Go toolchain, PG18 + TimescaleDB, pgx/v5, slog, chi v5, cobra, viper, sqlc.

---

## 1. Canonicalize(host) — the single canonicalization rule

**Invariant.** `domain.host`, `resource_host.host`, and every hostname compared against them exist in exactly one form: lowercase punycode (ASCII/A-label) FQDN, no trailing dot, ≤253 octets, ≥2 labels. This form is both the storage form and the API serving form; Unicode (U-label) display conversion is a frontend concern, out of scope.

**Function.** One implementation in the backend module, importable by `api`, `crawler`, and `v6ctl`:

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

Unit-test vectors (fixture table in 10-testing.md — Canonicalize vectors; these must pass): `DNB.no.`→`dnb.no`; `møre.no`→`xn--mre-0na.no` (corrected 2026-07-10: the design listed `xn--mre-qla.no`, which decodes to `märe`); `XN--MRE-QLA.no`→`xn--mre-qla.no`; reject: `_wildcard_.ph`, `a..b`, `1.2.3.4`, `[::1]`, `localhost` (1 label), 254-octet input, `http://x.no`.

**Mandated call sites and failure policy** (every ingress in the system; no other normalization code may exist):

| Ingress | When | On Canonicalize failure |
|---|---|---|
| Tranco import (§2 step 5 below) | per CSV line | count in `tranco_import.rejected_count`, log at debug, continue |
| Campaign PR validation (§4 below) | per YAML domain entry | CI check fails with the offending file, line, and reason |
| Campaign sync (§3 step 3 below) | per YAML domain entry, **before** entity lookup/creation and membership diff | entry skipped, counted under `rejected + reasons` in the sync report |
| Curated subdomain lists (§3.7 below) | on the filename (must round-trip to itself), and on each `<label>.<apex>` join — the join is what validates the label | whole file rejected (never a partial list), reported under its repo-relative path; a rejection also suspends the removal diff |
| POST /check | body domain — Canonicalize() first, then the POST /check-only policy layer (reject RFC 2606 TLDs, `.internal`, `.local`); owned by 07-api.md | `400 invalid-parameter` (RFC 9457 `application/problem+json`, 07-api.md — §2.5) |
| Resource discovery (§5 below) | the host inserted into `resource_host` is defined as Canonicalize() output | host skipped (not inserted), no error surfaced |
| `v6ctl` verbs taking a hostname (`resource add`, `shame add`, `domain add`, `disable`) | on argument parse | command errors (exit 1) with the reason |
| API path params | per request | 404 — **exception:** `GET /badge/{domain}.svg` returns 400 (owned by 07-api.md) |

The POST /check reserved-TLD list is a policy layer **on top of** Canonicalize, applied only there. No CHECK constraint enforces the form in the DB (application-enforced, single write path per table).

---

## 2. Tranco import

### 2.1 Trigger ownership and retry cycle

The `crawler` coordinator goroutine is the **sole scheduled trigger** — **no systemd timer is deployed for Tranco import** (deliberate; the D.3 timer inventory in 09-ops.md must not contain one). `v6ctl tranco import` invokes the identical code path (`internal/ingest`) for manual/break-glass runs.

- **Cycle start:** at `tranco.import_at` (default **23:15 UTC** — after Tranco generates the daily list 22:00–23:00 UTC) the coordinator starts a new import cycle.
- **Cycle end:** when a new list is successfully imported OR the next 23:15 cycle starts (retry state resets); max ~11 attempts/day.
- **Reschedule** = re-attempt after `tranco.retry_interval` (default 2h) unless the next 23:15 cycle starts first. Every non-success attempt outcome reschedules: lock busy, network/HTTP error, unchanged list, aborted-list short-circuit, sanity-guard abort.

**Serialization.** Every import execution — scheduled or v6ctl — is serialized by the `JobTrancoImport` advisory lock (`internal/lock`, ClassID 60660, job 2 — package and contract owned by 04-lifecycle-scheduling.md), acquired **before any download/parse** and released after the upsert transaction commits or on any exit. Lock not acquired → another import is running: the coordinator logs at INFO (`msg="singleton skipped, held elsewhere" job=tranco_import`) and treats the attempt as done (reschedules); `v6ctl tranco import` uses the blocking `Run` variant with the 5-minute wait.

**Staleness warning.** On every attempt, run:

```sql
SELECT max(imported_at) FROM tranco_import WHERE aborted = false;
```

If `now() - max(imported_at)` exceeds `tranco.stale_warn_after` (default 48h), send an ops-webhook WARNING (`"no new Tranco list for <N>h; ranks frozen at list <list_id>"`), rate-limited to once per 24h. **Decision:** the rate-limit state (last-warned timestamp) is held in process memory; with two crawler processes the worst case is two warnings per 24h, which is accepted — no persistence, no new column. Warning, not page: the unchanged-list short-circuit means staleness freezes ranks; it never delists.

### 2.2 Import attempt algorithm

One execution, numbered. HTTP client: 60s total timeout per request, no retries inside an attempt (the cycle's 2h reschedule is the retry).

1. **Acquire `JobTrancoImport`** (see above). Failure to acquire ends the attempt.
2. **Fetch list ID:** `GET https://tranco-list.eu/top-1m-id` → plain-text list ID (whitespace-trimmed, verified format: non-empty, ≤16 chars, alphanumeric — anything else is treated as a network/HTTP error). Then:
   - equals `list_id` of the most recent `tranco_import` row with `aborted = false` → no new list yet; attempt done (reschedule).
   - equals `list_id` of **any** `tranco_import` row with `aborted = true` → do **not** auto-reimport an aborted list (it would abort again and spam the webhook); reschedule. Operator override: `v6ctl tranco import --force` (which also bypasses the step-9 sanity guard; it does **not** bypass the advisory lock).
   - network/HTTP error → reschedule.
3. **Download:** `GET https://tranco-list.eu/top-1m.csv.zip` with **conditional GET**: send `If-None-Match` with the ETag remembered from the last successful download (the endpoint serves a strong ETag + Last-Modified and honors 304 — verified live 2026-07-06). This is the standard list = pay-level domains (eTLD+1 is the default artifact; no variant selection). **Decision:** the ETag is held in process memory only (no DB column); a fresh process sends an unconditional GET, which merely re-downloads ~7 MB. A **304** response means the artifact has not propagated despite a new list ID → treat as a non-success attempt, reschedule. Network/HTTP error → reschedule.
4. **Unzip:** exactly one inner file, always named `top-1m.csv`. A zip without that inner file → treat as parse failure: fire ops webhook ERROR, reschedule.
5. **Parse:** `rank,domain` CSV with **CRLF line endings**, no header. Per line: split on the first comma; parse rank as a positive integer; pass the host field through `Canonicalize` (§1). The live list contains `_wildcard_.ph`-style entries and mixed-case junk (largely already punycode: 1,452 `xn--` entries, pure ASCII). Lines failing rank-parse or Canonicalize are counted in `rejected_count`, logged at debug, and skipped. Track `line_count` = raw CSV lines read. On each surviving line also compute the host's ICANN public suffix via `publicsuffix-go` (§3.4's in-memory PSL) and stage it as `tld` (§6.9) — a pure derivation from the already-parsed host, no lookup.
6. **Stage:** open one transaction. Create the session-scoped Tranco staging table `tranco_staging (rank int, host text, tld text)` — its definition (temporary, `ON COMMIT DROP`) lives in 05-schema.md — Tranco staging table — and bulk-load the parsed rows via `pgx` `CopyFrom`.
7. **Counters:** compute inside the transaction:
   - `valid_rows` = `SELECT count(*) FROM tranco_staging` (post-rejection, pre-dedup). **Decision:** `valid_rows` is the pre-dedup row count (the guard measures list health; canonicalization folds are counted separately).
   - `duplicate_count` = `SELECT count(*) - count(DISTINCT host) FROM tranco_staging`. `duplicate_count > 0` is **normal, not an error** (canonicalization can fold two raw lines into one host).
8. **Sanity-guard inputs:**

   ```sql
   SELECT count(*) FROM domain WHERE rank IS NOT NULL;              -- ranked_count
   SELECT count(*) FROM domain d
   WHERE d.rank IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM tranco_staging s WHERE s.host = d.host);  -- would_delist
   ```
9. **Sanity guard** (before any rank change): abort when `valid_rows < tranco.min_rows` OR (`ranked_count > 0` AND `would_delist * 100.0 / ranked_count > budget`). The `ranked_count > 0` guard makes the very first import (empty DB) pass the delist check trivially; the `min_rows` check still applies. `--force` bypasses both conditions.

   **The budget scales with staleness:** `budget = min(tranco.max_delist_pct * max(days_since_last_success, 1), 10.0)`, where `days_since_last_success` comes from the step-2.1 staleness query (`max(imported_at) WHERE aborted = false`) and is **1** when no successful import is recorded. `tranco.max_delist_pct` describes ONE list's normal churn (~0.5% observed), but `would_delist` is measured against a DB frozen at the last successful import, which diverges further every day an import is missed. An unscaled guard therefore makes the first abort permanent — the gap only grows, so no later list can pass it, ranks stay frozen and no new domain ever enters. The 10% ceiling means waiting cannot admit a list that is simply broken; `--force` is the operator route past it and `min_rows` still applies underneath. The abort note carries the effective budget and the staleness that produced it. **On abort:** ROLLBACK (yesterday's ranks untouched), then in a **fresh transaction** insert the provenance row with `aborted = true` and the reason in `note` (no conflict target — abort rows may repeat):

   ```sql
   INSERT INTO tranco_import
     (list_id, list_date, line_count, rejected_count, duplicate_count, aborted, note)
   VALUES ($1, $2, $3, $4, $5, true, $6);
   ```

   Fire the ops webhook (ERROR: `"tranco import aborted: <note>"`), reschedule.
10. **Upsert** (still inside the step-6 transaction). **Staging dedup first:** a naive `ON CONFLICT DO UPDATE` fed duplicate hosts aborts the entire transaction (SQLSTATE 21000, "ON CONFLICT DO UPDATE command cannot affect row a second time") — the source SELECT dedupes with `DISTINCT ON (host)`, **lowest rank wins**. Rows present in today's list get re-entry semantics; new rows get `next_check_at` spread across the next 24h (prevents a thundering herd) and insert-time attribution (§6.5: ccTLD-or-sentinel country, sentinel ASN — implemented here as a set-based join). `$1` = sentinel `asn.id`, `$2` = sentinel `country.id` (both resolved by lookup at run start, never hardcoded — §6.7):

    ```sql
    INSERT INTO domain (host, rank, next_check_at, created_by, asn_id, country_id, tld)
    SELECT DISTINCT ON (s.host)
           s.host,
           s.rank,
           now() + (random() * interval '24 hours'),
           'tranco',
           $1,
           COALESCE(c.id, $2),
           s.tld
    FROM tranco_staging s
    LEFT JOIN country c ON c.tld = '.' || upper(substring(s.host from '[^.]+$'))
    ORDER BY s.host, s.rank ASC              -- MIN(rank) wins the fold
    ON CONFLICT (host) DO UPDATE SET
      rank        = excluded.rank,
      orphaned_at = NULL,
      disabled    = CASE WHEN domain.disabled_reason = 'delisted' THEN false ELSE domain.disabled END,
      disabled_reason = CASE WHEN domain.disabled_reason = 'delisted' THEN NULL ELSE domain.disabled_reason END,
      disabled_at = CASE WHEN domain.disabled_reason = 'delisted' THEN NULL ELSE domain.disabled_at END,
      next_check_at = CASE WHEN domain.disabled_reason IN ('delisted','dead') THEN now() ELSE domain.next_check_at END,
      updated_at  = now()
    WHERE domain.rank IS DISTINCT FROM excluded.rank
       OR domain.orphaned_at IS NOT NULL
       OR domain.disabled_reason IN ('delisted','dead');
    ```

    **Decision — the conflict UPDATE is guarded.** Without the trailing `WHERE`, the upsert rewrites every one of the ~1M ranked rows daily (`rank` is indexed, so each is a non-HOT update) — dead tuples, WAL, and autovacuum pressure on the hottest table for rows whose rank did not move. The guard restricts the rewrite to rows with an actual effect: a changed rank, an orphan flag to clear, or a delisted/dead re-entry. Unchanged rows are untouched (their `updated_at` deliberately does not advance — it records the last *effective* change, not list membership).

    `imported_count` = the statement's affected-row count — with the guard above, that is new rows plus rows whose rank/lifecycle actually changed, not the full list size (`line_count` records the latter). Existing rows keep their `created_by`, confirmed statuses, classification, attribution, and `tld` (the conflict SET list deliberately touches none of them; `tld` is immutable — a pure function of the immutable host, §6.9). The `LEFT JOIN country` implements the §6.5 insert-time country rule set-based: the final DNS label of the host, upper-cased and dot-prefixed, probed against `country.tld` (seed form `.NO`); no match → sentinel. **Decision:** this SQL join is the normative implementation of insert-time attribution for the Tranco path (equivalent to the per-host Go rule in §6.5, because the final label of the ICANN public suffix always equals the host's final DNS label).

    **Re-entry semantics** (why each CASE arm exists):
    - `delisted` → re-enabled directly (confirmed state was never reset — it is merely ≤30d stale; the immediate rescan via `next_check_at = now()` refreshes it; no changelog implications beyond real transitions).
    - `dead` → stays disabled but `next_check_at = now()`: the next claim runs a real scan and recovery goes through the commit machine's recovery step (03-state-machine.md — dead recovery) only if the domain actually resolves — re-listing alone never resurrects a dead domain.
    - `service` / `manual` → rank updated, remains disabled, stays out of the frontier.
11. **Delist** (same transaction; unaffected by the staging dedup):

    ```sql
    UPDATE domain d
    SET rank = NULL, updated_at = now()
    WHERE d.rank IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM tranco_staging s WHERE s.host = d.host);
    ```

    Affected-row count → `delisted`. Delisting sets **only** `rank = NULL`: the daily lifecycle sweep (04-lifecycle-scheduling.md — lifecycle sweep) is the single owner of orphan detection (`orphaned_at`, 30-day grace, `disabled_reason='delisted'`); the import never sets `orphaned_at` and never disables.
12. **Provenance** (same transaction; the partial unique index `idx_tranco_import_list ON tranco_import (list_id) WHERE NOT aborted` — 05-schema.md — makes re-runs of an already-recorded list a no-op):

    ```sql
    INSERT INTO tranco_import
      (list_id, list_date, line_count, imported_count, delisted,
       rejected_count, duplicate_count, aborted, note)
    VALUES ($1, $2, $3, $4, $5, $6, $7, false, NULL)
    ON CONFLICT (list_id) WHERE NOT aborted DO NOTHING;
    ```

    **Decision:** `list_date` = the UTC date of the zip response's `Last-Modified` header; if the header is absent, the current UTC date. (The rate-limited metadata API is deliberately not used.)
13. **Commit**, remember the new ETag, release the lock, log an INFO summary (`list_id`, all five counters), cycle ends.

**Attribution note (site content, not code):** cite the Tranco NDSS'19 paper + list permalink (`https://tranco-list.eu/list/<list_id>`) on the FAQ/about page; note that upstream provider licenses include CC BY-NC (Cloudflare Radar) — fine for this non-commercial project.

### 2.3 Config keys (registry: 09-ops.md)

```yaml
tranco:
  min_rows: 950000          # int; abort import below this many valid rows
  max_delist_pct: 2.0       # float; delist allowance per day since the last successful
                            # import, capped at 10% (§2.2 step 9)
  import_at: "23:15"        # string, UTC HH:MM; daily cycle start
  retry_interval: 2h        # duration; re-attempt spacing within a cycle
  stale_warn_after: 48h     # duration; ops-webhook warning threshold
```

### 2.4 v6ctl verbs

- **`v6ctl tranco import [--force]`** — runs the identical attempt algorithm under `Run(JobTrancoImport, wait=5m)` (blocking; deadline exceeded → exit 1, `"another tranco import is running"`). `--force` bypasses (a) the aborted-list short-circuit in step 2 and (b) the step-9 sanity guard. Exit 0 on success or on a no-new-list outcome (printed); exit 1 on abort or error.
- **`v6ctl tranco status`** — **Decision** (verb named in the design's v6ctl list; output pinned here): prints the 10 most recent `tranco_import` rows (`list_id, list_date, imported_at, aborted, line_count, imported_count, delisted, rejected_count, duplicate_count, note`) newest first, plus a staleness line (`hours since last successful import` vs `tranco.stale_warn_after`). Read-only; exit 0 always (exit 1 only on DB error).

---

## 3. Campaign repo sync

### 3.1 One implementation, serialized

There is exactly ONE sync implementation: `internal/campaign.Sync(ctx, cfg, pool)`. `v6ctl campaign sync` (webhook path: on merge to main, GitHub Actions `repository_dispatch` → operator CI runs it on the backend host) and the crawler daily tick (04-lifecycle-scheduling.md — daily tick, step 5, after the lifecycle sweep) both call it. The webhook is latency sugar; the cron is the guarantee — **both** stay. No other code touches the campaign checkout or the campaign tables' YAML-derived columns.

Sync serializes across processes with the `JobCampaignSync` session-level advisory lock (`internal/lock`, ClassID 60660, job 3), acquired **before any git operation** — the lock protects the shared checkout at `campaign.repo_path` as well as the DB. The lock lives on a dedicated pooled connection held for the whole run; the import transaction runs on the pool as usual. **Both sync paths use the blocking `Run(JobCampaignSync, wait=5m, …)` variant** — an explicitly requested sync and the daily tick's nested step each wait out a concurrent run rather than dropping the daily guarantee; deadline exceeded → exit 1 with `"another campaign sync is running"`. A crash mid-sync closes the connection and releases the lock automatically.

### 3.2 YAML format (normative)

Exactly five top-level keys per file; unknown keys are a validation error:

| Key | Type | Required |
|---|---|---|
| `title` | non-empty string | yes |
| `description` | non-empty string | yes |
| `uuid` | UUID string | no — only the import bot ever writes it |
| `domains` | non-empty list of hostname strings | yes |
| `tags` | list of strings (mandate/campaign tags, OPEN-12) | no |

**Decision (tags — OPEN-12):** `tags` are **free-form**, not a fixed enum — a bounded vocabulary would need a maintained registry that rejects legitimate new mandates. The one tag with fixed meaning is the literal `mandate`: campaigns carrying it are what the `GET /mandates` surface selects (`'mandate' = ANY(tags)`; 07-api.md — §5.6); descriptive companion tags (`eu-2030`, `sector-banking`) are free-form. Each tag is normalized to lowercase and must match `^[a-z0-9][a-z0-9-]{0,31}$` (kebab-case, ≤32 chars); ≤16 tags per file; whitespace-only/empty tags are a validation error; duplicates (post-normalization) are folded. The parsed, normalized list is written verbatim to `campaign.tags` (05-schema.md — campaign) by sync (§3.3); the `?tag=` filter and `/mandates` (07-api.md) read it. Registrar tagging is **not** added (deferred, §6.10). **Launch-state note (grilling round, 2026-07-10):** none of the 28 current campaign YAMLs carry a `tags:` key, so the `/mandates` surface and `?tag=` filter ship working but **empty** until tags are added to the campaign repo.

**Decision:** the list key is **`domains`** — verified against all 29 files in the live campaign repo and production's Go struct (`yaml:"domains"`); the design doc's phrase "`list` of hostnames" described the type, not the key. The parser (`internal/campaign`) is tolerant of the format variance found in the repo: 0/2/4-space indents, comments, trailing spaces (plain `gopkg.in/yaml.v3` unmarshal into a typed struct with `KnownFields(true)` gives exactly this: YAML-standard tolerance plus unknown-key errors).

Files considered: every `*.yml` / `*.yaml` in the repository's **`campaigns/`** directory only (no nesting below it, and nothing at the repo root). A checkout without that directory is an error, not an empty campaign set: sync aborts before any DB work rather than treating every campaign as removed.

### 3.3 Sync algorithm (normative)

After acquiring the lock, run `git -C <campaign.repo_path> pull --ff-only <campaign.git_remote>` (skipped when `campaign.pull=false` — containerized deployments, where the distroless image has no git and the checkout is a volume cloned/refreshed by the `campaign-init` compose service). **Decision:** a git failure (network, non-fast-forward, corrupt checkout) aborts the sync before any DB work: ops-webhook ERROR + exit 1; the checkout is never auto-repaired.

1. **Parse** every `campaigns/*.yml`/`*.yaml` per §3.2. Every domain entry goes through `Canonicalize` (§1) **before** entity lookup/creation and membership diff; entries failing it are skipped and counted under `rejected + reasons`. Then hosts are **deduped within each file** (first occurrence wins). The importer must never fail or warn on a host present in multiple campaign files: that is N legitimate membership rows for one domain row (membership model, 05-schema.md — campaign tables). Files failing schema/hostname/size validation (§4.2 checks, minus the git-diff UUID check) are rejected and reported; a rejected file **never partially imports**.
2. **Duplicate-uuid guard:** if one uuid appears in >1 file, keep the file whose path equals the DB's `campaign.source_file` for that uuid and reject the others; if none matches, reject all of them. (This defeats a copied-uuid file coexisting with the original.)
3. **Files with uuid.** A file whose uuid does not exist in `campaign` is ADOPTED: insert `campaign` with the file's own uuid. **The repo owns campaign identity, not the DB.** uuids are minted in the campaign repo (`make fix-uuids` fills empty `uuid:` fields) and pinned by PR validation (§4.2), so a uuid the DB has not seen means a campaign it does not have yet — including the production-data migration, whose YAML files already carry production uuids (08-migration-cutover.md). The DB cannot mint one instead: step 6's write-back needs `campaign.push`, which is false wherever the sync runs off a mounted checkout (the production crawler included), so a rejection here strands the campaign permanently — no later run can resolve it.

   A uuid **edited in place** on an existing file therefore forks rather than rejects: step 5 disables the row holding the old uuid and this step creates one for the new uuid, both reported as `disabled` + `created`. §4.2's UUID-trust check is what stops that reaching the DB in the first place.

   For known uuids, upsert by uuid:
   - update `name` (from `title`), `description`, and `tags` (the normalized `tags` list per §3.2, or `NULL`/empty when the key is absent);
   - if `source_file` differs, update it and log `"campaign renamed: old.yml → new.yml"`;
   - if `disabled = true`, set `disabled = false, updated_at = now()` and log `"campaign re-enabled (file re-appeared)"` — campaign row, memberships, and domain state were all preserved on soft delete, so the campaign reappears fully populated with no re-import and no changelog noise.

   Then **diff memberships** (current = `SELECT domain_id FROM campaign_domain WHERE campaign_id = $1`; desired = the entity ids of the file's canonicalized host list after step 3a below):

   a. **Ensure entity** per host (§3.4): existing `domain` row by host, else create.
   b. **Additions:** `INSERT INTO campaign_domain (campaign_id, domain_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`. On membership addition to an **existing** domain row, apply the re-entry rule (same as Tranco step 10): `disabled_reason = 'delisted'` → re-enable (`disabled=false, disabled_reason=NULL, disabled_at=NULL`) + `next_check_at = now()`; `'dead'` → keep disabled, `next_check_at = now()`; `'service'`/`'manual'` → unchanged.
   c. **Removals:** `DELETE FROM campaign_domain WHERE campaign_id = $1 AND domain_id <> ALL($2::bigint[])` (`$2` = desired ids). Membership removal deletes the membership row **only** — the entity remains; the lifecycle sweep handles orphaning (§3.6).
4. **Files without uuid:** first run `SELECT uuid FROM campaign WHERE source_file = $file` — if a row exists AND its uuid appears in no repo file, REUSE that uuid (a previous write-back push failed; this makes write-back idempotent and prevents duplicate campaigns). Otherwise generate a fresh UUIDv4. Insert `campaign (uuid, name, description, source_file, tags)` (`tags` = the normalized §3.2 list, or NULL) + memberships (per step 3's ensure/diff) inside the import transaction.
5. **Deletion (uuid-set diff, not source_file diff):** after steps 3–4:

   ```sql
   UPDATE campaign SET disabled = true, updated_at = now()
   WHERE NOT disabled
     AND uuid <> ALL($1::uuid[]);   -- $1 = all uuids seen in the repo this run,
                                    --      including newly generated ones
   ```

   Log each disabled campaign. Membership rows are kept (soft delete, history preserved); orphaned rank-NULL domains are handled by the lifecycle sweep (§3.6). A restored YAML *without* a uuid is a new campaign per step 4 (new uuid written back); the old disabled row stays soft-deleted. Consequence, stated for the implementer: a uuid edited in place = old campaign disabled by this step + a new campaign adopted by step 3 — both loudly in the report, and the public URL moves with the uuid, so §4.2 blocks it at PR time.
6. **Write-back:** after the import transaction commits, write generated uuids into their files (preserving each file's other content byte-for-byte apart from the inserted `uuid: <value>` line after `description:`), make ONE bot commit — message exactly `chore: assign campaign uuids [skip ci]` — and push via the deploy key to `campaign.git_remote`. On non-fast-forward: `git pull --rebase` and retry the push once; on final failure: ops-webhook alert and continue — step 4's reuse rule recovers on the next run. If no uuids were generated, no commit is made.
7. **Report** to the ops webhook: created/updated/renamed/re-enabled/disabled campaigns, membership adds/removes, rejected files with reasons (schema, hostname, size, duplicate uuid), rejected host entries + reasons, write-back status (pushed / nothing to push / failed).

All DB work in steps 3–5 runs in **one import transaction**; parsing (step 1–2) happens before it opens, write-back (step 6) after it commits.

### 3.4 Ensure-entity: subdomain auto-link and parent auto-create (PSL at import only)

Campaign YAML is the **only** subdomain source (Tranco contributes only `kind='apex'` rows). Each YAML entry is stored **as given** (post-Canonicalize). PSL evaluation happens at import/validation time only — the crawler never consults the PSL.

**Decision (library):** `github.com/weppos/publicsuffix-go` with the private-domains section disabled (ICANN section only) is the single PSL implementation for kind detection, parent derivation, and the PR-validation eTLD+1 check. (The design mentions `golang.org/x/net/publicsuffix` only for country attribution, where §6.5 needs no PSL at all — the final-label rule replaces it — so one PSL library suffices.)

Per canonicalized host:

1. `registrable := publicsuffix eTLD+1 of host` (ICANN section). Error (host **is** a public suffix, or the TLD is unknown) → the entry is invalid: rejected in PR validation; skipped + counted in sync. The same PSL evaluation also yields the host's public suffix (eTLD); capture it as `domain.tld` (§6.9) on every entity insert in steps 2–3 (apex, auto-created parent, and subdomain rows all get it — identical to the parent's for subdomains).
2. `host == registrable` → ensure a `domain` row: `kind='apex'`, `parent_id=NULL`. If absent, insert with `created_by='campaign'`, `rank=NULL`, `next_check_at=now()`, insert-time attribution (§6.5).
3. `host != registrable` → `kind='subdomain'`:
   a. Ensure the **parent** row for `registrable` first: if absent, insert `kind='apex'`, `created_by='parent_link'`, `rank=NULL`, `parent_id=NULL`, `next_check_at=now()`, insert-time attribution. The auto-created parent is a first-class entity (crawled, classified independently).
   b. Ensure the subdomain row: if absent, insert `kind='subdomain'`, `parent_id=<parent.id>`, `created_by='campaign'`, `rank=NULL`, `next_check_at=now()`, insert-time attribution. If a row already exists with `kind='apex'` for this host (impossible via these ingresses — Tranco is eTLD+1 and this path would have classified it subdomain — but defensive): leave `kind`/`parent_id` unchanged and log a WARN.

Inserts use `INSERT … ON CONFLICT (host) DO NOTHING` followed by a re-read of `id` (two sync paths cannot race — the advisory lock — but the frontier and live-check paths insert domains too).

Classification is per-entity; children never change a parent's tier. **There is deliberately no campaign YAML syntax for resources** — campaigns express endpoint intent through the `domains:` list itself (listing `api.dnb.no` alongside `dnb.no`); cross-domain dependencies are found by auto-discovery (§5) or added by `v6ctl resource add` (§5.5).

### 3.5 Config keys (registry: 09-ops.md)

```yaml
campaign:
  repo_path: /srv/whynoipv6-campaign   # string; shared checkout, owned by the service user
  git_remote: origin                   # string; push target for the bot commit (deploy key)
  pull: true                           # bool; git pull before parsing — false in containers (no git in the distroless image)
  push: true                           # bool; commit+push the uuid write-back — false in containers
  max_domains_per_file: 5000           # int; PR-validation size cap (§4); >1,723 — the Dutch central-government register
  max_subdomains_per_domain: 20        # int; entry cap for one subdomains/<apex>.yml list (§3.7)
```

Ops (Ansible; definitions in 09-ops.md): the checkout and the GitHub deploy key (write access, campaign repo only) are provisioned for the single service user that runs both `crawler` and CI-invoked `v6ctl`.

### 3.6 Lifecycle-sweep linkage (restated for the sweep's owner)

In the lifecycle sweep's linkage predicate (04-lifecycle-scheduling.md — lifecycle sweep, step 1a), campaign membership counts only if the campaign is enabled:

```sql
linked := EXISTS (SELECT 1 FROM campaign_domain cd
                  JOIN campaign c ON c.id = cd.campaign_id AND NOT c.disabled
                  WHERE cd.domain_id = d.id)
          OR EXISTS child
          OR EXISTS (SELECT 1 FROM curated_subdomain cs WHERE cs.domain_id = d.id)
          OR last_requested_at >= now() - lifecycle.live_check_linkage
```

Without the `NOT c.disabled` join, a disabled campaign's kept membership rows (§3.3 step 5) would pin its rank-NULL domains in the frontier forever. With it, they enter the normal `orphaned_at` → 30-day grace → `delisted` path, and campaign re-enable (§3.3 step 3) restores linkage before the next sweep or via the delisted re-entry rule.

### 3.7 Curated subdomain lists (`subdomains/<apex>.yml`)

Apex + www passing does not mean the *service* works over IPv6: login portals, APIs and checkout hosts live on subdomains that can be v4-only while the homepage scores green. Curated lists let a contributor name those hosts so the crawler checks them like any other domain. Results are **informational** — they never feed classification or the aggregate stats, because coverage is uneven by construction (a domain would otherwise score worse merely because someone bothered to list its API host).

Format — one file per parent, in the same repo and the same sync:

```yaml
# subdomains/nrk.no.yml
subdomains:
  - tv
  - radio
  - secure.login
```

Entries are labels **relative to the apex**, which is what keeps a list from reaching outside its own parent (the requester's reference format allows cross-domain hosts; this one deliberately does not — that is auto-discovery's job, §5). Rules, enforced by the parser and by PR validation (§4):

- The filename is the parent, and must be the canonical registrable apex in lowercase punycode: two spellings normalizing to one apex would otherwise be two files for one parent, last sync winning.
- Labels are validated by joining them to the apex and running `Canonicalize` (§1) — the same IDN mapping, LDH and length rules as every other host. Multi-level labels (`secure.login`) are allowed.
- The bare label `www` is rejected: the apex's own `www` dimension already covers it.
- Duplicates after normalization, and lists over `campaign.max_subdomains_per_domain`, are rejected. One bad entry rejects the whole file — the lists are small, and partial application would silently unlist the entries that did parse.
- An empty list is valid and equivalent to deleting the file.

Sync (step 5b of §3.3, same transaction):

1. Exactly one file may name an apex. Two that do (`nrk.no.yml` beside `nrk.no.yaml`) are **both** rejected: picking a winner by filename order would let a newly added file quietly supersede an established one.
2. The apex must already be tracked and enabled. A list naming an unknown apex is skipped and reported — this ingress adds subdomains to the index, it is not a side door for new apexes (contrast §3.4, where campaign entries *do* auto-create `parent_link` parents). Such a file owns no rows, so skipping it changes nothing. A list whose apex is **disabled** is skipped too, but its hosts are read back into the membership set: skipping must leave a list alone, not start its hosts' delist grace as a side effect of an operator disabling the parent.
3. Each host is ensured as `kind='subdomain'` with `parent_id`, `rank NULL`, `created_by='curated'`. A host already known from another ingress keeps its origin (`created_by` is provenance, `curated_subdomain` is membership) but **does** get `parent_id` backfilled when it has none: a live check run before the apex was tracked leaves the row parentless, and both the subdomain list and `subdomain_count` key on `parent_id`. An established link is never rewritten.
4. Membership is refreshed, then one set-based delete removes everything no longer listed.

Removal is the whole lifecycle story: this ingress never disables a row. Dropping the membership row removes the linkage arm (§3.6), so the daily sweep stamps the host and delists it after the 30-day grace, and re-listing re-enables it through the sweep's symmetric re-entry. Because of that, **any** rejected file suspends the removal diff for that run — otherwise a typo in one merged file would start a 30-day grace for every host it lists. The suspension is reported (`CuratedFrozen`), because a silently frozen diff looks exactly like a clean run.

---

## 4. PR-validation GitHub Action (campaign repo)

### 4.1 Shape

A single workflow in the **campaign repository**, new and tiny. It runs only on `pull_request`, and evaluates **only the `.yml`/`.yaml` files changed by the PR** (git diff against the merge base with main) — never the whole repo, so pre-existing issues in untouched files cannot fail an unrelated PR. The Action has **no DB access**.

**Decision:** the Action's checks are implemented as `v6ctl campaign validate` (§4.3), built from the backend repo inside the workflow — this keeps `Canonicalize` and the YAML parser single-sourced in Go instead of reimplemented in shell.

### 4.2 Checks (per changed file, in order)

- **YAML schema (blocking):** tolerant parse per §3.2; exactly the five keys `title` (non-empty string, required), `description` (non-empty string, required), `uuid` (optional), `domains` (required, non-empty list), `tags` (optional list of strings). Unknown keys → error.
- **Tags (blocking):** if `tags` is present, each entry must normalize to `^[a-z0-9][a-z0-9-]{0,31}$` (lowercase kebab, ≤32 chars) with ≤16 tags per file (§3.2 Decision); an empty/whitespace or over-long/over-count tag fails, listing the file and offending tag.
- **UUID trust (blocking, diff vs main):** contributors never invent UUIDs. Compare each file's `uuid:` value between the PR head and the merge-base with main (rename detection disabled — `--no-renames` — so renames appear as delete+add):
  - **Added file:** `uuid` must be absent or empty — UNLESS its value equals the uuid of **exactly one** file deleted in the same PR (a git rename, possibly undetected as such). Then it passes, and the bot comment states loudly: `"rename detected: old.yml → new.yml (uuid preserved)"`.
  - **Modified file:** `uuid` must be byte-identical to the value in main (absent stays absent — only the bot commit ever adds one).
  - **Deleted file:** allowed (that is how a campaign is retired; sync disables it, §3.3 step 5).
  - Any other introduction or change of a `uuid:` value → fail with: `"uuid values are assigned by the import bot; remove the uuid field"`.
- **Hostname validation (blocking):** per entry, `Canonicalize` (§1) plus the eTLD+1 check (§3.4 step 1: the host must have an ICANN-section registrable domain; both apexes and subdomains of it pass — a host that *is* a public suffix, or under an unknown TLD, fails). Failure lists file, line, entry, reason.
- **Within-file duplicate (blocking):** two entries in the same file normalizing to the same host → error listing the host and both line numbers. Scope: the changed file only.
- **Size cap (blocking):** ≤ `campaign.max_domains_per_file` (default 5000; raised from 1000 on 2026-08-01 — the live Dutch central-government file holds 1,723 hosts) list entries per file.
- **Cross-file duplicate (informational only — NEVER blocking):** for each host added by the PR that already appears in another campaign file (**Decision:** compared against all other `campaigns/` files at the PR head), the bot comment notes ``"`host` is also in `<other campaign title>`"``. This is expected and legitimate: the membership model exists precisely so one domain belongs to several campaigns and is still checked once per day. Do NOT implement any code path that rejects, warns-as-failure, or auto-dedupes across files.
- **Bot comment:** parsed summary per changed file (`"32 domains, 3 subdomains → parents auto-linked"` — subdomain count from the §3.4 PSL classification), plus the cross-file informational lines and any rename notices. Exit status reflects blocking checks only.

Changed `subdomains/*.yml` files (§3.7) are validated in the same run, by the same verb, and share the failure format and comment style. Their blocking checks:

- **Filename (blocking):** must be the canonical registrable apex — `Canonicalize` round-trips it unchanged, and PSL says it is an eTLD+1, not a public suffix or a subdomain.
- **One file per domain (blocking):** two files naming the same apex (`nrk.no.yml` beside `nrk.no.yaml`) fail, compared against **all** subdomain files at the PR head — so adding a second file is caught even when the first is untouched by the PR. Note this is the opposite stance to campaign files' cross-file duplicates above, and for the opposite reason: a domain belongs in many campaigns, but has exactly one subdomain list.
- **Entries (blocking):** each relative label must survive joining to the apex and `Canonicalize`; a full host (`api.nrk.no`) fails, telling the contributor to write the label; bare `www` fails as redundant against the apex's own `www` dimension; duplicates after normalization fail; the list may not exceed `campaign.max_subdomains_per_domain`. One bad entry fails the whole file.
- **Bot comment:** per file, the entry count and the resolved hosts, plus an informational line that the apex must already be tracked (CI has no DB and cannot check it — the sync reports that skip instead).

### 4.3 `v6ctl campaign validate`

```
v6ctl campaign validate [--repo <path>] [--base <git-ref>] [--comment-file <path>]
```

- `--repo` (default `.`): campaign checkout to validate.
- `--base` given (CI mode): changed-file set = `git -C <repo> diff --name-status --no-renames <base>...HEAD` filtered to `campaigns/*.yml`/`*.yaml`; runs all §4.2 checks including the UUID diff rule (reading each file's `uuid:` at `<base>` via `git show`).
- `--base` omitted (local mode): validates **every** `campaigns/` YAML file, skipping the UUID diff rule (no git required); cross-file duplicates still reported informationally.
- `--comment-file` (default: stdout): writes the bot-comment Markdown.
- Exit 0 = all blocking checks passed; exit 1 = at least one blocking failure; failures are also printed as `file:line: message` lines on stderr for the Action log.
- The verb never touches the DB or the network (safe to run in CI); it shares `internal/campaign`'s parser and `internal/domain.Canonicalize` with sync.

### 4.4 Workflow file (campaign repo, `.github/workflows/validate.yml`)

```yaml
name: validate
on: pull_request
permissions:
  contents: read
  pull-requests: write
jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/checkout@v4
        with:
          repository: lasseh/whynoipv6
          path: .backend
      - uses: actions/setup-go@v5
        with:
          go-version: stable
      - name: build v6ctl
        run: cd .backend/backend && go build -o /tmp/v6ctl ./cmd/v6ctl
      - name: validate changed campaign and subdomain files
        run: |
          /tmp/v6ctl campaign validate \
            --repo . \
            --base "origin/${{ github.base_ref }}" \
            --comment-file /tmp/comment.md
      - name: post bot comment
        if: always()
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          gh pr comment "${{ github.event.pull_request.number }}" \
            --body-file /tmp/comment.md --edit-last --create-if-none
```

**Decision:** the backend monorepo is checked out as `lasseh/whynoipv6` with the Go module at `backend/` (design §6 monorepo layout); `--edit-last --create-if-none` keeps exactly one bot comment per PR. The comment step runs `if: always()` so failures still get the summary.

### 4.5 One-time repo cleanup (before merging the Action)

Commit `chore: remove within-file duplicate hosts` in the campaign repo, deleting the 6 existing within-file duplicate entries — `Dutch_Central_Goverment.yml`: `magazines.rijksoverheid.nl`, `magazines.werkenvoornederland.nl`, `parlement.nl`, `services.belastingdienst.nl`, `temis.nl` (second occurrence of each); `German_Federal_Government.yml`: `bundesarchiv.de` (second occurrence). Do NOT touch cross-file duplicates — the 99 hosts appearing in multiple files today are intentional memberships.

---

## 5. Resource-host registry

Tables: `resource_host` (globally deduped host registry with confirmed `aaaa_status`) and `domain_resource` (the dependency link) — DDL in 05-schema.md — resource tables. `fonts.googleapis.com` is checked once per day, not once per dependent site. Hosts never write changelog rows (changelog is domain-scoped). Resources affect **Saint only** — hero/partial/sinner and the shame bar never read them.

**Phasing:** config key `crawler.resources.enabled` (bool, default `false`; flipped to `true` at phase-5 deploy; registry: 09-ops.md). While `false`: the crawler skips `resource_discovery` entirely, writes `resources = 'not_applicable'` to every `scan` row, excludes the resources dimension from the commit loop (03-state-machine.md owns that behavior), **and — Decision — the sweep worker (§5.3) does not run** (the registry is empty; the goroutine is simply not started when the flag is false at boot; flag changes take effect on restart).

### 5.1 Discovery (per domain scan, inside the per-domain commit transaction)

The engine's adapted `resource_discovery` check (owned by 01-engine.md) returns `(hosts []string, status)` where status ∈ {`ok`, `not_applicable`, `error`} and hosts is the full deduped external-host list (≤50). Inside the commit transaction (03-state-machine.md):

1. status = `error` → keep existing links untouched (a failed fetch is not evidence dependencies changed); skip to the roll-up. status = `not_applicable` → skip to the roll-up.
2. status = `ok` → canonicalize each host via `Canonicalize` (§1; failure → host skipped, not inserted, no error surfaced), collect into `hosts[]`, then run statements A–C (parameters: `$hosts` = text[], `$domain_id`):

   **A — registry insert** (new rows get `aaaa_status NULL`, `next_check_at = now()` via column defaults, so the sweep confirms them within one day):

   ```sql
   INSERT INTO resource_host (host)
   SELECT unnest($hosts::text[])
   ON CONFLICT (host) DO NOTHING;
   ```

   **B — link insert + dependent_count increment** (only genuinely new links bump the counter):

   ```sql
   WITH rh AS (
     SELECT id FROM resource_host WHERE host = ANY($hosts::text[])
   ), new_links AS (
     INSERT INTO domain_resource (domain_id, resource_host_id, source, required)
     SELECT $domain_id, id, 'discovered', TRUE FROM rh
     ON CONFLICT (domain_id, resource_host_id) DO NOTHING
     RETURNING resource_host_id
   )
   UPDATE resource_host SET dependent_count = dependent_count + 1
   WHERE id IN (SELECT resource_host_id FROM new_links);
   ```

   Statement B runs in the same transaction after A, so it sees A's rows. The `ON CONFLICT DO NOTHING` never downgrades an existing `source='manual'` link (the conflict arm touches nothing).

   **C — last_seen refresh** (covers both new and pre-existing links; a fresh insert's default `last_seen = now()` makes the double-touch harmless):

   ```sql
   UPDATE domain_resource
   SET last_seen = now()
   WHERE domain_id = $domain_id
     AND resource_host_id IN (SELECT id FROM resource_host WHERE host = ANY($hosts::text[]));
   ```
3. **Prune** (still only on `ok`; the 30-day window is a literal, not config):

   ```sql
   WITH del AS (
     DELETE FROM domain_resource
     WHERE domain_id = $domain_id
       AND source = 'discovered'
       AND last_seen < now() - INTERVAL '30 days'
     RETURNING resource_host_id
   )
   UPDATE resource_host SET dependent_count = dependent_count - 1
   WHERE id IN (SELECT resource_host_id FROM del);
   ```

   Manual links (`source='manual'`) are never pruned. `dependent_count` is maintained **only** by statements B, prune, and the v6ctl verbs (§5.5) — no other writer; a host whose count returns to 0 simply stops being swept (§5.3 predicate) and lingers harmlessly.

The `resources` observation consumed by the commit machine is produced by the registry **roll-up** over linked hosts' confirmed `aaaa_status` (`required = TRUE` links only; advisory `required = FALSE` links are excluded) — the roll-up algorithm and its interaction with pending counters are owned by 02-observation-model.md — resources roll-up (observation production); the commit machine that consumes the resulting observation is 03-state-machine.md.

### 5.2 Sweep worker — claim

A dedicated goroutine in **every** crawler process (both processes run it; SKIP LOCKED makes that safe — no singleton gating, matching the frontier claim loops). Load is negligible: see 00-overview.md — sizing constants, `RESOURCE_SWEEP_QPS`.

**Decision (lease = the schedule bump):** `resource_host` deliberately has no `claimed_at` column; the claim itself moves `next_check_at` 2 hours out, which doubles as the crash lease — a process dying mid-batch leaves its hosts scheduled exactly as a non-definitive result would.

```sql
UPDATE resource_host
SET next_check_at = now() + interval '2 hours'
WHERE id IN (
  SELECT id FROM resource_host
  WHERE next_check_at <= now()
    AND dependent_count > 0
  ORDER BY next_check_at ASC
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
RETURNING id, host, aaaa_status, aaaa_pending, aaaa_pending_count;
```

**Decision (constants, not config):** `$1` = 100 (`sweepBatchSize`), empty-claim sleep = 60s (`sweepEmptyPoll`); both are Go constants in `internal/crawler/resourcesweep.go` — the volume is too small to warrant config keys. Hosts in a batch are processed sequentially by the one goroutine (2–4 qps needs no fan-out).

### 5.3 Sweep worker — AAAA lookup and mapping

Per host, ONE AAAA lookup via the **bulk resolver** (the two local Unbound instances; resolver seam owned by 02-observation-model.md — no consensus quorum on this path), mapped to a sweep outcome:

| Lookup result | Outcome |
|---|---|
| ≥1 globally-routable AAAA (same bogon filter as the consensus mapping: loopback/link-local/ULA answers rejected) | definitive `supported` |
| NOERROR, no AAAA after CNAME chase | definitive `unsupported` |
| NXDOMAIN | definitive `no_record` |
| timeout / SERVFAIL / network error | **non-definitive** |

The sweep never produces `not_applicable`.

### 5.4 Sweep worker — host confirmation machine and next_check_at

Commit per host, mirroring the domain commit machine with N=2 (all writes are single-row UPDATEs on `resource_host`; one implicit transaction per host):

```
if outcome is non-definitive:
    # touch nothing else; the claim already set next_check_at = now()+2h — leave it
    return

if aaaa_status IS NULL:                     # first-ever definitive value commits immediately
    aaaa_status = outcome
    aaaa_pending = NULL; aaaa_pending_count = 0
elif outcome == aaaa_status:                # agreement: clear any pending candidate
    aaaa_pending = NULL; aaaa_pending_count = 0
elif outcome == aaaa_pending:               # second consecutive sighting of the candidate
    aaaa_pending_count += 1
    if aaaa_pending_count >= 2:             # N=2: two consecutive sweeps to change aaaa_status
        aaaa_status = outcome
        aaaa_pending = NULL; aaaa_pending_count = 0
else:                                       # new candidate
    aaaa_pending = outcome; aaaa_pending_count = 1

last_checked_at = now()
next_check_at   = now() + interval '24 hours'
```

`last_checked_at` is set only on definitive outcomes (non-definitive "touches nothing"). Hosts never write changelog rows. The deliberate **double hysteresis** (host N=2 stacked under the domain dimension's N=3) gives a worst-case ~5 days for a Saint transition; this is intentional — the domain level also absorbs link-set churn (rotating ad/CDN hosts across fetches), which host-level confirmation cannot.

### 5.5 Manual link verbs (operator-only; there is no HTTP admin surface)

- **`v6ctl resource add <domain> <host> [--advisory]`** — canonicalize both arguments (§1; failure → exit 1). Resolve `<domain>` to a `domain` row: **Decision:** no row → exit 1 (`"unknown domain — add it to a campaign or wait for Tranco"`); a disabled row is accepted with a printed warning (the link is inert until re-enable). Ensure the `resource_host` row (statement A pattern with a single host). Upsert the link — an operator add on an already-discovered pair upgrades it to `manual`:

  ```sql
  WITH up AS (
    INSERT INTO domain_resource (domain_id, resource_host_id, source, required)
    VALUES ($1, $2, 'manual', $3)          -- $3 = NOT --advisory
    ON CONFLICT (domain_id, resource_host_id)
    DO UPDATE SET source = 'manual', required = EXCLUDED.required
    RETURNING (xmax = 0) AS inserted
  )
  UPDATE resource_host SET dependent_count = dependent_count + 1
  WHERE id = $2 AND (SELECT inserted FROM up);
  ```

  `--advisory` writes `required = FALSE`: the link is visible on the detail API but excluded from the Saint roll-up. Manual links are never pruned by §5.1 step 3.
- **`v6ctl resource remove <domain> <host>`** — resolve both (unknown domain/host → exit 1). Delete the link and decrement:

  ```sql
  WITH del AS (
    DELETE FROM domain_resource
    WHERE domain_id = $1 AND resource_host_id = $2
    RETURNING resource_host_id
  )
  UPDATE resource_host SET dependent_count = dependent_count - 1
  WHERE id IN (SELECT resource_host_id FROM del);
  ```

  0 rows deleted → print `"no such link"`, exit 0. Removal is the **only** way to delete a `manual` link.

---

## 6. GeoIP / ASN attribution

### 6.1 Library, files, startup

The **IPinfo Lite** database — one combined file (`ipinfo_lite.mmdb`) carrying both country and ASN for IPv4+IPv6 — read with the generic mmdb reader `github.com/oschwald/maxminddb-golang/v2` (has `Close()`, netip-based; readers are safe for concurrent use). The record fields consumed are `asn` (textual, e.g. `"AS13335"`), `as_name`, and `country_code`. The filename is fixed (not config): `ipinfo_lite.mmdb`, in the directory given by config key `GEOIP_PATH` (uppercase-env viper convention; string, default `/var/lib/GeoIP`; registry: 09-ops.md). Only the **crawler binary** opens it (attribution is a scan-commit concern; the API never does GeoIP lookups; `v6ctl` needs it only via code paths it never exercises — insert-time attribution needs no mmdb, §6.5). The crawler **fails fast at startup** if the file is missing or unreadable. File freshness is operated by the `v6ctl-geoip-update.timer` (daily `OnCalendar=*-*-* 06:30` + 4h randomized delay) — unit definitions and the IPinfo token runbook live in 09-ops.md; the MaxMind → IPinfo switch is ADR 0001.

### 6.2 Attribution input IP

Computed from the scan's own base-composite answers for the apex (for `kind='subdomain'`, the host itself) — no extra DNS queries (the answer sets are part of the commit unit, 03-state-machine.md):

1. If the base AAAA quorum observation is `exists`: input IP = the **first** globally-routable AAAA in the recorded answer set (the designated resolver's answers in the fixed provider order, after the bogon filter — order pinned in 02-observation-model.md). AAAA always wins over A.
2. Else if the conditional bulk A lookup ran and returned `a_present`: input IP = the first A address in that answer.
3. Else (`nxdomain` / `empty`+`a_absent` / `a_error`): no input IP.

Address order within the RRset is "as returned"; cross-scan determinism is not required (attribution self-heals every scan).

### 6.3 ASN attribution (`domain.asn_id`)

1. IPinfo Lite lookup of the input IP → `asn` (parsed from `"AS<n>"` to a number) + `as_name` (organization name).
2. AS number ≠ 0: find the `asn` row by `number`; if absent:

   ```sql
   INSERT INTO asn (number, name) VALUES ($1, $2) ON CONFLICT (number) DO NOTHING;
   SELECT id FROM asn WHERE number = $1;
   ```

   New ASNs are auto-registered exactly as production does; existing names are **not** updated on later scans.
3. No input IP, lookup miss, or AS number 0 → the sentinel ASN row (§6.6).

### 6.4 Country attribution (`domain.country_id`) — ccTLD wins over server location

1. **ccTLD:** take the final label of the host's ICANN public suffix — equivalently (and implemented as) the host's **final DNS label**. Probe `country.tld` with `"." + strings.ToUpper(label)` (the seed stores production's dot-prefixed uppercase form, `.NO`). A match wins **unconditionally** — no GeoIP lookup is made.
2. **GeoIP fallback:** the input IP's IPinfo Lite `country_code`, matched against `country.code`.
3. **Sentinel** otherwise (no input IP, lookup miss, or unmapped code).

### 6.5 Insert-time attribution (no mmdb, no input IP)

Every code path that inserts a `domain` row outside a scan commit — Tranco import (§2.2 step 10), campaign sync ensure-entity (§3.4), live-check row creation (07-api.md) — attributes at insert with **no input IP**: country = ccTLD rule (§6.4 step 1) or sentinel; ASN = sentinel. Consequently `asn_id`/`country_id` are never NULL and no serializer ever handles NULL attribution. The Tranco path implements this as the set-based SQL join in §2.2 step 10; the Go paths use a helper in `internal/geoip` that needs only the in-memory `country.tld → id` map (loaded once per run) and the sentinel ids — it does not open mmdb files, so `v6ctl campaign sync` runs on hosts without GeoIP databases.

### 6.6 Timing rules

- Attribution (§6.2–6.4) is recomputed inside **every** scan-commit transaction, for every scanned entity (ranked, campaign, subdomain).
- Deferred scans (base observation `error`/`inconsistent` — no commit) do **not** touch attribution: a transient resolver failure must not flip a domain to Unknown. (Deliberate improvement over production, which degraded to Unknown on any IPLookup timeout.)
- Live checks never touch attribution on existing rows (Rule 0, 07-api.md); only their initial row insert attributes, per §6.5.

### 6.7 Sentinels and seeds

Seed data (migration ordering in 05-schema.md: sentinels land with the asn/country seed rows, before any domain row): `asn (number 0, name 'Unknown')`; `country (code 'UN', name 'Unknown', tld '.UN')`. The schema uses IDENTITY ids, so every component (crawler, ingest, campaign sync) resolves sentinel ids **once at startup/run-start by lookup** (`SELECT id FROM asn WHERE number = 0`; `SELECT id FROM country WHERE code = 'UN'`), never by literal id. Both sentinels appear in `/asns` and `/countries` listings exactly as they do in production.

### 6.8 mmdb hot reload

The crawler stats the mmdb file hourly; on mtime change it opens a new reader, swaps it via `atomic.Pointer[maxminddb.Reader]`, and `Close()`s the old one. Startup and each swap log the database's build epoch (slog key `geoip.build_epoch`); the loaded build epoch is also exported in `crawler_metrics` for the Grafana >7d staleness alert (09-ops.md — daily updates). A plain systemd restart after update is an acceptable operational substitute; the mtime swap avoids interrupting long crawl runs. Reload interval and filename are fixed, not config.

---

### 6.9 TLD pivot (`domain.tld`) — insert-time, publicsuffix-derived

`domain.tld` (05-schema.md — domain) extends §6's per-domain derivation to the league-table pivots. It is set at **insert** (not scan commit) by every path that creates a `domain` row, from the host's ICANN public suffix via the same `publicsuffix-go` library §3.4 uses for eTLD+1 — one derived write, no new lookup (the PSL is in-memory; the crawler never consults it — PSL at import/validation time only, §3.4 Decision). Stored **lowercase, no leading dot** (e.g. `no`, `gov`, `co.uk`) — distinct from `country.tld`'s dot-prefixed uppercase `.NO` form.

- **Apex entity** (`kind='apex'`): `tld` = the host's ICANN public suffix (`dnb.no`→`no`, `bbc.co.uk`→`co.uk`, `x.gov`→`gov`). Guaranteed non-NULL — every host reaching an insert has a registrable domain (a bare public suffix is rejected: §3.4 step 1 / Canonicalize's ≥2-label rule).
- **Subdomain entity** (`kind='subdomain'`): `tld` = the host's public suffix, identical to its parent's (`api.bbc.co.uk`→`co.uk`). Set the same way per row, never inherited by reference.

Insert paths and how each obtains it (no path adds a lookup):
- **Campaign ensure-entity (§3.4):** the `registrable`/public-suffix PSL call already runs per host; step 1 captures the eTLD and every apex/parent-link/subdomain insert stores it.
- **Tranco import (§2.2):** step 5 already runs Go per line; it computes the public suffix via `publicsuffix-go` and stages it (`tranco_staging.tld`), and step 10's set-based INSERT carries `s.tld`. **Decision:** `tld` is computed in Go at parse — **not** via the SQL final-label trick §2.2 uses for country — because that trick is correct only for ccTLD attribution (which keys on the final DNS label), whereas the `tld` pivot must carry the **full** multi-label public suffix (`co.uk`, not `uk`); computing it in SQL would make Tranco's `co.uk` domains disagree with campaign-sourced ones and corrupt the `?tld=` facet.
- **Live-check row creation (07-api.md):** the initial insert sets `tld` the same way (07 owns that path).

Existing rows never rewrite `tld` — it is a pure function of the immutable host (the Tranco and campaign conflict arms deliberately leave it untouched).

### 6.10 DNS-provider and hosting/CDN provider — scan-commit derived

`domain.dns_provider_id` (OPEN-4) and `domain.hosting_provider` (05-schema.md — domain) are recomputed inside **every** scan-commit transaction, on the same UPDATE as `asn_id`/`country_id`, from data the scan already collected — no extra DNS queries, no mmdb. §6.6's timing rules apply verbatim: deferred scans (base observation `error`/`inconsistent`, no commit) and live checks (Rule 0, 07-api.md) never touch them, so a transient resolver failure never nulls a domain's provider.

**`dns_provider_id` — the DNS-provider league table (OPEN-4, resolved YES 2026-07-09).** Input: the nameserver hosts the checker already resolved — the keys of the `dns_ns_ipv6` result's `details["nameservers"]` map (01-engine.md — §11.3), carried in the scan result the commit consumes (same "part of the commit unit" property §6.2 relies on for attribution). For `kind='subdomain'` the NS check ran against the host's own delegated zone, so its NS set is used directly. Algorithm:

1. Collect the domain's NS host set (canonical, lowercase) from the scan result. Empty (NS check non-definitive) → set `dns_provider_id = NULL`, done.
2. For each NS host, find the `dns_provider` row (05-schema.md — dns_provider) with the **longest** entry in `ns_suffixes[]` the NS host matches on a label boundary (`ns == suffix` or `ns` ends with `"." + suffix`).
3. The **longest matching suffix across all providers and all NS hosts** wins → set `dns_provider_id`; no match → NULL. (`ns_suffixes` are curated unambiguous, so a domain's NS set normally maps to one provider; the longest-match tie-break is defined for completeness.)

This is a pure lookup against the in-memory `dns_provider` snapshot (small; loaded once per run and refreshed like the country map). It powers `/providers` + `/providers/{id}/domains` and the `?provider=` filter (07-api.md — §5.6), indexed by `idx_domain_dns_provider` (05-schema.md).

**`hosting_provider` — the hosting/CDN axis.** A **normalized** tag derived from data already in the scan result, in order:

1. **CDN via CNAME chain:** if the base/www AAAA check set `details["cdn_detected"] = true` (01-engine.md — §11.2), map the matched CDN suffix in `details["cname_target"]`/`cname_chain` to a normalized tag using the **same** fixed CDN-suffix list §11.2 already carries: `cloudfront.net`→`cloudfront`; `cloudflare.net`/`cdn.cloudflarenet.com`→`cloudflare`; `akamaiedge.net`/`akamai.net`/`edgekey.net`→`akamai`; `fastly.net`→`fastly`; `azureedge.net`→`azure`; `edgecastcdn.net`→`edgecast`; `stackpathdns.com`→`stackpath`; `googleapis.com`→`google`.
2. **Else hosting-ASN fallback:** if the resolved input IP's ASN (already looked up for `asn_id`, §6.3 — no new lookup) is in the curated hosting/cloud-ASN→tag set, use that tag. **Decision — the launch seed set** (a Go constant map, extended as collected data shows gaps): AS16509/AS14618→`aws`; AS15169/AS396982→`google`; AS8075→`azure`; AS16276→`ovh`; AS24940→`hetzner`; AS14061→`digitalocean`; AS63949→`linode`; AS13335→`cloudflare`.
3. **Else** → NULL.

**Decision:** `hosting_provider` is a denormalized normalized-TEXT tag (05-schema.md — domain), **not** an FK — the CDN-suffix map and the hosting-ASN map are code constants, and the ASN fallback already has its own `asn` row, so a second reference table would duplicate `asn`. Both maps are Go constants co-located with the commit-path attribution helpers, single-sourced with §11.2's CDN-suffix list. Only **normalized** tags are written (raw `asn.name` org strings are deliberately not used — that would defeat a clean league facet). Registrar attribution is **deliberately not derived** here (RDAP cost at 1M scale — deferred, §10.3).

### 6.11 `v6ctl provider` — dns_provider seed and maintenance

The `dns_provider` table (05-schema.md — dns_provider) has a **single** write path: the operator verb group (matching the other reference-data verbs; no HTTP admin surface). It is seeded once from a curated list of major managed-DNS operators and their nameserver-host suffixes (Cloudflare `cloudflare.com`, AWS `awsdns-*`/`amazonaws.com`, Google `googledomains.com`, Azure `azure-dns.*`, …) and maintained as new operators appear.

- **`v6ctl provider add <name> <suffix> [<suffix>...]`** — upsert the provider by `name`; append the given nameserver-host suffixes to `ns_suffixes` (deduped). Suffixes are stored lowercase, no leading dot.
- **`v6ctl provider remove <name>`** — delete the provider row. Domains keep their last `dns_provider_id` until the next scan-commit recompute nulls it (§6.10 is self-healing).
- **`v6ctl provider list`** — print each provider with its suffix set and current mapped-domain count (`SELECT count(*) FROM domain WHERE dns_provider_id = $1`).

**Decision:** the mapping is **primarily** seeded via this verb (a checked-in seed script calling `provider add`) — it is reference **data**, belonging in the DB the same way the `asn`/`country` seeds do (05-schema.md — seeds), not tunable behavior. Two operational knobs are registered in the 09-ops.md config registry (§2.11): `dns_provider.seed_path` (optional path to a curated `ns_host → provider` seed file — **YAML**, a list of `{name, suffixes: [...]}` entries mirroring the `provider add` arguments; default `""` = no file, mapping seeded by the verb and derived from collected NS data only) and `dns_provider.refresh_interval` (default `24h` — the cadence at which the in-memory provider snapshot this section loads is rebuilt from the table + collected NS data, "refreshed like the country map", §6.10). New v6ctl verb group `provider add|remove|list` — flagged for the 09-ops.md verb inventory / systemd surface and registered in the deliverables above.

---

## 7. v6ctl shame (the `top_shame` writer)

`top_shame` (05-schema.md) has **no other write path**. Single-maintainer editorial tool; direct DB access like the other v6ctl verbs. Shame edits write **no changelog entries** (editorial action, not an observed status transition).

- **`v6ctl shame add <host> [--reason "..."]`** — canonicalize `<host>` (§1; a scheme/port/path in the argument is rejected, not stripped). Look up `domain` by host. **Exit 1** if: no row exists, `kind <> 'apex'`, `rank IS NULL`, or `disabled` — editorial picks must satisfy the publicly-ranked predicate (`rank IS NOT NULL AND NOT disabled`, 07-api.md), otherwise the row could never render. Then:

  ```sql
  INSERT INTO top_shame (domain_id, reason)
  VALUES ($1, $2)
  ON CONFLICT (domain_id) DO UPDATE SET reason = EXCLUDED.reason;
  ```

  Idempotent: re-add updates `reason`, preserves `added_at`; `--reason` omitted ⇒ NULL. If the domain's current `classification <> 'sinner'`, **warn but succeed**: `"added; will not render on /shame until classified sinner"` — rows are durable picks, visibility is computed at read time (the `/shame` visibility predicate, 07-api.md), matching production where fixed domains stay in the table but drop out of the view.
- **`v6ctl shame remove <host>`** — resolve host to `domain_id` (unknown host → exit 1), `DELETE FROM top_shame WHERE domain_id = $1`; 0 rows deleted → print `"not on the shame list"`, exit 0.
- **`v6ctl shame list`** — print all rows joined to domain: `host, rank, classification, reason, added_at, visible` where `visible = (classification = 'sinner' AND rank IS NOT NULL AND NOT disabled)`, ordered by rank.

Migration note: the seed migration does **not** populate `top_shame` (its FK needs phase-1 ingestion); population at cutover is an editorial re-entry via `v6ctl shame add` (08-migration-cutover.md — DNS-flip cutover, step 2), plus `v6ctl shame add` thereafter. There is no importer — start-fresh cutover, no history import (OPEN-9).

---

## 8. Config keys introduced by this file

All keys below are introduced here and consolidated in the registry (09-ops.md), which is the single source for the full table:

| Key | Type | Default | Owner |
|---|---|---|---|
| `tranco.min_rows` | int | 950000 | crawler + v6ctl |
| `tranco.max_delist_pct` | float | 2.0 | crawler + v6ctl |
| `tranco.import_at` | string (UTC `HH:MM`) | `"23:15"` | crawler |
| `tranco.retry_interval` | duration | 2h | crawler |
| `tranco.stale_warn_after` | duration | 48h | crawler |
| `campaign.repo_path` | string | `/srv/whynoipv6-campaign` | crawler + v6ctl |
| `campaign.git_remote` | string | `origin` | crawler + v6ctl |
| `campaign.pull` | bool | true | crawler + v6ctl |
| `campaign.push` | bool | true | crawler + v6ctl |
| `campaign.max_domains_per_file` | int | 5000 | v6ctl (validate) |
| `campaign.max_subdomains_per_domain` | int | 20 | crawler + v6ctl |
| `crawler.resources.enabled` | bool | false | crawler |
| `GEOIP_PATH` | string | `/var/lib/GeoIP` | crawler |

Hardcoded constants (deliberately not config): v6ctl advisory-lock wait 5m; discovery prune window 30 days; sweep batch 100 / empty poll 60s / non-definitive retry 2h / cadence 24h; host confirmation N=2; mmdb reload interval 1h; staleness-warning rate limit 24h.

---

## 9. Acceptance criteria (fixtures and test tables live in 10-testing.md)

1. Canonicalize passes the §1 vector table; no other normalization code exists in the module (grep gate: `strings.ToLower` on hostnames outside `internal/domain/host.go` fails review).
2. A Tranco CSV fixture containing CRLF lines, `_wildcard_.ph`, mixed-case duplicates folding to one host, and an IDN line imports with correct `line_count/rejected_count/duplicate_count/imported_count/delisted`, lowest-rank-wins fold, and 24h-spread `next_check_at`.
3. Re-importing the same list ID is a no-op (short-circuit in step 2; provenance `ON CONFLICT … DO NOTHING` as the second guard); an aborted list is never auto-retried; `--force` imports it.
4. A fixture where >2% of ranked rows would delist aborts with `aborted=true` + note and leaves all ranks unchanged; the same fixture with `--force` applies. The same fixture against a DB whose last successful import is 5 days old **imports** (the budget scaled to 10%), and a fixture past the 10% ceiling still aborts at that staleness.
5. Re-entry: a `delisted` row present in today's list is re-enabled with `next_check_at = now()`; a `dead` row stays disabled with `next_check_at = now()`; `service`/`manual` rows only get rank updates.
6. Campaign sync against a fixture repo covers: new file without uuid (insert + write-back), rename (source_file update), file deletion (soft-disable via uuid-set diff), re-appearance (re-enable, no membership churn), duplicate uuid across files (source_file match wins), unknown uuid (adopted, on a new file and on one whose uuid changed in place — the latter forking into disabled + created), subdomain entry (parent auto-created, `created_by='parent_link'`, `parent_id` set), and the membership re-entry rule.
7. `v6ctl campaign validate` reproduces every §4.2 blocking check on fixture PRs (added-file-with-uuid, modified-uuid, rename-with-preserved-uuid, within-file duplicate, oversize file) and never fails on cross-file duplicates.
7a. Curated subdomain lists (§3.7): the parser accepts relative labels (including multi-level) and rejects a full host, a bare `www`, a normalized duplicate, an over-cap list, an unknown key, and a filename that is not the canonical registrable apex; validation reports each failure at the offending entry's line. Sync against a fixture repo covers: unknown apex (skipped, reported, no rows created), tracked apex (children created `kind='subdomain'`/`created_by='curated'`, parent untouched), a host already known from another ingress (origin kept, parent backfilled so it appears in `subdomain_count`), entry dropped (membership removed, row left for the sweep), re-listing a delisted host (re-enabled and due), a disabled apex (skipped **without** unlisting its hosts), two files claiming one apex (both rejected), and any rejection suspending the removal diff.
8. Discovery statements A–C + prune maintain `dependent_count` exactly (property test: count equals `SELECT count(*) FROM domain_resource WHERE resource_host_id = X` after arbitrary interleavings); manual links survive prune; `required=FALSE` links are excluded from the roll-up input.
9. Sweep confirmation machine: NULL→first-definitive commits immediately; a status change requires exactly 2 consecutive definitive sightings; non-definitive outcomes change nothing except the claim's 2h bump.
10. Attribution: ccTLD beats GeoIP; deferred scans leave `asn_id/country_id` untouched; insert-time attribution yields ccTLD-or-sentinel country + sentinel ASN; sentinel ids are resolved by lookup, not literals.
11. Stats snapshot & counter recompute (§10): against a fixture DB of confirmed domains, the four `stats_*` upserts and the `country`/`asn` counter recompute produce the pinned counts under the publicly-ranked scope (`rank IS NOT NULL AND NOT disabled`, with the `stats_global_daily.disabled` exception); `top_heroes` counts `rank <= 1000 AND base = 'supported' AND www ≠ 'unsupported'`; a same-day re-run (`v6ctl stats recalc`) is a value-identical no-op (every statement is `ON CONFLICT DO UPDATE` or a set-based recompute); a country/ASN that drops to zero publicly-ranked members resets to `0/0/0`.

---

## 10. Daily stats snapshot and counter recompute

This section owns the SQL query bodies for **daily tick step 2** (product-stats snapshot) and **daily tick step 3** (country/ASN/DNS-provider counter recompute) — the tick step order, advisory lock (`JobDailyTick`), and failure containment are owned by 04-lifecycle-scheduling.md — The daily tick. The same snapshot upserts and counter recomputes are re-run verbatim by `v6ctl stats recalc` (§10.7), which the cutover runbook invokes once at DNS flip to seed the first real snapshot (08-migration-cutover.md — DNS-flip cutover). The columns they populate are read by the `/stats/*`, `/stats/overview`, `/asns`, and `/countries` serializers (07-api.md).

These are snapshots of **confirmed** domain state (the `domain.*_status`, `domain.classification`, `domain.saint` columns), **never** scan-derived: public graphs must match the public lists exactly, and a continuous aggregate over raw observations would wobble with scan timing and include unconfirmed values. All DDL (`stats_global_daily`, `stats_country_daily`, `stats_campaign_daily`, `stats_asn_daily`, and the `country`/`asn` counter columns) lives in 05-schema.md — stats tables and the `country`/`asn` tables.

Every statement is independently idempotent — the four snapshots are `INSERT … ON CONFLICT (<pk>) DO UPDATE SET <every counter> = excluded.<col>` (**DO UPDATE, never DO NOTHING**, so a same-day re-run overwrites), and the two recomputes are set-based `UPDATE`s. `day` is `CURRENT_DATE` for the three DATE-keyed tables and `CURRENT_DATE::timestamptz` (UTC midnight; the database runs in UTC) for `stats_asn_daily`. The tick runs them sequentially after the lifecycle sweep (step 1); ordering among the six is irrelevant because none reads another's output.

### 10.1 Scope predicates (pinned)

- **Publicly-ranked scope** (all `stats_global_daily`/`stats_country_daily`/`stats_asn_daily` counters and both counter recomputes, except the one exception below): `rank IS NOT NULL AND NOT disabled` — identical to the §5.1 publicly-ranked predicate in 07-api.md, so every figure matches the public lists exactly.
- **`stats_global_daily.disabled` exception:** counts `rank IS NOT NULL AND disabled` (visibility into how much of the ranked set is suppressed). It is the **only** stats counter scoped to disabled rows.
- **`stats_campaign_daily` scope:** campaign membership with `domain NOT disabled`; **rank is irrelevant** (campaign members are typically unranked). The job writes rows **only for `NOT disabled` campaigns** (the `JOIN campaign … AND NOT c.disabled` below); historical rows for a campaign that later gets disabled are retained untouched, and on re-enable the series resumes with a gap (frontends tolerate missing days).
- **The "v6-enabled" predicate** used by `country.v6sites`, `asn.count_v6`, and `stats_asn_daily.v6_domains`: `classification IN ('partial','hero')`. **Decision:** this is the "classification-based v6 definition" the design pins for the ported counter recomputes (design §2.6 step 3, replacing production's ad-hoc `base AND www AND ns` triple); by the classification ladder (03-state-machine.md — Classification) it is **identical to `base_status = 'supported'`** (ladder rules 4/5 are the only rules that fire on `base = 'supported'`), which is why the pinned examples in 07-api.md show `country.v6sites` == `stats_country_daily.base_supported` and `asn.count_v6` == `stats_asn_daily.v6_domains`.

Enum literals in these statements (`'supported'`, `'sinner'`, `'partial'`, `'hero'`, `'inactive'`, `'unknown'`, `'not_applicable'`, `'unsupported'`) are the `ipv6_status` and `classification` values defined in 05-schema.md.

### 10.2 `stats_global_daily` snapshot (`db/query/stats.sql`)

Population is the ranked set (`WHERE rank IS NOT NULL`); each counter carries its own `FILTER`, so the one `disabled` exception coexists with the `NOT disabled` majority in a single scan. `top_heroes`/`top_nameserver` use the same visibility scope plus `rank <= 1000` (which implies `rank IS NOT NULL`, excluding unranked campaign/subdomain rows). `top_heroes` counts web-facing IPv6 only (`base = 'supported'` and www does not *contradict* it — `not_applicable`/`no_record`/NULL never count against, only confirmed `www = 'unsupported'` excludes); it is deliberately **not** the §4.3 hero classification (no ns/conn/mx requirement). Both top-1k formulas are the design's fixed metrics: `rank <= 1000` (not production's `< 1000`) and `base = 'supported'` (not production's `base != 'unsupported'`).

```sql
INSERT INTO stats_global_daily (
  day, domains, sinners, partial, heroes, saints, inactive, unknown, disabled,
  base_supported, www_supported, ns_supported, mx_supported, conn_supported,
  resources_supported, top_heroes, top_nameserver, generated_at)
SELECT
  CURRENT_DATE,
  count(*) FILTER (WHERE NOT disabled),
  count(*) FILTER (WHERE NOT disabled AND classification = 'sinner'),
  count(*) FILTER (WHERE NOT disabled AND classification = 'partial'),
  count(*) FILTER (WHERE NOT disabled AND classification = 'hero'),
  count(*) FILTER (WHERE NOT disabled AND saint),
  count(*) FILTER (WHERE NOT disabled AND classification = 'inactive'),
  count(*) FILTER (WHERE NOT disabled AND classification = 'unknown'),
  count(*) FILTER (WHERE disabled),                                   -- the one exception
  count(*) FILTER (WHERE NOT disabled AND base_status  = 'supported'),
  count(*) FILTER (WHERE NOT disabled AND www_status   = 'supported'),
  count(*) FILTER (WHERE NOT disabled AND ns_status    = 'supported'),
  count(*) FILTER (WHERE NOT disabled AND mx_status    = 'supported'),
  count(*) FILTER (WHERE NOT disabled AND conn_status  = 'supported'),
  count(*) FILTER (WHERE NOT disabled AND resources_status = 'supported'),
  count(*) FILTER (WHERE NOT disabled AND rank <= 1000
                     AND base_status = 'supported'
                     AND www_status IS DISTINCT FROM 'unsupported'),
  count(*) FILTER (WHERE NOT disabled AND rank <= 1000
                     AND ns_status = 'supported'),
  now()
FROM domain
WHERE rank IS NOT NULL
ON CONFLICT (day) DO UPDATE SET
  domains             = excluded.domains,
  sinners             = excluded.sinners,
  partial             = excluded.partial,
  heroes              = excluded.heroes,
  saints              = excluded.saints,
  inactive            = excluded.inactive,
  unknown             = excluded.unknown,
  disabled            = excluded.disabled,
  base_supported      = excluded.base_supported,
  www_supported       = excluded.www_supported,
  ns_supported        = excluded.ns_supported,
  mx_supported        = excluded.mx_supported,
  conn_supported      = excluded.conn_supported,
  resources_supported = excluded.resources_supported,
  top_heroes          = excluded.top_heroes,
  top_nameserver      = excluded.top_nameserver,
  generated_at        = excluded.generated_at;
```

`generated_at = now()` is the crawl-freshness signal 05-schema.md documents on the column and the deterministic source of the envelope `meta.as_of` (07-api.md — §2.4); every rollup (including intraday re-runs via `v6ctl stats recalc`) refreshes it.

While `crawler.resources.enabled = false` (§5), `resources_status` is NULL everywhere, so `resources_supported = 0` and `saints = 0` — correct: no saints before the resources feature ships. `domains` is the total publicly-ranked count and thus equals `sinners + partial + heroes + inactive + unknown` (the ladder is total over confirmed `base`; a NULL `base` lands in `unknown`).

### 10.3 `stats_country_daily` snapshot (`db/query/stats.sql`)

Grouped by `domain.country_id` over the publicly-ranked scope; rows are written only for countries that have ≥1 publicly-ranked member (an empty country produces no row — its `/countries/{code}/stats` series simply has no point that day, which clients tolerate). `base_supported` here equals the country's `v6sites` counter (§10.6) by construction.

```sql
INSERT INTO stats_country_daily (
  day, country_id, domains, sinners, partial, heroes, base_supported, conn_supported)
SELECT
  CURRENT_DATE, country_id,
  count(*),
  count(*) FILTER (WHERE classification = 'sinner'),
  count(*) FILTER (WHERE classification = 'partial'),
  count(*) FILTER (WHERE classification = 'hero'),
  count(*) FILTER (WHERE base_status = 'supported'),
  count(*) FILTER (WHERE conn_status = 'supported')
FROM domain
WHERE rank IS NOT NULL AND NOT disabled
GROUP BY country_id
ON CONFLICT (day, country_id) DO UPDATE SET
  domains        = excluded.domains,
  sinners        = excluded.sinners,
  partial        = excluded.partial,
  heroes         = excluded.heroes,
  base_supported = excluded.base_supported,
  conn_supported = excluded.conn_supported;
```

### 10.4 `stats_campaign_daily` snapshot (`db/query/stats.sql`)

Grouped by `campaign_domain.campaign_id`, joining enabled campaigns to their non-disabled member domains (rank ignored, per §10.1). `v6_ready` is the R4 amended formula (07-api.md — R4): `base = 'supported' AND ns = 'supported' AND www IN ('supported','not_applicable')` — the `not_applicable` arm keeps subdomain-heavy campaigns (whose `www` is forced `not_applicable`, §3.4) from being pinned at 0%; NULL (unconfirmed) `www` does not count as ready, and `mx`/`conn` are excluded from `v6_ready`.

```sql
INSERT INTO stats_campaign_daily (
  day, campaign_id, domains, v6_ready, sinners, partial, heroes,
  base_supported, www_supported, ns_supported, mx_supported, conn_supported)
SELECT
  CURRENT_DATE, cd.campaign_id,
  count(*),
  count(*) FILTER (WHERE d.base_status = 'supported'
                     AND d.ns_status  = 'supported'
                     AND d.www_status IN ('supported','not_applicable')),
  count(*) FILTER (WHERE d.classification = 'sinner'),
  count(*) FILTER (WHERE d.classification = 'partial'),
  count(*) FILTER (WHERE d.classification = 'hero'),
  count(*) FILTER (WHERE d.base_status = 'supported'),
  count(*) FILTER (WHERE d.www_status  = 'supported'),
  count(*) FILTER (WHERE d.ns_status   = 'supported'),
  count(*) FILTER (WHERE d.mx_status   = 'supported'),
  count(*) FILTER (WHERE d.conn_status = 'supported')
FROM campaign_domain cd
JOIN campaign c ON c.id = cd.campaign_id AND NOT c.disabled
JOIN domain   d ON d.id = cd.domain_id   AND NOT d.disabled
GROUP BY cd.campaign_id
ON CONFLICT (day, campaign_id) DO UPDATE SET
  domains        = excluded.domains,
  v6_ready       = excluded.v6_ready,
  sinners        = excluded.sinners,
  partial        = excluded.partial,
  heroes         = excluded.heroes,
  base_supported = excluded.base_supported,
  www_supported  = excluded.www_supported,
  ns_supported   = excluded.ns_supported,
  mx_supported   = excluded.mx_supported,
  conn_supported = excluded.conn_supported;
```

### 10.5 `stats_asn_daily` snapshot (`db/query/stats.sql`)

Grouped by `domain.asn_id` over the publicly-ranked scope; `day` is a TIMESTAMPTZ (UTC midnight). `v6_domains` uses the §10.1 v6-enabled predicate. The sentinel ASN (number 0) appears as a normal group, exactly as in the `/asns` listing.

```sql
INSERT INTO stats_asn_daily (day, asn_id, domains, v6_domains, sinners, heroes)
SELECT
  CURRENT_DATE::timestamptz, asn_id,
  count(*),
  count(*) FILTER (WHERE classification IN ('partial','hero')),
  count(*) FILTER (WHERE classification = 'sinner'),
  count(*) FILTER (WHERE classification = 'hero')
FROM domain
WHERE rank IS NOT NULL AND NOT disabled
GROUP BY asn_id
ON CONFLICT (asn_id, day) DO UPDATE SET
  domains    = excluded.domains,
  v6_domains = excluded.v6_domains,
  sinners    = excluded.sinners,
  heroes     = excluded.heroes;
```

### 10.6 Country/ASN counter recompute (`db/query/country.sql`, `db/query/asn.sql`)

Tick step 3: the ported `update_country_metrics` / `update_asn_metrics` recomputes plus the parallel `dns_provider` recompute (OPEN-4), **corrected** (design §2.6 step 3): the v6 count is the classification-based v6-enabled predicate (§10.1), and the total count is over the publicly-ranked scope (production's proc counted *all* domains, ignoring rank/disabled). All three are set-based `UPDATE`s and inherently idempotent.

Each recompute is **two statements**: a reset of every dimension row to zero, then a targeted update from the aggregate. The reset is required for correctness — a country or ASN whose last publicly-ranked domain is delisted or disabled must fall to `0/0/0`; a JOIN-only update (production's form) would leave a stale non-zero count on rows absent from the aggregate. The daily row churn (≤251 countries, ~50–80k ASNs) is negligible.

`country` (`db/query/country.sql`) — `percent` is `NUMERIC(5,2)`, rounded to 2 decimals (killing production's pgtype ÷10 hack, 05-schema.md); a zero-`sites` country gets `percent = 0` (no divide-by-zero):

```sql
-- statement 1: reset
UPDATE country SET sites = 0, v6sites = 0, percent = 0;

-- statement 2: recompute over the publicly-ranked scope
UPDATE country c SET
  sites   = agg.sites,
  v6sites = agg.v6sites,
  percent = CASE WHEN agg.sites = 0 THEN 0
                 ELSE ROUND(agg.v6sites::numeric / agg.sites::numeric * 100, 2) END
FROM (
  SELECT country_id,
         count(*)                                                  AS sites,
         count(*) FILTER (WHERE classification IN ('partial','hero')) AS v6sites
  FROM domain
  WHERE rank IS NOT NULL AND NOT disabled
  GROUP BY country_id
) agg
WHERE c.id = agg.country_id;
```

`asn` (`db/query/asn.sql`) — `count_total` is the total publicly-ranked domain count in the ASN (every domain has IPv4; 07-api.md computes the wire `count_v4 = count_total - count_v6` server-side); `count_v6` is the v6-enabled count:

```sql
-- statement 1: reset
UPDATE asn SET count_total = 0, count_v6 = 0;

-- statement 2: recompute over the publicly-ranked scope
UPDATE asn a SET
  count_total = agg.count_total,
  count_v6    = agg.count_v6
FROM (
  SELECT asn_id,
         count(*)                                                  AS count_total,
         count(*) FILTER (WHERE classification IN ('partial','hero')) AS count_v6
  FROM domain
  WHERE rank IS NOT NULL AND NOT disabled
  GROUP BY asn_id
) agg
WHERE a.id = agg.asn_id;
```

`dns_provider` (`db/query/asn.sql` — colocated with the ASN recompute; OPEN-4 DNS-provider league table) — identical shape to `asn`, keyed by `domain.dns_provider_id`, over the same publicly-ranked scope, so `GET /providers` counts match the public lists exactly. `count_v4 = count_total − count_v6` is synthesized server-side in 07-api.md §4.6 (never stored), mirroring ASN. The provider set is small (tens to low hundreds of rows), so the reset+recompute is trivially cheap:

```sql
-- statement 1: reset
UPDATE dns_provider SET count_total = 0, count_v6 = 0;

-- statement 2: recompute over the publicly-ranked scope
UPDATE dns_provider p SET
  count_total = agg.count_total,
  count_v6    = agg.count_v6
FROM (
  SELECT dns_provider_id,
         count(*)                                                     AS count_total,
         count(*) FILTER (WHERE classification IN ('partial','hero')) AS count_v6
  FROM domain
  WHERE rank IS NOT NULL AND NOT disabled AND dns_provider_id IS NOT NULL
  GROUP BY dns_provider_id
) agg
WHERE p.id = agg.dns_provider_id;
```

### 10.7 `v6ctl stats recalc`

- **`v6ctl stats recalc`** — runs the four §10.2–§10.5 snapshot upserts and the three §10.6 counter recomputes (country, ASN, DNS-provider) for `CURRENT_DATE`, in one transaction, against the configured pool. It is the manual/break-glass and migration entry point to the identical code the tick calls (shared `internal/crawler` helper — the query bodies are single-sourced in `db/query/*.sql`). **Decision:** it does **not** acquire `JobDailyTick`; every statement is idempotent and value-identical to the tick's, so a concurrent tick is harmless (last writer wins with the same numbers). Read/write; exit 0 on success, exit 1 on DB error. Available as the manual/break-glass recompute at cutover to seed the first real stats snapshot on the freshly migrated DB (08-migration-cutover.md — DNS-flip cutover): on a freshly migrated DB it produces one all-zeros-or-seed `stats_global_daily` row for the cutover date, keyed rows only for keys with members, and the initial `country`/`asn` counters — all computed identically to every subsequent nightly tick.
