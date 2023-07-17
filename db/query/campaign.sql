-- name: InsertCampaignDomain :exec
-- The ON CONFLICT DO NOTHING clause prevents errors in case a record with the same campaign_id and site already exists.
INSERT INTO campaign_domain(campaign_id, site)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListCampaignDomain :many
-- Description: Retrieves a list of campaign domains with additional information from 'asn' and 'country' tables.
SELECT campaign_domain.*,
       asn.name as asname,
       country.country_name
FROM campaign_domain
         LEFT JOIN asn ON campaign_domain.asn_id = asn.id
         LEFT JOIN country ON campaign_domain.country_id = country.id
WHERE campaign_domain.campaign_id = $1
ORDER BY campaign_domain.id
LIMIT $2 OFFSET $3;

-- name: ViewCampaignDomain :one
SELECT campaign_domain.*,
       asn.name as asname,
       country.country_name
FROM campaign_domain
         LEFT JOIN asn ON campaign_domain.asn_id = asn.id
         LEFT JOIN country ON campaign_domain.country_id = country.id
WHERE site = $1
  AND campaign_id = $2
LIMIT 1;

-- name: CrawlCampaignDomain :many
SELECT *
FROM campaign_domain
ORDER BY id
LIMIT $1 OFFSET $2;

-- name: UpdateCampaignDomain :exec
UPDATE
    campaign_domain
SET check_aaaa = $3,
    check_www  = $4,
    check_ns   = $5,
    check_curl = $6,
    ts_aaaa    = $7,
    ts_www     = $8,
    ts_ns      = $9,
    ts_curl    = $10,
    ts_check   = $11,
    ts_updated = $12,
    asn_id     = $13,
    country_id = $14
WHERE site = $1
  AND campaign_id = $2;

-- name: DisableCampaignDomain :exec
UPDATE
    campaign_domain
SET disabled = TRUE
WHERE site = $1;

-- name: ListCampaign :many
-- Description: Retrieves a list of campaigns along with their associated domain count.
SELECT campaign.*,
       COUNT(campaign_domain.id) AS domain_count
FROM campaign
         LEFT JOIN campaign_domain ON campaign.uuid = campaign_domain.campaign_id
WHERE campaign.disabled = false
GROUP BY campaign.id
ORDER BY campaign.id;

-- name: GetCampaignByUUID :one
SELECT campaign.*,
       COUNT(campaign_domain.id) AS domain_count
FROM campaign
         LEFT JOIN campaign_domain ON campaign.uuid = campaign_domain.campaign_id
WHERE campaign.uuid = $1
GROUP BY campaign.id
LIMIT 1;

-- name: CreateCampaign :one
INSERT INTO campaign(name, description)
VALUES ($1, $2)
RETURNING *;

-- name: CreateOrUpdateCampaign :one
INSERT INTO campaign(uuid, name, description)
VALUES ($1, $2, $3)
ON CONFLICT (uuid) DO UPDATE
    SET name        = EXCLUDED.name,
        description = EXCLUDED.description
RETURNING *;


-- name: DeleteCampaignDomain :exec
DELETE
FROM campaign_domain
WHERE campaign_id = $1
  AND site = $2;
