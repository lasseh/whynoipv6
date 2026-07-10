-- db/query/domain.sql — sqlc query source (layout: 05-schema.md §10.2).

-- name: DomainByHost :one
SELECT id, host, kind, parent_id, disabled, disabled_reason
FROM domain WHERE host = $1;

-- name: DomainInsertEntity :one
INSERT INTO domain (host, kind, parent_id, rank, created_by, asn_id, country_id, tld, next_check_at)
VALUES ($1, $2, $3, NULL, $4, $5, $6, $7, now())
ON CONFLICT (host) DO NOTHING
RETURNING id;

-- name: DomainMembershipReEntry :exec
UPDATE domain SET
  disabled        = CASE WHEN disabled_reason = 'delisted' THEN false ELSE disabled END,
  disabled_at     = CASE WHEN disabled_reason = 'delisted' THEN NULL ELSE disabled_at END,
  next_check_at   = now(),
  disabled_reason = CASE WHEN disabled_reason = 'delisted' THEN NULL ELSE disabled_reason END,
  updated_at      = now()
WHERE id = $1 AND disabled_reason IN ('delisted', 'dead');
