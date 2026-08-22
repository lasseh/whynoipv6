package main

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// resourceCmd implements the §5.5 manual link verbs — the only writers of
// manual domain_resource links; there is no HTTP admin surface.
func resourceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resource",
		Short: "Manual resource-dependency links (domain_resource)",
	}

	var advisory bool
	addCmd := &cobra.Command{
		Use:   "add <domain> <host>",
		Short: "Link a resource host to a domain (upgrades a discovered link to manual)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dHost, err := domain.Canonicalize(args[0])
			if err != nil {
				return err
			}
			rHost, err := domain.Canonicalize(args[1])
			if err != nil {
				return err
			}
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			q := db.New(pool)

			d, err := q.DomainByHost(cmd.Context(), dHost)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("unknown domain %s — add it to a campaign or wait for Tranco", dHost)
			}
			if err != nil {
				return err
			}
			if d.Disabled {
				fmt.Printf("warning: %s is disabled; the link is inert until re-enable\n", dHost)
			}

			// Ensure the registry row (statement-A pattern), then the
			// manual upsert + conditional dependent_count bump (06 §5.5).
			// All three in one transaction: a failure partway used to
			// leave an orphan resource_host with no link pointing at it.
			if err := postgres.InTx(cmd.Context(), pool, func(q *db.Queries) error {
				if err := q.EnsureResourceHost(cmd.Context(), rHost); err != nil {
					return err
				}
				rhID, err := q.ResourceHostIDByHost(cmd.Context(), rHost)
				if err != nil {
					return err
				}
				return q.ResourceManualUpsert(cmd.Context(), db.ResourceManualUpsertParams{
					DomainID: d.ID, ResourceHostID: rhID, Required: !advisory,
				})
			}); err != nil {
				return err
			}
			fmt.Println("linked")
			return nil
		},
	}
	addCmd.Flags().BoolVar(&advisory, "advisory", false,
		"required=FALSE: visible on the detail API, excluded from the Saint roll-up")
	cmd.AddCommand(addCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "remove <domain> <host>",
		Short: "Delete a link (the only way to remove a manual one)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dHost, err := domain.Canonicalize(args[0])
			if err != nil {
				return err
			}
			rHost, err := domain.Canonicalize(args[1])
			if err != nil {
				return err
			}
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			q := db.New(pool)

			d, err := q.DomainByHost(cmd.Context(), dHost)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("unknown domain %s", dHost)
			}
			if err != nil {
				return err
			}
			rhID, err := q.ResourceHostIDByHost(cmd.Context(), rHost)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("unknown resource host %s", rHost)
			}
			if err != nil {
				return err
			}

			n, err := q.ResourceManualRemove(cmd.Context(), db.ResourceManualRemoveParams{
				DomainID: d.ID, ResourceHostID: rhID,
			})
			if err != nil {
				return err
			}
			if n == 0 {
				fmt.Println("no such link")
				return nil
			}
			fmt.Println("removed")
			return nil
		},
	})
	return cmd
}
