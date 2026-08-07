-- db/query/hosting.sql — hosting/CDN registry (layout: 05-schema.md §10.2).

-- Tick step 3 — hosting counter recompute, beside the ASN and DNS-provider
-- pairs (06-ingest.md §10.6).
--
-- Population and v6 predicate deliberately match RecomputeASNCounters and
-- RecomputeProviderCounters exactly: ranked, not disabled, and v6 meaning
-- classification IN ('partial','hero'). All three registries render in one
-- switcher on /metrics with shared axes and a shared colour ramp, so a
-- different population or a different v6 definition here would silently
-- change what the chart means when the reader switches entity.
-- name: ResetHostingCounters :exec
UPDATE hosting_provider SET count_total = 0, count_v6 = 0;

-- Upsert rather than UPDATE...FROM: a slug newly emitted by the attribution
-- code has no seeded row yet, and it must join the league under its own slug
-- as a display name rather than vanish. ON CONFLICT touches only the
-- counters, so the curated display names survive every tick.
-- name: RecomputeHostingCounters :exec
INSERT INTO hosting_provider (slug, name, count_total, count_v6)
SELECT hosting_provider, hosting_provider,
       count(*),
       count(*) FILTER (WHERE classification IN ('partial','hero'))
FROM domain
WHERE rank IS NOT NULL AND NOT disabled AND hosting_provider IS NOT NULL
GROUP BY hosting_provider
ON CONFLICT (slug) DO UPDATE SET
  count_total = excluded.count_total,
  count_v6    = excluded.count_v6;

-- The API hosting league (07 §4.6): exact stored counters, count_v4
-- synthesized server-side. Rows that attribution has never produced stay at
-- zero rather than being hidden — the seed set is the documented vocabulary.
-- name: HostingLeaderboard :many
SELECT slug, name, count_total, count_v6
FROM hosting_provider
ORDER BY count_total DESC, slug ASC;
