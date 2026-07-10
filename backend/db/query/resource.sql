-- Resource-dependency reads (07 §4.11). Forward list is bounded small
-- (exact count, no cursor); the reverse dependents list is served by the
-- domainlist builder.

-- name: DomainResourceList :many
SELECT rh.host, rh.aaaa_status, dr.source, dr.required, dr.first_seen, dr.last_seen, rh.last_checked_at
FROM domain_resource dr
JOIN resource_host rh ON rh.id = dr.resource_host_id
WHERE dr.domain_id = @domain_id
ORDER BY rh.host;

-- name: ResourceHostByHost :one
SELECT id, host, aaaa_status, dependent_count, last_checked_at
FROM resource_host WHERE host = @host;
