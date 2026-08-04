-- =============================================================================
-- 000006_curated_subdomains.up.sql — curated subdomain lists
-- All DDL is owned by docs/spec/05-schema.md. Do not add DDL elsewhere.
-- =============================================================================

-- Origin audit for rows created by the subdomains/<apex>.yml ingress (06).
-- PG 12+ permits ADD VALUE inside the transaction golang-migrate runs this file
-- in, but the value may not be *used* until that transaction commits — no
-- statement below may reference it.
ALTER TYPE created_by ADD VALUE 'curated';

-- Membership, not provenance: a row exists exactly while the host is listed in
-- a subdomains/<apex>.yml file (created_by records where the domain row itself
-- came from, which may be an older ingress). Read as lifecycle-sweep linkage
-- (04 §8) so a rank-NULL curated child keeps the frontier; the sync's
-- membership diff (06) owns the writes.
-- No ON DELETE CASCADE on domain_id, matching campaign_domain and 000004's
-- cascade removal: a hard-delete of a domain must go through an explicit unlink.
CREATE TABLE curated_subdomain (
  domain_id BIGINT PRIMARY KEY REFERENCES domain(id),
  added_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
