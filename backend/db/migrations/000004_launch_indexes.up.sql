-- =============================================================================
-- 000004_launch_indexes.up.sql — pre-launch review: supporting indexes + the
-- domain_resource FK cascade fix
-- =============================================================================

-- Global live-check rate limit (CheckJobRateGlobal) counts the last hour on
-- every POST /check; the existing indexes lead with status or requester_ip,
-- so the count seq-scanned check_job. Also serves PurgeCheckJobs/CheckJobReap.
CREATE INDEX idx_check_job_created ON check_job (created_at);

-- ASN leaderboard keyset (07 §4.6): (count, number) DESC per sort column —
-- without these every page is a full scan + top-N sort.
CREATE INDEX idx_asn_count_v6    ON asn (count_v6 DESC, number DESC);
CREATE INDEX idx_asn_count_total ON asn (count_total DESC, number DESC);

-- Reverse-dependents keyset (GET /resources/{host}/dependents): the walk
-- orders by ((rank IS NULL), COALESCE(rank, 0), id). This expression index
-- lets the planner scan domain in display order and probe domain_resource's
-- PK per row instead of sorting every dependent of a high-indegree host on
-- every page.
CREATE INDEX idx_domain_dependents_order
  ON domain ((rank IS NULL), COALESCE(rank, 0), id)
  WHERE NOT disabled;

-- domain_resource(domain_id) declared ON DELETE CASCADE, which would bypass
-- the paired CTE statements that maintain resource_host.dependent_count
-- (03 §12.3) and silently corrupt the counters. Drop the cascade: any future
-- hard-delete of a domain must go through an explicit unlink path.
ALTER TABLE domain_resource
  DROP CONSTRAINT domain_resource_domain_id_fkey,
  ADD CONSTRAINT domain_resource_domain_id_fkey
    FOREIGN KEY (domain_id) REFERENCES domain(id);
