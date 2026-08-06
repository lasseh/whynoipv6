-- =============================================================================
-- 000007_readonly_role.up.sql — read-only login role for Grafana
-- All DDL is owned by docs/spec/05-schema.md. Do not add DDL elsewhere.
-- =============================================================================

-- Grafana connects as the owning role today, which is a superuser. The
-- dashboards only ever SELECT, so that gap buys nothing and costs everything:
-- the blast radius of a Grafana bug or a mis-scoped public panel is the whole
-- database (09-ops.md §12.1). whynoipv6_ro is the replacement. It may read,
-- and nothing else.
--
-- Everything below is idempotent. A role is cluster-level state, so this file
-- has to survive a re-run, and a dump restored into a cluster that already
-- carries the role.

DO $$
DECLARE
  owner_role text := current_user;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'whynoipv6_ro') THEN
    -- Deliberately NOLOGIN with no password. A credential committed to git is
    -- a credential that ships to production; each environment sets its own
    -- (09-ops.md §12.2) and a re-run of this migration never resets it.
    --
    -- The exception handler is not belt-and-braces: a role is cluster-wide
    -- while a migration runs per-database, so the test harness migrating two
    -- databases concurrently can pass the EXISTS check in both before either
    -- CREATE lands.
    BEGIN
      CREATE ROLE whynoipv6_ro
        NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS
        CONNECTION LIMIT 20;
    EXCEPTION WHEN duplicate_object THEN
      NULL;
    END;
  END IF;

  -- Second line of defence behind the grants: even a statement that slipped
  -- through would abort, and no dashboard query can pin a connection open.
  EXECUTE 'ALTER ROLE whynoipv6_ro SET default_transaction_read_only = on';
  EXECUTE 'ALTER ROLE whynoipv6_ro SET statement_timeout = ''30s''';
  EXECUTE 'ALTER ROLE whynoipv6_ro SET idle_in_transaction_session_timeout = ''60s''';

  EXECUTE format('GRANT CONNECT ON DATABASE %I TO whynoipv6_ro', current_database());
  EXECUTE 'GRANT USAGE ON SCHEMA public TO whynoipv6_ro';
  EXECUTE 'GRANT SELECT ON ALL TABLES IN SCHEMA public TO whynoipv6_ro';

  -- Read-everything, write-nothing is the boundary being drawn here, so new
  -- tables should be readable without a follow-up migration. Scoped to the
  -- owning role because default privileges only apply to objects that role
  -- creates, and the owner differs between dev compose and production.
  EXECUTE format(
    'ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT SELECT ON TABLES TO whynoipv6_ro',
    owner_role);

  -- No CREATE anywhere: PG15+ already drops it from PUBLIC on the public
  -- schema, this makes it explicit and covers clusters restored from older
  -- dumps that still carry the old default.
  EXECUTE 'REVOKE CREATE ON SCHEMA public FROM whynoipv6_ro';
END
$$;
