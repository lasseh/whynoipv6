-- name: InsertDomain :exec
INSERT INTO domain(site)
VALUES ($1)
ON CONFLICT DO NOTHING;

-- name: ListDomain :many
SELECT *
FROM domain_view_list
WHERE base_domain = 'unsupported'
   OR www_domain = 'unsupported'
ORDER BY rank
LIMIT $1 OFFSET $2;

-- name: ListDomainHeroes :many
SELECT *
FROM domain_view_list
WHERE base_domain = 'supported'
  AND www_domain = 'supported'
  AND nameserver = 'supported'
  AND mx_record != 'unsupported'
ORDER BY rank
LIMIT $1 OFFSET $2;

-- name: CrawlDomain :many
SELECT *
FROM domain_crawl_list
WHERE id > $1
ORDER BY id
LIMIT $2;

-- name: ViewDomain :one
SELECT *
FROM domain_view_list
WHERE site = $1
LIMIT 1;

-- name: UpdateDomain :exec
UPDATE
    domain
SET base_domain    = $2,
    www_domain     = $3,
    nameserver     = $4,
    mx_record      = $5,
    v6_only        = $6,
    ts_base_domain = $7,
    ts_www_domain  = $8,
    ts_nameserver  = $9,
    ts_mx_record   = $10,
    ts_v6_only     = $11,
    ts_check       = $12,
    ts_updated     = $13,
    asn_id         = $14,
    country_id     = $15
WHERE site = $1;

-- name: DisableDomain :exec
UPDATE
    domain
SET disabled = TRUE
WHERE site = $1;

-- name: GetDomainsByName :many
SELECT *
FROM domain_view_list
WHERE site LIKE '%' || $1 || '%'
ORDER BY rank
LIMIT $2 OFFSET $3;

-- name: ListDomainShamers :many
SELECT *
FROM domain_shame_view;

-- name: InitSpaceTimestamps :exec
WITH DomainCount AS (SELECT count(*)::DECIMAL AS total_records
                     FROM domain),
     IntervalCalculation AS (SELECT (NOW() - '1 days'::INTERVAL)         AS calculatedStartTime,
                                    ('1 days'::INTERVAL) / total_records AS calculatedIntervalStep
                             FROM DomainCount),
     SpacedTimestampUpdates AS (SELECT d.id,
                                       ic.calculatedStartTime + ic.calculatedIntervalStep * 
                                       ROW_NUMBER() OVER (ORDER BY d.id) AS newSpacedTimestamp
                                FROM domain d,
                                     IntervalCalculation ic)
UPDATE domain
SET ts_check = stu.newSpacedTimestamp
FROM SpacedTimestampUpdates stu
WHERE domain.id = stu.id;

-- name: StoreDomainLog :exec
INSERT INTO domain_log(domain_id, data)
VALUES ($1, $2)
RETURNING *;

-- name: GetDomainLog :many
SELECT id,
       time,
       data
FROM domain_log
WHERE domain_id = $1
ORDER BY time DESC
LIMIT 90;
