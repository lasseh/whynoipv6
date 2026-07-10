-- db/query/asn.sql — sqlc query source (layout: 05-schema.md §10.2).

-- Sentinel lookup: binaries resolve the sentinel ASN once at startup by
-- lookup, never by literal id (05-schema.md §5).
-- name: ASNSentinelID :one
SELECT id FROM asn WHERE number = 0;
