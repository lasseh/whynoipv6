-- name: InsertCampaignDomain :exec
INSERT INTO campaign_domain(campaign_id, site)
VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: ListCampaignDomain :many
SELECT 
 campaign_domain.*,
 asn.name as asname,
 country.country_name
FROM campaign_domain
LEFT JOIN asn ON campaign_domain.asn_id = asn.id
LEFT JOIN country ON campaign_domain.country_id = country.id
WHERE campaign_domain.campaign_id = $1
AND ts_check IS NOT NULL
ORDER BY campaign_domain.id
LIMIT $2 
OFFSET $3;

-- name: ViewCampaignDomain :one
SELECT 
 campaign_domain.*,
 asn.name as asname,
 country.country_name
FROM campaign_domain
LEFT JOIN asn ON campaign_domain.asn_id = asn.id
LEFT JOIN country ON campaign_domain.country_id = country.id
WHERE site = $1
LIMIT 1;

-- name: CrawlCampaignDomain :many
SELECT *
FROM campaign_domain
LIMIT $1 
OFFSET $2;

-- name: UpdateCampaignDomain :exec
UPDATE campaign_domain
SET
check_aaaa = $2,
check_www = $3,
check_ns = $4,
check_curl = $5,
ts_aaaa = $6,
ts_www = $7,
ts_ns = $8,
ts_curl = $9,
ts_check = $10,
ts_updated = $11,
asn_id = $12,
country_id = $13
WHERE site = $1;

-- name: DisableCampaignDomain :exec
UPDATE campaign_domain
SET disabled = TRUE
WHERE site = $1;

-- name: ListCampaign :many
SELECT campaign.*, COUNT(campaign_domain.id)
FROM campaign
LEFT JOIN campaign_domain ON campaign.uuid = campaign_domain.campaign_id
WHERE campaign.disabled = false
GROUP BY campaign.id
ORDER BY id;

-- name: CreateCampaign :one
INSERT INTO campaign(name, description)
VALUES ($1, $2)
RETURNING *;
