-- db/query/check_job.sql — sqlc query source (layout: 05-schema.md §10.2).

-- Tick step 6 — the 30d check_job purge (04 §9; key: live_check.retention).
-- name: PurgeCheckJobs :execrows
DELETE FROM check_job WHERE created_at < now() - @retention::interval;

-- The §5.1 live-check job lifecycle.

-- name: CheckJobInsert :one
INSERT INTO check_job (host, requester_ip) VALUES (@host, @requester_ip)
RETURNING id, created_at;

-- name: CheckJobByID :one
SELECT id, host, status, result, error, created_at, completed_at
FROM check_job WHERE id = @id;

-- name: CheckJobDedupe :one
SELECT id, host, status, result, error, created_at, completed_at
FROM check_job
WHERE host = @host AND status = 'done' AND completed_at >= now() - @dedupe_window::interval
ORDER BY completed_at DESC
LIMIT 1;

-- Rate limiting (07 §6.3): /64-prefix and global hourly windows; min_created
-- feeds retry_after = ceil(3600 − (now − min(created_at))).
-- name: CheckJobRatePrefix :one
SELECT count(*)::int AS n, min(created_at) AS min_created
FROM check_job
WHERE requester_ip <<= @prefix::cidr AND created_at > now() - interval '1 hour';

-- name: CheckJobRateGlobal :one
SELECT count(*)::int AS n, min(created_at) AS min_created
FROM check_job
WHERE created_at > now() - interval '1 hour';

-- The consumer claim (07 §5.1.5): oldest pending or stale-processing row.
-- name: CheckJobClaim :one
UPDATE check_job SET status = 'processing', claimed_at = now()
WHERE id = (
  SELECT id FROM check_job
  WHERE status = 'pending'
     OR (status = 'processing' AND claimed_at < now() - @reclaim::interval)
  ORDER BY created_at
  LIMIT 1 FOR UPDATE SKIP LOCKED
) RETURNING id, host;

-- name: CheckJobComplete :exec
UPDATE check_job SET status = 'done', result = @result, completed_at = now()
WHERE id = @id;

-- name: CheckJobFail :exec
UPDATE check_job SET status = 'failed', error = @error, completed_at = now()
WHERE id = @id;

-- The reaper: every poller terminates ≤ fail_after.
-- name: CheckJobReap :execrows
UPDATE check_job SET status = 'failed', error = 'timed out', completed_at = now()
WHERE status IN ('pending', 'processing') AND created_at < now() - @fail_after::interval;
