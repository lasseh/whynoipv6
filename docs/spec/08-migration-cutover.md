# 08 — Production Migration & Cutover

**Purpose:** Specifies the one-time import of the live production database into the new
schema (`v6ctl migrate-import`) and the operational runbook that switches traffic from the old
backend to the new one. The import is the credibility bridge: production's current
statuses become seed confirmed values, and the full changelog archive is transformed
into the new field-level model so every historical entry renders byte-identically on the
frozen frontend.

**Deliverables:**
- `cmd/v6ctl/migrate_import.go` — the `v6ctl migrate-import` command and its sub-phases (entity
  resolution, seed statuses, changelog transform, per-scan history, top_shame, day-0
  snapshot), plus the `--verify-changelog` byte-equality gate.
- `internal/migrate/` — the reverse-map table, the changelog transform, the seed
  mapping, entity resolution, and the parity-gate checker (pure functions + a
  pgx-backed runner; the reverse-map render helper reuses `internal/api`'s
  `renderChangelog`, see 07-api.md — §3.11).
- The phase-4 cutover runbook and rollback plan (this file; operationalized by the
  Ansible deploy in 09-ops.md).
- The parity-gate list that phase 4 must pass before DNS switch.

**Companion files:**
- 05-schema.md — all target DDL (`domain`, `campaign`, `campaign_domain`, `changelog`,
  `scan`, `top_shame`, `stats_*`, the enums). This file writes rows; it never issues
  CREATE/ALTER.
- 07-api.md — §3.11 (the `renderChangelog` ladder the byte-equality gate replays),
  §2.8–§2.11 (legacy serialization, zero-result behavior), §2.7 (shortuuid codec) —
  the wire contract every parity gate asserts against.
- 06-ingest.md — §2 (Tranco import, which creates the `domain` rows this file UPDATEs),
  §3 (`campaign sync --adopt-unknown-uuids`, which establishes campaign identity before
  the changelog transform runs), §1 (`Canonicalize`).
- 04-lifecycle-scheduling.md — §9 (the daily tick, whose stats-snapshot step this file
  invokes once to write day-0 rows).
- 03-state-machine.md — the confirmed-status commit and classification truth table the
  seed values feed into.
- 10-testing.md — the golden-fixture harness; this file names the gates, 10 owns their
  mechanics and fixtures.
- 09-ops.md — config-key registry, systemd/Ansible deploy, ops webhook.

---

## 1. Scope, source of truth, and command shape

### 1.1 What `v6ctl migrate-import` does and does not do

`v6ctl migrate-import` runs **once**, during phase 4 (07-api.md ships, crawler already
operating from phase 3). It is a one-shot importer, not an ongoing process. It performs,
in order: entity resolution (§3), seed confirmed statuses (§4), the changelog history
transform (§6), the trailing-window per-scan history import (§7), the `top_shame` import
(§8), and the day-0 stats snapshot (§9). It writes into the tables owned by 05-schema.md
and never issues DDL.

It does **not** own: campaign/`campaign_domain` row creation (established by
`v6ctl campaign sync --adopt-unknown-uuids` in phase 1 — §5), the Tranco `domain` rows
(created by the phase-1 Tranco import, 06-ingest.md — §2; migrate UPDATEs them), or any
ongoing ingestion (06-ingest.md).

### 1.2 Source of truth

The import source is a **restored copy of the production database** — the retained
production dump loaded into a scratch PostgreSQL instance — reachable via a read-only
DSN. It is never the live production server (avoids load and racing the still-running old
crawler). Migrate opens two pools: `--source-dsn` (the restored production DB, read-only)
and `--dsn` (the new DB, read-write; the same target the API and crawler use). The
production dump is retained after cutover so a deeper history backfill stays possible
later (OPEN-7).

**Decision:** the production schema this file reads against is
`whynoipv6/db/migrations/01_schema.up.sql`. Source tables and their load-bearing
columns:

| Production table | Columns read |
|---|---|
| `domain` | `id, site, base_domain, www_domain, nameserver, mx_record, disabled, ts_base_domain, ts_www_domain, ts_nameserver, ts_mx_record, ts_check, ts_updated` (`v6_only`, `ts_v6_only`, `asn_id`, `country_id` are **not** read — see §4.3) |
| `changelog` | `id, ts, domain_id, message, ipv6_status` |
| `campaign_changelog` | `id, ts, domain_id (→ campaign_domain.id), campaign_id (dropped), message, ipv6_status` |
| `campaign_domain` | `id, site` (only for resolving `campaign_changelog.domain_id` → host) |
| `top_shame` | `site` |
| `domain_log` | `domain_id, time, data` (JSONB `{base_domain,www_domain,nameserver,mx_record}`) |
| `campaign_domain_log` | `domain_id (→ campaign_domain.id), time, data` (same JSONB shape) |

Production tables **not** used by migrate: `lists`, `sites` (Tranco rank is re-derived
fresh in phase 1, not migrated), `asn`, `country` (re-derived by GeoIP in phase 1),
`metrics` (day-0 stats are recomputed from confirmed state, §9), `campaign` (campaign
identity comes from YAML, §5).

### 1.3 Command shape

```
v6ctl migrate-import \
  --source-dsn <restored-production DSN>   # read-only
  --dsn <new DB DSN>                        # read-write (default: config)
  --history-window 90d                      # per-scan history depth (§7); OPEN-7 flag
  --verify-changelog                        # run the §6.5 byte-equality gate after import
  --dry-run                                 # resolve + count, write nothing, print the report
```

- **Concurrency: migrate takes no new advisory-lock key.** The lock registry
  (`internal/lock`, jobs 1–3) is complete and owned by 04-lifecycle-scheduling.md — §10;
  migrate does not add one. **Decision:** instead, migrate is run during the cutover
  window with the **new crawler paused** (runbook §10 step 3), so no crawl commit or daily
  tick races it. As defense-in-depth every migrate write is idempotent and guarded (the
  seed UPDATE's `… IS NULL` predicate in §4.1, and `ON CONFLICT DO NOTHING` on all
  changelog/history/shame inserts), so even a concurrent crawler commit is safe: the
  crawler is authoritative for anything it has already observed, and migrate never
  overwrites it.
- **Config keys** introduced (by name; registry: 09-ops.md): `migrate.source_dsn`
  (string, no default — must be passed or set), `migrate.history_window` (duration,
  default `2160h` = 90d).
- Idempotency: every write path uses `ON CONFLICT DO NOTHING`/`DO UPDATE` (specified per
  section) so a re-run after a partial failure converges. `--dry-run` is the pre-flight.
- The command is transactional **per sub-phase**, not globally (a single transaction over
  ~90M scan-history rows is impractical). Sub-phases are ordered so a crash between them
  leaves a re-runnable state (§10 runbook restarts migrate from the top; idempotent
  writes make completed sub-phases no-ops).

---

## 2. Preconditions (hard gates, checked at startup)

Migrate aborts with exit 1 and a named error if any fails. These are verification gates,
not cleanup steps.

1. **Phase-1 Tranco ingest has run:** `SELECT count(*) FROM domain WHERE rank IS NOT NULL`
   on the new DB is ≥ 900,000. (The seed UPDATEs target these rows; §4.)
2. **Campaigns established:** every distinct `campaign_id` in production
   `campaign_changelog` decodes to a `campaign.uuid` present in the new `campaign` table
   (populated by `v6ctl campaign sync --adopt-unknown-uuids`, §5). A missing uuid →
   abort `"campaign <uuid> not synced; run campaign sync --adopt-unknown-uuids first"`.
   (The changelog transform drops `campaign_id`, so this is a *warn-and-continue* gate
   only under `--force`; default is abort, because a missing campaign signals the phase-1
   sync was skipped.)
3. **Production status values are in-range:** on the source DB,
   `SELECT DISTINCT base_domain FROM domain` (and `www_domain`, `nameserver`,
   `mx_record`) must be a subset of `{'supported','unsupported','no_record'}`. Any other
   value → abort `"unexpected production status '<v>' in domain.<col>"`. This is the
   seed-mapping sanity guard (§4.2); it guarantees every seeded value is a legal
   `ipv6_status`. (`campaign_domain` status columns are not checked — they are not a seed
   source, §4.3.)
4. **New changelog is empty** (or only holds native post-cutover rows the crawler wrote
   since phase 3): `SELECT count(*) FROM changelog WHERE field = 'legacy'` = 0. A non-zero
   count means migrate already ran; re-running is safe (PK `ON CONFLICT DO NOTHING`) but
   the operator is warned `"legacy changelog rows already present; this is a re-run"`.

---

## 3. Entity resolution & canonicalization (the host→id map)

Every production row references a host indirectly. Migrate builds **one** authoritative
`map[string]int64` from canonical host → new `domain.id`, used by all sub-phases. This is
the single place entities are created.

### 3.1 Building the map

1. For every production `domain.site` and every production `campaign_domain.site`, and
   every host embedded in a `top_shame.site`, compute `h := Canonicalize(site)`
   (06-ingest.md — §1, the single rule; lowercase punycode FQDN). A `Canonicalize` error
   → the source row is **counted under `resolve_failures` and skipped** (its status/log/
   changelog history is dropped; logged at debug with the offending site). Canonicalize
   failures on decades-old junk are expected and must not fail the migration.
2. Look up `h` in the new `domain` table: `SELECT id FROM domain WHERE host = $1`.
3. **Hit** → record `map[h] = id`.
4. **Miss** (production host is not in the current Tranco top-1M and not a synced campaign
   domain) → **create the entity so its history is never orphaned:**

   ```sql
   INSERT INTO domain (host, kind, rank, created_by, asn_id, country_id, next_check_at)
   VALUES ($1, 'apex', NULL, 'import', $sentinel_asn, $sentinel_country, now())
   ON CONFLICT (host) DO NOTHING
   RETURNING id;
   ```

   `$sentinel_asn`/`$sentinel_country` are resolved once at run start by the §6.7-style
   lookup (06-ingest.md — §6.7 sentinels; never hardcoded). `kind='apex'` and
   `rank=NULL`: an imported legacy host with no Tranco rank is an unranked apex entity;
   it will be scanned on the slow lane and confirm its own status post-cutover. On
   `DO NOTHING` (a concurrent create), re-`SELECT` the id. `created_by='import'` marks it
   (05-schema.md — `created_by` enum reserves `'import'` for exactly this).

**Decision:** entity creation happens **only** in this map-build step, up front, covering
production `domain`, `campaign_domain`, and `top_shame` hosts in one pass. Seed statuses
(§4), the changelog transform (§6), and the history import (§7) then consume the finished
map and never create entities themselves. This removes the ordering hazard where the
changelog transform would create an entity that the seed step then finds unstatused —
every imported host has an entity before any status or history is written.

### 3.2 Host-form fidelity note (drives the changelog legacy path)

The map keys on `Canonicalize(site)`. Production `site` values are already lowercase and
overwhelmingly punycode (Tranco lists ship punycode), so `Canonicalize(site) == site` for
the vast majority. Where they differ (a legacy Unicode or mixed-case site), the new
entity's `host` is the canonical form, which differs from the raw string embedded in that
row's historical `changelog.message`. §6 routes those rows to the `field='legacy'`
passthrough automatically (the reconstructed structured message won't byte-match), so
host-form divergence is self-correcting — no special-casing needed here.

---

## 4. Seed confirmed statuses & `*_since` mapping

Production's current per-dimension statuses become the new entities' **seed confirmed
values**. Seeded values ARE confirmed values (03-state-machine.md — first-confirmation
rule): the anti-flap N-consecutive machine governs the first post-cutover divergence, and
a real flip publishes an ordinary changelog entry once confirmed.

### 4.1 The seed UPDATE

For every production `domain` row whose `Canonicalize(site)` resolved to `id = map[h]`,
UPDATE the new `domain` row:

```sql
UPDATE domain SET
  base_status = $base,   base_since = COALESCE($ts_base, $T0),
  www_status  = $www,    www_since  = COALESCE($ts_www,  $T0),
  ns_status   = $ns,     ns_since   = COALESCE($ts_ns,   $T0),
  mx_status   = $mx,     mx_since   = COALESCE($ts_mx,   $T0),
  updated_at  = now()
WHERE id = $id
  AND base_status IS NULL AND www_status IS NULL       -- only seed a still-unconfirmed
  AND ns_status IS NULL   AND mx_status IS NULL;        --   entity (idempotent re-run guard)
```

- `$base/$www/$ns/$mx` = production `base_domain/www_domain/nameserver/mx_record`, cast to
  `ipv6_status` (guaranteed legal by the §2 precondition-3 gate).
- `$ts_*` = production `ts_base_domain/ts_www_domain/ts_nameserver/ts_mx_record`.
  `$T0` = the migrate run's start timestamp (a single value for the whole run), used when
  a production `ts_*` is NULL — a seeded confirmed value must have a non-NULL `*_since`
  so the R3 timestamp-key mapping (07-api.md — §2.8 R3) never regresses.
- `base_pending`, `www_pending`, `ns_pending`, `mx_pending` stay NULL and the
  `*_pending_count` columns stay 0 (schema defaults; the UPDATE does not touch them):
  a seed carries no in-flight candidate.
- The WHERE guard (`… IS NULL`) makes the seed idempotent and prevents clobbering a value
  the crawler already confirmed between phase 3 and the migrate run (the crawler is the
  authority for anything it has since observed; migrate only fills genuinely-unseeded
  entities). Entities created in §3.1 step 4 (never crawled, all statuses NULL) always match.

### 4.2 Value mapping

Production stores exactly `{supported, unsupported, no_record}` (§2 gate). These map
1:1 onto the identically-named `ipv6_status` values. Production never produced
`not_applicable`; the new model may confirm `www = not_applicable` post-cutover, but no
seed value is ever `not_applicable`. A seeded `www_status = 'no_record'` is legal and
handled everywhere (classification rule 4 lists it; `legacyStatus` renders it, 07-api.md
— §2.8 R1).

### 4.3 conn and resources seed NULL — and why v6_only is dropped

**Decision:** `conn_status`, `conn_since`, `resources_status`, `resources_since` are
**not seeded** — they stay NULL (never confirmed). Production's `v6_only` column is
**not** read: the new `conn` dimension is a stricter, reachability-based check
(01-engine.md; design §2.8) that disagrees with production's `v6_only` by construction,
so importing it would seed values the new crawler would immediately contradict. Leaving
conn/resources NULL lets each entity's first definitive post-cutover observation of those
dimensions commit immediately with `old_value` NULL (03-state-machine.md — first
confirmation writes no changelog row), which is exactly what suppresses a changelog flood
at cutover (§4.5).

**Decision:** seed statuses come **exclusively** from production `domain`.
`campaign_domain` status columns are ignored: they were a duplicate crawl of the same
host, and the unified entity uses the main crawl's values. A campaign-only host (present
in `campaign_domain` but never in production `domain`) is created in §3.1 step 4 with all
statuses NULL and confirms its own status on its first post-cutover scan — the same
one-cycle gap conn/resources have, already documented.

### 4.4 Classification recompute

Immediately after the seed UPDATE, recompute the materialized classification for every
seeded entity from its confirmed values, using the 03-state-machine.md truth table
(single implementation — call the same `classify()` the crawler commit uses):

```sql
-- pseudo: for each seeded entity, set classification, class_flags, gold from
-- (base_status, www_status, ns_status, mx_status, conn_status=NULL, resources_status=NULL)
UPDATE domain SET classification = $c, class_flags = $flags, gold = $gold
WHERE id = $id;
```

**Consequence (must be documented in the OPEN-6 methodology-v2 note):** because
`conn_status` is NULL at seed, truth-table rule 4 (hero requires `conn = supported`)
can never fire for a seeded entity. Every `base_status = 'supported'` seed classifies as
**`partial`**, not `hero`, until the crawler confirms `conn` (≈ one crawl cycle, ~1 day).
Therefore the day-0 `heroes` count (§9) is near-zero and rises over the first day as conn
confirms. This is the intended, announced behavior of the cutover note (§4.5), not a bug;
`class_flags` are still fully derived at seed (`www_missing`, `ns_missing`,
`mail_missing` fire on confirmed `unsupported`), and `broken_v6`/`resources_v4only` stay
off until conn/resources confirm.

### 4.5 The no-changelog-flood cutover note (restated normatively)

Seeding writes statuses **directly into the `d_status` columns**, not through the commit
machine, so the seed itself emits **zero** changelog rows. Post-cutover, the first
definitive observation of a still-NULL dimension (conn, resources, and any dimension of a
§3.1-step-4-created or campaign-only entity) commits immediately with `old_value` NULL and
therefore writes **no** changelog row (03-state-machine.md — first-confirmation rule).
Net effect at cutover:

- **No changelog flood.** The public `/changelog` feed shows only the transformed
  history archive (§6) plus genuine post-cutover transitions.
- Detail pages show `conn`/`resources` (and any unseeded dimension) as unconfirmed for up
  to one crawl cycle (~1 day) after launch. The legacy API renders unconfirmed dimensions
  as `"no_record"` (07-api.md — §2.8 R1 `legacyStatus(nil) = "no_record"`), so the
  frontend degrades gracefully.

---

## 5. Campaign & `campaign_domain` migration (identity & shortuuid preservation)

**Decision:** campaigns and their memberships are **not** imported from the production DB.
Campaign identity is migrated through the YAML repo: the 28 campaign YAML files already
carry their production `uuid` values (design §7.2), and phase 1 runs
`v6ctl campaign sync --adopt-unknown-uuids` (06-ingest.md — §3; the `--adopt-unknown-uuids`
flag is used *exactly once*, here, and inserts each campaign with the file's uuid). This
establishes the new `campaign` rows (uuid preserved) and re-derives `campaign_domain`
membership by joining synced YAML domains onto the shared `domain` table.

**Why this preserves shared campaign links.** The API encodes `campaign.uuid` with the
frozen shortuuid codec (07-api.md — §2.7; `lithammer/shortuuid/v4` `DefaultEncoder`,
22-char base57, deterministic). Because the canonical `uuid` is byte-preserved from
production through the YAML, every previously-shared `/campaign/{token}/...` URL re-encodes
to the **same** 22-char token and keeps resolving. Migrate performs no uuid re-encoding
and touches no campaign-identity column — it only *reads* `campaign`/`campaign_domain`
(to resolve `campaign_changelog` hosts, §6) after §2 precondition-2 confirms they exist.

**`campaign_id` is dropped on changelog import.** Production `campaign_changelog` carries
`campaign_id`, but the new `changelog` table has no campaign column (05-schema.md). The
campaign changelog feed (07-api.md — §3.12, `GET /changelog/campaign`) re-derives campaign
membership by joining `changelog.domain_id → campaign_domain → campaign` at read time. A
historical campaign-changelog row whose entity is no longer a member of that campaign
simply won't appear in that campaign's feed (it still appears in the global feed if the
entity qualifies) — accepted, and a direct consequence of unifying to one entity per host.

---

## 6. Changelog history transform (the credibility archive)

The site's changelog is its credibility surface; every historical entry must render
byte-identically on the frozen frontend. The transform reverses production's message
strings back into the new field-level `(field, old_value, new_value)` model, with a
verified escape hatch for anything unmappable.

### 6.1 Sources

- Production `changelog` (`id, ts, domain_id, message, ipv6_status`). `domain_id`
  references production `domain.id`; resolve `production.domain.site → Canonicalize →
  map[h]` (§3) to get the new `domain.id`.
- Production `campaign_changelog` (`id, ts, domain_id, campaign_id, message,
  ipv6_status`). Here `domain_id` references `campaign_domain.id`; resolve
  `campaign_domain.id → campaign_domain.site → Canonicalize → map[h]`. `campaign_id` is
  **dropped** (§5).

Both source streams feed the **same** transform and the **same** new `changelog` table.
A row whose host fails resolution (§3.1) is skipped and counted under `resolve_failures`.

### 6.2 The reverse-map (prefix table, www-variant/longest first)

The forward render is 07-api.md — §3.11 `renderChangelog(field, old, new, host)`. The
reverse-map inverts it. Each production message pattern implies `(field, old, new)`. The
map is applied by **reconstructing the candidate forward message with the resolved
entity's canonical host `h` and testing exact string equality against the stored
message** — i.e. the reverse-map is `renderChangelog` run over every candidate
`(field, old, new)` triple, matched against the production `message`. Candidates are
tried **www-variant first, then base**, because `IPv6 enabled for www.{h}` and
`IPv6 enabled for {h}` share the `IPv6 enabled for ` prefix and only the www test
(longer) disambiguates.

| Production message (reconstructed with `h`) | field | old → new (canonical) |
|---|---|---|
| `IPv6 enabled for www.{h}` | www | unsupported → supported |
| `IPv6 lost for www.{h}` | www | supported → unsupported |
| `IPv4-only for www.{h}` | www | no_record → unsupported |
| `No DNS records found for www.{h}` | www | unsupported → no_record |
| `IPv6 enabled for {h}` | base | unsupported → supported |
| `IPv6 lost for {h}` | base | supported → unsupported |
| `IPv4-only for {h}` | base | no_record → unsupported |
| `No DNS records found for {h}` | base | unsupported → no_record |
| `IPv6 enabled nameserver for {h}` | ns | unsupported → supported |
| `Nameservers degraded to IPv4-only for {h}` | ns | supported → unsupported |
| `IPv4-only nameservers for {h}` | ns | no_record → unsupported |
| `No NS records found for {h}` | ns | unsupported → no_record |
| `IPv6 enabled MX records for {h}` | mx | unsupported → supported |
| `MX records degraded to IPv4-only for {h}` | mx | supported → unsupported |
| `IPv4-only MX records for {h}` | mx | no_record → unsupported |
| `No Mail records found for {h}` | mx | unsupported → no_record |

Match order within a field-group (to keep the exact-equality test unambiguous): the
`No … records`/`No DNS records` "any→no_record" pattern and the `IPv4-only` pattern are
distinct strings, so ordering only matters across www-vs-base (www first). The strings are
verbatim from production `crawl.go:416-495` (verified live against the reference repo).

### 6.3 Canonical ambiguous-old rule

Production collapsed two source transitions into one string in two places:
- `unsupported→supported` and `no_record→supported` both render `IPv6 enabled …`.
- `supported→no_record` and `unsupported→no_record` both render `No … records …`.

The stored `message` cannot tell which `old` value produced it. The importer canonically
records **`old = 'unsupported'`** in both ambiguous cases (as the table above already
shows: `IPv6 enabled` → `old=unsupported`; `No … records` → `old=unsupported`). This is
**render-safe by construction**: the forward ladder (07-api.md — §3.11) emits the
identical string for *every* `old` value the original row could have carried
(`unsupported→supported` and `no_record→supported` are one row of the ladder; `any→no_record`
is one row), so the byte-equality gate (§6.5) passes regardless of the true original
`old`.

### 6.4 Cross-check, insert, and the `field='legacy'` escape hatch

For each source row, after resolving `h` and finding a matched `(field, old, new)`:

1. **Cross-check:** the matched `new` value must equal the row's stored `ipv6_status`.
   (Production stored the new value of the changed field in `ipv6_status`.)
2. **Structured insert** when the pattern matched AND the cross-check passed AND
   `ipv6_status ∈ {supported, unsupported, no_record}`:

   ```sql
   INSERT INTO changelog (domain_id, ts, field, old_value, new_value)
   VALUES ($id, $ts, $field, $old, $new)
   ON CONFLICT (domain_id, ts, field) DO NOTHING;   -- PK-collision rule, §6.6
   ```
3. **Legacy path** when *any* of: no pattern matched, the cross-check failed, or
   `ipv6_status` is outside the three legacy statuses. Preserve the row verbatim:

   ```sql
   INSERT INTO changelog
     (domain_id, ts, field, old_value, new_value, legacy_message, legacy_status)
   VALUES ($id, $ts, 'legacy', NULL, NULL, $message, $ipv6_status)
   ON CONFLICT (domain_id, ts, field) DO NOTHING;
   ```

   The five legacy `/changelog*` endpoints admit `field='legacy'` rows into every feed
   the entity qualifies for (07-api.md — §3.12 coverage filter includes `OR c.field =
   'legacy'`) and render them verbatim (`message = legacy_message`, `ipv6_status =
   legacy_status`; 07-api.md — §3.11 legacy bypass). This is what makes phase-4's "old
   entries render identically" achievable **unconditionally** — an unmappable row is not
   dropped, it is passed through byte-exact.

`old_value` on structured rows is never NULL (§6.3 always supplies a concrete `old`);
`old_value`/`new_value` are NULL only on `field='legacy'` rows, satisfying the
`changelog_legacy_chk`/`changelog_old_value_chk`/`changelog_new_value_chk` constraints
(05-schema.md).

### 6.5 PK-collision rule

The new PK is `(domain_id, ts, field)`. Collisions occur when (a) the same change appears
in both `changelog` and `campaign_changelog` for the same unified entity at the same `ts`
and field, or (b) two legacy rows share `(domain_id, ts)` and both land as `field='legacy'`.
Rule:

1. First write wins via `ON CONFLICT (domain_id, ts, field) DO NOTHING`.
2. If the colliding rows are **value-identical** (same field/old/new, or same
   legacy_message/legacy_status), the `DO NOTHING` is correct — keep the one already
   present.
3. If they **differ** (a genuine second distinct change at the identical microsecond, or
   two distinct legacy messages at the same `ts`), the importer detects the conflict on
   the second row (0 rows affected) and **retries with `ts` bumped by +1 microsecond**,
   repeating until the insert succeeds. Display truncates to seconds (07-api.md — §2.10),
   so the ordering/appearance impact is nil.

**Decision:** the importer sorts source rows by `(ts, id)` before insert so the +1µs bump
is deterministic and reproducible across re-runs (a re-run re-derives the identical `ts`
sequence; idempotent under the PK).

### 6.6 Byte-equality verification gate (`--verify-changelog`)

The parity gate for the changelog archive. After the transform, for **every** imported
`changelog` row:

- Structured row (`field ∈ {base,www,ns,mx}`): compute
  `renderChangelog(field, old_value, new_value, host)` (07-api.md — §3.11, the *same*
  function the API serves) and assert the resulting `(message, ipv6_status)` **byte-equals**
  the original production `(message, ipv6_status)` of the source row it came from.
- Legacy row (`field='legacy'`): assert `(legacy_message, legacy_status)` byte-equals the
  original `(message, ipv6_status)` (trivially true — it was copied verbatim).

Any mismatch fails the gate (exit 1), prints the offending `(domain_id, ts)` and both
strings, and blocks cutover. This gate is a phase-4 parity gate (§11). Because the transform
routes anything that can't round-trip to the verbatim legacy path, a correctly-implemented
transform passes unconditionally; a gate failure means the reverse-map or the forward
ladder drifted and must be fixed before the DNS switch.

### 6.7 conn/resources/not_applicable historical rows

The transform only ever emits `field ∈ {base,www,ns,mx}` or `field='legacy'`. Production
never had conn/resources changelog entries, so none are imported. The legacy `/changelog*`
endpoints already exclude `conn`/`resources`/`not_applicable` rows (07-api.md — §3.12),
and none exist in the imported set anyway.

---

## 7. Per-scan history import (trailing window)

Import the trailing `--history-window` (default 90 days) of production per-scan history
into the slim `scan` hypertable, so per-domain graphs and the `GET /domain/{domain}/log`
endpoint (07-api.md — §3.7) have history from day one.

### 7.1 Sources and unification

- Production `domain_log` (`domain_id → production domain.site`, `time`, `data`).
- Production `campaign_domain_log` (`domain_id → campaign_domain.site`, `time`, `data`).

Both resolve through the §3 map to the unified new `domain.id`. A host that was both a
Tranco domain and a campaign domain has two production log streams that merge into one new
`scan` series; the PK conflict rule (below) dedupes identical timestamps.

Selection (per source, on the source DB):

```sql
SELECT domain_id, time, data FROM domain_log
WHERE time >= $window_start        -- $T0 - migrate.history_window
ORDER BY domain_id, time;
```

`$window_start = $T0 - --history-window`. The production dump is retained, so a later
deeper backfill re-runs with a larger window (OPEN-7).

### 7.2 Row transform

Production `data` JSONB is `{base_domain, www_domain, nameserver, mx_record}` (verified
against `whynoipv6/cmd/v6manage/cmd/crawl.go` `DomainLog`). Map each to a new `scan` row:

```sql
INSERT INTO scan
  (domain_id, ts, base, www, ns, mx, conn, resources,
   dnssec, ptr, smtp, parity, latency_v4_ms, latency_v6_ms,
   classification, country_id, asn_id)
VALUES
  ($id, $time,
   $base, $www, $ns, $mx,           -- data.* cast to observation
   'not_applicable', 'not_applicable',  -- conn, resources: never measured historically
   NULL, NULL, NULL, NULL, NULL, NULL,  -- informational dims + latency: absent
   $class,                              -- §7.3
   $country_id, $asn_id)                -- the entity's current attribution (denormalized)
ON CONFLICT (domain_id, ts) DO NOTHING;
```

- `$base/$www/$ns/$mx` = the JSONB values (`supported|unsupported|no_record`), cast to the
  `observation` enum (all three are legal observation values). A `data` object missing a
  key or holding a non-observation string → the whole row is skipped and counted under
  `scan_skips` (defensive; production always wrote the four keys).
- `conn` and `resources` are set to `'not_applicable'` (they were never measured; the
  columns are NOT NULL). `dnssec/ptr/smtp/parity` and both latency columns are NULL
  (nullable; no historical source).
- `$country_id/$asn_id` = the new entity's current `country_id`/`asn_id` (from phase-1
  GeoIP attribution). Production per-scan logs carried no per-scan attribution, and these
  columns exist only to let the `scan_daily_adoption` cagg slice without a JOIN
  (05-schema.md; design §4.7).
- `scan_detail` is **not** imported: production's thin `domain_log` has no engine
  `Details` evidence, and `scan_detail` is a 90-day debugging surface that fills from
  post-cutover scans. **Decision:** the fat `scan_detail` hypertable starts empty at
  cutover.

### 7.3 Classification stamp on historical rows

`scan.classification` is NOT NULL. Production had no per-scan classification. **Decision:**
stamp each imported scan row with `classify(base, www, ns, mx, conn='not_applicable',
resources='not_applicable')` computed from **that row's own** observations via the
03-state-machine.md truth table — not the entity's seeded (present-day) class, so past
rows are never back-stamped with a future state. With conn forced `not_applicable`, the
hero branch is unreachable, so imported historical rows classify only as
`unknown`/`inactive`/`sinner`/`partial`. This value is consumed only by the
measurement-flavored `scan_daily_adoption` cagg, whose DICTIONARY entry already states it
counts observations and is not comparable to the confirmed-state product stats (design
§4.7) — so the depressed historical `heroes` count in the cagg is expected and documented.

### 7.4 Volume & batching

~1M domains × up to 90 days ≈ tens of millions of rows. Import via `pgx` `CopyFrom` into
the `scan` hypertable in per-day (or per-`domain_id`-range) batches to bound memory; the
`ON CONFLICT (domain_id, ts) DO NOTHING` dedup requires an INSERT path, so use a
staging-table + `INSERT … SELECT … ON CONFLICT` pattern per batch rather than raw
`CopyFrom` (which cannot express `ON CONFLICT`). Batches are independent; a crash resumes
by re-running (idempotent under the PK). This sub-phase is the longest; the runbook (§10)
budgets for it.

---

## 8. `top_shame` import

The curated shame list is editorial data with no automatic write path (05-schema.md;
06-ingest.md — §7 `v6ctl shame`). Migrate imports the production list once.

- **Source:** the `top_shame` table (`site TEXT`) in the **restored production dump** —
  not the hardcoded `02_data.up.sql` seed (the live table is authoritative if the
  maintainer edited it). As of the audit it holds 12 hosts: twitter.com, twitch.tv,
  ebay.com, imgur.com, imdb.com, wordpress.com, github.com, paypal.com, stackoverflow.com,
  soundcloud.com, nytimes.com, w3schools.com.
- For each `site`, resolve `Canonicalize(site) → map[h]` (§3) and insert:

  ```sql
  INSERT INTO top_shame (domain_id)
  SELECT d.id FROM domain d WHERE d.host = $1
  ON CONFLICT (domain_id) DO NOTHING;    -- reason stays NULL: production has no reason column
  ```
- A site with **no matching domain row** (fell out of Tranco top-1M between dump and
  import, and §3 did not create it because it wasn't in production `domain`/`campaign_domain`
  — but note §3 *does* fold `top_shame.site` into the map, so a create normally happens
  first; this arm only fires if the site failed Canonicalize) is **logged as a warning and
  skipped** — it must not fail the migration. The operator re-adds it later via
  `v6ctl shame add` if desired.
- **Run order:** after §3 (entity resolution guarantees the FK target exists) and after
  §4 (so the entity has a classification; the read-side `/domain/topsinner` filter
  (07-api.md — §3.5) hides non-sinners at query time).
- Entities whose domains are **no longer sinners** (e.g. github.com, now IPv6-enabled) are
  imported anyway; the topsinner read filter hides them, exactly as production did. **Do
  not prune on import** — the row is a durable editorial pick; visibility is computed at
  read time.

---

## 9. Day-0 stats snapshot (required)

The `/metric/overview` endpoint (07-api.md — §3.17) reads the latest `stats_global_daily`
row and every `/stats/*` and `/metric/asn`, `/country` surface reads the `stats_*` tables
and the `country`/`asn` counter columns. If those are empty at cutover, the API returns
nothing. Per OPEN-6 ("serve migrated seed values immediately"), migrate's **final**
sub-phase writes day-0 rows.

**Decision:** migrate's last step invokes the **daily-tick stats snapshot and
country/ASN counter recompute** (04-lifecycle-scheduling.md — §9 steps 2-3) once against
the new DB — equivalent to running
`v6ctl stats recalc`. This writes a `day = $T0::date` row into `stats_global_daily`,
`stats_country_daily`, `stats_campaign_daily`, and `stats_asn_daily`, and runs the ported
`update_country_metrics`/`update_asn_metrics` counter recomputes onto the `country`/`asn`
rows — all computed over confirmed seed state via the visibility predicate
(`rank IS NOT NULL AND NOT disabled`; design §4.7). Reusing the tick's snapshot code (not a
bespoke migrate query) guarantees day-0 numbers are computed identically to every
subsequent day.

**Consequence:** day-0 `stats_global_daily.heroes` (and `gold`, `top_heroes` insofar as it
requires confirmed www) reflect the §4.4 reality — `heroes ≈ 0` because no seeded entity
has confirmed `conn`. The count rises to its true value within ~1 crawl cycle. This is
covered by the OPEN-6 methodology-v2 public note (design OPEN-6), the single public
changelog of all deliberate metric shifts; it must not be mistaken for a snapshot bug.

Day-0 rows are the reason `/metric/overview` never 500s on first boot (07-api.md — §3.17
requires a `stats_global_daily` row to exist).

---

## 10. Cutover runbook (order of operations)

Executed by the operator; the DB-write steps are the `v6ctl migrate-import` command, the traffic
steps are the 09-ops.md Ansible/nginx deploy. Assumes phases 1–3 are complete on the new
stack (schema migrated, Tranco ingested, campaigns synced, crawler soaked for ≥3 full
passes, backups live per 09-ops.md).

**Phase 4 cutover sequence:**

1. **Freeze the old crawler.** Stop the production crawler (`v6manage crawl` timer/
   service off). No new production statuses or changelog rows after this instant — the
   import window is now closed and consistent. The old **API stays up** (still serving the
   live frontend).
2. **Snapshot & restore the production DB** to the scratch instance that backs
   `--source-dsn` (a dump taken *after* the crawler freeze, so it is the final production
   state). Retain the dump (OPEN-7 later-backfill).
3. **Final import.** Pause the **new** crawler (stop its service, so no commit or daily
   tick races the seed — §1.3), then run `v6ctl migrate-import --source-dsn <restored> --dsn
   <new> --history-window 90d --verify-changelog`. Order inside the command is fixed: §3 entity
   resolution → §4 seed statuses + classification → §5 (no-op; campaigns already synced,
   precondition only) → §6 changelog transform → §7 per-scan history → §8 top_shame → §9
   day-0 snapshot → §6.6 `--verify-changelog` gate. `--dry-run` first to review the
   counts report (resolve_failures, seeded, changelog structured/legacy split,
   scan rows, shame hits/misses).
4. **Restore-drill cutover gate** (09-ops.md — restore drill): restore the *new* DB's
   latest backup to a scratch instance; assert `SELECT count(*) FROM changelog` matches
   the just-imported count, and the API binary starts against the restored DB with
   `GET /changelog` returning rows. A backup that has not been restore-tested is assumed
   broken.
5. **Parity gates** (§11): run the golden-fixture parity suite (10-testing.md harness) and
   the frontend E2E (playwright) against the new API pointed at the migrated DB, with the
   frontend still served from staging. **All gates in §11 must be green.** Any red gate
   halts cutover — fix and re-run migrate (idempotent) or the API, do not switch.
6. **Resume the new crawler, then DNS / nginx switch.** Restart the new crawler (paused in
   step 3) so it begins confirming conn/resources immediately. Then point
   `api.whynoipv6.com` (and the site) at the new API per the
   09-ops.md nginx/Ansible deploy. The frontend is unchanged (frozen); only the upstream
   moves. Watch the ops webhook + Grafana for error-rate and latency regressions.
7. **Soak & confirm.** Over the first ~24h the crawler confirms conn/resources; `heroes`/
   `gold` rise to their true values (§4.4). Verify the next daily tick (03:30 UTC) writes a
   day-1 `stats_*` row and that `/changelog` continuity holds (old entries render
   identically — the §6.6 gate already proved this offline).
8. **Decommission the old backend** only after ≥1 week of clean operation on the new stack
   (rollback window, §12). Keep the retained production dump indefinitely.

**Ordering invariants (never reorder):** freeze-before-dump (step 1 before 2) guarantees a
consistent import; verify-before-switch (step 5 before 6) guarantees the frozen frontend
keeps working; the old API stays up until step 6, so there is no public downtime.

---

## 11. Parity gates (phase-4 gate definitions)

These are the acceptance criteria that must be satisfied **before** the DNS switch (§10
step 5/6). Fixture mechanics and the golden-capture harness live in 10-testing.md; this
section defines *which* gates exist and *what* each asserts. A gate is red if any fixture
in it fails; all gates must be green to cut over.

**Gate G1 — changelog byte-equality (§6.6).** For every imported `changelog` row,
`renderChangelog(...)` (or the legacy passthrough) byte-equals the original production
`(message, ipv6_status)`. Run by `v6ctl migrate-import --verify-changelog`. This is the archive
credibility gate.

**Gate G2 — legacy endpoint golden parity.** Recorded production responses vs the new API,
for every endpoint in the 07-api.md legacy surface, **full-fidelity byte match** EXCEPT the
two documented-divergence endpoints in G3. Includes: `GET /` , `/ip`, `/domain/heroes`,
`/domain/topsinner`, `/domain/{domain}`, `/domain/{domain}/log`, `/country`,
`/country/{code}`, `/country/{code}/heroes`, all five `/changelog*` feeds, `/campaign`,
`/campaign/{uuid}`, `/campaign/{uuid}/{domain}`, `/campaign/{uuid}/{domain}/log`,
`/metric/overview`, `/metric/asn`. Assert envelope/no-envelope, field names/types,
shortuuid tokens (07-api.md — §2.7 vectors), the R1–R5 legacy serialization
(07-api.md — §2.8), and the zero-result 404-vs-`[]` map (07-api.md — §2.11) each with its
byte-exact body.

**Gate G3 — membership-divergence direction (not row-set equality).** For `GET /domain`
(sinners) and `GET /country/{code}/sinners`, assert **response shape and ordering**
(rank ascending) against production captures, and assert the documented divergence
direction `new_members ⊆ old_members` on each captured page (every domain the new backend
lists was also listed by production; the reverse need not hold, because www-only offenders
moved to `/domain/almost`). Do **not** assert row-set equality on these two.

**Gate G4 — synthetic membership (proves the negative production data can't).** Seed one
entity with confirmed `base=supported, www=unsupported` and one with `base=unsupported`.
Assert the first appears in `/domain/almost` and NOT in `/domain`; the second appears in
`/domain` and NOT in `/domain/almost`. Repeat for the country-scoped pair via the
fixture's country. (Fixtures owned by 10-testing.md.)

**Gate G5 — synthetic legacy-serialization branches.** Production captures can't exercise
R1's `not_applicable`/NULL→`"no_record"` projection or R2's error/inconsistent filter
(production never produced those). Synthetic fixtures assert `legacyStatus(NULL) =
"no_record"`, `legacyStatus(not_applicable) = "no_record"`, and that `GET
/domain/{domain}/log` excludes error/inconsistent scan rows (07-api.md — §2.8 R1/R2).

**Gate G6 — frontend E2E (playwright).** The frozen production frontend, pointed at the
new API, renders the domain list, a detail page, a campaign page, and the changelog feed
with **zero visual diffs** vs production, and old changelog entries render identically.

**Gate G7 — restore-drill (§10 step 4).** The new DB's latest backup restores to a scratch
instance; `SELECT count(*) FROM changelog` matches the imported count and the API starts
against the restored DB with `GET /changelog` returning rows.

---

## 12. Rollback plan

Cutover is DNS/upstream-only (§10 step 6) and additive on the data side; rollback is
therefore fast and lossless for the old stack.

**Trigger:** any of — a red parity gate discovered post-switch, an error-rate/latency
regression on the new API that can't be hotfixed within the operator's tolerance, or data
corruption in the migrated DB.

**Procedure:**
1. **Revert the DNS/nginx upstream** to the old backend (09-ops.md; the old API was left
   running until §10 step 6, or is restarted from its still-present service unit). The
   frozen frontend works against the old API unchanged — the whole compatibility contract
   exists to make this true.
2. **Re-freeze nothing / thaw the old crawler** only if the rollback is expected to last
   more than a crawl cycle — otherwise leave it frozen to keep the door open for a quick
   re-cutover. The old production DB was never mutated by migrate (migrate reads a
   *restored* dump, never the live production server — §1.2), so the old stack is exactly
   as it was at the §10 step-1 freeze.
3. **Diagnose on the new stack offline.** The new DB, crawler, and API keep running in the
   background (not public); fix forward, re-run `v6ctl migrate-import` (idempotent) if the fault
   was in the import, re-run the §11 gates, and re-attempt §10 step 6 when green.
4. **No data migration is undone.** Because the new DB is a separate system and the old DB
   is untouched, rollback never involves reversing writes — it is purely a traffic switch.

**Rollback window:** keep the old backend deployable and the retained production dump for
**≥1 week** after cutover (§10 step 8). Decommission only after clean steady-state operation.

---

## 13. Deliverables & config summary

**Go packages / files:**
- `cmd/v6ctl/migrate_import.go` — the `migrate-import` cobra command, flags (§1.3), advisory-lock
  acquisition, sub-phase orchestration, the `--dry-run` counts report, and the
  `--verify-changelog` gate wiring.
- `internal/migrate/resolve.go` — the §3 host→id map (Canonicalize + lookup + §3.1 step 4
  create-missing).
- `internal/migrate/seed.go` — the §4 seed UPDATE + §4.4 classification recompute (calls
  the shared `classify()`).
- `internal/migrate/changelog.go` — the §6 reverse-map, cross-check, legacy escape,
  PK-collision bump, and the §6.6 byte-equality gate (reuses `internal/api.renderChangelog`).
- `internal/migrate/history.go` — the §7 per-scan history importer (batched, idempotent).
- `internal/migrate/shame.go` — the §8 `top_shame` import.
- `internal/migrate/snapshot.go` — the §9 day-0 snapshot invocation (calls the daily-tick
  stats-snapshot function).

**Config keys** (registry: 09-ops.md): `migrate.source_dsn` (string, no default),
`migrate.history_window` (duration, default `2160h`).

**Concurrency:** no new advisory-lock key — migrate runs with the new crawler paused
(§10 step 3); writes are idempotent/guarded (§1.3).

**Parity gates:** G1–G7 (§11); all green is the precondition for §10 step 6 (DNS switch).
