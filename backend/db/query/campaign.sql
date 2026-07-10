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
