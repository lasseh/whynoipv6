-- db/query/stats.sql — sqlc query source (layout: 05-schema.md §10.2).

-- Tick step 2 — the four confirmed-state snapshot upserts (06-ingest.md §10).

-- name: SnapshotGlobalDaily :exec
INSERT INTO stats_global_daily (
  day, domains, sinners, partial, heroes, gold, inactive, unknown, disabled,
  base_supported, www_supported, ns_supported, mx_supported, conn_supported,
  resources_supported, top_heroes, top_nameserver, generated_at)
SELECT
  CURRENT_DATE,
  count(*) FILTER (WHERE NOT disabled),
  count(*) FILTER (WHERE NOT disabled AND classification = 'sinner'),
  count(*) FILTER (WHERE NOT disabled AND classification = 'partial'),
  count(*) FILTER (WHERE NOT disabled AND classification = 'hero'),
  count(*) FILTER (WHERE NOT disabled AND gold),
  count(*) FILTER (WHERE NOT disabled AND classification = 'inactive'),
  count(*) FILTER (WHERE NOT disabled AND classification = 'unknown'),
  count(*) FILTER (WHERE disabled),
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
  gold                = excluded.gold,
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

-- name: SnapshotCountryDaily :exec
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

-- name: SnapshotCampaignDaily :exec
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

-- name: SnapshotASNDaily :exec
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

-- The envelope meta source (07-api.md §2.4): generation = YYYYMMDD of
-- max(stats_global_daily.day); as_of = its generated_at, falling back to
-- day at 00:00:00Z when NULL (the day-0 seed row).
-- name: StatsGeneration :one
SELECT day, generated_at FROM stats_global_daily ORDER BY day DESC LIMIT 1;

-- The §4.10 time-series reads: bounded windows (≤366 rows/yr), ascending;
-- weekly sampling happens API-side over the fetched window.

-- name: StatsGlobalRange :many
SELECT day, domains, sinners, partial, heroes, gold, inactive, unknown, disabled,
       base_supported, www_supported, ns_supported, mx_supported, conn_supported,
       resources_supported, top_heroes, top_nameserver
FROM stats_global_daily
WHERE day >= @from_day AND day <= @to_day
ORDER BY day ASC;

-- name: StatsCountryRange :many
SELECT day, domains, sinners, partial, heroes, base_supported, conn_supported
FROM stats_country_daily
WHERE country_id = @country_id AND day >= @from_day AND day <= @to_day
ORDER BY day ASC;

-- name: StatsCampaignRange :many
SELECT day, domains, v6_ready, sinners, partial, heroes, base_supported,
       www_supported, ns_supported, mx_supported, conn_supported
FROM stats_campaign_daily
WHERE campaign_id = @campaign_id AND day >= @from_day AND day <= @to_day
ORDER BY day ASC;

-- name: StatsASNRange :many
SELECT day, domains, v6_domains, sinners, heroes
FROM stats_asn_daily
WHERE asn_id = @asn_id AND day >= @from_day::timestamptz AND day <= @to_day::timestamptz
ORDER BY day ASC;

-- The nightly dataset export (07 §5.3): one parameterized read covers the
-- three tiers — ranked_only for top100k/top1m, max_rank 0 = unbounded.
-- name: ExportRows :many
SELECT d.host, d.rank, d.kind, p.host AS parent,
       d.classification, d.class_flags, d.gold,
       d.base_status, d.www_status, d.ns_status,
       d.mx_status, d.conn_status, d.resources_status,
       d.base_since, d.www_since, d.ns_since, d.mx_since, d.conn_since, d.resources_since,
       d.tld, c.code, a.number AS asn,
       dp.name AS dns_provider, d.hosting_provider, d.last_checked_at
FROM domain d
JOIN country c ON c.id = d.country_id
JOIN asn a ON a.id = d.asn_id
LEFT JOIN dns_provider dp ON dp.id = d.dns_provider_id
LEFT JOIN domain p ON p.id = d.parent_id
WHERE NOT d.disabled
  AND (NOT @ranked_only::bool OR d.rank IS NOT NULL)
  AND (@max_rank::int = 0 OR d.rank <= @max_rank)
ORDER BY d.rank ASC NULLS LAST, d.id ASC;
