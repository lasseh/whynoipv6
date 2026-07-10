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
                   FROM unnest(ns_suffixes || $2::text[]) AS s)
WHERE id = $1;

-- name: ProviderDelete :execrows
DELETE FROM dns_provider WHERE name = $1;

-- name: ProviderDomainCount :one
SELECT count(*) FROM domain WHERE dns_provider_id = $1;

-- The attribution stamp: touches ONLY the pivot column, never the
-- commit/trust machine's columns (06-ingest.md §6.10).
-- name: DomainStampDNSProvider :exec
UPDATE domain SET dns_provider_id = $2 WHERE id = $1;

-- name: DomainStampHostingProvider :exec
UPDATE domain SET hosting_provider = $2 WHERE id = $1;

-- provider remove clears referencing domains first (FK); they re-stamp on
-- their next scan commit (06-ingest.md §6.11 self-healing).
-- name: ProviderClearDomains :execrows
UPDATE domain SET dns_provider_id = NULL
WHERE dns_provider_id = (SELECT id FROM dns_provider WHERE name = $1);
