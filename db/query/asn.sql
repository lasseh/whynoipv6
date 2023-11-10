-- name: CreateASN :one
-- The ON CONFLICT DO NOTHING clause prevents errors in case a record with the same ASN number already exists.
INSERT INTO asn(number, name)
VALUES ($1, $2)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: GetASByNumber :one
SELECT *
FROM asn
WHERE number = $1
LIMIT 1;

-- name: AsnByIPv4 :many
SELECT * 
FROM asn
WHERE count_v4 IS NOT NULL AND id != 1
ORDER BY count_v4 DESC
LIMIT $1 OFFSET $2;

-- name: AsnByIPv6 :many
SELECT * 
FROM asn
WHERE count_v4 IS NOT NULL AND id != 1
ORDER BY count_v6 DESC
LIMIT $1 OFFSET $2;

-- name: SearchAsNumber :many
SELECT *
FROM asn
WHERE number = $1
ORDER BY count_v4 DESC
LIMIT 100;

-- name: SearchAsName :many
SELECT *
FROM asn
WHERE name ILIKE '%' || $1 || '%'
ORDER BY count_v4 DESC
LIMIT 100;
