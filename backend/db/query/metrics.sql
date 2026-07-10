-- db/query/metrics.sql — sqlc query source (layout: 05-schema.md §10.2).

-- name: InsertCrawlerMetrics :exec
INSERT INTO crawler_metrics (run_id, worker, processed, succeeded, failed, qps,
                             p50_ms, p99_ms, active_slots, queue_depth,
                             dim_counters, geoip_build_epoch, is_final)
VALUES (@run_id, @worker, @processed, @succeeded, @failed, @qps,
        @p50_ms, @p99_ms, @active_slots, @queue_depth,
        @dim_counters, @geoip_build_epoch, @is_final);

-- The queue-depth probe (04 §15.1): O(due-set) via idx_domain_due, sampled
-- at most once per checkpoint.
-- name: QueueDepth :one
SELECT count(*) FROM domain
WHERE (NOT disabled OR disabled_reason IN ('dead', 'delisted'))
  AND next_check_at <= now()
  AND (claimed_at IS NULL OR claimed_at < now() - interval '30 minutes');
