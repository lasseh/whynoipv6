package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

	var (
		before string
		dryRun bool
	)
	pruneCmd := &cobra.Command{
		Use:   "prune --before <RFC3339>",
		Short: "Delete discovered links no scan has seen since a cutoff (manual links are kept)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cutoff, err := parseCutoff(before, time.Now())
			if err != nil {
				return err
			}
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			return resourcePrune(cmd.Context(), pool, os.Stdout, cutoff, dryRun)
		},
	}
	pruneCmd.Flags().StringVar(&before, "before", "", "delete links last seen before this RFC 3339 time, with its zone")
	pruneCmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would go without deleting anything")
	_ = pruneCmd.MarkFlagRequired("before")
	cmd.AddCommand(pruneCmd)
	return cmd
}

// parseCutoff insists on a zone and on the past. A cutoff at or after now
// matches every discovered link in the table, including ones a scan refreshed
// a second ago.
func parseCutoff(s string, now time.Time) (time.Time, error) {
	cutoff, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("--before: %w", err)
	}
	if !cutoff.Before(now) {
		return time.Time{}, fmt.Errorf("--before %s is not in the past: it would delete every discovered link", s)
	}
	return cutoff, nil
}

// resourcePrune is `v6ctl resource prune` (06 §5.5). It takes no advisory lock:
// the writer it races is the per-domain commit, which holds none, and the one
// lock it could share, JobDailyTick, would make the 03:30 coordinators skip the
// day's tick while a prune ran. Each host is one statement that re-tests
// last_seen and subtracts what it deleted, so a stop partway leaves every
// counter exact and a re-run finishes the job.
func resourcePrune(ctx context.Context, pool *pgxpool.Pool, w io.Writer, cutoff time.Time, dryRun bool) error {
	q := db.New(pool)
	ts := postgres.TS(cutoff)
	hosts, err := q.ResourcePruneCandidates(ctx, ts)
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		_, err := fmt.Fprintln(w, "nothing to prune")
		return err
	}
	var total int32
	for _, h := range hosts {
		links := h.Links
		if !dryRun {
			links, err = q.ResourcePruneHost(ctx, db.ResourcePruneHostParams{ResourceHostID: h.ID, Cutoff: ts})
			if err != nil {
				return fmt.Errorf("prune %s: %w", h.Host, err)
			}
		}
		total += links
		// A broken pipe must not stop a prune halfway through the hosts.
		_, _ = fmt.Fprintf(w, "%8d  %s\n", links, h.Host)
	}
	_, _ = fmt.Fprintf(w, "%d links across %d hosts\n", total, len(hosts))
	if dryRun {
		_, _ = fmt.Fprintln(w, "dry run: nothing written")
	}
	return nil
}
