-- Roles are cluster-level and may still hold privileges in other databases, so
-- the drop is strictly scoped: revoke what this migration granted, then drop
-- the role only if nothing else depends on it. A live Grafana datasource
-- pointed at whynoipv6_ro will stop working the moment this runs.

DO $$
DECLARE
  owner_role text := current_user;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'whynoipv6_ro') THEN
    RETURN;
  END IF;

  EXECUTE format(
    'ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public REVOKE SELECT ON TABLES FROM whynoipv6_ro',
    owner_role);
  EXECUTE 'REVOKE ALL ON ALL TABLES IN SCHEMA public FROM whynoipv6_ro';
  EXECUTE 'REVOKE ALL ON SCHEMA public FROM whynoipv6_ro';
  EXECUTE format('REVOKE ALL ON DATABASE %I FROM whynoipv6_ro', current_database());

  -- DROP ROLE fails if the role still owns or is granted objects in another
  -- database in this cluster; leaving it privilege-stripped is the safe
  -- outcome there, so the failure is swallowed rather than blocking the
  -- rollback.
  BEGIN
    EXECUTE 'DROP ROLE whynoipv6_ro';
  EXCEPTION WHEN dependent_objects_still_exist OR insufficient_privilege THEN
    RAISE NOTICE 'whynoipv6_ro kept: still referenced elsewhere in the cluster, privileges revoked';
  END;
END
$$;
