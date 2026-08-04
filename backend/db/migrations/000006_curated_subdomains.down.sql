-- created_by's 'curated' value stays: PostgreSQL has no ALTER TYPE ... DROP
-- VALUE. Harmless — no row can carry it once the ingress is gone, and 000001's
-- down drops the type outright.
DROP TABLE IF EXISTS curated_subdomain;
