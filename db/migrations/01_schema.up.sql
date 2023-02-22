-- CREATE EXTENSION pgcrypto;

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

CREATE TABLE "changelog" (
  "id" BIGSERIAL PRIMARY KEY, -- 
  "ts" TIMESTAMPTZ NOT NULL DEFAULT NOW(), -- Timestamp
  "domain_id" int NOT NULL, -- Site, ref: sites.site
  "message" text NOT NULL -- Message
);

CREATE TABLE "asn" (
  "id" BIGSERIAL PRIMARY KEY,
  "number" int NOT NULL, -- AS Number
  "name" text NOT NULL -- AS Name
);

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

CREATE TABLE "campaign_changelog" (
  "id" BIGSERIAL PRIMARY KEY, -- 
  "ts" TIMESTAMPTZ NOT NULL DEFAULT NOW(), -- Timestamp
  "domain_id" int NOT NULL, -- Site,
  "campaign_id" UUID NOT NULL REFERENCES campaign(uuid),
  "message" text NOT NULL -- Message
);

CREATE TABLE "campaign_domain" (
  "id" BIGSERIAL PRIMARY KEY,
  "campaign_id" UUID NOT NULL REFERENCES campaign(uuid),
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
  UNIQUE(campaign_id,site)
);
ALTER TABLE "campaign_domain" ADD FOREIGN KEY ("asn_id") REFERENCES "asn" ("id");
ALTER TABLE "campaign_changelog" ADD FOREIGN KEY ("domain_id") REFERENCES "campaign_domain" ("id");

CREATE TABLE "metrics" (
    id BIGSERIAL PRIMARY KEY,
    measurement VARCHAR(255) NOT NULL,
    time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    data jsonb NOT NULL
);

-- VIEWS ------------------------------------------------------
CREATE or REPLACE VIEW domain_view_list AS
SELECT 
 domain.*, 
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
SELECT 
 domain.*, 
 sites.rank,
 asn.name as asname,
 country.country_name
FROM domain
RIGHT JOIN sites ON domain.site = sites.site
LEFT JOIN asn ON domain.asn_id = asn.id
LEFT JOIN country ON domain.country_id = country.id
WHERE domain.disabled = FALSE AND check_aaaa = FALSE OR check_www = FALSE
ORDER BY sites.rank LIMIT 100;

-- CREATE MATERIALIZED VIEW domain_view_heroes AS
CREATE VIEW domain_view_heroes AS
SELECT 
 domain.*, 
 sites.rank,
 asn.name as asname,
 country.country_name
FROM domain
RIGHT JOIN sites ON domain.site = sites.site
LEFT JOIN asn ON domain.asn_id = asn.id
LEFT JOIN country ON domain.country_id = country.id
WHERE domain.disabled = FALSE AND check_aaaa = TRUE AND check_www = TRUE AND check_ns = TRUE
ORDER BY sites.rank LIMIT 100;

CREATE or REPLACE VIEW domain_crawl_list AS
SELECT * 
FROM domain
WHERE (ts_check < now() - '3 days' :: interval) 
OR (ts_check IS NULL)
AND disabled is FALSE 
ORDER BY id;

CREATE or REPLACE VIEW changelog_view AS
SELECT
changelog.*,
domain.site
FROM changelog
JOIN domain on changelog.domain_id = domain.id
ORDER BY changelog.id DESC;

CREATE or REPLACE VIEW changelog_campaign_view AS
SELECT
campaign_changelog.*,
campaign_domain.site
FROM campaign_changelog
JOIN campaign_domain on campaign_changelog.domain_id = campaign_domain.id
ORDER BY campaign_changelog.id DESC;

-- Index
CREATE UNIQUE INDEX "index_country_code" ON "country" USING btree ("country_code");
CREATE INDEX "index_changelog_domain" ON "changelog" USING btree ("domain_id");
