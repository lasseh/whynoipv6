-- name: InsertDomain :exec
INSERT INTO domain(site)
VALUES ($1) ON CONFLICT DO NOTHING;

-- name: ListDomain :many
SELECT * 
FROM domain_view_index;

-- name: ListDomainHeroes :many
SELECT * 
FROM domain_view_heroes;

-- name: CrawlDomain :many
SELECT * 
FROM domain_crawl_list 
LIMIT $1
OFFSET $2;

-- name: ViewDomain :one
SELECT * 
FROM domain_view_list
WHERE site = $1
LIMIT 1;

-- name: UpdateDomain :exec
UPDATE domain SET 
check_aaaa = $2,
check_www = $3,
check_ns = $4,
check_curl = $5,
ts_aaaa = $6,
ts_www = $7,
ts_ns = $8,
ts_curl = $9,
ts_check = $10,
ts_updated = $11,
asn_id = $12,
country_id = $13
WHERE site = $1;

-- name: DisableDomain :exec
UPDATE domain 
SET disabled = TRUE
WHERE site = $1;

