-- =============================================================================
-- 000001_base_schema.up.sql — WhyNoIPv6 base schema
-- All DDL is owned by docs/spec/05-schema.md. Do not add DDL elsewhere.
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- -----------------------------------------------------------------------------
-- Enums
-- -----------------------------------------------------------------------------

-- Public status model: what the API/classification ever sees.
CREATE TYPE ipv6_status AS ENUM ('supported', 'unsupported', 'no_record', 'not_applicable');

-- Raw observation outcomes; internal only. 'partial', 'error' and 'inconsistent'
-- never reach public output (classification and the API read only the confirmed
-- ipv6_status columns, which remain 4-valued).
CREATE TYPE observation AS ENUM
  ('supported', 'partial', 'unsupported', 'no_record', 'not_applicable',
   'error', 'inconsistent');

CREATE TYPE domain_kind     AS ENUM ('apex', 'subdomain');
CREATE TYPE created_by      AS ENUM ('tranco', 'campaign', 'parent_link', 'live_check');
CREATE TYPE classification  AS ENUM ('unknown', 'inactive', 'sinner', 'partial', 'hero');
CREATE TYPE disabled_reason AS ENUM ('dead', 'service', 'manual', 'delisted');
CREATE TYPE resource_source AS ENUM ('discovered', 'manual');
CREATE TYPE check_job_status AS ENUM ('pending', 'processing', 'done', 'failed');

-- -----------------------------------------------------------------------------
-- Reference tables (must precede domain: asn_id/country_id are NOT NULL FKs)
-- -----------------------------------------------------------------------------

CREATE TABLE asn (
  id          INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  number      BIGINT NOT NULL UNIQUE,
  name        TEXT NOT NULL DEFAULT 'Unknown',
  count_total INT NOT NULL DEFAULT 0,   -- recomputed by the daily tick (06)
  count_v6    INT NOT NULL DEFAULT 0    -- recomputed by the daily tick (06)
);

CREATE TABLE country (
  id      INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  code    CHAR(2) NOT NULL UNIQUE,
  name    TEXT NOT NULL,
  tld     TEXT,                          -- dot-prefixed uppercase, e.g. '.NO'
  sites   INT NOT NULL DEFAULT 0,        -- recomputed by the daily tick (06)
  v6sites INT NOT NULL DEFAULT 0,        -- recomputed by the daily tick (06)
  percent NUMERIC(5,2) NOT NULL DEFAULT 0  -- (5,2): kills the legacy pgtype ÷10 hack
);

-- DNS-provider league table (OPEN-4): ns_host suffix -> provider mapping.
-- Must precede domain (domain.dns_provider_id is a nullable FK). The crawler
-- resolves a domain's provider by longest matching nameserver-host suffix in
-- ns_suffixes at scan commit (06); binary inclusion + counts only, no scores.
CREATE TABLE dns_provider (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name        TEXT NOT NULL,
  ns_suffixes TEXT[] NOT NULL DEFAULT '{}', -- nameserver host suffixes mapping to this provider
  count_total INT NOT NULL DEFAULT 0,       -- domains mapped to this provider; recomputed by the daily tick (06), mirrors asn
  count_v6    INT NOT NULL DEFAULT 0        -- of those, classification hero|partial; recomputed by the daily tick (06), mirrors asn
);

-- -----------------------------------------------------------------------------
-- domain — entity + confirmed state + frontier (one wide table, ~60 columns)
-- -----------------------------------------------------------------------------

CREATE TABLE domain (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  host          TEXT NOT NULL UNIQUE,          -- lowercase punycode FQDN (Canonicalize)
  kind          domain_kind NOT NULL DEFAULT 'apex',
  parent_id     BIGINT REFERENCES domain(id),  -- subdomain -> registrable parent
  rank          INT,                           -- Tranco rank; NULL = unranked
  created_by    created_by NOT NULL,

  -- Confirmed status per core dimension (NULL = never confirmed).
  -- One 5-column group per dimension d in {base, www, ns, mx, conn, resources}:
  --   d_status        confirmed value -> public
  --   d_observed      last raw observation (debug/telemetry only)
  --   d_pending       candidate value awaiting confirmation
  --   d_pending_count consecutive counted observations of d_pending
  --   d_since         when the confirmed value last changed
  base_status              ipv6_status,
  base_observed            observation,
  base_pending             ipv6_status,
  base_pending_count       SMALLINT NOT NULL DEFAULT 0,
  base_since               TIMESTAMPTZ,

  www_status               ipv6_status,
  www_observed             observation,
  www_pending              ipv6_status,
  www_pending_count        SMALLINT NOT NULL DEFAULT 0,
  www_since                TIMESTAMPTZ,

  ns_status                ipv6_status,
  ns_observed              observation,
  ns_pending               ipv6_status,
  ns_pending_count         SMALLINT NOT NULL DEFAULT 0,
  ns_since                 TIMESTAMPTZ,

  mx_status                ipv6_status,
  mx_observed              observation,
  mx_pending               ipv6_status,
  mx_pending_count         SMALLINT NOT NULL DEFAULT 0,
  mx_since                 TIMESTAMPTZ,

  conn_status              ipv6_status,
  conn_observed            observation,
  conn_pending             ipv6_status,
  conn_pending_count       SMALLINT NOT NULL DEFAULT 0,
  conn_since               TIMESTAMPTZ,

  resources_status         ipv6_status,
  resources_observed       observation,
  resources_pending        ipv6_status,
  resources_pending_count  SMALLINT NOT NULL DEFAULT 0,
  resources_since          TIMESTAMPTZ,

  -- Informational dimensions: latest observation only, no confirmation machinery.
  dnssec_observed  observation,
  ptr_observed     observation,
  smtp_observed    observation,
  parity_observed  observation,
  latency_v4_ms    INT,
  latency_v6_ms    INT,

  -- Materialized classification, recomputed on every confirmed commit (03).
  classification  classification NOT NULL DEFAULT 'unknown',
  class_flags     TEXT[] NOT NULL DEFAULT '{}',   -- broken_v6, www_missing, ns_missing,
                                                  -- mail_missing, resources_v4only
  gold            BOOLEAN NOT NULL DEFAULT FALSE, -- hero + all resources v6 (badge)

  asn_id      INT NOT NULL REFERENCES asn(id),     -- sentinel row when unknown (§6);
  country_id  INT NOT NULL REFERENCES country(id), --   no serializer ever handles NULL

  -- League-table pivots (07 filters/leaderboards: ?tld=, ?provider=). Filled at
  -- ingest/commit (06); exposed on the domain resource (07 §5.2/§5.3).
  tld              TEXT,                            -- eTLD/public suffix (publicsuffix);
                                                    --   ingest sets it NOT NULL for apex rows (06)
  dns_provider_id  BIGINT REFERENCES dns_provider(id), -- ns_host suffix -> provider (OPEN-4);
                                                    --   NULL until a mapping matches
  hosting_provider TEXT,                            -- normalized CDN/hosting tag (CNAME-chain CDN
                                                    --   detection + resolved-IP ASN); NULL when unknown
  -- registrar is deliberately DEFERRED (RDAP cost at 1M scale) — a future column, not added now.

  disabled        BOOLEAN NOT NULL DEFAULT FALSE,
  disabled_reason disabled_reason,
  disabled_at     TIMESTAMPTZ,

  -- Lifecycle bookkeeping (04)
  dead_streak       SMALLINT NOT NULL DEFAULT 0, -- consecutive unresolvable scans
  orphaned_at       TIMESTAMPTZ,                 -- linkage lost; starts the 30d delist grace
  last_requested_at TIMESTAMPTZ,                 -- last POST /check for this host

  -- Frontier / scheduling (04)
  next_check_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  claimed_at      TIMESTAMPTZ,                 -- worker lease; reclaim after 30 min
  last_checked_at TIMESTAMPTZ,
  last_counted_at TIMESTAMPTZ,                 -- last scan that advanced anti-flap counters (03)
  error_streak    SMALLINT NOT NULL DEFAULT 0, -- consecutive non-definitive base/www scans

  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_domain_host_trgm ON domain USING gin (host gin_trgm_ops); -- search
-- Claim-path index: leading range column = next_check_at, so the claim scans
-- ONLY the due set. Predicate must textually match the claim query (04).
CREATE INDEX idx_domain_due ON domain (next_check_at)
  WHERE NOT disabled OR disabled_reason IN ('dead', 'delisted');
CREATE INDEX idx_domain_rank    ON domain (rank) WHERE rank IS NOT NULL;
CREATE INDEX idx_domain_sinners ON domain (rank)
  WHERE classification = 'sinner' AND rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_heroes  ON domain (rank)
  WHERE classification = 'hero'   AND rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_partial ON domain (rank)
  WHERE classification = 'partial' AND rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_parent  ON domain (parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX idx_domain_country ON domain (country_id, classification, rank)
  WHERE rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_asn     ON domain (asn_id, classification, rank)
  WHERE rank IS NOT NULL AND NOT disabled;
-- Same shape as idx_domain_country: an ASN-scoped rank-keyset page
-- (GET /asns/{n}/domains, 07) must seek within the ASN, not sort a
-- hyperscaler's 100k+ rows per page.
-- Provider/TLD pivots are allowed only under an indexed public scope (OPEN-2: no
-- GIN, no unscoped provider/tld scan). These mirror idx_domain_country's shape so
-- the ?tld=/?provider= filters (07) compose with class + rank ordering.
CREATE INDEX idx_domain_tld ON domain (tld, classification, rank)
  WHERE rank IS NOT NULL AND NOT disabled;
CREATE INDEX idx_domain_dns_provider ON domain (dns_provider_id, classification, rank)
  WHERE dns_provider_id IS NOT NULL AND rank IS NOT NULL AND NOT disabled;

-- Table storage settings: the claim/commit cycle updates every active row >=2x/day —
-- claimed_at at claim, next_check_at + status columns at commit; the commit update
-- is always non-HOT because next_check_at is indexed.
ALTER TABLE domain SET (
  fillfactor = 90,
  autovacuum_vacuum_scale_factor = 0.02,
  autovacuum_analyze_scale_factor = 0.02
);

-- -----------------------------------------------------------------------------
-- Campaigns — membership, not duplication
-- -----------------------------------------------------------------------------

CREATE TABLE campaign (
  id          INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  uuid        UUID NOT NULL UNIQUE,              -- from YAML; API exposes the raw UUID (07)
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  source_file TEXT,                              -- provenance: YAML filename
  tags        TEXT[],                            -- mandate/campaign tags (OPEN-12); backs the
                                                 --   ?tag= filter and the /mandates surface (07).
                                                 --   TEXT[] over a campaign_tag table: simplest
                                                 --   form, no join (rejected: the heavier table).
  disabled    BOOLEAN NOT NULL DEFAULT FALSE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_campaign_tags ON campaign USING gin (tags);  -- ?tag= filter + /mandates (07)

CREATE TABLE campaign_domain (
  campaign_id INT NOT NULL REFERENCES campaign(id) ON DELETE CASCADE,
  domain_id   BIGINT NOT NULL REFERENCES domain(id),
  added_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (campaign_id, domain_id)
);
CREATE INDEX idx_campaign_domain_domain ON campaign_domain (domain_id);

-- -----------------------------------------------------------------------------
-- Resources (issue #23) — globally deduped host registry + dependency links
-- -----------------------------------------------------------------------------

CREATE TABLE resource_host (
  id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  host               TEXT NOT NULL UNIQUE,       -- lowercase punycode (Canonicalize)
  aaaa_status        ipv6_status,                -- confirmed (host-level N=2 anti-flap)
  aaaa_pending       ipv6_status,
  aaaa_pending_count SMALLINT NOT NULL DEFAULT 0,
  last_checked_at    TIMESTAMPTZ,
  next_check_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  dependent_count    INT NOT NULL DEFAULT 0      -- maintained on link/unlink (06)
);
CREATE INDEX idx_resource_host_due ON resource_host (next_check_at);

CREATE TABLE domain_resource (
  domain_id        BIGINT NOT NULL REFERENCES domain(id) ON DELETE CASCADE,
  resource_host_id BIGINT NOT NULL REFERENCES resource_host(id),
  source           resource_source NOT NULL,     -- discovered | manual
  required         BOOLEAN NOT NULL DEFAULT TRUE, -- manual entries can be advisory-only
  first_seen       TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen        TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (domain_id, resource_host_id)      -- operator add upgrades source to 'manual'
);
CREATE INDEX idx_domain_resource_host ON domain_resource (resource_host_id);
                                                 -- "who depends on X" (advocacy gold)

-- -----------------------------------------------------------------------------
-- Service-domain review queue (candidates, never auto-disable)
-- -----------------------------------------------------------------------------

CREATE TABLE service_candidate (
  domain_id   BIGINT PRIMARY KEY REFERENCES domain(id),
  reasons     TEXT[] NOT NULL,
  detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  dismissed   BOOLEAN NOT NULL DEFAULT FALSE
);

-- -----------------------------------------------------------------------------
-- On-demand live checks (07), SKIP LOCKED consumer with a claimed_at lease
-- -----------------------------------------------------------------------------

CREATE TABLE check_job (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  host         TEXT NOT NULL,                 -- validated, lowercase punycode
  requester_ip INET NOT NULL,
  status       check_job_status NOT NULL DEFAULT 'pending',
  claimed_at   TIMESTAMPTZ,                   -- consumer lease; reclaim after 5 min
  result       JSONB,                         -- shared-mapper output (07)
  error        TEXT,                          -- set when status = 'failed'
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ
);
CREATE INDEX idx_check_job_claim ON check_job (created_at)
  WHERE status IN ('pending','processing');                         -- claim + reaper
CREATE INDEX idx_check_job_requester ON check_job (requester_ip, created_at); -- rate limiting
CREATE INDEX idx_check_job_host_done ON check_job (host, completed_at DESC)
  WHERE status = 'done';                                            -- host-side dedupe

-- -----------------------------------------------------------------------------
-- Editorial picks (written only by v6ctl shame add, 06)
-- -----------------------------------------------------------------------------

CREATE TABLE top_shame (
  domain_id BIGINT PRIMARY KEY REFERENCES domain(id),
  reason    TEXT,
  added_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- -----------------------------------------------------------------------------
-- Tranco import provenance
-- -----------------------------------------------------------------------------

CREATE TABLE tranco_import (
  id              INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  list_id         TEXT NOT NULL,
  list_date       DATE NOT NULL,
  line_count      INT,                        -- raw CSV lines
  imported_count  INT,                        -- rows in the upsert insert
  delisted        INT,                        -- ranked yesterday, absent today
  rejected_count  INT,                        -- failed Canonicalize/validation
  duplicate_count INT,                        -- staging fold count (>0 is normal)
  aborted         BOOLEAN NOT NULL DEFAULT FALSE,  -- import sanity guard
  note            TEXT,                       -- abort reason
  imported_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- One successful import per list; abort rows may repeat (a --force retry of an
-- aborted list must still be able to record its success). Target of the
-- idempotent-write guard's ON CONFLICT (list_id) WHERE NOT aborted DO NOTHING (06).
CREATE UNIQUE INDEX idx_tranco_import_list ON tranco_import (list_id) WHERE NOT aborted;

-- -----------------------------------------------------------------------------
-- Product stats snapshots (public, API-served, forever). Snapshots of CONFIRMED
-- state, written by the daily tick (06); serve the /stats endpoints (07).
-- Plain tables except stats_asn_daily (~50-80k rows/day -> hypertable in 000002).
-- -----------------------------------------------------------------------------

CREATE TABLE stats_global_daily (
  day DATE PRIMARY KEY,
  domains INT, sinners INT, partial INT, heroes INT, gold INT, inactive INT,
  unknown INT, disabled INT,
  base_supported INT, www_supported INT, ns_supported INT, mx_supported INT,
  conn_supported INT, resources_supported INT,
  top_heroes INT,        -- Tranco top-1000 with web-facing IPv6
  top_nameserver INT,    -- Tranco top-1000 with IPv6-capable nameservers
  generated_at TIMESTAMPTZ  -- crawl-freshness signal set by the rollup (06); deterministic
                            --   source for the envelope meta.as_of (07). NULL -> as_of falls
                            --   back to day at 00:00:00Z; meta.generation derives from max(day).
);

CREATE TABLE stats_country_daily (
  day DATE, country_id INT, domains INT, sinners INT, partial INT, heroes INT,
  base_supported INT, conn_supported INT,
  PRIMARY KEY (day, country_id)
);

CREATE TABLE stats_campaign_daily (
  day DATE, campaign_id INT, domains INT, v6_ready INT, sinners INT, partial INT,
  heroes INT, base_supported INT, www_supported INT, ns_supported INT,
  mx_supported INT, conn_supported INT,
  PRIMARY KEY (day, campaign_id)
);

CREATE TABLE stats_asn_daily (
  day TIMESTAMPTZ NOT NULL, asn_id INT NOT NULL,
  domains INT, v6_domains INT, sinners INT, heroes INT,
  PRIMARY KEY (asn_id, day)
);

-- -----------------------------------------------------------------------------
-- Time-series tables (hypertable conversion + policies in 000002; no FKs)
-- -----------------------------------------------------------------------------

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
-- No extra (domain_id, ts DESC) index: the primary key (domain_id, ts) already
-- serves backward per-domain scans; an additional index is pure write/storage overhead.

-- Fat scan payload: full engine Details JSONB (per-check evidence, record sets,
-- TLS cert info, resource host lists). Short retention; debugging + detail page.
-- tls and spf deliberately have NO typed columns anywhere: informational-only,
-- they live exclusively in details JSONB (accepted design, not an omission).
CREATE TABLE scan_detail (
  domain_id   BIGINT NOT NULL,
  ts          TIMESTAMPTZ NOT NULL,
  details     JSONB NOT NULL,
  duration_ms INT,
  PRIMARY KEY (domain_id, ts)
);
-- No result_id column: idempotency is the worker-fixed timestamp T +
-- ON CONFLICT (domain_id, ts) DO NOTHING under the PK (03); no unique constraint
-- on a synthetic id is possible on a hypertable and nothing consumes it.

-- Structured field-level changelog. FOREVER — the credibility surface.
-- Start-fresh (OPEN-9): no history import, so there are no legacy/unmappable rows —
-- the legacy_message/legacy_status columns, the field='legacy' domain value, and the
-- three legacy CHECK constraints are gone; old_value/new_value are NOT NULL outright.
CREATE TABLE changelog (
  domain_id BIGINT NOT NULL,
  ts        TIMESTAMPTZ NOT NULL DEFAULT now(),
  field     TEXT NOT NULL,                     -- base|www|ns|mx|conn|resources
  old_value ipv6_status NOT NULL,              -- never NULL: the first confirmation of a
                                               --   dimension writes no changelog row at all (03)
  new_value ipv6_status NOT NULL,
  PRIMARY KEY (domain_id, ts, field)
);
CREATE INDEX idx_changelog_ts ON changelog (ts DESC);  -- global recent-changes feed
-- (The PK leads with domain_id and cannot serve the sitewide GET /changelog feed.)

-- Operational crawler metrics (Grafana only). Checkpoint rows per run.
CREATE TABLE crawler_metrics (
  ts TIMESTAMPTZ NOT NULL DEFAULT now(),
  run_id UUID NOT NULL, worker TEXT NOT NULL,
  processed INT, succeeded INT, failed INT, qps REAL,
  p50_ms INT, p99_ms INT, active_slots INT, queue_depth INT,
  dim_counters JSONB,                          -- per-dimension tallies; includes the
                                               --   lease_lost counter (03 fence aborts)
  geoip_build_epoch TIMESTAMPTZ,               -- build date of the loaded mmdb pair (06);
                                               --   backs the Grafana GeoIP-staleness alert (09)
  is_final BOOLEAN NOT NULL DEFAULT FALSE
);

-- Unbound resolver metrics (Grafana only), one row/min/host from
-- `v6ctl ops unbound-stats` parsing `unbound-control stats` (resetting variant,
-- so every row holds per-interval deltas). Mechanism: 09-ops.md.
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
