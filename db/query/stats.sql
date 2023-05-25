-- name: DomainStats :one
SELECT
 count(1) filter (WHERE "ts_check" IS NOT NULL) AS "total_sites",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_aaaa = TRUE) AS "total_aaaa",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_www = TRUE) AS "total_www",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_aaaa = TRUE AND check_www = TRUE) AS "total_both",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_ns = TRUE) AS "total_ns",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_aaaa = TRUE AND check_www = TRUE AND rank < 1000) AS "top_1k",
 count(1) filter (WHERE "ts_check" IS NOT NULL AND check_ns = TRUE AND rank < 1000) AS "top_ns"
FROM domain_view_list;


-- Update `v6sites` in `country` table based on `domain` table
WITH v6_country_count AS (
  SELECT 
    country_id AS country, 
    COUNT(country_id) AS v6sites 
  FROM 
    domain 
  WHERE 
    country_id IS NOT NULL 
    AND check_aaaa = TRUE 
  GROUP BY 
    country_id
)
UPDATE 
  country 
SET 
  v6sites = v6_country_count.v6sites 
FROM 
  v6_country_count 
WHERE 
  country.id = v6_country_count.country;

-- Update `sites` in `country` table based on `domain` table
WITH country_count AS (
  SELECT 
    country_id AS country, 
    COUNT(country_id) AS sites 
  FROM 
    domain 
  WHERE 
    country_id IS NOT NULL 
  GROUP BY 
    country_id
)
UPDATE 
  country 
SET 
  sites = country_count.sites 
FROM 
  country_count 
WHERE 
  country.id = country_count.country;

-- Update `percent` in `country` table based on current `sites` and `v6sites`
UPDATE 
  country 
SET 
  percent = ROUND((v6sites::numeric / NULLIF(sites, 0)::numeric) * 100, 1) 
WHERE 
  v6sites IS NOT NULL;
