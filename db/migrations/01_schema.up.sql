-- Add the pgcrypto extension for using the gen_random_uuid() function. 
-- This needs to be done by a superuser.
-- CREATE EXTENSION IF NOT EXISTS pgcrypto; 

CREATE TABLE "lists" (
  "id" BIGSERIAL PRIMARY KEY,
  "name" text UNIQUE NOT NULL,
  "ts" timestamptz NOT NULL
);

CREATE TABLE "sites" (
  "id" BIGSERIAL PRIMARY KEY,
  "list_id" BIGINT NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
  "rank" BIGINT NOT NULL,
  "site" TEXT NOT NULL,
  UNIQUE (list_id, rank),
  UNIQUE (list_id, site)
);
CREATE INDEX idx_sites_rank ON sites(rank);
CREATE INDEX idx_sites_site ON sites(site);

CREATE TABLE "changelog" (
  "id" BIGSERIAL PRIMARY KEY, -- Unique identifier for each change
  "ts" TIMESTAMPTZ NOT NULL DEFAULT NOW(), -- Timestamp 
  "domain_id" int NOT NULL, -- Site, ref: sites.site
  "message" text NOT NULL, -- Message
  "ipv6_status" boolean NOT NULL DEFAULT NULL -- Status of IPv6
);
CREATE INDEX idx_changelog_domain_id ON changelog(domain_id);

CREATE TABLE "asn" (
  "id" BIGSERIAL PRIMARY KEY,
  "number" int NOT NULL, -- AS Number
  "name" text NOT NULL, -- AS Name
  "count_v4" INT NULL, -- number of sites with v4-only in this ASN
  "count_v6" INT NULL, -- number of sites with v6 support in this ASN
  "percent_v4" FLOAT NULL, -- percent of sites with v4-only in this ASN
  "percent_v6" FLOAT NULL -- percent of sites with v6 support in this ASN
);
CREATE INDEX idx_asn_id ON asn(id);

DROP TYPE IF EXISTS "continents";
CREATE TYPE "continents" AS ENUM (
  'Africa',
  'Antarctica',
  'Asia',
  'Europe',
  'Oceania',
  'North America',
  'South America'
);

CREATE TABLE "country" (
  "id" BIGSERIAL PRIMARY KEY,
  "country_name" character varying(100) NOT NULL, -- Country name
  "country_code" character varying(2) NOT NULL, -- ISO 3166-1 alpha-2
  "country_tld" character varying(5) NOT NULL, -- top level domain
  "continent" continents, -- Continent
  "sites" integer NOT NULL DEFAULT 0, -- number of sites in this country
  "v6sites" integer NOT NULL DEFAULT 0, -- number of sites in this country with v6
  "percent" numeric(4,1) NOT NULL DEFAULT 0 -- percent of sites in this country
);
CREATE INDEX idx_country_id ON country(id);
CREATE UNIQUE INDEX idx_country_country_code ON country(country_code);

CREATE TABLE "domain" (
  "id" BIGSERIAL PRIMARY KEY,
  "site" TEXT NOT NULL,
  "check_aaaa" boolean NOT NULL DEFAULT FALSE, -- Check AAAA Record
  "check_www" boolean NOT NULL DEFAULT FALSE, -- Check AAAA Record for WWW
  "check_ns" boolean NOT NULL DEFAULT FALSE, -- Check NS Record
  "check_curl" boolean NOT NULL DEFAULT FALSE, -- Check Curl 
  "asn_id" BIGINT, -- map to asn table
  "country_id" BIGINT, -- map to country table
  "disabled" boolean NOT NULL DEFAULT FALSE, -- ignore domain: faulty, spam or disabled
  "ts_aaaa" TIMESTAMPTZ, -- timestamp of last AAAA check
  "ts_www" TIMESTAMPTZ, -- timestamp of last AAAA WWW check
  "ts_ns" TIMESTAMPTZ, -- timestamp of last NS check
  "ts_curl" TIMESTAMPTZ, -- timestamp of last curl check
  "ts_check" TIMESTAMPTZ, -- timestamp of last check
  "ts_updated" TIMESTAMPTZ, --  timestamp of last update
  UNIQUE(site)
);
ALTER TABLE "domain" ADD FOREIGN KEY ("asn_id") REFERENCES "asn" ("id");
ALTER TABLE "domain" ADD FOREIGN KEY ("country_id") REFERENCES "country" ("id");
ALTER TABLE "changelog" ADD FOREIGN KEY ("domain_id") REFERENCES "domain" ("id");
CREATE INDEX idx_domain_site ON domain(site);
CREATE INDEX idx_domain_check_aaaa ON domain(check_aaaa);
CREATE INDEX idx_domain_check_www ON domain(check_www);
CREATE INDEX idx_domain_check_ns ON domain(check_ns);
CREATE INDEX idx_domain_check_aaaa_www ON domain(check_aaaa, check_www);
CREATE INDEX idx_domain_check_aaaa_www_ns ON domain(check_aaaa, check_www, check_ns);
CREATE INDEX idx_domain_country_check_aaaa_www_ns ON domain(country_id, check_aaaa, check_www, check_ns);
CREATE INDEX idx_domain_asn_id ON domain(asn_id);
CREATE INDEX idx_domain_country_id ON domain(country_id);
CREATE INDEX idx_domain_ts_check ON domain(ts_check);
CREATE INDEX idx_domain_disabled ON domain(disabled);

CREATE TABLE "top_shame" (
  "id" BIGSERIAL PRIMARY KEY,
  "site" TEXT NOT NULL,
  UNIQUE(site)
);

CREATE TABLE "stats_asn" (
  "id" BIGSERIAL PRIMARY KEY,
  "asn_id" BIGINT NOT NULL, -- AS Number
  "v4_count" integer NOT NULL DEFAULT 0, -- number of sites with v4-only in this ASN
  "v4_percent" numeric(4,1) NOT NULL DEFAULT 0, -- percent of sites with v4-only in this ASN
  "v6_count" integer NOT NULL DEFAULT 0, -- number of sites with v6-only in this ASN
  "v6_percent" numeric(4,1) NOT NULL DEFAULT 0, -- percent of sites with v6-only in this ASN
  "ts" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE "stats_asn" ADD FOREIGN KEY ("asn_id") REFERENCES "asn" ("id");

-- Campaign 
CREATE TABLE "campaign" (
  "id" BIGSERIAL PRIMARY KEY,
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(), 
  "uuid" UUID UNIQUE DEFAULT gen_random_uuid () NOT NULL,
  "name" text NOT NULL,
  "description" text NOT NULL,
  "disabled" boolean NOT NULL DEFAULT FALSE
);
CREATE INDEX idx_campaign_domain_uuid ON campaign(uuid);
CREATE INDEX idx_campaign_disabled ON campaign(disabled);

CREATE TABLE "campaign_domain" (
  "id" BIGSERIAL PRIMARY KEY,
  "campaign_id" UUID NOT NULL REFERENCES campaign(uuid) ON DELETE CASCADE,
  "site" TEXT NOT NULL,
  "check_aaaa" boolean NOT NULL DEFAULT FALSE, -- Check AAAA Record
  "check_www" boolean NOT NULL DEFAULT FALSE, -- Check AAAA Record for WWW
  "check_ns" boolean NOT NULL DEFAULT FALSE, -- Check NS Record
  "check_curl" boolean NOT NULL DEFAULT FALSE, -- Check Curl 
  "asn_id" BIGINT REFERENCES asn(id) ON DELETE SET NULL,
  "country_id" BIGINT, -- map to country table
  "disabled" boolean NOT NULL DEFAULT FALSE, -- ignore domain: faulty, spam or disabled
  "ts_aaaa" TIMESTAMPTZ, -- timestamp of last AAAA check
  "ts_www" TIMESTAMPTZ, -- timestamp of last AAAA WWW check
  "ts_ns" TIMESTAMPTZ, -- timestamp of last NS check
  "ts_curl" TIMESTAMPTZ, -- timestamp of last curl check
  "ts_check" TIMESTAMPTZ, -- timestamp of last check
  "ts_updated" TIMESTAMPTZ, --  timestamp of last update
  UNIQUE(campaign_id,site)
);
CREATE INDEX idx_campaign_domain_campaign_id ON campaign_domain(campaign_id, site);
CREATE INDEX idx_campaign_domain_check_aaaa ON campaign_domain(check_aaaa);
CREATE INDEX idx_campaign_domain_check_www ON campaign_domain(check_www);
CREATE INDEX idx_campaign_domain_check_ns ON campaign_domain(check_ns);
CREATE INDEX idx_campaign_domain_check_aaaa_www ON campaign_domain(check_aaaa, check_www);
CREATE INDEX idx_campaign_domain_check_aaaa_www_ns ON campaign_domain(check_aaaa, check_www, check_ns);
CREATE INDEX idx_campaign_domain_asn_id ON campaign_domain(asn_id);
CREATE INDEX idx_campaign_domain_country_id ON campaign_domain(country_id);
CREATE INDEX idx_campaign_domain_ts_check ON campaign_domain(ts_check);
CREATE INDEX idx_campaign_domain_disabled ON campaign_domain(disabled);

CREATE TABLE "campaign_changelog" (
  "id" BIGSERIAL PRIMARY KEY, -- Unique identifier for each change
  "ts" TIMESTAMPTZ NOT NULL DEFAULT NOW(), -- Timestamp of the change
  "domain_id" INT NOT NULL REFERENCES campaign_domain(id) ON DELETE CASCADE, -- Foreign key referencing campaign_domain table
  "campaign_id" UUID NOT NULL REFERENCES campaign(uuid) ON DELETE CASCADE, -- Foreign key referencing campaign table
  "message" TEXT NOT NULL, -- Message describing the change
  "ipv6_status" boolean NOT NULL DEFAULT NULL -- Status of IPv6
);
CREATE INDEX idx_campaign_changelog_domain_id ON campaign_changelog(domain_id);
CREATE INDEX idx_campaign_changelog_campaign_id ON campaign_changelog(campaign_id);

CREATE TABLE "metrics" (
    id BIGSERIAL PRIMARY KEY,
    measurement VARCHAR(255) NOT NULL,
    time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    data jsonb NOT NULL
);

-- VIEWS ------------------------------------------------------
CREATE or REPLACE VIEW domain_view_list AS
SELECT domain.*,
       sites.rank,
       asn.name as asname,
       country.country_name
FROM domain
         RIGHT JOIN sites ON domain.site = sites.site
         LEFT JOIN asn ON domain.asn_id = asn.id
         LEFT JOIN country ON domain.country_id = country.id
WHERE domain.disabled = FALSE
ORDER BY sites.rank;

-- CREATE MATERIALIZED VIEW domain_view_index AS
CREATE VIEW domain_view_index AS
SELECT domain.*,
       sites.rank,
       asn.name as asname,
       country.country_name
FROM domain
         RIGHT JOIN sites ON domain.site = sites.site
         LEFT JOIN asn ON domain.asn_id = asn.id
         LEFT JOIN country ON domain.country_id = country.id
WHERE domain.disabled = FALSE AND check_aaaa = FALSE
   OR check_www = FALSE
ORDER BY sites.rank
LIMIT 100;

CREATE VIEW domain_view_heroes AS
SELECT domain.*,
       sites.rank,
       asn.name as asname,
       country.country_name
FROM domain
         RIGHT JOIN sites ON domain.site = sites.site
         LEFT JOIN asn ON domain.asn_id = asn.id
         LEFT JOIN country ON domain.country_id = country.id
WHERE domain.disabled = FALSE
  AND check_aaaa = TRUE
  AND check_www = TRUE
  AND check_ns = TRUE
ORDER BY sites.rank
LIMIT 100;

CREATE or REPLACE VIEW domain_crawl_list AS
SELECT *
FROM domain
WHERE (disabled is FALSE)
  AND ((ts_check < now() - '3 days' :: interval) OR (ts_check IS NULL))
ORDER BY id;

CREATE or REPLACE VIEW changelog_view AS
SELECT changelog.*,
       domain.site
FROM changelog
         JOIN domain on changelog.domain_id = domain.id
ORDER BY changelog.id DESC;

CREATE or REPLACE VIEW changelog_campaign_view AS
SELECT campaign_changelog.*,
       campaign_domain.site
FROM campaign_changelog
         JOIN campaign_domain on campaign_changelog.domain_id = campaign_domain.id
ORDER BY campaign_changelog.id DESC;

CREATE OR REPLACE VIEW "domain_shame_view" AS
SELECT domain.id      AS "id",
       domain.site    AS "site",
       domain.check_aaaa,
       domain.check_www,
       domain.check_ns,
       domain.check_curl,
       domain.asn_id,
       domain.country_id,
       domain.disabled,
       domain.ts_aaaa,
       domain.ts_www,
       domain.ts_ns,
       domain.ts_curl,
       domain.ts_check,
       domain.ts_updated,
       top_shame.id   AS "shame_id",
       top_shame.site AS "shame_site"
FROM domain
         JOIN
     top_shame
     ON
         domain."site" = top_shame."site"
WHERE domain."check_aaaa" = FALSE
ORDER BY domain.id;

-- Stored Procedures ------------------------------------------------------
CREATE OR REPLACE FUNCTION update_country_metrics() RETURNS VOID AS $$
BEGIN
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
    v6sites = COALESCE(v6_country_count.v6sites, 0)
  FROM
    v6_country_count
  WHERE
    country.id = v6_country_count.country;
  
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
    sites = country_count.sites,
    percent = ROUND((COALESCE(country.v6sites, 0)::numeric / NULLIF(country_count.sites, 0)::numeric) * 100, 1)
  FROM
    country_count
  WHERE
    country.id = country_count.country;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION update_asn_metrics() RETURNS VOID AS $$
BEGIN
  WITH v4_count AS (
    SELECT
      asn_id,
      COUNT(asn_id) AS count_v4
    FROM
      domain
    WHERE
      asn_id IS NOT NULL
    GROUP BY
      asn_id
  ),
  v6_count AS (
    SELECT
      asn_id,
      COUNT(asn_id) AS count_v6
    FROM
      domain
    WHERE
      asn_id IS NOT NULL AND check_aaaa = TRUE AND check_www = TRUE AND check_ns = TRUE
    GROUP BY
      asn_id
  )
  UPDATE
    asn
  SET
    count_v4 = COALESCE(v4_count.count_v4, 0),
    count_v6 = COALESCE(v6_count.count_v6, 0),
    percent_v4 = ROUND((COALESCE(v4_count.count_v4, 0)::numeric / NULLIF(COALESCE(v4_count.count_v4, 0) + COALESCE(v6_count.count_v6, 0), 0)::numeric) * 100, 1),
    percent_v6 = ROUND((COALESCE(v6_count.count_v6, 0)::numeric / NULLIF(COALESCE(v4_count.count_v4, 0) + COALESCE(v6_count.count_v6, 0), 0)::numeric) * 100, 1)
  FROM
    v4_count
  FULL OUTER JOIN
    v6_count
  ON
    v4_count.asn_id = v6_count.asn_id
  WHERE
    asn.id = COALESCE(v4_count.asn_id, v6_count.asn_id);
END;
$$ LANGUAGE plpgsql;
