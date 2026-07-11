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

-- ASN auto-registration (06-ingest.md §6.3): pool-side, before the commit
-- transaction (03 §3 A). ON CONFLICT DO NOTHING + re-read; names are never
-- updated on later scans.
-- name: ASNEnsure :one
INSERT INTO asn (number, name) VALUES ($1, $2)
ON CONFLICT (number) DO NOTHING
RETURNING id;

-- name: ASNIDByNumber :one
SELECT id FROM asn WHERE number = $1;

-- The API network leaderboard (07 §4.6): keyset over (count, number) desc;
-- the ILIKE arm serves ?q= substring match.
-- name: ASNByNumber :one
SELECT number, name, count_total, count_v6 FROM asn WHERE number = @number;

-- The (count, number) leaderboard walks are builder-built in
-- internal/postgres/asnlist.go (05-schema §10.2 — one seek builder derives
-- the sort column and both walk directions).
