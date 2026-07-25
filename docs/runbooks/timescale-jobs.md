# Runbook — TimescaleDB background jobs

The hypertables (`scan`, `scan_detail`, `changelog`, `crawler_metrics`,
`unbound_stats`, `stats_asn_daily`) rely on Timescale policies:
columnstore/compression and retention. The `scan_daily_adoption`
continuous aggregate refreshes hourly (`start_offset` 3d, `end_offset`
1h) and has its own columnstore policy after 90d. A stuck policy shows
up as unbounded disk growth or a stale cagg, not as an application
error.

## Triggers

- Disk-usage alert on the DB volume.
- `SELECT * FROM timescaledb_information.job_errors ORDER BY
  finish_time DESC LIMIT 20;` shows repeating failures.
- A retention window that should have trimmed data has not
  (`SELECT min(ts) FROM scan;` far older than the policy).

## Diagnosis

1. Job inventory + last run:

   ```sql
   SELECT job_id, application_name, schedule_interval, last_run_status,
          last_run_started_at, next_start
   FROM timescaledb_information.jobs
   ORDER BY job_id;
   ```

2. Failures: `timescaledb_information.job_errors` (job_id → the
   failing policy).
3. Chunk state: `SELECT * FROM chunks_detailed_size('scan') ORDER BY
   total_bytes DESC LIMIT 10;` and
   `SELECT * FROM timescaledb_information.chunks WHERE hypertable_name
   = 'scan' AND NOT is_compressed ORDER BY range_start LIMIT 10;`.

## Recovery

- Run a policy immediately: `CALL run_job(<job_id>);` and re-check
  `job_errors`.
- A policy erroring on one bad chunk: compress/drop that chunk by hand
  (`SELECT compress_chunk('<chunk>');` /
  `SELECT drop_chunks('scan', older_than => INTERVAL '730 days');`),
  then `CALL run_job(...)` again.
- Scheduler wedged (all jobs idle past next_start): check
  `timescaledb.max_background_workers` and restart PostgreSQL in a
  maintenance window; policies catch up on their own.
- Never disable retention to "fix" disk pressure — trim the window in
  a migration instead so the policy stays declarative.

## Notes

- The pgtest harness runs with `timescaledb.max_background_workers=0`;
  production must NOT (policies would silently never run). Verify with
  `SHOW timescaledb.max_background_workers;` after any Postgres config
  change.
