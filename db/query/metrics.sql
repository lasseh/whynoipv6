-- name: StoreMetric :exec
INSERT INTO metrics(measurement, data)
VALUES ($1, $2)
RETURNING *;

-- name: GetMetric :many
SELECT time, data 
FROM metrics
WHERE measurement = $1
ORDER BY time DESC;
