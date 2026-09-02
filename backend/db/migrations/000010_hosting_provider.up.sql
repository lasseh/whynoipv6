-- =============================================================================
-- 000010_hosting_provider.up.sql — the hosting/CDN registry and its league
-- All DDL is owned by docs/spec/05-schema.md. Do not add DDL elsewhere.
-- =============================================================================

-- domain.hosting_provider has been a write-only pivot: attributed at commit
-- time, filterable via /domains?hosting=, never aggregated. 07-api.md §4.6 is
-- explicit that a hosting league "needs a stats source rather than a live
-- GROUP BY domain (§3.3); until one exists it is a scoped filter, not a
-- leaderboard collection". This is that stats source — counters recomputed by
-- the daily tick beside the ASN and DNS-provider ones, not per request.
--
-- slug is the join key that domain.hosting_provider stores and that
-- /domains?hosting= takes; name is the display string. dns_provider conflates
-- the two (its name IS the key) and has no UNIQUE on it despite an
-- upsert-by-name seed contract — a latent bug that is not copied here.
CREATE TABLE hosting_provider (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  slug        TEXT NOT NULL UNIQUE,   -- matches domain.hosting_provider
  name        TEXT NOT NULL,          -- display only; never a join key
  count_total INT NOT NULL DEFAULT 0,
  count_v6    INT NOT NULL DEFAULT 0
);

-- The vocabulary is compile-time constant: the union of the CDN-suffix and
-- hosting-ASN maps in internal/ingest/hosting.go. Seeded by migration rather
-- than by a v6ctl command (the dns_provider route) because nothing here is
-- operator-tunable — a new tag only appears when that Go table changes.
-- Slugs not listed here still surface: the tick upserts them with name =
-- slug, so a newly attributed CDN is never silently dropped from the league.
INSERT INTO hosting_provider (slug, name) VALUES
  ('akamai',       'Akamai'),
  ('aws',          'AWS'),
  ('azure',        'Azure'),
  ('cloudflare',   'Cloudflare'),
  ('cloudfront',   'CloudFront'),
  ('digitalocean', 'DigitalOcean'),
  ('edgecast',     'Edgecast'),
  ('fastly',       'Fastly'),
  ('google',       'Google'),
  ('hetzner',      'Hetzner'),
  ('linode',       'Linode'),
  ('ovh',          'OVH'),
  ('stackpath',    'StackPath')
-- Idempotent, so `migrate force N-1 && migrate up` does not die here and
-- leave the version dirty again (review issue 67). DO UPDATE, not DO NOTHING:
-- the display names are corrected occasionally, and this INSERT is the only
-- place they are written — the tick's upsert sets name = slug.
ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name;

-- Closes a standing OPEN-2 violation: hosting_provider is a public filter
-- (?hosting=, internal/postgres/domainlist.go) that had no index, while its
-- two siblings idx_domain_tld and idx_domain_dns_provider have carried this
-- exact shape since 000001. Same predicate, so the filter composes with
-- class + rank ordering instead of scanning.
CREATE INDEX idx_domain_hosting_provider ON domain (hosting_provider, classification, rank)
  WHERE hosting_provider IS NOT NULL AND rank IS NOT NULL AND NOT disabled;
