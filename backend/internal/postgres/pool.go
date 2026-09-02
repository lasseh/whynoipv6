package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool opens the application pool for DATABASE_URL. Pool sizing rides
// in the DSN (09-ops §2.1); the one runtime parameter set here is the
// session time zone, pinned to UTC on every connection. The daily
// snapshots key on CURRENT_DATE and the stats readers compare against UTC
// midnight, so a server whose initdb inherited a local zone would write
// the previous date and skew every day-keyed surface (and the ETag
// generation) by a day. Pinning it per session makes the deployment's
// timezone GUC irrelevant.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	return pool, nil
}
