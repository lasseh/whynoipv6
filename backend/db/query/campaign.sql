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
WHERE campaign_id = $1 AND domain_id <> ALL($2::bigint[]);

-- name: CampaignDisableAbsent :many
UPDATE campaign SET disabled = true, updated_at = now()
WHERE NOT disabled AND uuid <> ALL($1::uuid[])
RETURNING uuid, name;

-- The public campaign surface (07 §4.7): exact member counts (bounded sets),
-- ?tag= via the GIN-indexed tags array.
-- name: CampaignPublicList :many
SELECT c.uuid, c.name, c.description, c.source_file, c.tags,
       (SELECT count(*) FROM campaign_domain cd WHERE cd.campaign_id = c.id) AS domain_count
FROM campaign c
WHERE NOT c.disabled AND (@tag::TEXT = '' OR @tag = ANY(c.tags))
ORDER BY c.name, c.id;

-- name: CampaignPublicDetail :one
SELECT c.id, c.uuid, c.name, c.description, c.source_file, c.tags, c.disabled,
       (SELECT count(*) FROM campaign_domain cd WHERE cd.campaign_id = c.id) AS domain_count
FROM campaign c WHERE c.uuid = @uuid;

-- name: CampaignAdoption :one
SELECT day, domains, v6_ready FROM stats_campaign_daily
WHERE campaign_id = @campaign_id ORDER BY day DESC LIMIT 1;
