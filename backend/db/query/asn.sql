-- db/query/asn.sql — sqlc query source (layout: 05-schema.md §10.2).

-- Sentinel lookup: binaries resolve the sentinel ASN once at startup by
-- lookup, never by literal id (05-schema.md §5).
-- name: ASNSentinelID :one
SELECT id FROM asn WHERE number = 0;

-- Tick step 3 — ASN + DNS-provider counter recompute (06-ingest.md §10.6).
-- name: ResetASNCounters :exec
UPDATE asn SET count_total = 0, count_v6 = 0;

-- name: RecomputeASNCounters :exec
UPDATE asn a SET
  count_total = agg.count_total,
  count_v6    = agg.count_v6
FROM (
  SELECT asn_id,
         count(*)                                                      AS count_total,
         count(*) FILTER (WHERE classification IN ('partial','hero'))  AS count_v6
  FROM domain
  WHERE rank IS NOT NULL AND NOT disabled
  GROUP BY asn_id
) agg
WHERE a.id = agg.asn_id;

-- name: ResetProviderCounters :exec
UPDATE dns_provider SET count_total = 0, count_v6 = 0;

-- name: RecomputeProviderCounters :exec
UPDATE dns_provider p SET
  count_total = agg.count_total,
  count_v6    = agg.count_v6
FROM (
  SELECT dns_provider_id,
         count(*)                                                      AS count_total,
         count(*) FILTER (WHERE classification IN ('partial','hero'))  AS count_v6
  FROM domain
  WHERE rank IS NOT NULL AND NOT disabled AND dns_provider_id IS NOT NULL
  GROUP BY dns_provider_id
) agg
WHERE p.id = agg.dns_provider_id;
