-- Resource-dependency reads (07 §4.11). Forward list is bounded small
-- (exact count, no cursor); the reverse dependents list is served by the
-- domainlist builder.

-- name: DomainResourceList :many
SELECT rh.host, rh.aaaa_status, dr.source, dr.required, dr.first_seen, dr.last_seen, rh.last_checked_at
FROM domain_resource dr
JOIN resource_host rh ON rh.id = dr.resource_host_id
WHERE dr.domain_id = @domain_id
ORDER BY rh.host;

-- name: ResourceHostByHost :one
SELECT id, host, aaaa_status, dependent_count, last_checked_at
FROM resource_host WHERE host = @host;

-- The worker's pre-commit roll-up input (02 §6): required links only.
-- name: DomainRequiredLinks :many
SELECT rh.aaaa_status
FROM domain_resource dr
JOIN resource_host rh ON rh.id = dr.resource_host_id
WHERE dr.domain_id = @domain_id AND dr.required;

-- The live-check registry probe (07 §5.1.4): one set-based read.
-- name: ResourceHostStatuses :many
SELECT host, aaaa_status
FROM resource_host
WHERE host = ANY(@hosts::text[]) AND aaaa_status IS NOT NULL;

-- The sweep claim (06 §5.2): the schedule bump IS the crash lease.
-- name: ResourceSweepClaim :many
UPDATE resource_host
SET next_check_at = now() + interval '2 hours'
WHERE id IN (
  SELECT id FROM resource_host
  WHERE next_check_at <= now()
    AND dependent_count > 0
  ORDER BY next_check_at ASC
  LIMIT @batch
  FOR UPDATE SKIP LOCKED
)
RETURNING id, host, aaaa_status, aaaa_pending, aaaa_pending_count;

-- The sweep host commit (06 §5.4): one single-row write per definitive
-- outcome.
-- name: ResourceSweepCommit :exec
UPDATE resource_host
SET aaaa_status = @aaaa_status, aaaa_pending = @aaaa_pending,
    aaaa_pending_count = @aaaa_pending_count,
    last_checked_at = now(), next_check_at = now() + interval '24 hours'
WHERE id = @id;

-- name: ResourceHostIDByHost :one
SELECT id FROM resource_host WHERE host = @host;

-- The §5.5 manual verbs. The (xmax = 0) probe distinguishes a genuine
-- insert (bump dependent_count) from a conflict update (don't).
-- name: ResourceManualUpsert :exec
WITH up AS (
  INSERT INTO domain_resource (domain_id, resource_host_id, source, required)
  VALUES (@domain_id, @resource_host_id, 'manual', @required)
  ON CONFLICT (domain_id, resource_host_id)
  DO UPDATE SET source = 'manual', required = EXCLUDED.required
  RETURNING (xmax = 0) AS inserted
)
UPDATE resource_host SET dependent_count = dependent_count + 1
WHERE id = @resource_host_id AND (SELECT inserted FROM up);

-- name: ResourceManualRemove :execrows
WITH del AS (
  DELETE FROM domain_resource
  WHERE domain_id = @domain_id AND resource_host_id = @resource_host_id
  RETURNING resource_host_id
)
UPDATE resource_host SET dependent_count = dependent_count - 1
WHERE id IN (SELECT resource_host_id FROM del);
