-- =============================================================================
-- 000009_changelog_daily.up.sql — daily transition roll-up over changelog
-- All DDL is owned by docs/spec/05-schema.md. Do not add DDL elsewhere.
-- =============================================================================

-- The roll-up behind GET /stats/changes ("IPv6 gained and lost per day").
--
-- This is confirmed_state, NOT the measurement flavour. Do not conflate it
-- with scan_daily_adoption in 000002: that one aggregates raw scan
-- observations and is deliberately never served (07-api.md §4.10, OPEN-5
-- resolved NO). changelog is the confirmed-transition log — a row exists only
-- after a flip survived ConfirmN consecutive counted observations — so
-- aggregating it is the same population the public lists report.
--
-- Grouped by field as well as day even though the endpoint serves base only.
-- The API filters to field = 'base' because that is what "gained IPv6" means
-- and what stats_global_daily.base_supported counts; aggregating all six
-- dimensions would multiply one adoption into several rows AND bias the
-- result, since shadowTransition suppresses conn/resources -> not_applicable
-- (the dominant loss path) but not their gain path. Keeping the dimension in
-- the aggregate means a per-field series later needs no new cagg.
--
-- A cagg rather than a plain GROUP BY because changelog is forever-retained
-- and goes columnstore after 60 days, where there is no btree to lean on: an
-- unmaterialized 90-day window would decompress a widening slice of the table
-- on every request.
CREATE MATERIALIZED VIEW changelog_daily
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 day', ts) AS day,
       field,
       count(*) FILTER (WHERE new_value = 'supported') AS gained,
       count(*) FILTER (WHERE old_value = 'supported') AS lost
FROM changelog GROUP BY 1, 2 WITH NO DATA;

-- Unlike scan_daily_adoption, real-time aggregation is ON here. That cagg
-- serves yesterday's picture and can lag an hour; this one is on the live
-- changelog surface, whose ETag is seeded from max(changelog.ts), so a
-- transition committed a minute ago must be visible or the endpoint would
-- advertise fresh data it then declines to show. It also means correctness
-- does not depend on the policy having run: unmaterialized buckets are
-- unioned from the raw table.
ALTER MATERIALIZED VIEW changelog_daily
  SET (timescaledb.materialized_only = false);

-- Ordering rule: start_offset (90d) < changelog retention (none — forever).
-- Buckets older than the offset are served through the real-time union rather
-- than the materialization, which is the correct trade: the default window is
-- 90 days and older reads are rare.
SELECT add_continuous_aggregate_policy('changelog_daily',
  start_offset      => INTERVAL '90 days',
  end_offset        => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 hour');
