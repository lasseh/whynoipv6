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
func NewPool(ctx context.Context, dsn string, params ...RuntimeParam) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	for _, p := range params {
		cfg.ConnConfig.RuntimeParams[p.Name] = p.Value
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	return pool, nil
}

// RuntimeParam is a per-session GUC a binary sets on its own pool.
type RuntimeParam struct{ Name, Value string }

// APIStatementTimeout bounds any single statement the read API issues
// (09-ops §7 erratum, review issue 44). A two-character `?q=` yields no
// trigram and terms like "com" match nearly every row, so a search is a
// scan-and-sort over ~1M rows; without this a runaway one occupies a
// backend for the full 30 s request timeout while every other origin miss
// queues behind it.
//
// Set on the API's pool, deliberately NOT on the database role: the
// crawler shares that role, and its claim batches and stats rollups run
// far longer than 5 s by design.
var APIStatementTimeout = RuntimeParam{Name: "statement_timeout", Value: "5s"}
