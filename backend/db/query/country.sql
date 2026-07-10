-- db/query/country.sql — sqlc query source (layout: 05-schema.md §10.2).

-- Sentinel lookup: binaries resolve the sentinel country once at startup by
-- lookup, never by literal id (05-schema.md §5).
-- name: CountrySentinelID :one
SELECT id FROM country WHERE code = 'UN';
