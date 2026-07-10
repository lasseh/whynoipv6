-- =============================================================================
-- 000002_timescaledb.up.sql — hypertable conversions, policies, cagg
-- Modern columnstore API only (enable_columnstore / orderby /
-- CALL add_columnstore_policy). segmentby is deliberately unset on every
-- columnstore table (1 row/domain/day would collapse compression); because
-- orderby is set explicitly, TimescaleDB >= 2.20 does not auto-select a
-- segmentby (PR #8033).
-- =============================================================================

-- scan: ~1M slim rows/day, 2y retention (single-digit GB compressed).
SELECT create_hypertable('scan', by_range('ts', INTERVAL '7 days'));
ALTER TABLE scan SET (timescaledb.enable_columnstore,
                      timescaledb.orderby = 'domain_id, ts DESC');
CALL add_columnstore_policy('scan', after => INTERVAL '14 days');
SELECT add_retention_policy('scan', drop_after => INTERVAL '2 years');

-- scan_detail: 1-2 GB/day raw JSONB, 90d retention (~15-40 GB compressed).
SELECT create_hypertable('scan_detail', by_range('ts', INTERVAL '1 day'));
ALTER TABLE scan_detail SET (timescaledb.enable_columnstore,
                             timescaledb.orderby = 'domain_id, ts DESC');
CALL add_columnstore_policy('scan_detail', after => INTERVAL '3 days');
SELECT add_retention_policy('scan_detail', drop_after => INTERVAL '90 days');

-- changelog: ~1-3k rows/day, kept FOREVER — no retention policy.
SELECT create_hypertable('changelog', by_range('ts', INTERVAL '30 days'));
ALTER TABLE changelog SET (timescaledb.enable_columnstore,
                           timescaledb.orderby = 'ts DESC, domain_id');
CALL add_columnstore_policy('changelog', after => INTERVAL '60 days');

-- crawler_metrics: Grafana-only checkpoints; no columnstore (tiny), 90d retention.
SELECT create_hypertable('crawler_metrics', by_range('ts', INTERVAL '7 days'));
SELECT add_retention_policy('crawler_metrics', drop_after => INTERVAL '90 days');

-- unbound_stats: ~1,440 rows/day/host; no columnstore, 30d retention.
SELECT create_hypertable('unbound_stats', by_range('ts', INTERVAL '7 days'));
SELECT add_retention_policy('unbound_stats', drop_after => INTERVAL '30 days');

-- stats_asn_daily: ~50-80k rows/day, kept forever — no retention policy.
SELECT create_hypertable('stats_asn_daily', by_range('day', INTERVAL '90 days'));
ALTER TABLE stats_asn_daily SET (timescaledb.enable_columnstore,
                                 timescaledb.orderby = 'asn_id, day DESC');
CALL add_columnstore_policy('stats_asn_daily', after => INTERVAL '180 days');

-- ---------------------------------------------------------------------------
-- Observed-adoption continuous aggregate (Grafana + research; NOT public stats).
-- Measurement-flavored: counts observations over ALL scanned entities,
-- unfiltered — deliberately NOT comparable to the confirmed-state stats_* tables
-- (DICTIONARY.md must state this; see the datasets section of 07).
-- WITH NO DATA is required to create a cagg inside the migration's implicit
-- transaction; the policy below performs the first refresh.
-- ---------------------------------------------------------------------------
CREATE MATERIALIZED VIEW scan_daily_adoption
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 day', ts) AS day, country_id,
       count(*) AS scanned,
       count(*) FILTER (WHERE base = 'supported') AS base_v6,
       count(*) FILTER (WHERE conn = 'supported') AS conn_v6,
       count(*) FILTER (WHERE classification = 'hero')   AS heroes,
       count(*) FILTER (WHERE classification = 'sinner') AS sinners
FROM scan GROUP BY 1, 2 WITH NO DATA;
-- Stable policy API only (timescaledb_experimental.add_policies is early-access,
-- SELECT-invoked, and has no schedule-interval parameter — do not use it).
-- Ordering rule: cagg refresh start_offset (3d) < scan retention (2y). OK.
-- Real-time aggregation stays off (default since TS 2.13) — yesterday's data is fine.
SELECT add_continuous_aggregate_policy('scan_daily_adoption',
  start_offset      => INTERVAL '3 days',
  end_offset        => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 hour');
ALTER MATERIALIZED VIEW scan_daily_adoption
  SET (timescaledb.enable_columnstore,
       timescaledb.orderby = 'day DESC, country_id');
CALL add_columnstore_policy('scan_daily_adoption', after => INTERVAL '90 days');
