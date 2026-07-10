-- db/query/shame.sql — sqlc query source (layout: 05-schema.md §10.2).

-- v6ctl shame — the single top_shame write path (06-ingest.md §7).

-- name: ShameEligibleDomain :one
SELECT id, classification FROM domain
WHERE host = $1 AND kind = 'apex' AND rank IS NOT NULL AND NOT disabled;

-- name: ShameUpsert :exec
INSERT INTO top_shame (domain_id, reason)
VALUES ($1, $2)
ON CONFLICT (domain_id) DO UPDATE SET reason = EXCLUDED.reason;

-- name: ShameRemove :execrows
DELETE FROM top_shame WHERE domain_id = (SELECT id FROM domain WHERE host = $1);

-- name: ShameList :many
SELECT d.host, d.rank, d.classification, ts.reason, ts.added_at,
       (d.classification = 'sinner' AND d.rank IS NOT NULL AND NOT d.disabled) AS visible
FROM top_shame ts
JOIN domain d ON d.id = ts.domain_id
ORDER BY d.rank NULLS LAST;
