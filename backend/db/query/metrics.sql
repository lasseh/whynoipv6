-- db/query/metrics.sql — sqlc query source (layout: 05-schema.md §10.2).

-- name: InsertCrawlerMetrics :exec
INSERT INTO crawler_metrics (run_id, worker, processed, succeeded, failed, qps,
                             p50_ms, p99_ms, queue_depth,
                             dim_counters, geoip_build_epoch, is_final)
VALUES (@run_id, @worker, @processed, @succeeded, @failed, @qps,
        @p50_ms, @p99_ms, @queue_depth,
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

-- The public throughput read behind GET /stats/crawler (07 §4.10).
--
-- The SELECT list is the entire point of this query and must stay these two
-- values. crawler_metrics is internal telemetry: worker, run_id, queue_depth,
-- qps, p50_ms/p99_ms, dim_counters (which carries the lease_lost fence-abort
-- counter) and is_final all describe how the crawler is deployed, not what it
-- found, and none of them is public. A contract test asserts negatively that
-- those names never appear in the response — widening this list will fail it.
--
-- checked_24h sums the checkpoint deltas (processed resets at every
-- checkpoint, so summing across workers and rows is correct) and counts check
-- operations attempted, including retries and failures — deliberately not
-- distinct hosts, so it can exceed the tracked-domain count. is_final rows are
-- included: they carry the shutdown tail delta.
--
-- latest is the newest observation regardless of the 24h window, so a dead
-- crawler shows a stale timestamp rather than null. The idle loop checkpoints
-- every 5 minutes even on a drained frontier, so staleness means a dead
-- process, not a quiet one.
-- name: CrawlerThroughput :one
SELECT
  (SELECT COALESCE(sum(processed), 0)::bigint FROM crawler_metrics
     WHERE ts >= now() - interval '24 hours') AS checked_24h,
  (SELECT max(ts) FROM crawler_metrics)::timestamptz AS latest;

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
     AND next_check_at <= now()
     AND (claimed_at IS NULL OR claimed_at < now() - interval '30 minutes')) AS queue_depth;
