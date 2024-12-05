-- Add the pgcrypto extension for using the gen_random_uuid() function. 
-- This needs to be done by a superuser.
-- CREATE EXTENSION IF NOT EXISTS pgcrypto; 

CREATE TABLE "lists" (
  "id" BIGSERIAL PRIMARY KEY,
  "name" TEXT UNIQUE NOT NULL,
  "ts" TIMESTAMPTZ NOT NULL
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
  "domain_id" BIGINT NOT NULL, -- Site, ref: domain.id
  "message" TEXT NOT NULL, -- Message
  "ipv6_status" TEXT NOT NULL -- Status of the changelog
);
CREATE INDEX idx_changelog_domain_id ON changelog(domain_id);

CREATE TABLE "asn" (
  "id" BIGSERIAL PRIMARY KEY,
  "number" INT NOT NULL, -- AS Number
  "name" TEXT NOT NULL, -- AS Name
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
  "country_name" VARCHAR(100) NOT NULL, -- Country name
  "country_code" CHAR(2) NOT NULL, -- ISO 3166-1 alpha-2
  "country_tld" VARCHAR(5) NOT NULL, -- top level domain
  "continent" continents, -- Continent
  "sites" INT NOT NULL DEFAULT 0, -- number of sites in this country
  "v6sites" INT NOT NULL DEFAULT 0, -- number of sites in this country with v6
  "percent" NUMERIC(4,1) NOT NULL DEFAULT 0 -- percent of sites in this country
);
CREATE INDEX idx_country_id ON country(id);
CREATE UNIQUE INDEX idx_country_country_code ON country(country_code);

CREATE TABLE "domain" (
  "id" BIGSERIAL PRIMARY KEY,
  "site" TEXT NOT NULL,
  "base_domain" TEXT NOT NULL DEFAULT 'unsupported', -- Check AAAA Record 
  "www_domain" TEXT NOT NULL DEFAULT 'unsupported', -- Check AAAA Record for WWW
  "nameserver" TEXT NOT NULL DEFAULT 'unsupported', -- Check NS Record
  "mx_record" TEXT NOT NULL DEFAULT 'unsupported', -- Check MX Record
  "v6_only" TEXT NOT NULL DEFAULT 'unsupported', -- Check Curl 
  "asn_id" BIGINT, -- map to asn table
  "country_id" BIGINT, -- map to country table
  "disabled" BOOLEAN NOT NULL DEFAULT FALSE, -- ignore domain: faulty, spam or disabled
  "ts_base_domain" TIMESTAMPTZ, -- timestamp of last AAAA check
  "ts_www_domain" TIMESTAMPTZ, -- timestamp of last AAAA WWW check
  "ts_nameserver" TIMESTAMPTZ, -- timestamp of last NS check
  "ts_mx_record" TIMESTAMPTZ, -- timestamp of last MX check
  "ts_v6_only" TIMESTAMPTZ, -- timestamp of last curl check
  "ts_check" TIMESTAMPTZ, -- timestamp of last check
  "ts_updated" TIMESTAMPTZ, --  timestamp of last update
  UNIQUE(site)
);
ALTER TABLE "domain" ADD FOREIGN KEY ("asn_id") REFERENCES "asn" ("id");
ALTER TABLE "domain" ADD FOREIGN KEY ("country_id") REFERENCES "country" ("id");
ALTER TABLE "changelog" ADD FOREIGN KEY ("domain_id") REFERENCES domain(id);
CREATE INDEX idx_domain_site ON domain(site);
CREATE INDEX idx_domain_base_domain ON domain(base_domain);
CREATE INDEX idx_domain_www_domain ON domain(www_domain);
CREATE INDEX idx_domain_nameserver ON domain(nameserver);
CREATE INDEX idx_domain_mx_record ON domain(mx_record);
CREATE INDEX idx_domain_v6_only ON domain(v6_only);
CREATE INDEX idx_domain_base_domain_www ON domain(base_domain, www_domain);
CREATE INDEX idx_domain_base_domain_www_ns ON domain(base_domain, www_domain, nameserver);
CREATE INDEX idx_domain_country_base_domain_www_ns ON domain(country_id, base_domain, www_domain, nameserver);
CREATE INDEX idx_domain_country_base_domain_www ON domain(country_id, base_domain, www_domain);
CREATE INDEX idx_domain_asn_id ON domain(asn_id);
CREATE INDEX idx_domain_country_id ON domain(country_id);
CREATE INDEX idx_domain_ts_check ON domain(ts_check);
CREATE INDEX idx_domain_disabled ON domain(disabled);

CREATE TABLE "top_shame" (
  "id" BIGSERIAL PRIMARY KEY,
  "site" TEXT NOT NULL,
  UNIQUE(site)
);

-- Campaign 
CREATE TABLE "campaign" (
  "id" BIGSERIAL PRIMARY KEY,
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(), 
  "uuid" UUID UNIQUE DEFAULT gen_random_uuid () NOT NULL,
  "name" TEXT NOT NULL,
  "description" TEXT NOT NULL,
  "disabled" BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX idx_campaign_domain_uuid ON campaign(uuid);
CREATE INDEX idx_campaign_disabled ON campaign(disabled);

CREATE TABLE "campaign_domain" (
  "id" BIGSERIAL PRIMARY KEY,
  "campaign_id" UUID NOT NULL REFERENCES campaign(uuid) ON DELETE CASCADE,
  "site" TEXT NOT NULL,
  "base_domain" TEXT NOT NULL DEFAULT 'unsupported', -- Check AAAA Record
  "www_domain" TEXT NOT NULL DEFAULT 'unsupported', -- Check AAAA Record for WWW
  "nameserver" TEXT NOT NULL DEFAULT 'unsupported', -- Check NS Record
  "mx_record" TEXT NOT NULL DEFAULT 'unsupported', -- Check MX Record
  "v6_only" TEXT NOT NULL DEFAULT 'unsupported', -- Check Curl 
  "asn_id" BIGINT REFERENCES asn(id) ON DELETE SET NULL,
  "country_id" BIGINT, -- map to country table
  "disabled" BOOLEAN NOT NULL DEFAULT FALSE, -- ignore domain: faulty, spam or disabled
  "ts_base_domain" TIMESTAMPTZ, -- timestamp of last AAAA check
  "ts_www_domain" TIMESTAMPTZ, -- timestamp of last AAAA WWW check
  "ts_nameserver" TIMESTAMPTZ, -- timestamp of last NS check
  "ts_mx_record" TIMESTAMPTZ, -- timestamp of last MX check
  "ts_v6_only" TIMESTAMPTZ, -- timestamp of last curl check
  "ts_check" TIMESTAMPTZ, -- timestamp of last check
  "ts_updated" TIMESTAMPTZ, --  timestamp of last update
  UNIQUE(campaign_id,site)
);
CREATE INDEX idx_campaign_domain_campaign_id ON campaign_domain(campaign_id, site);
CREATE INDEX idx_campaign_domain_base_domain ON campaign_domain(base_domain);
CREATE INDEX idx_campaign_domain_www_domain ON campaign_domain(www_domain);
CREATE INDEX idx_campaign_domain_nameserver ON campaign_domain(nameserver);
CREATE INDEX idx_campaign_domain_mx_record ON campaign_domain(mx_record);
CREATE INDEX idx_campaign_domain_v6_only ON campaign_domain(v6_only);
CREATE INDEX idx_campaign_domain_base_domain_www ON campaign_domain(base_domain, www_domain);
CREATE INDEX idx_campaign_domain_base_domain_www_ns ON campaign_domain(base_domain, www_domain, nameserver);
CREATE INDEX idx_campaign_domain_asn_id ON campaign_domain(asn_id);
CREATE INDEX idx_campaign_domain_country_id ON campaign_domain(country_id);
CREATE INDEX idx_campaign_domain_ts_check ON campaign_domain(ts_check);
CREATE INDEX idx_campaign_domain_disabled ON campaign_domain(disabled);

CREATE TABLE "campaign_changelog" (
  "id" BIGSERIAL PRIMARY KEY, -- Unique identifier for each change
  "ts" TIMESTAMPTZ NOT NULL DEFAULT NOW(), -- Timestamp of the change
  "domain_id" BIGINT NOT NULL REFERENCES campaign_domain(id) ON DELETE CASCADE, -- Foreign key referencing campaign_domain table
  "campaign_id" UUID NOT NULL REFERENCES campaign(uuid) ON DELETE CASCADE, -- Foreign key referencing campaign table
  "message" TEXT NOT NULL, -- Message describing the change
  "ipv6_status" TEXT NOT NULL -- Status of IPv6
);
CREATE INDEX idx_campaign_changelog_domain_id ON campaign_changelog(domain_id);
CREATE INDEX idx_campaign_changelog_campaign_id ON campaign_changelog(campaign_id);

CREATE TABLE "metrics" (
    id BIGSERIAL PRIMARY KEY,
    measurement VARCHAR(255) NOT NULL,
    time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    data jsonb NOT NULL
);
CREATE INDEX idx_metrics_measurement_time ON metrics (measurement, time DESC);

CREATE TABLE "domain_log" (
    "id" BIGSERIAL PRIMARY KEY,
    "domain_id" BIGINT NOT NULL REFERENCES domain(id) ON DELETE CASCADE,
    "time" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "data" jsonb NOT NULL
);
CREATE INDEX idx_domain_log_id ON domain_log(domain_id);
CREATE INDEX idx_domain_log_time ON domain_log(domain_id, time DESC);

CREATE TABLE "campaign_domain_log" (
    "id" BIGSERIAL PRIMARY KEY,
    "domain_id" BIGINT NOT NULL REFERENCES campaign_domain(id) ON DELETE CASCADE,
    "time" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "data" jsonb NOT NULL
);
CREATE INDEX idx_campaign_domain_log_id ON campaign_domain_log(domain_id);
CREATE INDEX idx_campaign_domain_log_time ON campaign_domain_log(domain_id, time DESC);

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
WHERE domain.disabled = FALSE;
-- ORDER BY sites.rank;

-- This view is used by the crawler to get a list of domains to crawl.
CREATE or REPLACE VIEW domain_crawl_list AS
SELECT *
FROM domain
WHERE (disabled is FALSE)
  AND ((ts_check < now() - '1 days' :: interval) OR (ts_check IS NULL));
-- ORDER BY id;

--
CREATE or REPLACE VIEW changelog_view AS
SELECT changelog.*,
       domain.site
FROM changelog
         JOIN domain on changelog.domain_id = domain.id
ORDER BY changelog.id DESC;

-- 
CREATE or REPLACE VIEW changelog_campaign_view AS
SELECT campaign_changelog.*,
       campaign_domain.site
FROM campaign_changelog
         JOIN campaign_domain on campaign_changelog.domain_id = campaign_domain.id
ORDER BY campaign_changelog.id DESC;

-- 
CREATE OR REPLACE VIEW "domain_shame_view" AS
SELECT domain.id      AS "id",
       domain.site    AS "site",
       domain.base_domain,
       domain.www_domain,
       domain.nameserver,
       domain.mx_record,
       domain.v6_only,
       domain.asn_id,
       domain.country_id,
       domain.disabled,
       domain.ts_base_domain,
       domain.ts_www_domain,
       domain.ts_nameserver,
       domain.ts_mx_record,
       domain.ts_v6_only,
       domain.ts_check,
       domain.ts_updated,
       top_shame.id   AS "shame_id",
       top_shame.site AS "shame_site"
FROM domain
         JOIN
     top_shame
     ON
         domain.site = top_shame.site
WHERE domain.base_domain = 'unsupported'
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
      AND base_domain = 'supported'
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
      asn_id IS NOT NULL AND base_domain = 'supported' AND www_domain = 'supported' AND nameserver = 'supported'
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
