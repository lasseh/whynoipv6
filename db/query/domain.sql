-- name: InsertDomain :exec
INSERT INTO domain(site)
VALUES ($1)
ON CONFLICT DO NOTHING;

-- name: ListDomain :many
SELECT *
FROM domain_view_list
WHERE check_aaaa = FALSE
   OR check_www = FALSE
LIMIT $1 OFFSET $2;

-- name: ListDomainHeroes :many
SELECT *
FROM domain_view_list
WHERE check_aaaa = TRUE
  AND check_www = TRUE
  AND check_ns = TRUE
LIMIT $1 OFFSET $2;

-- name: CrawlDomain :many
SELECT *
FROM domain_crawl_list
LIMIT $1 OFFSET $2;

-- name: ViewDomain :one
SELECT *
FROM domain_view_list
WHERE site = $1
LIMIT 1;

-- name: UpdateDomain :exec
UPDATE
    domain
SET check_aaaa = $2,
    check_www  = $3,
    check_ns   = $4,
    check_curl = $5,
    ts_aaaa    = $6,
    ts_www     = $7,
    ts_ns      = $8,
    ts_curl    = $9,
    ts_check   = $10,
    ts_updated = $11,
    asn_id     = $12,
    country_id = $13
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
LIMIT $2 OFFSET $3;

-- name: ListDomainShamers :many
SELECT *
FROM domain_shame_view;
