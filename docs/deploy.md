# Deploy & Operations

How to get WhyNoIPv6 running — the local Docker dev environment, every one-off
import/tool command, and the production layout. Architecture background is in
[`architecture.md`](architecture.md); the one-time production cutover checklist is
[`runbooks/cutover.md`](runbooks/cutover.md).

## 1. Local development (Docker Compose)

Everything dev-related runs from the repo root `compose.yaml`.

### Prerequisites

- Docker with the compose plugin.
- A gitignored `.env` at the repo root — **required**, not optional: `api`,
  `crawler`, and `geoip-init` declare it as an `env_file`, so `docker compose`
  refuses to start without it. Copy the tracked `.env.example` and fill it in.
- A free [IPinfo](https://ipinfo.io/signup) token for the GeoIP database, set as
  `IPINFO_TOKEN` in that `.env` — the `geoip-init` service uses it to fetch
  `ipinfo_lite.mmdb`.
- **Working IPv6 on the host.** The crawler preflights IPv6 egress before claiming
  any work; without v6 connectivity it will start but sit idle, retrying the
  preflight every 60s.

The same `.env` carries the four compose tuning knobs (all optional — compose
falls back to the defaults below):

| Variable | Default | What it changes |
| --- | --- | --- |
| `UNBOUND_DEV_PROFILE` | `dev` | which `deploy/unbound/<profile>/` config both recursors mount; `dev` caps fan-out for OrbStack laptops, `dev-fast` uses the prod numbers (`outgoing-range` 256 → 8192) |
| `CRAWLER_REPLICAS` | `1` | crawler processes; the prod shape is `2` |
| `CRAWLER_WORKER_SLOTS` | `4` | per-process fan-out; `4` on laptops (OrbStack's 16k UDP conntrack limit), `64` on native-netfilter hosts |
| `FRONTEND_API_URL` | `http://localhost:8080` | host-visible API origin baked into the frontend bundle at build time |

Those four are read by compose itself (replica count, volume paths, build args),
so they resolve by `${...}` interpolation and keep working via the defaults above
even when absent. Every *other* key in `.env` is passed straight through into the
`api`, `crawler`, and `geoip-init` containers — that is how the optional
`TAILLIGHT_URL` / `TAILLIGHT_API_KEY` log shipping (09-ops §13) reaches them. An
explicit `environment:` entry in `compose.yaml` wins over `.env` on conflicts.

### Start the stack

```sh
make compose-up          # = docker compose up --build -d
```

This brings up, in dependency order:

| Service | What it is |
| --- | --- |
| `db` | `timescale/timescaledb:latest-pg18`, host port `15432` → container `5432` (15432 because the dev host runs a native postgres on 5432), user/pass/db `whynoipv6` |
| `unbound1`, `unbound2` | bulk recursors, host ports `5301`/`5302` |
| `migrate` | init container: `v6ctl migrate up`, then exits |
| `api` | the API on `http://localhost:8080` |
| `frontend` | nginx-served production bundle on `http://localhost:8081` |
| `geoip-init` | init container: `v6ctl geoip update` → fetches `ipinfo_lite.mmdb` into the `geoip` volume |
| `crawler` | the scanning daemon (starts after `migrate` + `geoip-init` succeed) |
| `unbound-stats` | sidecar scraping both recursors into `unbound_stats` every 60s |
| `grafana` | dashboards + alert rules A1–A5 from `deploy/grafana/`, `http://127.0.0.1:3000` (admin/admin) |

Useful daily commands:

```sh
docker compose logs -f crawler            # watch the crawler work
docker compose logs -f api
docker compose exec db psql -U whynoipv6  # poke the database (container-internal)
psql -h localhost -p 15432 -U whynoipv6   # ...or from the host, on the published port
docker compose up -d --build api crawler  # rebuild + restart after a code change
make compose-down                         # stop everything AND delete the volumes
```

### Seed it with data

A fresh database has no domains. Import the Tranco top-1M (downloads the current
list, ~1M rows):

```sh
docker compose run --rm --entrypoint /v6ctl migrate tranco import
docker compose run --rm --entrypoint /v6ctl migrate tranco status   # verify
```

The crawler picks the new rows up immediately (rank order by default). Day-1 counts
read low by design: statuses only go public after N consecutive confirming scans.

### Frontend dev server

```sh
make frontend-dev        # vite on http://localhost:5173
```

The dev server reads `frontend/.env.development` for `VITE_API_URL` — point it at
`http://localhost:8080` (the compose API). `make frontend-build` type-checks and
builds the production bundle.

## 2. One-off tools & imports (the v6ctl catalog)

All operator actions are `v6ctl` verbs. In the dev stack, run them with the
`migrate` service as the base (it has `DATABASE_URL` wired up and no other job):

```sh
docker compose run --rm --entrypoint /v6ctl migrate <verb> [args...]
```

| Command | What it does |
| --- | --- |
| `migrate up` | apply pending schema migrations (embedded; forward-only, no `down`) |
| `migrate version` | show current schema version + dirty flag |
| `migrate force <n>` | stamp a version after manual repair |
| `tranco import [--force]` | one import attempt of the Tranco top-1M (`--force` bypasses the sanity guard: ≥950k rows, ≤2% delist) |
| `tranco status` | last 10 imports + staleness check |
| `campaign sync` | sync the campaign-YAML checkout into the DB (see below) |
| `campaign validate --repo <path> [--base <ref>]` | CI-style validation of campaign YAML, no DB needed |
| `geoip update [--token …] [--dir …]` | download a fresh IPinfo Lite mmdb (atomic replace; crawler hot-reloads hourly) |
| `provider seed [--path …]` | load the curated DNS-provider mapping (embedded list by default; idempotent) |
| `provider add <name> <ns-suffix>…` / `remove` / `list` | curate the DNS-provider mapping |
| `shame add <host> [--reason …]` / `remove` / `list` | curate the editorial top-shame list |
| `disable <host>` / `disable --service-list <file>` | manually disable hosts (e.g. service/CDN domains); `make service-list` is the dev-stack shortcut for the curated `service_domains.txt` |
| `enable <host>` | re-enable a manually disabled host |
| `service-candidates list` / `confirm <host>` / `dismiss <host>` | triage auto-detected service domains |
| `resource add <domain> <host> [--advisory]` / `remove` | curate manual page-resource links |
| `stats recalc` | re-run today's stats snapshots and counters |
| `export` | write the nightly static dataset snapshots (CSV.gz + Parquet) |
| `ops unbound-stats` | scrape both Unbound instances into `unbound_stats` |

Two verbs need more than `DATABASE_URL`:

```sh
# geoip update — no DB, needs the token + the geoip volume (this is what geoip-init runs):
docker compose run --rm geoip-init

# export — needs the datasets volume, so base it on the api service. --user root
# because the named volume is root-owned while the distroless image runs as
# nonroot (same mismatch geoip-init solves with `user: root`); the api reads
# the root-owned output files fine:
docker compose run --rm --user root --entrypoint /v6ctl api export

# campaign sync — the campaign volume (cloned/refreshed by campaign-init) is
# mounted on the crawler service, which also sets CAMPAIGN_PULL/PUSH=false
# (the distroless image has no git):
docker compose run --rm --entrypoint /v6ctl crawler campaign sync
```

### The DNS-provider mapping

`domain.dns_provider_id` — which managed-DNS operator runs a domain's zone, and
so the `/providers` league table — is stamped at scan-commit from the mapping in
the `dns_provider` table. **An empty table means every domain attributes to
NULL and the league table is empty**, so seeding is a required provisioning
step, like the GeoIP mmdb.

`v6ctl provider seed` loads the curated list embedded in the binary
([`backend/db/seed/dns_provider.yaml`](../backend/db/seed/dns_provider.yaml)) —
no files or config needed, and it is idempotent, so re-running after editing the
list only adds. Compose runs it as `provider-init` before the crawler starts;
production runs it once after `migrate up`. Point `--path` (or
`dns_provider.seed_path`) at your own file to override the embedded list.

Stamping happens on each domain's next scan commit, so edits take effect within
one frontier pass — there is no backfill step. To find operators still worth
adding, rank the unattributed domains by their observed nameserver suffix:

```sql
SELECT substring(ns FROM '[^.]+\.[^.]+\.?$') AS suffix, count(*) AS domains
FROM domain d
JOIN LATERAL (
  SELECT jsonb_object_keys(sd.details->'results'->'dns_ns_ipv6'->'details'->'nameservers') AS ns
  FROM scan_detail sd WHERE sd.domain_id = d.id ORDER BY sd.ts DESC LIMIT 1
) ns ON TRUE
WHERE d.dns_provider_id IS NULL AND d.rank IS NOT NULL
GROUP BY 1 ORDER BY domains DESC LIMIT 50;
```

## 3. Production deployment

Production is one operator VM: native binaries under systemd, nginx in front,
Postgres + TimescaleDB and two Unbound instances on the same host. Everything below
ships from [`deploy/`](../deploy/).

### Build & install

```sh
make build-linux         # static linux/amd64 binaries in backend/dist/
```

- Binaries → `/opt/whynoipv6/bin/{api,crawler,v6ctl}`
- Config → `/etc/whynoipv6/config.yaml` (optional; compiled defaults are sane) and
  `/etc/whynoipv6/env` (secrets: `DATABASE_URL`, `IPINFO_TOKEN`, `OPS_WEBHOOK_URL`, …).
  Config precedence: defaults < yaml < environment; every key is logged (redacted)
  at startup.
- State dirs: `/var/lib/whynoipv6/datasets` (snapshots), `/var/lib/GeoIP`
  (`ipinfo_lite.mmdb` — provision once with `v6ctl geoip update` before first
  crawler start; a missing mmdb is fatal).
- Migrations: `v6ctl migrate up` (also the rollout step for schema changes).
- Frontend bundle: `make frontend-build` (reads `frontend/.env.production` for
  `VITE_API_URL`), then copy the contents of `frontend/dist/` to
  `/var/www/whynoipv6.com/` — the root `deploy/nginx/whynoipv6.com.conf` serves.
  There is no Make target for the copy; it is a manual rsync today.

### systemd units ([`deploy/systemd/`](../deploy/systemd/))

Long-running services:

| Unit | Runs |
| --- | --- |
| `whynoipv6-api.service` | `/opt/whynoipv6/bin/api` (after postgresql) |
| `whynoipv6-crawler.service` | `/opt/whynoipv6/bin/crawler` (after postgresql + both unbounds; 90s stop budget for the drain) |
| `unbound@1.service`, `unbound@2.service` | the two bulk recursors ([`deploy/unbound/`](../deploy/unbound/): `127.0.0.1:53` and `:5353`, remote-control on `8953`/`8954`) |

Timers (each oneshot alerts the ops webhook via `whynoipv6-notify@` on failure):

| Timer | Schedule | Runs |
| --- | --- | --- |
| `whynoipv6-export.timer` | daily 04:30 | `v6ctl export` (dataset snapshots) |
| `whynoipv6-logical-export.timer` | Sun 04:30 | zstd CSV export of `domain`+`changelog`, rsynced off-host |
| `whynoipv6-unbound-stats.timer` | every minute | `v6ctl ops unbound-stats` |
| `pgbackrest-diff.timer` | Mon–Sat 03:30 | differential backup |
| `pgbackrest-full.timer` | Sun 03:30 | full backup |
| `pgbackrest-verify.timer` | daily 05:00 | backup freshness/WAL monitor |

The crawler needs **no external scheduler**: the daily housekeeping tick (03:30 UTC)
and the Tranco import (23:15 UTC, 2h retries) are internal schedules, deduplicated
across processes by Postgres advisory locks.

> **GeoIP refresh note:** the `deploy/systemd/geoipupdate.timer.d/` drop-in and
> `deploy/geoip/GeoIP.conf` are the legacy MaxMind path. The crawler now reads
> IPinfo Lite (ADR 0001); recurring refresh in production is `v6ctl geoip update`
> — a dedicated timer unit for it is an open follow-up.

### nginx ([`deploy/nginx/`](../deploy/nginx/))

- `api.whynoipv6.com.conf` — proxies to the api on loopback `[::1]:8080`, serves
  `/datasets` as static files straight from the export directory.
- `whynoipv6.com.conf` — serves the production frontend bundle from
  `/var/www/whynoipv6.com` (the deploy target of `frontend/dist/`) as a SPA,
  including the legacy singular→plural 301 map (`/domain/x` → `/domains/x`).
  umami runs fully first-party so ad blockers don't drop it: `/stats.js` and
  `/api/send` proxy to Umami Cloud (resolved at request time via the host's
  unbound instances), with `X-Umami-Client-IP` carrying the real visitor IP
  for geolocation.

### Backups ([`deploy/pgbackrest/`](../deploy/pgbackrest/))

pgBackRest to an off-host sftp repo (encrypted, zstd, 4 full backups retained), with
the verify timer alerting if the newest backup is older than 26h or WAL archiving
stalls. Weekly logical CSV exports are the second, independent restore path.

### Monitoring

Grafana alert rules are provisioned from
[`deploy/grafana/alerts.yaml`](../deploy/grafana/alerts.yaml): crawler stalled,
frontier lag, error ratio, TimescaleDB job failures, Unbound down. Heartbeats go to
healthchecks.io (`ops.healthcheck_url`), failures to the ops webhook
(`ops.webhook_url`).

### Campaign repo automation ([`deploy/campaign-repo/`](../deploy/campaign-repo/))

Workflows staged for the separate `whynoipv6-campaign` repo: PRs are validated with
`v6ctl campaign validate` (bot comment, no DB), and a push to `main` dispatches a
`campaign-sync` event so the backend can run `v6ctl campaign sync`.
