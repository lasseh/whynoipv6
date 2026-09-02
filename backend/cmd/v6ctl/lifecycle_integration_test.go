//go:build integration

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/config"
	"github.com/lasseh/whynoipv6/internal/lock"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

// TestStatsRecalcTakesTheTickLock is review issue 25: 04 §10 lists
// `stats recalc` among the lock.Run-gated verbs, and it ran unlocked.
//
// The test holds JobDailyTick on a second connection and gives the command
// a context that expires almost immediately. Locked, the recalc never
// starts — it is still waiting when the deadline passes, and today's
// snapshot row is absent. Unlocked it runs straight through the tick's own
// rollup and writes the row.
func TestStatsRecalcTakesTheTickLock(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	// The command builds its own pool from DATABASE_URL.
	t.Setenv("DATABASE_URL", pool.Config().ConnString())

	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = lock.TryRun(ctx, pool, lock.JobDailyTick, func(context.Context) error {
			close(held)
			<-release
			return nil
		})
	}()
	select {
	case <-held:
	case <-time.After(10 * time.Second):
		t.Fatal("could not take the tick lock")
	}
	defer func() {
		close(release)
		<-done
	}()

	// Clear the table so "a row exists" means "the recalc ran".
	if _, err := pool.Exec(ctx, `DELETE FROM stats_global_daily`); err != nil {
		t.Fatal(err)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := runV6ctl(t, cmdCtx, "stats", "recalc"); err == nil {
		t.Error("stats recalc succeeded while the daily tick held its lock")
	}

	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM stats_global_daily`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("the snapshot was written (%d rows): the recalc ran without waiting for the lock", rows)
	}
}

// runV6ctl executes one command through a root wired like main()'s, minus
// the logger and signal plumbing.
func runV6ctl(t *testing.T, ctx context.Context, args ...string) error {
	t.Helper()
	root := &cobra.Command{
		Use:           "v6ctl",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load("v6ctl")
			if err != nil {
				return err
			}
			cmd.SetContext(context.WithValue(cmd.Context(), ctxKey{}, cfg))
			return nil
		},
	}
	root.AddCommand(statsCmd())
	root.SetArgs(args)
	root.SetOut(os.Stderr)
	return root.ExecuteContext(ctx)
}
