-- db/query/scan.sql — sqlc query source (layout: 05-schema.md §10.2).

-- The detail page's evidence block: the latest scan_detail payload
-- (07 §4.3 ?include=evidence).
-- name: LatestScanDetail :one
SELECT details FROM scan_detail WHERE domain_id = @domain_id ORDER BY ts DESC LIMIT 1;
