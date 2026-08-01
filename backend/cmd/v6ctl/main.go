// Command v6ctl is the operator CLI of the whynoipv6 backend.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
			cfg.LogSummary(log)
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
