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
WHERE domain_view_list.country_id = $1
  AND (
      domain_view_list.check_aaaa = FALSE
      OR domain_view_list.check_www = FALSE
    )
ORDER BY domain_view_list.id
LIMIT $2 OFFSET $3;

-- name: ListDomainHeroesByCountry :many
SELECT *
FROM domain_view_list
WHERE country_id = $1
  AND check_aaaa = TRUE
  AND check_www = TRUE
  AND check_ns = TRUE
ORDER BY id
LIMIT $2 OFFSET $3;

-- name: ListCountry :many
SELECT *
FROM country
ORDER BY sites DESC;
