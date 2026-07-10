-- 000002_timescaledb.down.sql — dev-only. Drops the cagg so 000001.down can drop
-- the scan hypertable. Hypertable conversions and policies are not individually
-- reversed: policies and chunks are destroyed with their tables in 000001.down;
-- a full dev reset is `dropdb`.
DROP MATERIALIZED VIEW IF EXISTS scan_daily_adoption;
