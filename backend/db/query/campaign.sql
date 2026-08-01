-- db/query/campaign.sql — sqlc query source (layout: 05-schema.md §10.2).

-- name: CampaignByUUID :one
SELECT id, uuid, name, source_file, disabled FROM campaign WHERE uuid = $1;

-- name: CampaignUUIDBySourceFile :one
SELECT uuid FROM campaign WHERE source_file = $1;

-- name: CampaignUpdateFromFile :one
UPDATE campaign
SET name = $2, description = $3, tags = $4, source_file = $5,
    disabled = false, updated_at = now()
WHERE uuid = $1
RETURNING id;

-- name: CampaignInsert :one
INSERT INTO campaign (uuid, name, description, source_file, tags)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: CampaignMembers :many
SELECT domain_id FROM campaign_domain WHERE campaign_id = $1;

-- name: CampaignAddMember :execrows
INSERT INTO campaign_domain (campaign_id, domain_id)
VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: CampaignRemoveMembersNotIn :execrows
DELETE FROM campaign_domain
WHERE campaign_id = @campaign_id AND domain_id <> ALL(@domain_ids::bigint[]);

-- name: CampaignDisableAbsent :many
UPDATE campaign SET disabled = true, updated_at = now()
WHERE NOT disabled AND uuid <> ALL($1::uuid[])
RETURNING uuid, name;

-- The public campaign surface (07 §4.7): exact member counts (bounded sets),
-- ?tag= via the GIN-indexed tags array. Each row carries the same adoption
-- pair as the detail via a lateral read of the latest stats_campaign_daily
-- row (the set is tens of rows, so the per-row join is trivially cheap).
-- name: CampaignPublicList :many
SELECT c.uuid, c.name, c.description, c.source_file, c.tags,
       (SELECT count(*) FROM campaign_domain cd WHERE cd.campaign_id = c.id) AS domain_count,
       s.day AS adoption_day, s.domains AS adoption_domains, s.v6_ready AS adoption_v6_ready
FROM campaign c
LEFT JOIN LATERAL (
    SELECT day, domains, v6_ready FROM stats_campaign_daily scd
    WHERE scd.campaign_id = c.id ORDER BY day DESC LIMIT 1
) s ON true
WHERE NOT c.disabled AND (@tag::TEXT = '' OR @tag = ANY(c.tags))
ORDER BY c.name, c.id;

-- name: CampaignPublicDetail :one
SELECT c.id, c.uuid, c.name, c.description, c.source_file, c.tags, c.disabled,
       (SELECT count(*) FROM campaign_domain cd WHERE cd.campaign_id = c.id) AS domain_count
FROM campaign c WHERE c.uuid = @uuid;

-- name: CampaignAdoption :one
SELECT day, domains, v6_ready FROM stats_campaign_daily
WHERE campaign_id = @campaign_id ORDER BY day DESC LIMIT 1;

-- The mandate campaigns a domain belongs to (07 §4.3): drives the domain
-- detail's mandate badge. Bounded by the handful of tagged campaigns.
-- name: DomainMandates :many
SELECT c.uuid, c.name
FROM campaign c
JOIN campaign_domain cd ON cd.campaign_id = c.id
WHERE cd.domain_id = @domain_id AND NOT c.disabled AND 'mandate' = ANY(c.tags)
ORDER BY c.name;
