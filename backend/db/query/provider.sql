-- db/query/provider.sql — dns_provider reference data + attribution stamp
-- (05-schema.md — dns_provider; 06-ingest.md §6.10/§6.11).

-- name: ProviderList :many
SELECT id, name, ns_suffixes, count_total, count_v6 FROM dns_provider ORDER BY name;

-- name: ProviderByName :one
SELECT id, name, ns_suffixes FROM dns_provider WHERE name = $1;

-- name: ProviderInsert :one
INSERT INTO dns_provider (name, ns_suffixes) VALUES ($1, $2) RETURNING id;

-- name: ProviderAppendSuffixes :exec
UPDATE dns_provider
SET ns_suffixes = (SELECT array_agg(DISTINCT s ORDER BY s)
                   FROM unnest(ns_suffixes || @suffixes::text[]) AS s)
WHERE id = @id;

-- name: ProviderDelete :execrows
DELETE FROM dns_provider WHERE name = $1;

-- name: ProviderDomainCount :one
SELECT count(*) FROM domain WHERE dns_provider_id = $1;

-- Tick step 3 — DNS-provider counter recompute (06-ingest.md §10.6).
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

-- provider remove clears referencing domains first (FK); they re-stamp on
-- their next scan commit (06-ingest.md §6.11 self-healing).
-- name: ProviderClearDomains :execrows
UPDATE domain SET dns_provider_id = NULL, updated_at = now()
WHERE dns_provider_id = (SELECT id FROM dns_provider WHERE name = $1);

-- The API DNS-provider league table (07 §4.6): exact stored counters,
-- count_v4 synthesized server-side.
-- name: ProviderDetail :one
SELECT id, name, count_total, count_v6 FROM dns_provider WHERE id = @id;

-- name: ProviderLeaderboard :many
SELECT id, name, count_total, count_v6 FROM dns_provider ORDER BY count_total DESC, id;
