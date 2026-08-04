-- db/query/domain.sql — sqlc query source (layout: 05-schema.md §10.2).

-- name: DomainByHost :one
SELECT id, host, kind, parent_id, disabled, disabled_reason
FROM domain WHERE host = $1;

-- name: DomainInsertEntity :one
INSERT INTO domain (host, kind, parent_id, rank, created_by, asn_id, country_id, tld, next_check_at)
VALUES ($1, $2, $3, NULL, $4, $5, $6, $7, now())
ON CONFLICT (host) DO NOTHING
RETURNING id;

-- name: DomainMembershipReEntry :exec
UPDATE domain SET
  disabled        = CASE WHEN disabled_reason = 'delisted' THEN false ELSE disabled END,
  disabled_at     = CASE WHEN disabled_reason = 'delisted' THEN NULL ELSE disabled_at END,
  next_check_at   = now(),
  disabled_reason = CASE WHEN disabled_reason = 'delisted' THEN NULL ELSE disabled_reason END,
  updated_at      = now()
WHERE id = $1 AND disabled_reason IN ('delisted', 'dead');

-- The frontier claim (04-lifecycle-scheduling.md §3). One statement per claim
-- cycle; all rows in a batch share one claimed_at (the lease token L). The
-- eligibility predicate textually matches idx_domain_due (05-schema §1.7).
-- name: ClaimBatchByRank :many
UPDATE domain SET claimed_at = now()
WHERE id IN (
  SELECT id FROM domain
  WHERE (NOT disabled OR disabled_reason IN ('dead', 'delisted'))
    AND next_check_at <= now()
    AND (claimed_at IS NULL OR claimed_at < now() - interval '30 minutes')
  ORDER BY rank ASC NULLS LAST, next_check_at ASC
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
RETURNING
  id, host, kind, rank, claimed_at,
  disabled, disabled_reason, disabled_at,
  dead_streak, error_streak, last_counted_at,
  asn_id, country_id,
  base_status, base_pending, base_pending_count, base_since,
  www_status, www_pending, www_pending_count, www_since,
  ns_status, ns_pending, ns_pending_count, ns_since,
  mx_status, mx_pending, mx_pending_count, mx_since,
  conn_status, conn_pending, conn_pending_count, conn_since,
  resources_status, resources_pending, resources_pending_count, resources_since;

-- The claim.order=age variant: aging pressure valve, no sort at all beyond
-- the index order (04 §3).
-- name: ClaimBatchByAge :many
UPDATE domain SET claimed_at = now()
WHERE id IN (
  SELECT id FROM domain
  WHERE (NOT disabled OR disabled_reason IN ('dead', 'delisted'))
    AND next_check_at <= now()
    AND (claimed_at IS NULL OR claimed_at < now() - interval '30 minutes')
  ORDER BY next_check_at ASC
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
RETURNING
  id, host, kind, rank, claimed_at,
  disabled, disabled_reason, disabled_at,
  dead_streak, error_streak, last_counted_at,
  asn_id, country_id,
  base_status, base_pending, base_pending_count, base_since,
  www_status, www_pending, www_pending_count, www_since,
  ns_status, ns_pending, ns_pending_count, ns_since,
  mx_status, mx_pending, mx_pending_count, mx_since,
  conn_status, conn_pending, conn_pending_count, conn_since,
  resources_status, resources_pending, resources_pending_count, resources_since;

-- The daily lifecycle sweep S1–S5 (04-lifecycle-scheduling.md §8): one
-- transaction, set-based; the linkage predicate is spelled identically in
-- every statement. @live_check_linkage / @delist_grace / @slow_lane_every.

-- name: SweepClearOrphans :execrows
UPDATE domain d
SET orphaned_at = NULL, updated_at = now()
WHERE d.orphaned_at IS NOT NULL
  AND (d.rank IS NOT NULL
    OR EXISTS (SELECT 1 FROM campaign_domain cd
               JOIN campaign c ON c.id = cd.campaign_id AND NOT c.disabled
               WHERE cd.domain_id = d.id)
    OR EXISTS (SELECT 1 FROM domain ch WHERE ch.parent_id = d.id)
    OR EXISTS (SELECT 1 FROM curated_subdomain cs WHERE cs.domain_id = d.id)
    OR d.last_requested_at >= now() - @live_check_linkage::interval);

-- name: SweepReenableDelisted :execrows
UPDATE domain d
SET disabled = false, disabled_reason = NULL, disabled_at = NULL,
    orphaned_at = NULL, next_check_at = now(), updated_at = now()
WHERE d.disabled AND d.disabled_reason = 'delisted'
  AND (d.rank IS NOT NULL
    OR EXISTS (SELECT 1 FROM campaign_domain cd
               JOIN campaign c ON c.id = cd.campaign_id AND NOT c.disabled
               WHERE cd.domain_id = d.id)
    OR EXISTS (SELECT 1 FROM domain ch WHERE ch.parent_id = d.id)
    OR EXISTS (SELECT 1 FROM curated_subdomain cs WHERE cs.domain_id = d.id)
    OR d.last_requested_at >= now() - @live_check_linkage::interval);

-- name: SweepDelistLiveCheck :execrows
UPDATE domain d
SET disabled = true, disabled_reason = 'delisted', disabled_at = now(),
    next_check_at = now() + @slow_lane_every::interval, updated_at = now()
WHERE NOT d.disabled AND d.rank IS NULL AND d.created_by = 'live_check'
  AND NOT (
       EXISTS (SELECT 1 FROM campaign_domain cd
               JOIN campaign c ON c.id = cd.campaign_id AND NOT c.disabled
               WHERE cd.domain_id = d.id)
    OR EXISTS (SELECT 1 FROM domain ch WHERE ch.parent_id = d.id)
    OR EXISTS (SELECT 1 FROM curated_subdomain cs WHERE cs.domain_id = d.id)
    -- NULL-safe (deviation from 04 §8's literal text): a never-live-checked
    -- row has last_requested_at NULL; the raw >= comparison makes NOT(...)
    -- NULL and silently exempts the row from S3–S5.
    OR COALESCE(d.last_requested_at >= now() - @live_check_linkage::interval, false));

-- name: SweepStampOrphans :execrows
UPDATE domain d
SET orphaned_at = now(), updated_at = now()
WHERE NOT d.disabled AND d.rank IS NULL AND d.created_by <> 'live_check'
  AND NOT (
       EXISTS (SELECT 1 FROM campaign_domain cd
               JOIN campaign c ON c.id = cd.campaign_id AND NOT c.disabled
               WHERE cd.domain_id = d.id)
    OR EXISTS (SELECT 1 FROM domain ch WHERE ch.parent_id = d.id)
    OR EXISTS (SELECT 1 FROM curated_subdomain cs WHERE cs.domain_id = d.id)
    -- NULL-safe (deviation from 04 §8's literal text): a never-live-checked
    -- row has last_requested_at NULL; the raw >= comparison makes NOT(...)
    -- NULL and silently exempts the row from S3–S5.
    OR COALESCE(d.last_requested_at >= now() - @live_check_linkage::interval, false))
  AND d.orphaned_at IS NULL;

-- name: SweepDelistExpired :execrows
UPDATE domain d
SET disabled = true, disabled_reason = 'delisted', disabled_at = now(),
    next_check_at = now() + @slow_lane_every::interval, updated_at = now()
WHERE NOT d.disabled AND d.rank IS NULL AND d.created_by <> 'live_check'
  AND NOT (
       EXISTS (SELECT 1 FROM campaign_domain cd
               JOIN campaign c ON c.id = cd.campaign_id AND NOT c.disabled
               WHERE cd.domain_id = d.id)
    OR EXISTS (SELECT 1 FROM domain ch WHERE ch.parent_id = d.id)
    OR EXISTS (SELECT 1 FROM curated_subdomain cs WHERE cs.domain_id = d.id)
    -- NULL-safe (deviation from 04 §8's literal text): a never-live-checked
    -- row has last_requested_at NULL; the raw >= comparison makes NOT(...)
    -- NULL and silently exempts the row from S3–S5.
    OR COALESCE(d.last_requested_at >= now() - @live_check_linkage::interval, false))
  AND d.orphaned_at IS NOT NULL AND d.orphaned_at < now() - @delist_grace::interval;

-- Operator disable/enable (P2.14; glossary: service/manual lifecycle).

-- name: DomainDisable :execrows
UPDATE domain
SET disabled = true, disabled_reason = @reason, disabled_at = now(), updated_at = now()
WHERE host = @host AND NOT disabled;

-- name: DomainEnable :execrows
UPDATE domain
SET disabled = false, disabled_reason = NULL, disabled_at = NULL,
    next_check_at = now(), updated_at = now()
WHERE host = @host AND disabled AND disabled_reason IN ('manual', 'service');

-- name: DomainDetailByHost :one
SELECT d.id, d.host, d.rank, d.kind, p.host AS parent,
  d.classification, d.class_flags, d.saint,
  d.base_status, d.base_since, d.www_status, d.www_since,
  d.ns_status, d.ns_since, d.mx_status, d.mx_since,
  d.conn_status, d.conn_since, d.resources_status, d.resources_since,
  d.dnssec_observed, d.ptr_observed, d.smtp_observed, d.parity_observed,
  d.latency_v4_ms, d.latency_v6_ms,
  d.tld, c.code AS country_code, c.name AS country_name, c.tld AS country_tld,
  a.number AS asn_number, a.name AS asn_name,
  dp.id AS provider_id, dp.name AS provider_name,
  d.hosting_provider,
  (SELECT count(*) FROM domain ch WHERE ch.parent_id = d.id AND NOT ch.disabled) AS subdomain_count,
  d.disabled, d.last_checked_at, d.created_at
FROM domain d
JOIN country c ON c.id = d.country_id
JOIN asn a ON a.id = d.asn_id
LEFT JOIN dns_provider dp ON dp.id = d.dns_provider_id
LEFT JOIN domain p ON p.id = d.parent_id
WHERE d.host = @host;

-- name: SubdomainExactCount :one
SELECT count(*) FROM domain WHERE parent_id = @parent_id AND NOT disabled;

-- The badge read (07 §5.2): read-only, zero side effects, any kind/origin.
-- name: BadgeDomain :one
SELECT classification, saint, disabled FROM domain WHERE host = @host;

-- The §5.1 live-check domain reads/writes.

-- name: DomainConfirmed :one
SELECT id, kind, classification, class_flags, saint,
       base_status, base_since, www_status, www_since, ns_status, ns_since,
       mx_status, mx_since, conn_status, conn_since, resources_status, resources_since,
       last_checked_at, disabled, disabled_reason
FROM domain WHERE host = @host;

-- Lifecycle re-entry (07 §5.1.6): every POST /check on an existing host.
-- name: DomainLiveCheckReentry :exec
UPDATE domain SET
  last_requested_at = now(),
  disabled        = CASE WHEN disabled_reason = 'delisted' THEN false ELSE disabled END,
  disabled_at     = CASE WHEN disabled_reason = 'delisted' THEN NULL ELSE disabled_at END,
  orphaned_at     = CASE WHEN disabled_reason = 'delisted' THEN NULL ELSE orphaned_at END,
  next_check_at   = CASE WHEN disabled_reason IN ('delisted', 'dead') THEN now() ELSE next_check_at END,
  disabled_reason = CASE WHEN disabled_reason = 'delisted' THEN NULL ELSE disabled_reason END,
  updated_at      = now()
WHERE host = @host;

-- The consumer's entity insert (07 §5.1.5 step 2): parent only if it
-- ALREADY exists — live checks never auto-ensure parents.
-- name: DomainInsertLiveCheck :one
INSERT INTO domain (host, kind, parent_id, rank, created_by, asn_id, country_id, tld, last_requested_at, next_check_at)
VALUES (@host, @kind, @parent_id, NULL, 'live_check', @asn_id, @country_id, @tld, now(), now())
ON CONFLICT (host) DO NOTHING
RETURNING id;

-- name: RankedDomainCount :one
SELECT count(*) FROM domain WHERE rank IS NOT NULL;
