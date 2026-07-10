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

-- The frontier claim (04-lifecycle-scheduling.md §3). One statement per claim
-- cycle; all rows in a batch share one claimed_at (the lease token L). The
-- eligibility predicate textually matches idx_domain_due (05-schema §1.7).
-- name: ClaimBatchByRank :many
UPDATE domain SET claimed_at = now()
WHERE id IN (
  SELECT id FROM domain
  WHERE (NOT disabled OR disabled_reason IN ('dead', 'delisted'))
    AND next_check_at <= now()
    AND (claimed_at IS NULL OR claimed_at < now() - interval '30 minutes')
  ORDER BY rank ASC NULLS LAST, next_check_at ASC
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
RETURNING
  id, host, kind, rank, claimed_at,
  disabled, disabled_reason, disabled_at,
  dead_streak, error_streak, last_counted_at,
  asn_id, country_id,
  base_status, base_pending, base_pending_count, base_since,
  www_status, www_pending, www_pending_count, www_since,
  ns_status, ns_pending, ns_pending_count, ns_since,
  mx_status, mx_pending, mx_pending_count, mx_since,
  conn_status, conn_pending, conn_pending_count, conn_since,
  resources_status, resources_pending, resources_pending_count, resources_since;

-- The claim.order=age variant: aging pressure valve, no sort at all beyond
-- the index order (04 §3).
-- name: ClaimBatchByAge :many
UPDATE domain SET claimed_at = now()
WHERE id IN (
  SELECT id FROM domain
  WHERE (NOT disabled OR disabled_reason IN ('dead', 'delisted'))
    AND next_check_at <= now()
    AND (claimed_at IS NULL OR claimed_at < now() - interval '30 minutes')
  ORDER BY next_check_at ASC
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
RETURNING
  id, host, kind, rank, claimed_at,
  disabled, disabled_reason, disabled_at,
  dead_streak, error_streak, last_counted_at,
  asn_id, country_id,
  base_status, base_pending, base_pending_count, base_since,
  www_status, www_pending, www_pending_count, www_since,
  ns_status, ns_pending, ns_pending_count, ns_since,
  mx_status, mx_pending, mx_pending_count, mx_since,
  conn_status, conn_pending, conn_pending_count, conn_since,
  resources_status, resources_pending, resources_pending_count, resources_since;
