package main

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/domain"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// shameCmd is the single top_shame write path (06-ingest.md §7): editorial
// picks, no changelog entries, visibility computed at read time.
func shameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shame",
		Short: "Curated editorial shame list (top_shame)",
	}

	var reason string
	addCmd := &cobra.Command{
		Use:   "add <host>",
		Short: "Add (or update the reason of) an editorial pick",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host, err := domain.Canonicalize(args[0])
			if err != nil {
				return err
			}
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			q := db.New(pool)

			row, err := q.ShameEligibleDomain(cmd.Context(), host)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%s is not eligible: picks must be an existing, ranked, non-disabled apex", host)
			}
			if err != nil {
				return err
			}
			var reasonPtr *string
			if reason != "" {
				reasonPtr = &reason
			}
			if err := q.ShameUpsert(cmd.Context(), db.ShameUpsertParams{DomainID: row.ID, Reason: reasonPtr}); err != nil {
				return err
			}
			if row.Classification != db.ClassificationSinner {
				fmt.Printf("added; will not render on /shame until classified sinner (currently %s)\n", row.Classification)
			} else {
				fmt.Println("added")
			}
			return nil
		},
	}
	addCmd.Flags().StringVar(&reason, "reason", "", "editorial reason (NULL when omitted)")
	cmd.AddCommand(addCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "remove <host>",
		Short: "Remove a pick",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host, err := domain.Canonicalize(args[0])
			if err != nil {
				return err
			}
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			n, err := db.New(pool).ShameRemove(cmd.Context(), host)
			if err != nil {
				return err
			}
			if n == 0 {
				fmt.Println("not on the shame list")
				return nil
			}
			fmt.Println("removed")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all picks with computed visibility",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			rows, err := db.New(pool).ShameList(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Println("host\trank\tclassification\tvisible\treason\tadded_at")
			for i := range rows {
				r := &rows[i]
				reason := ""
				if r.Reason != nil {
					reason = *r.Reason
				}
				rank := fmtPtr(r.Rank)
				fmt.Printf("%s\t%s\t%s\t%t\t%s\t%s\n", r.Host, rank, r.Classification,
					derefBool(r.Visible), reason, r.AddedAt.Time.Format("2006-01-02"))
			}
			return nil
		},
	})
	return cmd
}

func derefBool(b *bool) bool { return b != nil && *b }
