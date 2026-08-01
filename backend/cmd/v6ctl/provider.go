package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/db/seed"
	"github.com/lasseh/whynoipv6/internal/ingest"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

func providerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "DNS-provider (ns_host → provider) mapping maintenance",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "add <name> <suffix> [<suffix>...]",
		Short: "Upsert a provider and append nameserver-host suffixes",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			if err := ingest.ProviderAdd(cmd.Context(), db.New(pool), args[0], args[1:]); err != nil {
				return err
			}
			fmt.Printf("provider %s: added %d suffix(es)\n", args[0], len(args)-1)
			return nil
		},
	})

	var seedPath string
	seedCmd := &cobra.Command{
		Use:   "seed",
		Short: "Upsert providers from the curated seed YAML (idempotent)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Embedded curated list by default; --path beats the registry key,
			// which beats the embed — so a stock install seeds with no wiring.
			path := seedPath
			if path == "" {
				path = cfgFromCmd(cmd).String("dns_provider.seed_path")
			}
			raw, src := seed.DNSProviders, "embedded seed"
			if path != "" {
				b, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("provider seed: %w", err)
				}
				raw, src = b, path
			}
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			n, err := ingest.SeedProviders(cmd.Context(), pool, raw)
			if err != nil {
				return err
			}
			fmt.Printf("provider seed: %d provider(s) upserted from %s\n", n, src)
			return nil
		},
	}
	seedCmd.Flags().StringVar(&seedPath, "path", "", "override seed YAML path (default: dns_provider.seed_path, else the embedded list)")
	cmd.AddCommand(seedCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "remove <name>",
		Short: "Delete a provider (stamped domains self-heal on the next scan commit)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			q := db.New(pool)
			_, err = q.ProviderClearDomains(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			n, err := q.ProviderDelete(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if n == 0 {
				fmt.Println("no such provider")
				return nil
			}
			fmt.Println("removed; stamped domains recompute on their next scan commit")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List providers with suffix sets and mapped-domain counts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			q := db.New(pool)
			rows, err := q.ProviderList(cmd.Context())
			if err != nil {
				return err
			}
			for i := range rows {
				r := &rows[i]
				id := r.ID
				n, err := q.ProviderDomainCount(cmd.Context(), &id)
				if err != nil {
					return err
				}
				fmt.Printf("%s\t%d domains\t%s\n", r.Name, n, strings.Join(r.NsSuffixes, ","))
			}
			return nil
		},
	})
	return cmd
}
