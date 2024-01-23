-- name: CrawlerStats :one
-- Used by the crawler to store total stats in the metric table
SELECT
 count(1) AS "domains",
 count(1) filter (WHERE base_domain = 'supported') AS "base_domain",
 count(1) filter (WHERE www_domain = 'supported') AS "www_domain",
 count(1) filter (WHERE nameserver = 'supported') AS "nameserver",
 count(1) filter (WHERE mx_record = 'supported') AS "mx_record",
 count(1) filter (WHERE base_domain = 'supported' AND www_domain = 'supported') AS "heroes",
 count(1) filter (WHERE base_domain != 'unsupported' AND www_domain != 'unsupported' AND rank < 1000) AS "top_heroes",
 count(1) filter (WHERE nameserver = 'supported' AND rank < 1000) AS "top_nameserver"
FROM domain_view_list;

-- name: CalculateCountryStats :exec
SELECT update_country_metrics();

-- name: CalculateASNStats :exec
SELECT update_asn_metrics();
