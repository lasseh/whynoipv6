-- name: GetCountry :one
SELECT *
FROM country
WHERE country_code = $1
LIMIT 1;

-- name: GetCountryTld :one
SELECT *
FROM country
WHERE country_tld = $1
LIMIT 1;

-- name: ListDomainsByCountry :many
SELECT *
FROM domain_view_list
WHERE country_id = $1
 AND (check_aaaa = FALSE OR check_www = FALSE)
 AND ts_check IS NOT NULL
ORDER BY id
LIMIT 50;

-- name: ListDomainHeroesByCountry :many
SELECT *
FROM domain_view_list
WHERE
 country_id = $1
 AND check_aaaa = TRUE
 AND check_www = TRUE
 AND check_ns = TRUE
ORDER BY id
LIMIT 50;

-- name: ListCountry :many
SELECT *
FROM country
ORDER BY id;

-- name: UpdateCountryStats :one
UPDATE country
SET
 sites = $2,
 v6sites = $3,
 percent = $4
WHERE id = $1 
RETURNING *;
