//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestPoolRuntimeParams (review issue 44): the API's statement_timeout is a
// per-pool runtime parameter, not a role setting — the crawler shares the
// role and its claim batches and stats rollups run far longer than 5s.
//
// timezone is asserted alongside it because both travel the same way, and
// UTC is what keeps the day-keyed snapshots honest (09-ops §2.1).
func TestPoolRuntimeParams(t *testing.T) {
	dsn := pgtest.NewDB(t).Config().ConnString()
	ctx := context.Background()

	t.Run("timezone is pinned without any parameter", func(t *testing.T) {
		pool, err := NewPool(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		var tz, timeout string
		if err := pool.QueryRow(ctx, "SHOW timezone").Scan(&tz); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, "SHOW statement_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if tz != "UTC" {
			t.Errorf("timezone = %q, want UTC", tz)
		}
		// The crawler opens its pool this way and must NOT inherit a 5s cap.
		if timeout == "5s" {
			t.Error("statement_timeout leaked onto a pool that did not ask for it")
		}
	})

	t.Run("the API pool caps statements at 5s", func(t *testing.T) {
		pool, err := NewPool(ctx, dsn, APIStatementTimeout)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		var timeout string
		if err := pool.QueryRow(ctx, "SHOW statement_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if timeout != "5s" {
			t.Errorf("statement_timeout = %q, want 5s", timeout)
		}

		// And it actually fires: a statement past the cap is cancelled by
		// the server, not left running until the request timeout.
		start := time.Now()
		_, err = pool.Exec(ctx, "SELECT pg_sleep(8)")
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "57014" { // query_canceled
			t.Fatalf("pg_sleep(8) returned %v, want a 57014 query_canceled", err)
		}
		if elapsed := time.Since(start); elapsed > 7*time.Second {
			t.Errorf("cancelled after %s: the cap did not bound it", elapsed)
		}
	})
}
