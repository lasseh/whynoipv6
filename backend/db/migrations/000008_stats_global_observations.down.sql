ALTER TABLE stats_global_daily
  DROP COLUMN tracked_total,
  DROP COLUMN ptr_supported,
  DROP COLUMN ptr_graded,
  DROP COLUMN smtp_supported,
  DROP COLUMN smtp_graded;
