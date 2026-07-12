# 09 — Operations, Packaging & the Config Registry

_Status: Round 3.0 — API redesign folded in (docs/api-design-research.md, decisions 2026-07-09): clean root API, keyset pagination, RFC 9457, no legacy compat, no history import._

**Purpose:** Everything a single maintainer needs to build, configure, deploy, and
run the three binaries on his own VMs. This file is the **single source of truth for
every configuration key** across `api`, `crawler`, and `v6ctl` (§2), and it owns all
deploy artifacts: systemd units and timers, nginx vhosts, Unbound deployment + stats,
the docker-compose dev environment, backup & restore, the IPinfo Lite lifecycle,
liveness heartbeats + Grafana alert rules, logging conventions, and the
Makefile/`.golangci.yml`/CI gate.

**Deliverables (Go packages, files, and artifacts this file governs):**
- `internal/config` — viper loader for all three binaries (the two-tier model, §1).
- `internal/notify` — ops-webhook + healthchecks.io ping client (used by crawler and v6ctl).
- `internal/lock` reuse — advisory-lock helper (defined in 04-lifecycle-scheduling.md; this file only deploys the binaries that use it).
- `cmd/{api,crawler,v6ctl}/main.go` — the `slog` handler installation and startup config summary (§13).
- `deploy/systemd/*.{service,timer}` — all unit files (§4, §5).
- `deploy/nginx/api.whynoipv6.com.conf` — the API + datasets vhost (§7).
- `deploy/unbound/` — `unbound@.service`, `unbound-base.conf`, per-instance drop-ins (§8).
- `deploy/pgbackrest/` — `pgbackrest.conf`, `whynoipv6-export.sh` logical export (§10).
- `v6ctl geoip update` systemd service + daily timer, `IPINFO_TOKEN` from vault (§11).
- `deploy/grafana/alerts.yaml` — provisioned alert rules (§12).
- `Makefile`, `compose.yaml` at the monorepo root; `backend/.golangci.yml` with the Go module (see 00-overview.md §4); CI workflow (§14). The root `Makefile` orchestrates `cd backend && …`; all `make`/`go`/`sqlc`/lint invocations run from `backend/`.

**Companion files an implementer of this one must have open:**
- `00-overview.md` — the canonical sizing constants (`WORKER_SLOTS`, scan rate, resolver-load rows) cited here by name.
- `05-schema.md` — table/column names referenced here (`unbound_stats`, `crawler_metrics`, `changelog`, `domain`, `tranco_import`, `stats_*`, `timescaledb_information` views). **No DDL is restated here.**
- `04-lifecycle-scheduling.md` — the daily-tick step list, the advisory-lock registry, and the scheduling/lifecycle keys whose registry entries live here.
- `07-api.md` — the HTTP server baseline (§1 Server baseline: §1.3 CORS, §1.4 headers, §1.6 timeouts/shutdown, §1.7 middleware order), the `GET /datasets` contract (§7), and the live-check rate-limit keys.
- `01-engine.md` / `02-observation-model.md` — the resolver/preflight/consensus keys whose registry entries live here, and the fact that consensus provider addresses are **package constants, not config**.

---

## 1. Configuration model (viper conventions + secrets)

**Decision — two-tier viper model.** The design introduces config in two shapes: a
small set of UPPERCASE deployment keys (`API_LISTEN`, `GEOIP_PATH`, …, following
production's `app.env` convention) and a larger set of nested tuning sections
(`claim:`, `consensus:`, `lifecycle:`, …, shown as YAML in the design). Both are served
by one loader with a single precedence chain, identical in all three binaries:

```
compiled-in default  <  optional YAML file  <  environment variable
(viper.SetDefault)      (/etc/whynoipv6/config.yaml)   (viper.AutomaticEnv)
```

Loader construction (`internal/config`, one exported `Load(binary string) (*Config, error)`):

```go
v := viper.New()
registerDefaults(v)                       // every key in §2 gets a SetDefault (this is what
                                          //   makes AutomaticEnv resolve nested keys)
v.SetConfigName("config")
v.SetConfigType("yaml")
v.AddConfigPath("/etc/whynoipv6")
v.AddConfigPath(".")                      // dev
if err := v.ReadInConfig(); err != nil {
    var nf viper.ConfigFileNotFoundError
    if !errors.As(err, &nf) { return nil, fmt.Errorf("read config: %w", err) }
    // absent config file is normal in production — defaults + env only
}
v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
v.AutomaticEnv()
```

- **Env-var name = the dotted key path, upper-cased, `.`→`_`.** So
  `consensus.per_provider_qps` → `CONSENSUS_PER_PROVIDER_QPS`, `worker_slots` →
  `WORKER_SLOTS`, `crawler.resources.enabled` → `CRAWLER_RESOURCES_ENABLED`. The
  UPPERCASE deployment keys (`API_LISTEN`, `DATABASE_URL`, `GEOIP_PATH`,
  `DATASETS_DIR`, `PUBLIC_BASE_URL`, `LOG_LEVEL`) map to themselves. The registry (§2) gives the exact
  env name for every key.
- **Production ships no YAML.** The deploy artifact set is exactly the three binaries
  (§3). Production runs on compiled-in defaults plus env overrides supplied by the
  systemd `EnvironmentFile`. `/etc/whynoipv6/config.yaml` exists only as an optional
  operator/dev convenience; when absent the loader proceeds silently.
- **List/struct keys.** `resolver.bulk_upstreams` (`[]string`) is env-overridable as a
  comma-separated value (`RESOLVER_BULK_UPSTREAMS="127.0.0.1:53,127.0.0.1:5353"`).
  `cadence.bands` (a list of `{min_rank,max_rank,every}` objects) is **YAML-only** — it
  cannot be set through a single env var; the default `[]` (uniform daily cadence)
  needs no override in production.

**Decision — canonical key form.** Config keys use the **top-level section names
exactly as the design doc's YAML blocks show them** (`claim.*`, `cadence.*`,
`recheck_*`, `anti_flap.*`, `consensus.*`, `checks.*`, `resolver.*`, `preflight.*`,
`worker_slots`, `service_detect.*`, `lifecycle.*`, `tranco.*`, `campaign.*`,
`live_check.*`, `ops.*`, `unbound_stats.*`, `badge.*`, `datasets.*`, `feed.*`,
`export.*`, `dns_provider.*`). The **single exception is
`crawler.resources.enabled`**, which the design and every other spec file spell with
the `crawler.` prefix — it is kept verbatim. (04-lifecycle-scheduling.md's §16 table
uses the same top-level spelling as this registry — `claim.batch_size`, `cadence.default`,
`worker_slots` — with `crawler.resources.enabled` as the only `crawler.`-prefixed key,
matching the design's YAML and files 01/02/03.)

**Secrets handling.** Three values are secrets and must never appear in the YAML file,
in logs, or in the git tree:

| Secret | Key | Supplied via |
|---|---|---|
| Postgres password | inside `DATABASE_URL` | systemd `EnvironmentFile=/etc/whynoipv6/env` (0640 root:whynoipv6), Ansible-vault templated |
| Ops-webhook URL (bearer path) | `ops.webhook_url` | same env file |
| healthchecks.io ping URLs | `ops.healthcheck_url`, `ops.healthcheck_tick_url` | same env file (one per crawler process, §5.5/§12) |

The IPinfo token and pgBackRest credentials are not backend config — they live in the
`v6ctl-geoip-update.service` environment and `/etc/pgbackrest/pgbackrest.conf`, both
Ansible-vault templated (§10, §11). The startup config summary (§13) redacts every secret: `DATABASE_URL` is
logged host+db only (`postgres://…@dbhost:5432/whynoipv6`), and webhook/ping URLs are
logged as `set`/`unset`, never their value.

**`DATABASE_URL` shape.** A libpq/pgx URL: `postgres://USER:PASS@HOST:5432/whynoipv6`.
Pool sizing is carried in the DSN itself (pgxpool honours `pool_max_conns`,
`pool_min_conns`, `pool_max_conn_lifetime` query params) — there is deliberately **no
separate pool-size config key**. Recommended production value:
`?pool_max_conns=32&sslmode=verify-full` on the crawler (which also needs a dedicated
connection per held advisory lock, 04-lifecycle-scheduling.md), `?pool_max_conns=16` on
the API. Required (no default); a missing/empty `DATABASE_URL` is a fatal startup error
in every binary.

---

## 2. Consolidated config registry (the single source)

Every configuration key across all three binaries. Durations are Go
`time.Duration` strings (`720h`, `10s`). "Owner" is the binary(ies) that read the key;
"From" is the spec file that introduces the key by name (its meaning is normative
there; the value/default is normative here). Constants that the design pins as
**package constants, not config** — consensus provider names/addresses/order, the
2s/4s consensus timeouts, canary interval, `LeaseReclaim=30m`, `PreflightFreshness=5m`,
tick time `03:30 UTC`, v6ctl lock wait `5m`, `checkpointEvery=1000` — are **not** in
this registry; they are recorded in their owning files and cited here only where a unit
needs them.

### 2.1 Global deployment keys (UPPERCASE env)

| Key / env var | Type | Default | Owner | From | Meaning |
|---|---|---|---|---|---|
| `DATABASE_URL` | string (pgx DSN) | — (required) | api, crawler, v6ctl | 05,06 | Postgres connection string; pool params in the DSN (§1). |
| `API_LISTEN` | string `host:port` | `[::1]:8080` | api | 07 §1.1 | HTTP bind; IPv6 loopback by design (nginx-fronted). Override to `:8080` only for dev. |
| `GEOIP_PATH` | string (dir) | `/var/lib/GeoIP` | crawler | 05,11 | Directory holding `ipinfo_lite.mmdb` (IPinfo Lite, country + ASN); hourly mtime check + atomic reader swap. |
| `DATASETS_DIR` | string (dir) | `/var/lib/whynoipv6/datasets` | api, v6ctl(export) | 07 §7.2 | Dataset snapshot root; API reads `manifest.json`, `v6ctl export` writes snapshots. |
| `PUBLIC_BASE_URL` | string (URL) | `https://api.whynoipv6.com` | api | 07 | Public origin for absolute Atom/JSON-Feed self-links (report §6.4) and any absolute dataset/manifest URLs (report §6.3); the API binds `[::1]:8080` behind nginx and cannot infer its own origin. |
| `LOG_LEVEL` | string enum `debug\|info\|warn\|error` | `info` (api, crawler) / `warn` (v6ctl) | all | 13 | slog level (§13). |

### 2.2 Crawler engine & scheduling (top-level sections; crawler binary)

| Key | env var | Type | Default | From | Meaning |
|---|---|---|---|---|---|
| `claim.batch_size` | `CLAIM_BATCH_SIZE` | int | `200` | 04 | `$1` of the frontier claim query. |
| `claim.empty_poll_interval` | `CLAIM_EMPTY_POLL_INTERVAL` | duration | `10s` | 04 | Sleep after a zero-row claim. |
| `claim.order` | `CLAIM_ORDER` | enum `rank\|age` | `rank` | 04 | Claim `ORDER BY` policy; `age` is the aging pressure valve. |
| `worker_slots` | `WORKER_SLOTS` | int | `64` | 04 | Concurrent domain slots **per process** (2 procs → 128 provisioned; sizing constant `WORKER_SLOTS`, 00-overview.md). |
| `cadence.default` | `CADENCE_DEFAULT` | duration | `24h` | 03,04 | Base per-domain cadence. |
| `cadence.bands` | (YAML only) | list `{min_rank,max_rank,every}` | `[]` | 03,04 | Per-rank-band cadence overrides. |
| `recheck_inconsistent` | `RECHECK_INCONSISTENT` | duration | `2h` | 03,04 | Pull-in lane for `inconsistent` base/www. |
| `recheck_error` | `RECHECK_ERROR` | duration | `6h` | 03,04 | Pull-in lane for `error` base/www. |
| `recheck_backoff_max` | `RECHECK_BACKOFF_MAX` | duration | `720h` | 03,04 | Backoff cap (== 30d slow lane). |
| `anti_flap.min_confirm_spacing` | `ANTI_FLAP_MIN_CONFIRM_SPACING` | duration | `12h` | 03 | Minimum spacing between counted confirmations. |
| `preflight.probe_host` | `PREFLIGHT_PROBE_HOST` | string `host:port` | `one.one.one.one:443` | 01 | IPv6 self-preflight AAAA + tcp6 dial target. |
| `preflight.retry_interval` | `PREFLIGHT_RETRY_INTERVAL` | duration | `60s` | 04 | Sleep after a failed preflight before retrying. |
| `service_detect.indegree_threshold` | `SERVICE_DETECT_INDEGREE_THRESHOLD` | int | `100` | 04 | `resource_host.dependent_count` threshold for service-domain heuristic (b). |
| `resolver.bulk_upstreams` | `RESOLVER_BULK_UPSTREAMS` | []string `host:port` | `["127.0.0.1:53","127.0.0.1:5353"]` | 01 | Bulk resolver upstreams = the two local Unbound instances (§8). |
| `checks.max_ns_lookups` | `CHECKS_MAX_NS_LOOKUPS` | int | `4` | 01 | Per-host AAAA detail cap for `dns_ns_ipv6` (≤0 is a config error). |
| `checks.max_mx_lookups` | `CHECKS_MAX_MX_LOOKUPS` | int | `5` | 01 | Per-host AAAA detail cap for `dns_mx_ipv6`. |
| `crawler.resources.enabled` | `CRAWLER_RESOURCES_ENABLED` | bool | `true` | 02 | Resource-dependency dimension; **on by default** (the `ipv6_only` fold depends on it — ADR). `false` is an emergency ops brake only: the crawler skips discovery and writes `resources=not_applicable`. |

### 2.3 Consensus resolver (crawler binary)

Provider names/addresses (`1.1.1.1`, `8.8.8.8`, `9.9.9.9` + their v6 forms), the fixed
`cloudflare→google→quad9` order, the 2s per-attempt / 4s per-provider timeouts, and the
5-minute canary interval are **package constants, not config** (02-observation-model.md
§2). Only the rate ceiling and breaker thresholds are tunable:

| Key | env var | Type | Default | From | Meaning |
|---|---|---|---|---|---|
| `consensus.per_provider_qps` | `CONSENSUS_PER_PROVIDER_QPS` | int | `15` | 02 | Per-provider token-bucket rate **per process** (2 procs → 30 qps/provider ceiling vs ~24 qps demand; sizing: 00-overview.md). |
| `consensus.fastlane_breaker.nondefinitive_rate` | `CONSENSUS_FASTLANE_BREAKER_NONDEFINITIVE_RATE` | float | `0.05` | 02 | Trip the fast-lane breaker above this (error+inconsistent)/total rate. |
| `consensus.fastlane_breaker.window` | `CONSENSUS_FASTLANE_BREAKER_WINDOW` | duration | `15m` | 02 | Rolling window for the fast-lane breaker. |
| `consensus.fastlane_breaker.min_samples` | `CONSENSUS_FASTLANE_BREAKER_MIN_SAMPLES` | int | `500` | 02 | Minimum samples before the fast-lane breaker can trip. |
| `consensus.fastlane_breaker.recover_below` | `CONSENSUS_FASTLANE_BREAKER_RECOVER_BELOW` | float | `0.02` | 02 | Re-enable pull-ins once the rate stays below this for one full window. |
| `consensus.provider_breaker.failure_rate` | `CONSENSUS_PROVIDER_BREAKER_FAILURE_RATE` | float | `0.50` | 02 | Drop a provider above this non-answer rate. |
| `consensus.provider_breaker.window` | `CONSENSUS_PROVIDER_BREAKER_WINDOW` | duration | `15m` | 02 | Rolling window for the provider breaker. |
| `consensus.provider_breaker.min_samples` | `CONSENSUS_PROVIDER_BREAKER_MIN_SAMPLES` | int | `200` | 02 | Minimum samples before dropping a provider. |
| `consensus.provider_breaker.recovery_probes` | `CONSENSUS_PROVIDER_BREAKER_RECOVERY_PROBES` | int | `3` | 02 | Consecutive canary successes before a dropped provider is restored. |

### 2.4 Lifecycle (crawler binary)

| Key | env var | Type | Default | From | Meaning |
|---|---|---|---|---|---|
| `lifecycle.dead_streak` | `LIFECYCLE_DEAD_STREAK` | int | `7` | 04 | Consecutive unresolvable scans before `disabled_reason='dead'`. |
| `lifecycle.slow_lane_every` | `LIFECYCLE_SLOW_LANE_EVERY` | duration | `720h` | 04 | Revalidation cadence for disabled dead/delisted rows (30d). |
| `lifecycle.delist_grace` | `LIFECYCLE_DELIST_GRACE` | duration | `720h` | 04 | `orphaned_at` age before rank-NULL rows are delisted (30d). |
| `lifecycle.live_check_linkage` | `LIFECYCLE_LIVE_CHECK_LINKAGE` | duration | `168h` | 04 | Frontier lifetime granted by a `POST /check` (7d). |

### 2.5 Tranco import (crawler coordinator + v6ctl)

| Key | env var | Type | Default | From | Meaning |
|---|---|---|---|---|---|
| `tranco.min_rows` | `TRANCO_MIN_ROWS` | int | `950000` | 06 | Abort import below this many valid rows. |
| `tranco.max_delist_pct` | `TRANCO_MAX_DELIST_PCT` | float | `2.0` | 06 | Abort if more than this % of ranked rows would delist. |
| `tranco.import_at` | `TRANCO_IMPORT_AT` | string `HH:MM` UTC | `23:15` | 06 | Daily import-cycle start (coordinator goroutine; no systemd timer). |
| `tranco.retry_interval` | `TRANCO_RETRY_INTERVAL` | duration | `2h` | 06 | Re-attempt spacing within a cycle. |
| `tranco.stale_warn_after` | `TRANCO_STALE_WARN_AFTER` | duration | `48h` | 06 | Ops-webhook warning when no successful import for this long (rate-limited 1/24h). |

### 2.6 Campaign sync (crawler tick + CI-invoked v6ctl)

| Key | env var | Type | Default | From | Meaning |
|---|---|---|---|---|---|
| `campaign.repo_path` | `CAMPAIGN_REPO_PATH` | string (dir) | `/srv/whynoipv6-campaign` | 06 | Shared checkout owned by the service user (git pull + write-back). |
| `campaign.git_remote` | `CAMPAIGN_GIT_REMOTE` | string | `origin` | 06 | Push target for the bot UUID write-back commit (deploy key). |
| `campaign.max_domains_per_file` | `CAMPAIGN_MAX_DOMAINS_PER_FILE` | int | `1000` | 06 | Per-YAML-file domain cap (schema validation). |

### 2.7 Live check (api rate-limiter + crawler consumer)

| Key | env var | Type | Default | Owner | From | Meaning |
|---|---|---|---|---|---|---|
| `live_check.workers` | `LIVE_CHECK_WORKERS` | int | `4` | crawler | 07 | Concurrent engine slots for check jobs. |
| `live_check.job_budget` | `LIVE_CHECK_JOB_BUDGET` | duration | `60s` | crawler | 07 | Per-job engine deadline. |
| `live_check.reclaim_after` | `LIVE_CHECK_RECLAIM_AFTER` | duration | `5m` | crawler | 07 | Processing-lease reclaim. |
| `live_check.fail_after` | `LIVE_CHECK_FAIL_AFTER` | duration | `15m` | crawler, api | 07 | pending/processing → failed; also the API poller termination bound. |
| `live_check.retention` | `LIVE_CHECK_RETENTION` | duration | `720h` | crawler | 07 | 30d `check_job` purge, in the daily tick. |
| `live_check.rate_ip_per_hour` | `LIVE_CHECK_RATE_IP_PER_HOUR` | int | `10` | api | 07 | Per-IP `POST /check` rate limit. |
| `live_check.rate_global_per_hour` | `LIVE_CHECK_RATE_GLOBAL_PER_HOUR` | int | `500` | api | 07 | Global `POST /check` rate limit. |
| `live_check.dedupe_window` | `LIVE_CHECK_DEDUPE_WINDOW` | duration | `1h` | api | 07 | Collapse duplicate `POST /check` for the same host. |

### 2.8 Ops / observability (all binaries)

| Key | env var | Type | Default | Owner | From | Meaning |
|---|---|---|---|---|---|---|
| `ops.webhook_url` | `OPS_WEBHOOK_URL` | string (URL) | `""` (disabled) | crawler, v6ctl | 04 | Ops-webhook endpoint for all alerts/summaries (§12). Empty = no webhook (dev). |
| `ops.healthcheck_url` | `OPS_HEALTHCHECK_URL` | string (URL) | `""` (disabled) | crawler | 04,12 | **This process's** healthchecks.io ping URL (one per crawler instance). Empty = disabled. |
| `ops.healthcheck_tick_url` | `OPS_HEALTHCHECK_TICK_URL` | string (URL) | `""` (disabled) | crawler | 04,12 | Daily-tick healthchecks.io check (coordinator only). |
| `ops.healthcheck_min_interval` | `OPS_HEALTHCHECK_MIN_INTERVAL` | duration | `60s` | crawler | 04,12 | Minimum spacing between per-process heartbeat pings. |
| `taillight.url` | `TAILLIGHT_URL` | string (URL) | `""` (disabled) | all | 09 §13 | Taillight applog ingest endpoint (`…/api/v1/applog/ingest`). When set, slog records fan out to Taillight via the `logshipper` handler (§13). Empty = local JSON only. |
| `taillight.api_key` | `TAILLIGHT_API_KEY` | string (secret) | `""` | all | 09 §13 | Taillight API key with `ingest` scope. Redacted in the startup summary. |

### 2.9 Unbound stats (v6ctl on the Unbound host)

| Key | env var | Type | Default | Owner | From | Meaning |
|---|---|---|---|---|---|---|
| `unbound_stats.control` | `UNBOUND_STATS_CONTROL` | string (cmd) | `unbound-control` | v6ctl | 09 §8 | Path/args to `unbound-control` (override for chroot setups); `v6ctl ops unbound-stats` runs the **resetting** `stats` variant. |

### 2.10 API serving — badge, datasets, feeds, CSV export (api binary; export job = v6ctl)

Serving-layer knobs the redesigned API introduces (07-api.md). Values track the crawl
cadence and the report's §6/§7 defaults; behavior is normative in 07-api.md.

| Key | env var | Type | Default | Owner | From | Meaning |
|---|---|---|---|---|---|---|
| `badge.cache_ttl` | `BADGE_CACHE_TTL` | duration | `24h` | api | 07 | `Cache-Control: max-age` for `/badge/{host}.svg` + `.json` (daily crawl cadence; report §6.2/§7.1). |
| `datasets.manifest_cache_ttl` | `DATASETS_MANIFEST_CACHE_TTL` | duration | `5m` | api | 07 | `Cache-Control: max-age` for `GET /datasets` — the manifest re-read from `DATASETS_DIR/manifest.json` each request (report §6.3). |
| `datasets.retention_days` | `DATASETS_RETENTION_DAYS` | int | `90` | v6ctl(export) | 07 §7.4 | Daily-snapshot retention applied by `v6ctl export`; first-of-month snapshots kept forever (report §6.3). |
| `feed.recent_window` | `FEED_RECENT_WINDOW` | int | `50` | api | 07 | Latest-N transitions per Atom/JSON-Feed scope — the fixed recent window, no pagination (report §6.4; OPEN-15 = keep the latest-50 cap). |
| `export.csv_max_rows` | `EXPORT_CSV_MAX_ROWS` | int | `10000` | api | 07 | Row cap for `?format=csv` list responses; larger "give me everything" pulls are steered to the static datasets (report §6.5). |

`DATASETS_DIR` (§2.1) is the snapshot root; `PUBLIC_BASE_URL` (§2.1) supplies the absolute
origin for the manifest/feed URLs. The `POST /check` rate-limit keys already live in §2.7
(`live_check.rate_ip_per_hour`, `live_check.rate_global_per_hour`,
`live_check.dedupe_window`); the redesign's per-/64 prefix keying and the
`RateLimit`/`RateLimit-Policy` response headers (report §7.3) are behavior, not new config,
so nothing is added here for them.

### 2.11 DNS-provider mapping (OPEN-4 — crawler builds it; v6ctl seeds it)

Config for the `ns_host → provider` mapping table that backs `/providers` +
`/providers/{id}/domains` and the `?provider=` pivot (report §5.6/§10.3). The mapping
**table DDL is owned by 05-schema.md** and the refresh mechanism by 06-ingest.md; this file
only registers the two tuning knobs.

| Key | env var | Type | Default | Owner | From | Meaning |
|---|---|---|---|---|---|---|
| `dns_provider.seed_path` | `DNS_PROVIDER_SEED_PATH` | string (file) | `""` (none) | crawler, v6ctl | 06 | Path to the curated `ns_host → provider` seed mapping (YAML — a list of `{name, suffixes}` entries, 06 §6.11); empty = mapping derived from collected NS data only. |
| `dns_provider.refresh_interval` | `DNS_PROVIDER_REFRESH_INTERVAL` | duration | `24h` | crawler | 06 | Rebuild cadence for the mapping from collected NS data (in the daily tick). |

---

## 3. Filesystem & user layout

- **System user `whynoipv6`** — no shell, no home. Runs `api`, `crawler`, and every
  `v6ctl` timer/verb. Created by Ansible (`useradd --system --no-create-home --shell /usr/sbin/nologin`).
- `/opt/whynoipv6/bin/{api,crawler,v6ctl}` — the three release binaries (static
  linux/amd64). **This is the entire deploy artifact set** — migrations are embedded in
  `v6ctl` (below); no config or migration files ship to the host.
- `/etc/whynoipv6/env` — `root:whynoipv6` `0640`, env-format. Holds the secrets and any
  non-default overrides: `DATABASE_URL`, `API_LISTEN`, `GEOIP_PATH`, `DATASETS_DIR`,
  `PUBLIC_BASE_URL`, `LOG_LEVEL`, `OPS_WEBHOOK_URL`, `OPS_HEALTHCHECK_URL` (per process),
  `OPS_HEALTHCHECK_TICK_URL`, and any tuning override (`WORKER_SLOTS`,
  `CRAWLER_RESOURCES_ENABLED`, …). Ansible-vault templated.
- `/etc/whynoipv6/config.yaml` — **optional**; present only if the operator prefers YAML
  over env for the nested tuning sections. Absent in the reference deployment.
- `/var/lib/whynoipv6/datasets/` — owned `whynoipv6`, world-readable; nginx serves it
  read-only (§7). Layout and atomic-publish mechanics are in 07-api.md — On-disk layout (§7.2) and Atomic publish procedure (§7.4).
- `/var/lib/GeoIP/` — written by `v6ctl geoip update` (systemd timer, §11), read by `crawler`.
- `/var/backups/whynoipv6/` — logical CSV exports staging (DB VM, §10).

**Migrations are embedded in `v6ctl` via `go:embed` + golang-migrate's `iofs` source.**
`v6ctl migrate up|down|version` reads the embedded FS; no migrations directory ships to
the host. `db/migrations/` in the repo stays the sqlc/dev source of truth (`000001` base schema —
`CREATE TABLE`s for `unbound_stats` + `crawler_metrics` incl. their columns; `000002`
timescale hypertable conversion of those tables; `000003` seed — 05-schema.md).

Example `/etc/whynoipv6/env` (values are Ansible-vault templated):

```sh
DATABASE_URL=postgres://whynoipv6:__VAULT__@db.internal:5432/whynoipv6?pool_max_conns=32&sslmode=verify-full
API_LISTEN=[::1]:8080
GEOIP_PATH=/var/lib/GeoIP
DATASETS_DIR=/var/lib/whynoipv6/datasets
PUBLIC_BASE_URL=https://api.whynoipv6.com
LOG_LEVEL=info
OPS_WEBHOOK_URL=https://ops.example.net/hooks/__VAULT__
OPS_HEALTHCHECK_URL=https://hc-ping.com/__VAULT_CRAWLER_1__
OPS_HEALTHCHECK_TICK_URL=https://hc-ping.com/__VAULT_TICK__
```

---

## 4. systemd service units (`deploy/systemd/`)

The repo ships these files verbatim; Ansible copies them and manages
`{{ }}`-free content only (per-process env goes in `/etc/whynoipv6/env`, not the unit).

**`whynoipv6-api.service`** (backend host):

```ini
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
ReadOnlyPaths=/var/lib/whynoipv6/datasets

[Install]
WantedBy=multi-user.target
```

**`whynoipv6-crawler.service`** (crawler host — one unit per host; a second host is
resilience, not capacity, and coordinates only through the shared frontier + SKIP
LOCKED):

```ini
[Unit]
Description=WhyNoIPv6 crawler
After=network-online.target postgresql.service unbound@1.service unbound@2.service
Wants=network-online.target

[Service]
User=whynoipv6
EnvironmentFile=/etc/whynoipv6/env
ExecStart=/opt/whynoipv6/bin/crawler
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/whynoipv6/datasets
ReadOnlyPaths=/var/lib/GeoIP
TimeoutStopSec=90

[Install]
WantedBy=multi-user.target
```

Graceful shutdown: both binaries drain on `SIGTERM` inside systemd's 90s stop budget.
The crawler finishes in-flight scans (drain budget `80s`, 04-lifecycle-scheduling.md),
writes a final `crawler_metrics` row, and lets uncommitted claims expire back to the
frontier; the API calls `server.Shutdown(ctx)` with a 15s drain (07-api.md — Timeouts & graceful shutdown, §1.6). The
crawler is a single unit even though it runs `WORKER_SLOTS` goroutines internally; there
is no per-slot unit.

**`whynoipv6-notify@.service`** (installed on every host — backend, crawler, DB VM — as
the `OnFailure=` target for oneshot units):

```ini
[Unit]
Description=WhyNoIPv6 ops-webhook failure notice for %i

[Service]
Type=oneshot
User=whynoipv6
EnvironmentFile=/etc/whynoipv6/env
ExecStart=/usr/bin/curl -fsS --max-time 15 -X POST "${OPS_WEBHOOK_URL}" \
  -H "Content-Type: application/json" \
  -d "{\"level\":\"error\",\"source\":\"systemd\",\"unit\":\"%i\",\"host\":\"%H\"}"
```

`%i` is the failed unit name (set via `OnFailure=whynoipv6-notify@%n.service`). If
`OPS_WEBHOOK_URL` is empty the curl is a no-op failure that systemd ignores.

**Oneshot timer service units** (`Type=oneshot`, `User=whynoipv6`,
`EnvironmentFile=/etc/whynoipv6/env`, each with
`OnFailure=whynoipv6-notify@%n.service`): `whynoipv6-export.service`,
`whynoipv6-unbound-stats.service` (crawler/Unbound host), and on the DB VM
`whynoipv6-logical-export.service`, `pgbackrest-full.service`, `pgbackrest-diff.service`,
`pgbackrest-verify.service`. Their `ExecStart` lines are in §5.

---

## 5. Timer inventory (complete, across all hosts)

`v6ctl` exits non-zero on failure; every oneshot unit's
`OnFailure=whynoipv6-notify@%n.service` routes that to the ops webhook — no separate
alerting infrastructure. The **Tranco import has no timer**: it is fired by the crawler
coordinator goroutine under the `JobTrancoImport` advisory lock (06-ingest.md,
04-lifecycle-scheduling.md). The **daily tick has no timer**: the coordinator fires it at
`03:30 UTC` under `JobDailyTick`. Campaign sync runs from the tick **and** on a
GitHub Actions `repository_dispatch` webhook → operator CI runs `v6ctl campaign sync` (06-ingest.md).

| Timer | Host | OnCalendar (UTC) | ExecStart | Notes |
|---|---|---|---|---|
| `whynoipv6-export.timer` | backend | `04:30`, `Persistent=true` | `v6ctl export` | Datasets snapshot after the 03:30 stats tick (1h headroom); applies the 07-api.md §7.4 retention (dailies 90d, first-of-month kept). A late tick degrades to yesterday's stats, never a failure. |
| `whynoipv6-unbound-stats.timer` | crawler/Unbound | `*:*:00` (every 60s) | `v6ctl ops unbound-stats` | §8; ~1,440 rows/day/host. |
| `v6ctl-geoip-update.timer` | crawler | `*-*-* 06:30` + `RandomizedDelaySec=4h` | `v6ctl geoip update` | §11; IPinfo Lite refreshes daily. Crawler picks up new mmdb via §11's hourly mtime check — no restart. |
| `whynoipv6-logical-export.timer` | DB VM | `Sun 04:30`, `Persistent=true` | `/usr/local/bin/whynoipv6-export.sh` | Weekly CSV export of `changelog` + `domain` (§10.3). **Decision:** unit named `whynoipv6-logical-export` (design gave only the script path) to disambiguate from the backend `whynoipv6-export` datasets unit — the two share the name "export" but run on different hosts. |
| `pgbackrest-full.timer` | DB VM | `Sun 03:30`, `Persistent=true` | `pgbackrest --stanza=whynoipv6 --type=full backup` | §10.1; 03:30 is outside the crawl write window and the 23:15 Tranco window. |
| `pgbackrest-diff.timer` | DB VM | `Mon..Sat 03:30`, `Persistent=true` | `pgbackrest --stanza=whynoipv6 --type=diff backup` | §10.1. |
| `pgbackrest-verify.timer` | DB VM | `05:00`, `Persistent=true` | `/usr/local/bin/whynoipv6-backup-verify.sh` | §10.5 nightly backup+version monitoring. **Decision:** unit/script named `pgbackrest-verify` (design described the job, unnamed). |

`v6ctl-geoip-update` units (`deploy/systemd/`):

```ini
# v6ctl-geoip-update.service
[Service]
Type=oneshot
Environment=IPINFO_TOKEN={{ vault_ipinfo_token }}
Environment=GEOIP_PATH=/var/lib/GeoIP
ExecStart=/usr/local/bin/v6ctl geoip update

# v6ctl-geoip-update.timer
[Timer]
OnCalendar=*-*-* 06:30
RandomizedDelaySec=4h
Persistent=true

[Install]
WantedBy=timers.target
```

(The empty `OnCalendar=` clears the packaged schedule before setting ours.)

---

## 6. Deploy procedure (Ansible order + migration point)

Production is provisioned by the operator's existing Ansible. The deploy is
expand/contract and **forward-only** — old binaries run between the migration and the
restarts, so every migration shipped with release N must keep release N−1 binaries
working.

1. **CI** builds and publishes the release artifacts `api`, `crawler`, `v6ctl` (static
   linux/amd64, migrations embedded in `v6ctl`). See §14.
2. **Ansible** copies the three binaries to `/opt/whynoipv6/bin/` (they land beside the
   still-running old processes — safe, nothing re-execs) and installs any changed unit
   files from `deploy/systemd/` (`daemon-reload` after).
3. **Migrate:** `sudo -u whynoipv6 /opt/whynoipv6/bin/v6ctl migrate up` — **forward-only;
   no down-migrations in production.** This is the single migration point.
4. `systemctl restart whynoipv6-crawler` (drains gracefully, §4).
5. `systemctl restart whynoipv6-api`.
6. **Verify:** `systemctl is-active whynoipv6-api whynoipv6-crawler` both `active`;
   `curl -6 -sf -o /dev/null http://[::1]:8080/livez` and
   `curl -6 -sf -o /dev/null http://[::1]:8080/readyz` both return `200` (the
   07-api.md §2.7 health endpoints — `readyz` additionally proves DB reachability);
   newest `crawler_metrics` row age `< 10 min` in Grafana.

**Rollback** = redeploy the previous release's binaries (steps 2, 4, 5 only). The
expand/contract contract keeps the already-applied migration compatible. **Never migrate
down in production.**

Backups are prod infrastructure **from phase 3 onward** — the pgBackRest stanza, WAL
archiving, and one verified restore drill must exist before the first full-scale sweep
writes confirmed state (§10).

---

## 7. nginx vhost (`deploy/nginx/api.whynoipv6.com.conf`)

One server block serves both the API (reverse-proxied to `[::1]:8080`) and the static
datasets tree (`/datasets/`, served from disk). TLS is Let's Encrypt (certbot, paths
Ansible-managed). The proxy-header block and the datasets locations are normative in
07-api.md (the proxy-header block in §1.2, the datasets layout/nginx split in §7.2/§7.6)
and copied here because this vhost file is a deploy artifact this spec owns.

Resources sit at the **root** — there is **no `/v1` segment** (report §3.1), so a single
`location /` fronts every API path (leaderboards, `/domains*`, badge `.svg`/`.json`, the
Atom/JSON change feeds, `/datasets`, `POST /check`). The **app owns every response
`Cache-Control` per endpoint class** (07-api.md — Cache-Control by endpoint class; report
§7.1: list `public, s-maxage`; badge `max-age=86400`; manifest `max-age=300`; terminal poll
`max-age=60`; `no-store` only on `POST /check`, in-flight poll, `/ip`, health), so this vhost
**never** sets a blanket `no-store`/`no-cache` and **never** hides the app's headers —
the RFC 9457 `application/problem+json` Content-Type and the
`RateLimit`/`RateLimit-Policy`/`Retry-After` response headers (report §7.3) pass through
unmodified. Edge compression is gzip here, Brotli at the CDN (report §7.4).

```nginx
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name api.whynoipv6.com;

    ssl_certificate     /etc/letsencrypt/live/api.whynoipv6.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/api.whynoipv6.com/privkey.pem;

    # --- edge compression (§7.4): gzip at the origin, Brotli at the CDN.
    #     Covers JSON, RFC 9457 problem+json, the Atom/JSON feeds, CSV, and text SVG.
    #     The pre-compressed dataset payloads opt out (gzip off) below. ---
    gzip on;
    gzip_vary on;
    gzip_proxied any;
    gzip_min_length 256;
    gzip_types application/json application/problem+json application/atom+xml
               application/feed+json text/csv image/svg+xml;

    # --- datasets: dated snapshots are immutable forever ---
    location ~ ^/datasets/\d{4}-\d{2}-\d{2}/ {
        root /var/lib/whynoipv6;
        add_header Cache-Control "public, max-age=31536000, immutable";
        add_header Access-Control-Allow-Origin "*";
        gzip off;   # payloads are pre-compressed (.csv.gz) or binary (.parquet)
    }

    # --- datasets: latest/ symlink + DICTIONARY.md, mutable, short TTL ---
    location /datasets/ {
        root /var/lib/whynoipv6;
        autoindex off;
        add_header Cache-Control "public, max-age=3600";
        add_header Access-Control-Allow-Origin "*";
        gzip off;
    }

    # --- datasets manifest: exact match → API (app sets Cache-Control: max-age=300) ---
    location = /datasets {
        proxy_pass http://[::1]:8080;
        proxy_set_header X-Real-IP        $remote_addr;
        proxy_set_header X-Forwarded-For  $proxy_add_x_forwarded_for;
        proxy_set_header Host             $host;
    }

    # --- everything else → API: leaderboards, /domains*, badge .svg/.json, the
    #     Atom/JSON change feeds, POST /check, /ip, health. The app owns every
    #     Cache-Control (badge max-age=86400, lists public s-maxage, no-store on
    #     POST /check + in-flight poll + /ip + health); do NOT add_header here and do
    #     NOT proxy_hide_header the app's Cache-Control, the RFC 9457
    #     application/problem+json Content-Type, or the RateLimit / RateLimit-Policy /
    #     Retry-After headers — all pass through unmodified. ---
    location / {
        proxy_pass http://[::1]:8080;
        proxy_set_header X-Real-IP        $remote_addr;
        proxy_set_header X-Forwarded-For  $proxy_add_x_forwarded_for;
        proxy_set_header Host             $host;
    }
}

server {
    listen 80;
    listen [::]:80;
    server_name api.whynoipv6.com;
    return 301 https://$host$request_uri;
}
```

`root /var/lib/whynoipv6` (not `alias`) maps `/datasets/...` directly under the
datasets dir; nginx follows the `latest` symlink by default. `X-Real-IP` is the single
source of truth for `GET /ip` and the `POST /check` per-IP + per-/64 rate limiter (report
§7.3) — trusting it is
safe only because the API bind (`API_LISTEN=[::1]:8080`) is unreachable except through
this proxy (07-api.md — Real client IP, §1.2). The frontend (`whynoipv6.com`) is a separate vhost owned by
the frontend deploy, out of scope here.

---

## 8. Unbound deployment + stats collection (`deploy/unbound/`)

**Topology.** Two local Unbound instances on the crawler host, listening on
`127.0.0.1:53` (instance 1) and `127.0.0.1:5353` (instance 2) — exactly the default
`resolver.bulk_upstreams` value (§2.2). They are the **bulk resolver**: all non-consensus
DNS (NS/MX chains + host AAAA, A, PTR, TXT/SPF, DNSSEC, the resource-host sweep, and the
consensus path's conditional A lookup). We run two for redundancy, not capacity — a
single tuned instance handles 6–20k qps against the sized ~140–190 qps bulk demand
(00-overview.md). Unbound **is the cache** (the in-process TTL cache is deleted in the
lift, 01-engine.md); `dns_dnssec` requires a **validating** upstream, so DNSSEC
validation must be on (Unbound's default).

**`unbound@.service`** (templated; instance = `%i`):

```ini
[Unit]
Description=Unbound recursive resolver instance %i
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/sbin/unbound -d -p -c /etc/unbound/instances/%i.conf
Restart=on-failure
RestartSec=2
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/unbound

[Install]
WantedBy=multi-user.target
```

Enabled as `unbound@1` and `unbound@2`. Per-instance drop-ins
(`/etc/unbound/instances/1.conf`, `/etc/unbound/instances/2.conf`) differ only in the
listen address and control port; both `include:` the shared `unbound-base.conf`.

**`unbound-base.conf`** (tuning per NLnet Labs guidance for the top-1M workload — a
small shared set of NS/MX providers gives high cache-hit rates):

```conf
server:
    do-ip4: yes
    do-ip6: yes
    do-udp: yes
    do-tcp: yes
    num-threads: 4
    outgoing-range: 8192
    num-queries-per-thread: 4096
    rrset-cache-size: 512m
    msg-cache-size: 256m
    cache-min-ttl: 0
    cache-max-ttl: 86400
    qname-minimisation: yes
    prefetch: yes
    harden-dnssec-stripped: yes
    auto-trust-anchor-file: "/var/lib/unbound/root.key"   # DNSSEC validation ON
    hide-identity: yes
    hide-version: yes
    verbosity: 1
    statistics-cumulative: no
    extended-statistics: yes
    username: "unbound"
    directory: "/etc/unbound"
```

Instance drop-in `1.conf`:

```conf
include: "/etc/unbound/unbound-base.conf"
server:
    interface: 127.0.0.1@53
remote-control:
    control-enable: yes
    control-interface: 127.0.0.1
    control-port: 8953
```

Instance `2.conf` is identical with `interface: 127.0.0.1@5353` and
`control-port: 8954`. Tuning (`num-threads`, cache sizes) is finalized during phase-2
verification against the sized load.

**Stats collection.** `v6ctl ops unbound-stats` runs the **resetting** variant
(`unbound-control stats`, config key `unbound_stats.control`, §2.9) so every row holds
per-interval deltas and Grafana rate math is a plain division by the 60s interval. It
parses the `key=value` output and inserts **one row** into the `unbound_stats`
hypertable (defined in 05-schema.md — columns `ts, host, num_queries, cache_hits,
cache_miss, rcode_servfail, rcode_nxdomain, recursion_time_avg_ms, requestlist_avg,
raw`; `recursion_time_avg_ms = total.recursion.time.avg × 1000`; `raw` is the full stats
dump as JSONB). Invoked once per instance by `whynoipv6-unbound-stats.timer` (every 60s,
§5). No `unbound_exporter`, no Prometheus. **This spec does not restate the DDL** —
`unbound_stats` is `CREATE TABLE`'d (with its full column list — `num_queries`,
`cache_hits`, `cache_miss`, `rcode_servfail`, `rcode_nxdomain`, `recursion_time_avg_ms`,
`requestlist_avg`, `raw`) in 05-schema.md migration `000001` and converted to a hypertable
in `000002`.

---

## 9. docker-compose dev environment (`compose.yaml`)

Dev/CI only (brief: "Docker + docker-compose; systemd for prod"). Brings up TimescaleDB,
**two Unbound instances** (the bulk resolver; consensus still hits the real public
resolvers, which are package constants), a one-shot migrate step, `api`, and `crawler`.
Modern Compose spec (no `version:` key); rolling image tags per house rules
(TimescaleDB on **pg18**).

```yaml
services:
  db:
    image: timescale/timescaledb:latest-pg18
    environment:
      POSTGRES_USER: whynoipv6
      POSTGRES_PASSWORD: whynoipv6
      POSTGRES_DB: whynoipv6
    ports: ["5432:5432"]
    volumes: ["pgdata:/var/lib/postgresql/data"]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U whynoipv6"]
      interval: 5s
      timeout: 5s
      retries: 10

  unbound1:
    image: mvance/unbound:latest
    volumes: ["./deploy/unbound/dev/unbound1.conf:/opt/unbound/etc/unbound/unbound.conf:ro"]
    ports: ["5301:53/udp", "5301:53/tcp"]

  unbound2:
    image: mvance/unbound:latest
    volumes: ["./deploy/unbound/dev/unbound2.conf:/opt/unbound/etc/unbound/unbound.conf:ro"]
    ports: ["5302:53/udp", "5302:53/tcp"]

  migrate:
    build: {context: ./backend, dockerfile: Dockerfile}
    entrypoint: ["/v6ctl", "migrate", "up"]
    environment:
      DATABASE_URL: postgres://whynoipv6:whynoipv6@db:5432/whynoipv6?sslmode=disable
    depends_on:
      db: {condition: service_healthy}

  api:
    build: {context: ./backend, dockerfile: Dockerfile}
    entrypoint: ["/api"]
    ports: ["8080:8080"]
    environment:
      DATABASE_URL: postgres://whynoipv6:whynoipv6@db:5432/whynoipv6?sslmode=disable
      API_LISTEN: ":8080"                      # dev override: non-loopback so host can reach it
      DATASETS_DIR: /var/lib/whynoipv6/datasets
      LOG_LEVEL: debug
    volumes: ["datasets:/var/lib/whynoipv6/datasets"]
    depends_on:
      migrate: {condition: service_completed_successfully}

  # Init container: fetch the IPinfo Lite mmdb into the geoip volume (§11 dev
  # equivalent). Runs as root to write the root-owned volume; crawler reads :ro.
  geoip-init:
    build: {context: ./backend, dockerfile: Dockerfile}
    entrypoint: ["/v6ctl", "geoip", "update"]
    user: root
    environment:
      IPINFO_TOKEN: ${IPINFO_TOKEN:-}          # set in gitignored .env
      GEOIP_PATH: /var/lib/GeoIP
    volumes: ["geoip:/var/lib/GeoIP"]

  crawler:
    build: {context: ./backend, dockerfile: Dockerfile}
    entrypoint: ["/crawler"]
    environment:
      DATABASE_URL: postgres://whynoipv6:whynoipv6@db:5432/whynoipv6?sslmode=disable
      RESOLVER_BULK_UPSTREAMS: "unbound1:53,unbound2:53"
      GEOIP_PATH: /var/lib/GeoIP
      LOG_LEVEL: debug
    volumes:
      - "geoip:/var/lib/GeoIP:ro"
      - "datasets:/var/lib/whynoipv6/datasets"
    depends_on:
      migrate: {condition: service_completed_successfully}
      geoip-init: {condition: service_completed_successfully}

volumes:
  pgdata:
  datasets:
  geoip:
```

**Decision — dev image + address choices:** `timescale/timescaledb:latest-pg18` and
`mvance/unbound:latest` (rolling tags; no upstream official Unbound image exists).
`API_LISTEN=":8080"` in dev only (production is `[::1]:8080`). `RESOLVER_BULK_UPSTREAMS`
points at the two compose Unbound services by name. The design mandates the compose
shape ("timescale image, unbound ×2, api, crawler") but pins none of these values; these
are the simplest choices consistent with §8's two-instance topology. `Dockerfile` is the
multi-stage build in §14; the dev `unbound1.conf`/`unbound2.conf` mirror
`unbound-base.conf` (§8) with `access-control: 0.0.0.0/0 allow` for the compose network.

---

## 10. Backup & restore

The database is the only stateful component. `scan`/`scan_detail` are re-derivable by
re-crawling; **`changelog` (kept forever) and `domain` confirmed state
(`*_status/_since/_observed`, disabled/dead/delisted lifecycle) are NOT** — they are the
product's credibility surface and must survive loss of the DB host.

### 10.1 Physical backups — pgBackRest (the authoritative recovery path)

pgBackRest (current release), installed on the DB VM via Ansible. If Postgres runs in
Docker, mount the data dir + socket into a pgbackrest sidecar built from the same
PG18+timescaledb image family (library versions must match); if Postgres runs natively
under systemd, run pgBackRest natively. Either way **the repo lives off-host**.

Mode: continuous WAL archiving + weekly full + daily differential → PITR across the whole
retention window.

`postgresql.conf` (Ansible template):

```conf
archive_mode = on
archive_command = 'pgbackrest --stanza=whynoipv6 archive-push %p'
archive_timeout = 15min          # bounds worst-case loss to <=15 min of changelog writes
```

`deploy/pgbackrest/pgbackrest.conf` skeleton (host-specific values are Ansible vars;
secrets in vault):

```ini
[global]
repo1-type=sftp                      # default: second VM; alternative: s3
repo1-path=/srv/pgbackrest/whynoipv6
repo1-sftp-host={{ backup_host }}
repo1-sftp-host-user=pgbackrest
repo1-sftp-private-key-file=/etc/pgbackrest/id_ed25519
repo1-retention-full=4               # 4 weekly fulls ~= 28-day PITR window
repo1-cipher-type=aes-256-cbc
repo1-cipher-pass={{ vault_pgbackrest_cipher_pass }}
compress-type=zst
start-fast=y

[whynoipv6]
pg1-path={{ pg_data_dir }}
```

Off-host is mandatory (the repo must never live only on the DB host). Default sftp to a
second VM; S3-compatible object storage (`repo1-type=s3` + bucket/endpoint/key) is an
equivalent drop-in. Schedule = the `pgbackrest-full`/`pgbackrest-diff` timers (§5). Both
set `OnFailure=whynoipv6-notify@%n.service`. Sizing: with 05-schema retention (scan 2y
compressed single-digit GB, scan_detail 90d ≈ 15–40 GB) the repo stays under 100 GB. **Do
not** exclude scan/scan_detail — pgBackRest backs up the cluster; partial-cluster physical
backup is not a thing.

### 10.2 TimescaleDB restore requirements (runbook)

1. **Physical restore** requires the target to run the **same PostgreSQL major version**
   and a timescaledb shared library **of the exact extension version** current at backup
   time. Both are recorded nightly (§10.5 is the version-of-record). Keep the Ansible
   role's PG + timescaledb versions in lockstep with prod. **Never** run
   `ALTER EXTENSION timescaledb UPDATE;` without immediately taking a fresh full backup
   afterward.
2. **Logical restore** (only for the §3 dataset artifacts or an ad-hoc pg_dump): create
   the matching extension version first, then `SELECT timescaledb_pre_restore();` →
   restore → `SELECT timescaledb_post_restore();`. Plain `pg_dump` of individual
   hypertables is **forbidden as a backup strategy** (it silently misses
   `_timescaledb_internal` chunks unless the whole database is dumped).
3. **Restore procedure (scratch / DR):** provision a VM/container with matching
   PG18+timescaledb → install `pgbackrest.conf` pointing at the repo with `pg1-path` set
   to the empty data dir → `pgbackrest --stanza=whynoipv6 restore` (add
   `--type=time --target='…'` for PITR) → start PG → verify per §10.4.

### 10.3 Belt-and-suspenders weekly logical export

The two irreplaceable tables, exported as plain CSV via `COPY` (reads through every
hypertable chunk; restore path has zero PG/extension version coupling). Script
`deploy/pgbackrest/whynoipv6-export.sh` → `/usr/local/bin/whynoipv6-export.sh`, driven by
`whynoipv6-logical-export.timer` (Sun 04:30, DB VM, §5):

```sh
#!/usr/bin/env bash
set -euo pipefail
d=$(date +%F); out=/var/backups/whynoipv6
psql -Atq service=whynoipv6 -c "COPY (SELECT * FROM changelog ORDER BY ts) TO STDOUT WITH (FORMAT csv, HEADER)" | zstd -q -o "$out/changelog-$d.csv.zst"
psql -Atq service=whynoipv6 -c "COPY (SELECT * FROM domain ORDER BY id) TO STDOUT WITH (FORMAT csv, HEADER)" | zstd -q -o "$out/domain-$d.csv.zst"
rsync -a "$out/" pgbackrest@{{ backup_host }}:/srv/logical-exports/whynoipv6/
```

Retention on the backup host: last 8 weeklies + first-of-month for 12 months (find
`-mtime` in the Ansible role). Failure → ops webhook (via the unit's `OnFailure`). All
other tables (`campaign*`, `tranco*`, `resource_host`, `crawler_metrics`, `unbound_stats`)
are re-derivable from the campaign YAML repo, Tranco, or re-crawling, and are covered by
the physical backup anyway.

### 10.4 Restore drills (an untested backup is assumed broken)

- **Phase-3 gate:** pgBackRest stanza created, first full backup completed, WAL archiving
  confirmed (`pgbackrest check`), and one full restore to a scratch instance succeeds —
  **before** the first production sweep is declared done.
- **Phase-4 cutover gate (DNS-flip cutover, 08 — no data import):** restore the latest
  backup to a scratch instance; `SELECT count(*) FROM changelog` on the restore matches the
  live DB as of the backup timestamp; the API binary starts against the restored DB and
  `GET /changelog` returns the envelope (empty `items` at launch — the changelog starts
  fresh under the start-fresh cutover and fills from cutover onward, report §9 / 08).
- **Quarterly:** repeat the phase-4 drill (timebox 1h) plus one spot-check that a weekly
  CSV export loads into a fresh vanilla PG (`\copy changelog FROM ...`). Record date +
  result in the ops notes.

### 10.5 Backup monitoring (`pgbackrest-verify`)

`pgbackrest-verify.timer` (nightly 05:00, DB VM) runs
`/usr/local/bin/whynoipv6-backup-verify.sh`:
`pgbackrest --stanza=whynoipv6 info --output=json` and
`psql -Atc "SELECT version(), (SELECT extversion FROM pg_extension WHERE extname='timescaledb')"`,
appends both to `/var/log/pgbackrest/verify.log` (**the version-of-record for §10.2 item
1**), and alerts the ops webhook if any of: newest backup older than **26 h**, newest
archived WAL older than **1 h**, or the last logical-export timer failed. Optionally
surface the same three as a Grafana panel via an exec/textfile collector — but the
webhook alert is the required part.

---

## 11. IPinfo Lite lifecycle (`deploy/geoip/`)

Replaces production's repo-bundled Jan-2023 mmdb files. The attribution logic, the
`GEOIP_PATH` key, and the crawler's hourly mtime check + atomic reader swap are owned by
06-ingest.md — §6.8 (`internal/geoip`); this section owns the update lifecycle. The
MaxMind → IPinfo switch is ADR 0001.

1. **Token:** free IPinfo account + API token. Store it in Ansible vault
   (`vault_ipinfo_token`). No account-ID/license-key pair, no distro package — IPinfo
   Lite is one CC BY-SA 4.0 file (country + ASN, IPv4+IPv6) fetched by token.
2. **Ansible:** deploy the `v6ctl` binary (already shipped for migrations). No
   `geoipupdate` package, no `/etc/GeoIP.conf`. The updater is `v6ctl geoip update`,
   which downloads `ipinfo_lite.mmdb` into `GEOIP_PATH` atomically (temp file →
   mmdb-verify → rename), so the crawler's mtime swap never sees a torn file.
3. **Timer:** a `v6ctl-geoip-update.service` (`Environment=IPINFO_TOKEN={{ vault_ipinfo_token }}`,
   `ExecStart=/usr/local/bin/v6ctl geoip update`) on a daily timer,
   `OnCalendar=*-*-* 06:30` + `RandomizedDelaySec=4h` (§5); IPinfo Lite refreshes every
   24 h. Pickup by the crawler is the hourly mtime check + atomic reader swap — no tick
   step, no restart. (Dev equivalent: the compose `geoip-init` service.)
4. **Monitoring:** the crawler exports the loaded mmdb build epoch into
   `crawler_metrics.geoip_build_epoch` (05-schema.md — crawler_metrics);
   Grafana alerts when it is older than **7 days** (daily updates → a 7-day-stale epoch
   means a broken token or timer).

Attribution: IPinfo Lite (CC BY-SA 4.0) requires crediting IPinfo — the frontend footer
carries the link (12-frontend §9.4). The filename (`ipinfo_lite.mmdb`) and the reload
interval are fixed, not config. Only the directory (`GEOIP_PATH`, default
`/var/lib/GeoIP`) is configurable.

---

## 12. Crawl liveness, heartbeats & Grafana alert rules

**Observability model (pinned).** Grafana reads Postgres **directly**
(`crawler_metrics`, frontier queries, `unbound_stats`, `timescaledb_information` views).
No Prometheus, no `/metrics` endpoints on any binary. External liveness is
healthchecks.io pings.

**Heartbeats (phase 2).** One healthchecks.io check **per crawler process** (e.g.
`wni6-crawler-1`, `wni6-crawler-2`) plus one for the daily tick (`wni6-daily-tick`):

- After every successful claim-cycle commit, the process pings its own check's success
  URL (`ops.healthcheck_url`), throttled to at most one ping per
  `ops.healthcheck_min_interval` (60s).
- On preflight failure, the process pings the **`/fail`** endpoint of the same check (in
  addition to the ops-webhook alert).
- Per-process check config: Period = 15 min, Grace = 30 min → a dead/hung process is
  signalled within ≤45 min instead of ~23h.
- The daily tick keeps its own separate check (`ops.healthcheck_tick_url`, Period = 24h,
  Grace = 2h), pinged at daily-tick step 7 only, and only if steps 1–3 (lifecycle +
  stats) succeeded.
- Empty URL disables a heartbeat (dev/staging default).
- Ansible templates one crawler unit environment per process, each with its own
  `OPS_HEALTHCHECK_URL` in `/etc/whynoipv6/env` (or a per-instance drop-in).

The **idle-checkpoint rule** (04-lifecycle-scheduling.md) keeps A1 valid when the
frontier is drained: each process writes a `crawler_metrics` row (processed=0,
is_final=false, current queue_depth/active_slots) whenever no checkpoint has been written
for 5 minutes.

**`internal/notify`** implements both the ops-webhook POST and the healthchecks.io ping
(lifted from production's `toolbox.HealthCheckUpdate`/`HealthFail` semantics, IRC
dropped). A delivery failure logs at `warn` and is otherwise swallowed — a webhook outage
must never stall a crawl.

**Grafana alert rules** (`deploy/grafana/alerts.yaml`, provisioned on the Postgres
datasource, notification policy → the ops webhook). Thresholds are tunable starting
points:

- **A1 crawler stalled:** `SELECT count(*) FROM crawler_metrics WHERE ts > now() - interval '15 minutes'` == 0 → **critical** (valid at all times thanks to the idle-checkpoint rule).
- **A2 frontier lag:** count of active domains with `next_check_at < now() - interval '6 hours'` (the active-domain predicate from the claim SQL, 04-lifecycle-scheduling.md) > 50,000 → **warning**; > 200,000 → **critical**. Catches silent throughput collapse the heartbeats can't see.
- **A3 error ratio:** `SELECT coalesce(sum(failed)::float / nullif(sum(processed),0), 0) FROM crawler_metrics WHERE ts > now() - interval '1 hour'` > 0.20 → **warning** (complements, does not replace, the consensus fast-lane/provider breakers, which remain the primary error-path alerts).
- **A4 TimescaleDB jobs:** `SELECT count(*) FROM timescaledb_information.job_stats WHERE last_run_status = 'Failed'` > 0 → **warning**.
- **A5 Unbound/scraper down:** `SELECT count(*) FROM unbound_stats WHERE ts > now() - interval '5 minutes'` == 0 → **critical**.

The **ops webhook is the single alert sink** for: Tranco import aborts + staleness
warnings, the consensus fast-lane/provider breakers, backup/WAL/export failures
(§10.5 + `OnFailure`), timer failures (`whynoipv6-notify@`), the daily-tick ops summary,
the weekly service-candidate digest, and campaign-sync reports. `ops.webhook_url` empty =
no webhook (dev).

---

## 13. Logging conventions (normative — all three binaries)

**Handler.** Each `cmd/*/main.go` calls once at startup:
`slog.SetDefault(slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})))`.
`w` is `os.Stdout` for `api` and `crawler` (systemd → journald captures stdout; no log
files, no rotation logic in the binaries). **`v6ctl` writes slog to `os.Stderr`** so
command output on stdout stays pipeable. JSON always — no format knob. `lvl` comes from
`LOG_LEVEL` (§2.1).

**Taillight shipping (optional).** When `taillight.url` is set (§2.8), the installed
handler is a `logshipper.MultiHandler` fanning out to the local JSON handler **and** a
[Taillight](https://github.com/lasseh/taillight) shipper (`pkg/logshipper`): service
`whynoipv6`, component = binary name, `MinLevel` = `LOG_LEVEL`, batching defaults.
The shipper is non-blocking (drops on overflow rather than stalling the process).
Each binary drains it on shutdown via the flush func returned by `InstallLogger`;
a malformed `taillight.url` is a fatal startup error like any other misconfiguration.

**Standard attribute keys (exact names):**
- `component` — binary name (`api`|`crawler`|`v6ctl`), stamped once on the local JSON
  handler via `WithAttrs` (the Taillight shipper carries it as its first-class
  `component` field instead, so it is not duplicated into `attrs`).
- `run_id` — the crawler run UUID, identical to `crawler_metrics.run_id`; stamped on a per-run child logger.
- `worker` — worker identity string, identical to `crawler_metrics.worker`.
- `domain` — the eTLD+1 (or registry host) on any per-domain/per-host line.
- `duration_ms` — int64 milliseconds for timed operations.
- `err` — error text (`slog.String("err", err.Error())`).

**Level policy:**
- `debug` — per-domain scan outcomes, per-check observations, claim-batch contents,
  live-check job steps, resource-sweep per-host results. **Off in production** (results
  are durable in `scan` + aggregated in `crawler_metrics`; debug is for local
  troubleshooting).
- `info` — lifecycle events only: startup (config summary, secrets redacted per §1),
  graceful shutdown, run start/end (`run_id`, totals), Tranco import summary,
  migration/phase actions, and the API access log (below). Optionally one line per
  `crawler_metrics` checkpoint (~1k/day — acceptable).
- `warn` — actionable, non-fatal anomalies: preflight failure (alongside the webhook),
  quorum-inconsistency rate above threshold, claim starvation, lease-fence aborts (mirrors
  the `lease_lost` counter), Tranco import aborted by the sanity guard,
  webhook/heartbeat delivery failure, singleton-skip-that-should-not-happen.
- `error` — bugs and unexpected states only: recovered panics (chi
  `middleware.Recoverer` wired to slog), DB errors aborting a commit unit, invariant
  violations. **A domain that fails its scan is a scan observation, not an error** — it
  goes to `debug` + the metrics counters.

**Volume rule (normative).** In steady state nothing is emitted per-domain or per-check
above `debug`. Per-domain failures during incidents (e.g. a resolver outage) aggregate
into `crawler_metrics` error counters — alerted via Grafana (A3) and the daily
ops-webhook summary — **never** per-line warn/error spam. This keeps journald's default
rate limiting (`RateLimitBurst=10000`/30s) irrelevant even at 1M domains/day.

**API access log.** chi stack order (07-api.md — Middleware order, §1.7): `RealIP` → `RequestID` → **slog
access-log middleware** (a small custom one — do **not** use chi's default text logger) →
`Recoverer` → `Timeout(30s)` → CORS → security headers → per-route Cache-Control. One
`info` line per request with `request_id`, `method`, `path`, `status`, `bytes`,
`duration_ms`, `remote_ip`. **Exclude the health endpoints** (`GET /livez`, `GET /readyz` — 07-api.md §2.7) from the access log.

---

## 14. Makefile, `.golangci.yml`, CI

### 14.1 `Makefile`

Make is the universal interface; never invoke `go`/`golangci-lint`/`sqlc` directly when a
target exists. Builds all three binaries (the template's single-binary target is
generalized to the monorepo's three commands).

```makefile
GO      ?= go
GOFLAGS ?=
BINARIES := api crawler v6ctl

GOTESTSUM := $(shell command -v gotestsum 2>/dev/null)
ifdef GOTESTSUM
  TEST_CMD := gotestsum --format testdox --
else
  TEST_CMD := $(GO) test
endif

.PHONY: build build-linux test test-integration lint tidy vulncheck generate coverage compose-up compose-down clean help

##@ General
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Build
build: ## Build all three binaries
	@for b in $(BINARIES); do $(GO) build $(GOFLAGS) -o bin/$$b ./cmd/$$b; done

build-linux: ## Build static linux/amd64 release binaries
	@for b in $(BINARIES); do CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	  $(GO) build -ldflags="-s -w" -o dist/$$b ./cmd/$$b; done

##@ Quality
test: ## Run tests (race detector always on)
	$(TEST_CMD) -race ./...

lint: ## Run golangci-lint (vet, fmt, imports included)
	golangci-lint run ./...

test-integration: ## Integration tests (dockerized PG18+Timescale; //go:build integration files)
	$(TEST_CMD) -race -tags=integration ./...

coverage: ## Tests with coverage report
	$(TEST_CMD) -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

vulncheck: ## Scan for known vulnerabilities
	govulncheck ./...

##@ Codegen
generate: ## Regenerate sqlc + OpenAPI codegen (activates once openapi.yaml exists); fail if stale
	sqlc generate
	@if [ -f ../openapi/openapi.yaml ]; then \
	  oapi-codegen -config ../openapi/oapi-codegen.yaml ../openapi/openapi.yaml; \
	  openapi-typescript ../openapi/openapi.yaml -o ../openapi/schema.ts; \
	else \
	  echo "openapi/openapi.yaml not present yet (pre-P4.5) — sqlc only"; \
	fi
	@git diff --exit-code || (echo "generated code is out of date — run codegen and commit" && exit 1)

##@ Utility
tidy: ## Verify go.mod/go.sum are tidy
	$(GO) mod tidy
	@git diff --exit-code go.mod go.sum || (echo "go.mod/go.sum not tidy" && exit 1)

compose-up: ## Start the dev environment
	docker compose up --build -d

compose-down: ## Stop the dev environment and remove volumes
	docker compose down -v

clean: ## Remove build artifacts
	rm -rf bin dist coverage.out coverage.html

.DEFAULT_GOAL := help
```

Two decisions the recipes encode: **(a) `generate` is self-gating** — `openapi/openapi.yaml` is authored in P4.5, but `make generate` is an every-commit gate from Phase 0, so the oapi-codegen/openapi-typescript steps run only once the file exists (sqlc runs always); and **(b) the TypeScript schema is emitted at `openapi/schema.ts`**, never into `frontend/` — this build does not touch the frontend subtree (00-overview.md §4); the rebuilt frontend imports the schema from `openapi/`.

### 14.2 `.golangci.yml`

The house `.golangci.yml` from the Go stack template, verbatim (single lint target;
`errcheck govet staticcheck gosimple ineffassign unused gocritic goimports revive
errname errorlint exhaustive goconst godot misspell noctx prealloc unconvert unparam
wastedassign`; `goimports.local-prefixes: github.com/`; `revive` unexported-return
disabled; `gocritic` diagnostic/style/performance tags; `exhaustive`
default-signifies-exhaustive; `run.timeout: 5m`; `issues` max-issues 0).

### 14.3 `Dockerfile` (multi-stage, builds all three binaries)

```dockerfile
FROM golang:alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /api     ./cmd/api && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /crawler ./cmd/crawler && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /v6ctl   ./cmd/v6ctl

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /api /crawler /v6ctl /
ENTRYPOINT ["/api"]
```

Rolling `golang:alpine` (newest Go, no pin) and current distroless nonroot; compose
overrides the entrypoint per service (§9). Migrations are embedded via `go:embed`, so the
image needs no migrations directory. **The Dockerfile lives at `backend/Dockerfile` and
builds with `context: ./backend`** (00-overview.md §4) — the `COPY go.mod go.sum ./` and
`./cmd/*` paths resolve against the Go module root, not the monorepo root.

### 14.4 CI expectations (monorepo)

CI runs on every PR and on the default branch. The pipeline: `make tidy` → `make lint` →
`make generate` (**the generated-code staleness gate** — sqlc always; oapi-codegen +
openapi-typescript once `openapi/openapi.yaml` exists (§14.1), their committed output
current, honouring the brief's single-commit backend+API-schema sync promise) →
`make test` (unit suite: fake-DNS tests for `internal/consensus` + the resolver seam,
mapper/commit/classify vectors, OpenAPI contract + response tests for `internal/api` —
10-testing.md) → `make test-integration` (the dockerized Postgres+Timescale
`//go:build integration` suite for `internal/postgres` and the end-to-end scenarios —
10-testing.md) → `make vulncheck` → `make build-linux` (publishes the three release
artifacts consumed by the Ansible deploy, §6). Any stale generated output, untidy
go.mod, lint finding, or test failure fails the build.

---

## 15. Acceptance criteria

Fixture tables live in 10-testing.md; an implementation of this file is done when:

1. **Registry completeness:** every key in §2 is registered via `viper.SetDefault` at
   startup, resolves from its documented env var, and appears in the startup config
   summary; a config-key present in any other spec file but absent from §2 is a defect.
   The sole no-default key is `DATABASE_URL` — required in every binary and asserted by
   criterion 4, not by this completeness check. (There is no longer any one-shot
   importer/`migrate.*` carve-out: the legacy migration importer and its config were
   deleted when the cutover collapsed to a pure DNS flip, 08-migration-cutover.md.)
2. **Env override:** setting `WORKER_SLOTS`, `CONSENSUS_PER_PROVIDER_QPS`,
   `CRAWLER_RESOURCES_ENABLED`, and `RESOLVER_BULK_UPSTREAMS` (comma-separated) via the
   environment overrides their defaults with no YAML present; the loader starts cleanly
   when `/etc/whynoipv6/config.yaml` is absent.
3. **Secret redaction:** the info-level startup summary logs `DATABASE_URL` host+db only
   and webhook/ping URLs as `set`/`unset`; grepping the log for the DB password or a full
   webhook URL returns nothing.
4. **Required key:** every binary exits non-zero with a clear message when `DATABASE_URL`
   is empty.
5. **Units parse:** `systemd-analyze verify` passes on every file in `deploy/systemd/`;
   the timer inventory (§5) has one `.timer`+`.service` pair per scheduled job and every
   oneshot service sets `OnFailure=whynoipv6-notify@%n.service`.
6. **Deploy dry-run:** on a scratch host the §6 order (copy → `v6ctl migrate up` →
   restart crawler → restart api → verify) brings both units to `active` and
   `curl -6 http://[::1]:8080/livez` + `curl -6 http://[::1]:8080/readyz` both
   return `200` (07-api.md §2.7).
7. **nginx:** `nginx -t` passes on the vhost; a request to
   `/datasets/2026-07-06/whynoipv6-top1m.csv.gz` is served from disk with
   `Cache-Control: …immutable`, and `GET /datasets` is proxied to the API.
8. **compose:** `make compose-up` brings up db + 2 unbound + api + crawler; the crawler's
   bulk resolver answers through the compose Unbound services; `curl localhost:8080/readyz`
   returns `200`.
9. **Backups:** the phase-3 gate (§10.4) is reproducible in CI/staging — stanza create,
   full backup, `pgbackrest check`, and one scratch restore that starts the API and
   returns `changelog` rows.
10. **Liveness:** with an empty frontier the idle-checkpoint rule keeps a
    `crawler_metrics` row landing within 5 min so A1 does not false-fire; a killed crawler
    process trips its healthchecks.io check within ≤45 min.
11. **Logging:** in steady state no line above `debug` is emitted per-domain; a forced DB
    commit failure logs at `error`; a scan that fails does **not**.
```
