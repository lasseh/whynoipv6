// Command v6ctl is the operator CLI of the whynoipv6 backend.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/config"
)

type ctxKey struct{}

// cfgFromCmd returns the config loaded by the root PersistentPreRunE.
func cfgFromCmd(cmd *cobra.Command) *config.Config {
	return cmd.Context().Value(ctxKey{}).(*config.Config)
}

func main() {
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
			log := cfg.InstallLogger()
			cfg.LogSummary(log)
			cmd.SetContext(context.WithValue(cmd.Context(), ctxKey{}, cfg))
			return nil
		},
	}
	root.AddCommand(migrateCmd())
	root.AddCommand(trancoCmd())
	root.AddCommand(campaignCmd())
	root.AddCommand(providerCmd())
	root.AddCommand(serviceCandidatesCmd())
	root.AddCommand(disableCmd())
	root.AddCommand(enableCmd())
	root.AddCommand(statsCmd())
	root.AddCommand(opsCmd())
	root.AddCommand(shameCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "v6ctl: "+err.Error())
		os.Exit(1)
	}
}
