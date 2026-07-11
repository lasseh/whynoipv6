-- Changelog read surface (07 §4.8). The paginating global + per-domain
-- feeds are builder-built in internal/postgres/changeloglist.go (05-schema
-- §10.2 — one seek builder derives both walk directions); the scoped
-- country/campaign feeds below are capped to the latest-50 recent window
-- (OPEN-15 guardrail) and stay sqlc.

-- name: ChangelogByCountry :many
SELECT cl.ts, d.host, cl.field, cl.old_value, cl.new_value
FROM changelog cl JOIN domain d ON d.id = cl.domain_id
WHERE d.country_id = @country_id
ORDER BY cl.ts DESC, cl.domain_id DESC, cl.field DESC
LIMIT 50;

-- name: ChangelogByCampaign :many
SELECT cl.ts, d.host, cl.field, cl.old_value, cl.new_value
FROM changelog cl
JOIN domain d ON d.id = cl.domain_id
JOIN campaign_domain cd ON cd.domain_id = cl.domain_id AND cd.campaign_id = @campaign_id
ORDER BY cl.ts DESC, cl.domain_id DESC, cl.field DESC
LIMIT 50;

-- The ?scope=campaign global feed (07 §4.8): transitions of domains in ANY
-- campaign. Driven from the bounded campaign_domain set via a lateral read
-- of the (domain_id, ts, field) PK — never a sparse probe of the global ts
-- index. Same latest-50 recent-window cap as the other scoped feeds.
-- name: ChangelogAnyCampaign :many
SELECT sub.ts, d.host, sub.field, sub.old_value, sub.new_value
FROM (SELECT DISTINCT domain_id FROM campaign_domain) m
CROSS JOIN LATERAL (
  SELECT cl.ts, cl.domain_id, cl.field, cl.old_value, cl.new_value
  FROM changelog cl
  WHERE cl.domain_id = m.domain_id
  ORDER BY cl.ts DESC, cl.field DESC
  LIMIT 50
) sub
JOIN domain d ON d.id = sub.domain_id
ORDER BY sub.ts DESC, sub.domain_id DESC, sub.field DESC
LIMIT 50;

-- name: CampaignHasMember :one
SELECT EXISTS(
  SELECT 1 FROM campaign_domain WHERE campaign_id = @campaign_id AND domain_id = @domain_id
)::bool;

-- name: ChangelogMaxTS :one
SELECT COALESCE(max(ts), 'epoch'::timestamptz)::timestamptz FROM changelog;

-- The §4.9 confirmed-trajectory replay: the full transition history of one
-- domain, ascending, reconstructed API-side (never raw scan observations).
-- name: ChangelogReplay :many
SELECT ts, field, old_value, new_value FROM changelog
WHERE domain_id = @domain_id ORDER BY ts ASC, field ASC;

-- The §4.9 latency overlay: last scan measurement per day — the only value
-- history takes from the scan hypertable.
-- name: ScanLatencyDaily :many
SELECT DISTINCT ON (ts::date) ts::date AS day, latency_v4_ms, latency_v6_ms
FROM scan
WHERE domain_id = @domain_id AND ts >= @from_ts::TIMESTAMPTZ AND ts < @to_ts::TIMESTAMPTZ
ORDER BY ts::date, ts DESC;

