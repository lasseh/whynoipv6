-- name: StoreMetric :exec
INSERT INTO metrics(measurement, data)
VALUES ($1, $2)
RETURNING *;

-- name: GetMetric :many
SELECT time,
       data
FROM metrics
WHERE measurement = $1
ORDER BY time DESC;

-- name: TotalStats :one
SELECT
  time,
  ((data->>'total_sites')::NUMERIC) as TotalSites,
  ((data->>'total_ns')::NUMERIC) as TotalNs,
  ((data->>'total_aaaa')::NUMERIC) as TotalAaaa,
  ((data->>'total_www')::NUMERIC) as TotalWww,
  ((data->>'total_both')::NUMERIC) as TotalBoth
FROM
  metrics
WHERE measurement = 'domains' LIMIT 1;
