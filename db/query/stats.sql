-- name: DomainStats :one
-- Used by the crawler to store total stats in the metric table
SELECT
 count(1) filter (WHERE "ts_check" IS NOT NULL) AS "total_sites",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_aaaa = TRUE) AS "total_aaaa",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_www = TRUE) AS "total_www",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_aaaa = TRUE AND check_www = TRUE) AS "total_both",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_ns = TRUE) AS "total_ns",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_aaaa = TRUE AND check_www = TRUE AND rank < 1000) AS "top_1k",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_ns = TRUE AND rank < 1000) AS "top_ns"
FROM domain_view_list;


-- name: CalculateCountryStats :exec
SELECT update_country_metrics();

-- name: CalculateASNStats :exec
SELECT update_asn_metrics();
