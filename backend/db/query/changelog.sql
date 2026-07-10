-- Changelog read surface (07 §4.8). Global + per-domain feeds paginate on
-- the (ts, domain_id, field) DESC keyset; the scoped country/campaign feeds
-- are capped to the latest-50 recent window (OPEN-15 guardrail).

-- name: ChangelogGlobal :many
SELECT cl.ts, d.host, cl.field, cl.old_value, cl.new_value, cl.domain_id
FROM changelog cl JOIN domain d ON d.id = cl.domain_id
WHERE (@field::TEXT = '' OR cl.field = @field)
  AND (NOT @with_from::BOOL OR cl.ts >= @from_ts::TIMESTAMPTZ)
  AND (NOT @with_to::BOOL OR cl.ts <= @to_ts::TIMESTAMPTZ)
  AND (NOT @with_seek::BOOL OR (cl.ts, cl.domain_id, cl.field) < (@seek_ts::TIMESTAMPTZ, @seek_domain::BIGINT, @seek_field::TEXT))
ORDER BY cl.ts DESC, cl.domain_id DESC, cl.field DESC
LIMIT @lim;

-- name: ChangelogByDomain :many
SELECT cl.ts, d.host, cl.field, cl.old_value, cl.new_value, cl.domain_id
FROM changelog cl JOIN domain d ON d.id = cl.domain_id
WHERE cl.domain_id = @domain_id
  AND (@field::TEXT = '' OR cl.field = @field)
  AND (NOT @with_from::BOOL OR cl.ts >= @from_ts::TIMESTAMPTZ)
  AND (NOT @with_to::BOOL OR cl.ts <= @to_ts::TIMESTAMPTZ)
  AND (NOT @with_seek::BOOL OR (cl.ts, cl.domain_id, cl.field) < (@seek_ts::TIMESTAMPTZ, @seek_domain::BIGINT, @seek_field::TEXT))
ORDER BY cl.ts DESC, cl.domain_id DESC, cl.field DESC
LIMIT @lim;

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
