-- ADR 0004: rank-band cadence and its telemetry were removed at launch.
-- active_slots was write-only (no reader survived the cleanup); crawler_metrics
-- is an uncompressed hypertable, so the drop is a plain catalog change and the
-- 90d retention ages out historical values regardless.
ALTER TABLE crawler_metrics DROP COLUMN active_slots;
