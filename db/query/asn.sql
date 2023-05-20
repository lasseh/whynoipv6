-- name: CreateASN :one
-- The ON CONFLICT DO NOTHING clause prevents errors in case a record with the same ASN number already exists.
INSERT INTO asn(number,name)
VALUES ($1, $2) ON CONFLICT DO NOTHING
RETURNING *;

-- name: GetASByNumber :one
SELECT * 
FROM asn
WHERE number = $1
LIMIT 1;
