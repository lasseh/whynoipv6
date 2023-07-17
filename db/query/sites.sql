-- name: ListSites :many
SELECT *
FROM sites
ORDER BY rank
LIMIT $1 OFFSET $2;
