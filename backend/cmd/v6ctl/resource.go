package main

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/domain"
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
			pool, _, err := newPool(cmd)
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
			if err := q.EnsureResourceHost(cmd.Context(), rHost); err != nil {
				return err
			}
			var rhID int64
			if err := pool.QueryRow(cmd.Context(),
				"SELECT id FROM resource_host WHERE host = $1", rHost).Scan(&rhID); err != nil {
				return err
			}
			_, err = pool.Exec(cmd.Context(), `
				WITH up AS (
				  INSERT INTO domain_resource (domain_id, resource_host_id, source, required)
				  VALUES ($1, $2, 'manual', $3)
				  ON CONFLICT (domain_id, resource_host_id)
				  DO UPDATE SET source = 'manual', required = EXCLUDED.required
				  RETURNING (xmax = 0) AS inserted
				)
				UPDATE resource_host SET dependent_count = dependent_count + 1
				WHERE id = $2 AND (SELECT inserted FROM up)`,
				d.ID, rhID, !advisory)
			if err != nil {
				return err
			}
			fmt.Println("linked")
			return nil
		},
	}
	addCmd.Flags().BoolVar(&advisory, "advisory", false,
		"required=FALSE: visible on the detail API, excluded from the Gold roll-up")
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
			pool, _, err := newPool(cmd)
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
			var rhID int64
			err = pool.QueryRow(cmd.Context(),
				"SELECT id FROM resource_host WHERE host = $1", rHost).Scan(&rhID)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("unknown resource host %s", rHost)
			}
			if err != nil {
				return err
			}

			tag, err := pool.Exec(cmd.Context(), `
				WITH del AS (
				  DELETE FROM domain_resource
				  WHERE domain_id = $1 AND resource_host_id = $2
				  RETURNING resource_host_id
				)
				UPDATE resource_host SET dependent_count = dependent_count - 1
				WHERE id IN (SELECT resource_host_id FROM del)`, d.ID, rhID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				fmt.Println("no such link")
				return nil
			}
			fmt.Println("removed")
			return nil
		},
	})
	return cmd
}
