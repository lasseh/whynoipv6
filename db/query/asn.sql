-- name: CreateASN :one
INSERT INTO asn(number,name)
VALUES ($1, $2) ON CONFLICT DO NOTHING
RETURNING *;

-- name: GetASByNumber :one
SELECT * 
FROM asn
WHERE number = $1
LIMIT 1;
