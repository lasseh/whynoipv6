-- db/query/country.sql — sqlc query source (layout: 05-schema.md §10.2).

-- Sentinel lookup: binaries resolve the sentinel country once at startup by
-- lookup, never by literal id (05-schema.md §5).
-- name: CountrySentinelID :one
SELECT id FROM country WHERE code = 'UN';

-- Insert-time ccTLD attribution probe (06-ingest.md §6.5): '.NO'-form input.
-- name: CountryIDByTLD :one
SELECT id FROM country WHERE tld = $1;

-- The in-memory country.tld -> id map for insert-time/commit attribution
-- (06-ingest.md §6.5); loaded once per run.
-- name: CountryTLDMap :many
SELECT id, tld FROM country WHERE tld IS NOT NULL;

-- name: CountryCodeMap :many
SELECT id, code FROM country;

-- Tick step 3 — country counter recompute (06-ingest.md §10.6): reset, then
-- recompute over the publicly-ranked scope.
-- name: ResetCountryCounters :exec
UPDATE country SET sites = 0, v6sites = 0, percent = 0;

-- name: RecomputeCountryCounters :exec
UPDATE country c SET
  sites   = agg.sites,
  v6sites = agg.v6sites,
  percent = CASE WHEN agg.sites = 0 THEN 0
                 ELSE ROUND(agg.v6sites::numeric / agg.sites::numeric * 100, 2) END
FROM (
  SELECT country_id,
         count(*)                                                      AS sites,
         count(*) FILTER (WHERE classification IN ('partial','hero'))  AS v6sites
  FROM domain
  WHERE rank IS NOT NULL AND NOT disabled
  GROUP BY country_id
) agg
WHERE c.id = agg.country_id;

-- name: CountryIDByCode :one
SELECT id FROM country WHERE code = upper(@code)::char(2);

-- The API country representations (07 §4.5).
-- name: CountryByCode :one
SELECT code, name, tld, sites, v6sites, percent FROM country WHERE code = upper(@code)::char(2);

-- name: CountryLeaderboard :many
SELECT code, name, tld, sites, v6sites, percent FROM country;
