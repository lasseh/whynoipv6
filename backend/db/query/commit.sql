-- db/query/commit.sql — the per-domain commit write unit (03-state-machine.md
-- §12). One pgx.Batch in one pgx.Tx; statement 1 is the lease-fenced UPDATE.

-- name: CommitDomain :execrows
UPDATE domain SET
  base_status = @base_status, base_observed = @base_observed,
  base_pending = @base_pending, base_pending_count = @base_pending_count, base_since = @base_since,
  www_status = @www_status, www_observed = @www_observed,
  www_pending = @www_pending, www_pending_count = @www_pending_count, www_since = @www_since,
  ns_status = @ns_status, ns_observed = @ns_observed,
  ns_pending = @ns_pending, ns_pending_count = @ns_pending_count, ns_since = @ns_since,
  mx_status = @mx_status, mx_observed = @mx_observed,
  mx_pending = @mx_pending, mx_pending_count = @mx_pending_count, mx_since = @mx_since,
  conn_status = @conn_status, conn_observed = @conn_observed,
  conn_pending = @conn_pending, conn_pending_count = @conn_pending_count, conn_since = @conn_since,
  resources_status = @resources_status, resources_observed = @resources_observed,
  resources_pending = @resources_pending, resources_pending_count = @resources_pending_count,
  resources_since = @resources_since,
  dnssec_observed = @dnssec_observed, ptr_observed = @ptr_observed,
  smtp_observed = @smtp_observed, parity_observed = @parity_observed,
  latency_v4_ms = @latency_v4_ms, latency_v6_ms = @latency_v6_ms,
  classification = @classification, class_flags = @class_flags, saint = @saint,
  asn_id = @asn_id, country_id = @country_id,
  -- The provider pivots ride the same fenced UPDATE (06 §6.10): stamped only
  -- on definitive-base scans (the stamp flags), untouched otherwise — a lost
  -- lease can no longer leave pivots from a discarded scan.
  dns_provider_id = CASE WHEN @stamp_dns_provider_id::boolean THEN @dns_provider_id ELSE dns_provider_id END,
  hosting_provider = CASE WHEN @stamp_hosting_provider::boolean THEN @hosting_provider ELSE hosting_provider END,
  disabled = @disabled, disabled_reason = @disabled_reason, disabled_at = @disabled_at,
  dead_streak = @dead_streak, error_streak = @error_streak,
  next_check_at = @next_check_at, last_checked_at = @ts, last_counted_at = @last_counted_at,
  claimed_at = NULL, updated_at = now()
WHERE id = @domain_id AND claimed_at = @lease;

-- name: InsertChangelog :exec
INSERT INTO changelog (domain_id, ts, field, old_value, new_value)
VALUES (@domain_id, @ts, @field, @old_value, @new_value);

-- name: InsertScan :exec
INSERT INTO scan (domain_id, ts, base, www, ns, mx, conn, resources,
                  dnssec, ptr, smtp, parity, latency_v4_ms, latency_v6_ms,
                  classification, country_id, asn_id)
VALUES (@domain_id, @ts, @base, @www, @ns, @mx, @conn, @resources,
        @dnssec, @ptr, @smtp, @parity, @latency_v4_ms, @latency_v6_ms,
        @classification, @country_id, @asn_id)
ON CONFLICT (domain_id, ts) DO NOTHING;

-- name: InsertScanDetail :exec
INSERT INTO scan_detail (domain_id, ts, details, duration_ms)
VALUES (@domain_id, @ts, @details, @duration_ms)
ON CONFLICT (domain_id, ts) DO NOTHING;

-- name: EnsureResourceHost :exec
INSERT INTO resource_host (host) VALUES (@rhost)
ON CONFLICT (host) DO NOTHING;

-- SQLUpsertDomainResource and SQLPruneDomainResources live as Go constants
-- in internal/postgres/commitflush.go: their multi-CTE shape (insert +
-- conditional counter bump + refresh) exceeds sqlc's analyzer (the
-- 05-schema.md §10.2 escape hatch). The SQL text there is the one owned by
-- 03-state-machine.md §12.3.
