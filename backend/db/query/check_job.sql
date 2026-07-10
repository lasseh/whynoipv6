-- db/query/check_job.sql — sqlc query source (layout: 05-schema.md §10.2).

-- Tick step 6 — the 30d check_job purge (04 §9; key: live_check.retention).
-- name: PurgeCheckJobs :execrows
DELETE FROM check_job WHERE created_at < now() - @retention::interval;
