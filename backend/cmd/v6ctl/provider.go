package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

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
			pool, _, err := newPool(cmd)
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

	cmd.AddCommand(&cobra.Command{
		Use:   "remove <name>",
		Short: "Delete a provider (stamped domains self-heal on the next scan commit)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, _, err := newPool(cmd)
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
			pool, _, err := newPool(cmd)
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
