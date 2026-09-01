// Command v6ctl is the operator CLI of the whynoipv6 backend.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/config"
)

type ctxKey struct{}

// cfgFromCmd returns the config loaded by the root PersistentPreRunE.
func cfgFromCmd(cmd *cobra.Command) *config.Config {
	return cmd.Context().Value(ctxKey{}).(*config.Config)
}

// newPool opens the pgx pool from the config loaded by the root PersistentPreRunE.
func newPool(cmd *cobra.Command) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(cmd.Context(), cfgFromCmd(cmd).DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return pool, nil
}

// singletonWait is the blocking advisory-lock wait for operator-triggered
// singleton runs (`tranco import`, `campaign sync`): hardcoded, no config
// key (04-lifecycle-scheduling.md §10).
const singletonWait = 5 * time.Minute

// underOps reports whether cmd sits in the `ops` command subtree. Those are
// timer-driven scrapes (unbound-stats fires every minute), so their config
// summary logs at debug — at info it would ship a full config dump to the
// log sink every firing (09-ops.md §13).
func underOps(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "ops" {
			return true
		}
	}
	return false
}

func main() {
	flushLogs := func() {}
	root := &cobra.Command{
		Use:           "v6ctl",
		Short:         "WhyNoIPv6 operator CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load("v6ctl")
			if err != nil {
				return err
			}
			log, flush, err := cfg.InstallLogger()
			if err != nil {
				return err
			}
			flushLogs = flush
			level := slog.LevelInfo
			if underOps(cmd) {
				level = slog.LevelDebug
			}
			cfg.LogSummary(log, level)
			cmd.SetContext(context.WithValue(cmd.Context(), ctxKey{}, cfg))
			return nil
		},
	}
	root.AddCommand(migrateCmd())
	root.AddCommand(geoipCmd())
	root.AddCommand(trancoCmd())
	root.AddCommand(campaignCmd())
	root.AddCommand(providerCmd())
	root.AddCommand(serviceCandidatesCmd())
	root.AddCommand(disableCmd())
	root.AddCommand(enableCmd())
	root.AddCommand(statsCmd())
	root.AddCommand(opsCmd())
	root.AddCommand(logsCmd())
	root.AddCommand(shameCmd())
	root.AddCommand(exportCmd())
	root.AddCommand(resourceCmd())

	// SIGINT/SIGTERM cancel the command context so long-running commands
	// (tranco import, campaign sync, export) unwind cleanly, mirroring
	// cmd/api; the Taillight drain below still runs.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := root.ExecuteContext(ctx)
	stop()
	flushLogs() // drain the Taillight shipper regardless of command outcome
	if err != nil {
		fmt.Fprintln(os.Stderr, "v6ctl: "+err.Error())
		os.Exit(1)
	}
}
