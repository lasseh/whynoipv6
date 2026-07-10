-- db/query/tranco.sql — sqlc query source (layout: 05-schema.md §10.2).

-- name: TrancoLatestSuccessListID :one
SELECT list_id FROM tranco_import WHERE NOT aborted ORDER BY imported_at DESC LIMIT 1;

-- name: TrancoListWasAborted :one
SELECT EXISTS(SELECT 1 FROM tranco_import WHERE list_id = $1 AND aborted);

-- name: TrancoLastSuccessAt :one
SELECT max(imported_at)::timestamptz FROM tranco_import WHERE aborted = false;

-- name: TrancoInsertAborted :exec
INSERT INTO tranco_import
  (list_id, list_date, line_count, rejected_count, duplicate_count, aborted, note)
VALUES ($1, $2, $3, $4, $5, true, $6);

-- name: TrancoInsertProvenance :execrows
INSERT INTO tranco_import
  (list_id, list_date, line_count, imported_count, delisted,
   rejected_count, duplicate_count, aborted, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, false, NULL)
ON CONFLICT (list_id) WHERE NOT aborted DO NOTHING;

-- name: TrancoRecentImports :many
SELECT id, list_id, list_date, line_count, imported_count, delisted,
       rejected_count, duplicate_count, aborted, note, imported_at
FROM tranco_import ORDER BY imported_at DESC LIMIT 10;
