package migrations

import (
	"errors"
	"net/url"
	"strings"
)

// DriverURL rewrites the application's DATABASE_URL into the golang-migrate
// pgx/v5 driver URL: the scheme becomes pgx5:// and the pgxpool-only
// `pool_*` query keys are dropped. Only pgxpool.ParseConfig strips those;
// the migrate driver opens through database/sql and the pgx stdlib, which
// forward every unknown key as a server runtime parameter — with the
// documented production DSN (`?pool_max_conns=32`, 09-ops §2.1) the
// server answers `FATAL: unrecognized configuration parameter
// "pool_max_conns"`. The migration path needs no pool sizing.
func DriverURL(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "postgres", "postgresql", "pgx5":
	default:
		return "", errors.New("DATABASE_URL must be a postgres:// URL for migrations")
	}
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(k, "pool_") {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	u.Scheme = "pgx5"
	return u.String(), nil
}
