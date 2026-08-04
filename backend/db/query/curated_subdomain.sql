-- db/query/curated_subdomain.sql — sqlc query source (layout: 05-schema.md §10.2).

-- Membership of the curated subdomain lists (subdomains/<apex>.yml, 06): adds
-- first, then one set-based removal of everything no longer listed, in the sync
-- transaction. The removal must only run on a complete parse — a partial file
-- set would drop live membership and start its 30-day delist grace.

-- name: CuratedSubdomainAdd :execrows
INSERT INTO curated_subdomain (domain_id) VALUES ($1) ON CONFLICT DO NOTHING;

-- An empty (not NULL) array deletes every row: an emptied subdomains/ directory
-- drops all membership, recoverable within the delist grace (04 §8). A NULL
-- array matches nothing and would silently skip the diff.
-- name: CuratedSubdomainRemoveNotIn :execrows
DELETE FROM curated_subdomain WHERE domain_id <> ALL(@domain_ids::bigint[]);

-- What one apex currently has listed. Read when the sync skips a list (its apex
-- is disabled) so those ids still enter the membership set: skipping a file must
-- leave it alone, not silently start its hosts' 30-day delist grace.
-- name: CuratedSubdomainIDsByParent :many
SELECT cs.domain_id FROM curated_subdomain cs
JOIN domain d ON d.id = cs.domain_id
WHERE d.parent_id = @parent_id;
