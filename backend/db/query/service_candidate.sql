-- db/query/service_candidate.sql — sqlc query source (layout: 05-schema.md §10.2).

-- Tick step 4 — candidate detection, never auto-disable (04 §9).

-- name: DetectServiceCandidatesApex :execrows
INSERT INTO service_candidate (domain_id, reasons)
SELECT d.id, ARRAY['apex_www_no_record']
FROM domain d
WHERE d.rank IS NOT NULL AND NOT d.disabled
  AND d.base_status = 'no_record'
  AND d.www_status  = 'not_applicable'
  AND d.ns_status IN ('supported', 'unsupported')
ON CONFLICT (domain_id) DO NOTHING;

-- name: DetectServiceCandidatesIndegree :execrows
INSERT INTO service_candidate (domain_id, reasons)
SELECT d.id, ARRAY['high_dependency_indegree']
FROM resource_host rh
JOIN domain d ON d.host = rh.host
WHERE rh.dependent_count >= @indegree_threshold
  AND d.rank IS NOT NULL AND NOT d.disabled
ON CONFLICT (domain_id) DO NOTHING;

-- Operator triage (P2.14; 04 — service/manual lifecycle).

-- name: ServiceCandidateList :many
SELECT sc.domain_id, d.host, sc.reasons, sc.detected_at
FROM service_candidate sc
JOIN domain d ON d.id = sc.domain_id
WHERE NOT sc.dismissed
ORDER BY sc.detected_at;

-- name: ServiceCandidateResolve :execrows
UPDATE service_candidate SET dismissed = true
WHERE domain_id = (SELECT id FROM domain WHERE host = @host) AND NOT dismissed;
