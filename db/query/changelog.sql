-- name: ListChangelog :many
SELECT *
FROM changelog_view
LIMIT $1 OFFSET $2;

-- name: GetChangelogByDomain :many
SELECT *
FROM changelog_view
WHERE site = $1
LIMIT $2 OFFSET $3;

-- name: GetChangelogByCampaign :many
SELECT *
FROM changelog_campaign_view
WHERE campaign_id = $1
LIMIT $2 OFFSET $3;

-- name: GetChangelogByCampaignDomain :many
SELECT *
FROM changelog_campaign_view
WHERE campaign_id = $1
  AND site = $2
LIMIT $3 OFFSET $4;

-- name: CreateChangelog :one
INSERT INTO changelog (domain_id, message, ipv6_status)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateCampaignChangelog :one
INSERT INTO campaign_changelog (domain_id, campaign_id, message, ipv6_status)
VALUES ($1, $2, $3, $4)
RETURNING *;
