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
      domain_view_list.base_domain = 'unsupported'
      OR domain_view_list.www_domain = 'unsupported'
    )
ORDER BY domain_view_list.id
LIMIT $2 OFFSET $3;

-- name: ListDomainHeroesByCountry :many
SELECT *
FROM domain_view_list
WHERE country_id = $1
  AND base_domain = 'supported'
  AND www_domain = 'supported'
  AND nameserver = 'supported'
  AND mx_record != 'unsupported'
ORDER BY rank
LIMIT $2 OFFSET $3;

-- name: ListCountry :many
SELECT *
FROM country
ORDER BY sites DESC;
