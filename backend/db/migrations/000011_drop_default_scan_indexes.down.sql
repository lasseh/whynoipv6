-- Recreate the two indexes create_hypertable would have made, so a down/up
-- round trip lands on the same schema either way.
CREATE INDEX IF NOT EXISTS scan_ts_idx ON scan (ts DESC);
CREATE INDEX IF NOT EXISTS scan_detail_ts_idx ON scan_detail (ts DESC);
