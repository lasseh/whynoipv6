-- db/query/stats.sql — sqlc query source (layout: 05-schema.md §10.2).

-- Tick step 2 — the four confirmed-state snapshot upserts (06-ingest.md §10).

-- name: SnapshotGlobalDaily :exec
-- The aggregates below run FROM domain WHERE rank IS NOT NULL, so they all
-- count the ranked subset. `live` deliberately does not inherit that
-- predicate: tracked_total and the PTR/SMTP pairs describe the whole live
-- population. Folding them into the main FILTER list would silently
-- reproduce `domains`, and nothing would catch it until someone compared the
-- two columns. Referenced five times below, so PG materializes it once.
--
-- The PTR clause admits 'partial' and the SMTP clause does not: infoSMTP
-- (internal/observe/observe.go) folds a partial EHLO to unsupported before
-- storage, because a half-working EHLO does not accept mail. The
-- base_status / mx_status guards are load-bearing — both observation columns
-- are overwritten on every commit regardless of the confirmed status, so
-- dropping the guards balloons the denominators.
WITH live AS (
  SELECT
    count(*) FILTER (WHERE NOT disabled) AS tracked_total,
    count(*) FILTER (WHERE NOT disabled AND base_status = 'supported'
                       AND ptr_observed IN ('supported','partial')) AS ptr_supported,
    count(*) FILTER (WHERE NOT disabled AND base_status = 'supported'
                       AND ptr_observed IN ('supported','partial','unsupported')) AS ptr_graded,
    count(*) FILTER (WHERE NOT disabled AND mx_status = 'supported'
                       AND smtp_observed = 'supported') AS smtp_supported,
    count(*) FILTER (WHERE NOT disabled AND mx_status = 'supported'
                       AND smtp_observed IN ('supported','unsupported')) AS smtp_graded
  FROM domain
)
INSERT INTO stats_global_daily (
  day, domains, sinners, partial, heroes, saints, inactive, unknown, disabled,
  base_supported, www_supported, ns_supported, mx_supported, conn_supported,
  resources_supported, top_heroes, top_nameserver,
  tracked_total, ptr_supported, ptr_graded, smtp_supported, smtp_graded,
  generated_at)
SELECT
  CURRENT_DATE,
  count(*) FILTER (WHERE NOT disabled),
  count(*) FILTER (WHERE NOT disabled AND classification = 'sinner'),
  count(*) FILTER (WHERE NOT disabled AND classification = 'partial'),
  count(*) FILTER (WHERE NOT disabled AND classification = 'hero'),
  count(*) FILTER (WHERE NOT disabled AND saint),
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
  (SELECT tracked_total  FROM live),
  (SELECT ptr_supported  FROM live),
  (SELECT ptr_graded     FROM live),
  (SELECT smtp_supported FROM live),
  (SELECT smtp_graded    FROM live),
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
  tracked_total       = excluded.tracked_total,
  ptr_supported       = excluded.ptr_supported,
  ptr_graded          = excluded.ptr_graded,
  smtp_supported      = excluded.smtp_supported,
  smtp_graded         = excluded.smtp_graded,
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
  -- the campaign v6-ready predicate — must match domain.V6Ready (07 §3.2)
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
SELECT day, domains, sinners, partial, heroes, saints, inactive, unknown, disabled,
       base_supported, www_supported, ns_supported, mx_supported, conn_supported,
       resources_supported, top_heroes, top_nameserver,
       tracked_total, ptr_supported, ptr_graded, smtp_supported, smtp_graded
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

-- The multi-network series behind GET /stats/networks: the top-N networks in
-- one request, because the panel draws seven small multiples and doing that
-- through /asns/{number}/stats is seven round trips.
--
-- Keyed on asn_id and reported as asn.number, never grouped by asn.name.
-- Names are not unique — five ASNs are called "Google LLC", six "Microsoft
-- Corporation" — so a name-keyed aggregate averages unrelated networks
-- together. That defect has been shipped twice already (fabricated Hetzner
-- movement in a chart, and two Grafana panels fixed in 6711b2f). Two rows here
-- may legitimately share a name; the stable key is the number.
--
-- Selection is by size on the newest day *inside the requested window*, so a
-- historical window ranks by what was large then rather than by what is large
-- now. AS0 is the Unknown sentinel seeded in 000003 and is never a network.
-- name: StatsTopNetworks :many
WITH bounds AS (
  SELECT max(day) AS newest FROM stats_asn_daily
  WHERE day >= @from_day::timestamptz AND day <= @to_day::timestamptz
), top AS (
  SELECT s.asn_id,
         row_number() OVER (ORDER BY s.domains DESC NULLS LAST, s.asn_id ASC) AS rank
  FROM stats_asn_daily s
  JOIN asn a ON a.id = s.asn_id
  WHERE s.day = (SELECT newest FROM bounds) AND a.number <> 0
  ORDER BY s.domains DESC NULLS LAST, s.asn_id ASC
  LIMIT @top_n
)
SELECT a.number AS asn, a.name, s.day, s.domains, s.v6_domains
FROM stats_asn_daily s
JOIN top t ON t.asn_id = s.asn_id
JOIN asn a ON a.id = s.asn_id
WHERE s.day >= @from_day::timestamptz AND s.day <= @to_day::timestamptz
ORDER BY t.rank ASC, s.day ASC;

-- name: StatsASNRange :many
SELECT day, domains, v6_domains, sinners, heroes
FROM stats_asn_daily
WHERE asn_id = @asn_id AND day >= @from_day::timestamptz AND day <= @to_day::timestamptz
ORDER BY day ASC;

-- The nightly dataset export (07 §5.3): one parameterized read covers the
-- three tiers — ranked_only for top100k/top1m, max_rank 0 = unbounded.
-- name: ExportRows :many
SELECT d.host, d.rank, d.kind, p.host AS parent,
       d.classification, d.class_flags, d.saint,
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
