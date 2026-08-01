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

-- name: InsertUnboundStats :exec
INSERT INTO unbound_stats (host, num_queries, cache_hits, cache_miss,
                           rcode_servfail, rcode_nxdomain,
                           recursion_time_avg_ms, requestlist_avg, raw)
VALUES (@host, @num_queries, @cache_hits, @cache_miss,
        @rcode_servfail, @rcode_nxdomain,
        @recursion_time_avg_ms, @requestlist_avg, @raw);

-- The tick step-7 ops digest (04 §9). scanned sums the checkpoint deltas
-- (crawler_metrics.processed resets every checkpoint) instead of counting
-- raw scan rows — the scan hypertable has no ts-leading index and a 24h
-- count(*) would seq-scan the newest chunk.
-- name: TickSummaryCounts :one
SELECT
  (SELECT COALESCE(sum(processed), 0)::bigint FROM crawler_metrics
     WHERE ts >= now() - interval '24 hours') AS scanned,
  (SELECT count(*) FROM changelog WHERE ts >= now() - interval '24 hours') AS transitions,
  (SELECT count(*) FROM domain WHERE (NOT disabled OR disabled_reason IN ('dead', 'delisted'))
     AND next_check_at <= now()) AS queue_depth;
