package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// InTx runs fn inside one transaction: begin, hand fn a *db.Queries bound to
// the tx, then commit — or roll back if fn returns an error or panics. It is
// the same higher-order shape as lock.TryRun, and it exists so "either every
// write or none" is a property of the call rather than a sequence each caller
// remembers to spell out.
//
// The daemon paths (sweep, tick, ingest, commit flush, campaign sync, the
// live-check store) hand-roll begin/rollback/commit because each needs the tx
// handle for its own reasons; this is for the callers whose whole need is the
// atomicity.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(q *db.Queries) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Same shape as the daemon paths: a rollback after a successful commit
	// is a no-op, and this one also covers an early return or a panic.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(db.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
