-- =============================================================================
-- 000008_stats_global_observations.up.sql — tracked total + PTR/SMTP dailies
-- All DDL is owned by docs/spec/05-schema.md. Do not add DDL elsewhere.
-- =============================================================================

-- Daily state counts over `domain` that had no home, so /metrics read frozen
-- fixtures for them. They are snapshots of what is true today, which is what
-- this table is — hence columns here rather than new endpoints.
--
-- tracked_total is deliberately not a redefinition of `domains`: the rollup
-- is FROM domain WHERE rank IS NOT NULL, so `domains` stays ranked-only and
-- the whole live population gets its own column. The two differ by the
-- ex-Tranco, campaign, curated and parent-link rows.
--
-- Nullable with no backfill: ptr_observed / smtp_observed are current-state
-- columns overwritten on every commit, and the raw observations retained in
-- `scan` carry no same-day base_status/mx_status to grade them against — a
-- backfill would count a different population than the live rollup. Rows
-- written before this migration stay NULL and the frontend renders an em
-- dash rather than inventing a zero.
ALTER TABLE stats_global_daily
  ADD COLUMN tracked_total  INT,  -- every live row, ranked or not (cf. domains)
  ADD COLUMN ptr_supported  INT,  -- v6-answering hosts that resolve back to a name
  ADD COLUMN ptr_graded     INT,  -- v6-answering hosts where PTR was gradeable
  ADD COLUMN smtp_supported INT,  -- v6-reachable MX that presented a banner
  ADD COLUMN smtp_graded    INT;  -- v6-reachable MX where SMTP was gradeable
