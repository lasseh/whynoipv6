package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/crawler"
	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// serviceCandidatesCmd triages auto-detected service domains
// (04-lifecycle-scheduling.md — service lifecycle).
func serviceCandidatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service-candidates",
		Short: "Triage detected service-domain candidates",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List open (undismissed) candidates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			rows, err := db.New(pool).ServiceCandidateList(cmd.Context())
			if err != nil {
				return err
			}
			for i := range rows {
				r := &rows[i]
				fmt.Printf("%s\t%s\t%s\n", r.Host, strings.Join(r.Reasons, ","),
					r.DetectedAt.Time.Format("2006-01-02"))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "confirm <host>",
		Short: "Confirm: disable the domain with disabled_reason='service' (leaves the frontier)",
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
			// Both writes or neither: a failure between them used to
			// leave the domain disabled with its candidate still open,
			// and the recovery was prose for the operator to act on.
			if err := postgres.InTx(cmd.Context(), pool, func(q *db.Queries) error {
				reason := db.DisabledReasonService
				n, err := q.DomainDisable(cmd.Context(), db.DomainDisableParams{Host: host, Reason: &reason})
				if err != nil {
					return err
				}
				if n == 0 {
					return fmt.Errorf("%s not found or already disabled", host)
				}
				_, err = q.ServiceCandidateResolve(cmd.Context(), host)
				return err
			}); err != nil {
				return err
			}
			fmt.Printf("%s disabled (service); it leaves the frontier\n", host)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "dismiss <host>",
		Short: "Dismiss: keep the domain untouched; the candidate is never re-flagged",
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
			n, err := db.New(pool).ServiceCandidateResolve(cmd.Context(), host)
			if err != nil {
				return err
			}
			if n == 0 {
				fmt.Println("no open candidate for", host)
				return nil
			}
			fmt.Printf("%s dismissed (domain untouched)\n", host)
			return nil
		},
	})
	return cmd
}

func disableCmd() *cobra.Command {
	var serviceList string
	cmd := &cobra.Command{
		Use:   "disable [<host>]",
		Short: "Operator disable: manual for one host, or --service-list for a curated batch",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			q := db.New(pool)

			if serviceList != "" {
				f, err := os.Open(serviceList)
				if err != nil {
					return err
				}
				defer func() { _ = f.Close() }()
				reason := db.DisabledReasonService
				disabled, skipped := 0, 0
				sc := bufio.NewScanner(f)
				for sc.Scan() {
					line := strings.TrimSpace(sc.Text())
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					host, err := domain.Canonicalize(line)
					if err != nil {
						fmt.Printf("skipped %q: %v\n", line, err)
						skipped++
						continue
					}
					n, err := q.DomainDisable(cmd.Context(), db.DomainDisableParams{Host: host, Reason: &reason})
					if err != nil {
						return err
					}
					if n > 0 {
						disabled++
					} else {
						skipped++
					}
				}
				if err := sc.Err(); err != nil {
					return err
				}
				fmt.Printf("service list applied: %d disabled, %d skipped\n", disabled, skipped)
				return nil
			}

			if len(args) != 1 {
				return fmt.Errorf("a host argument or --service-list is required")
			}
			host, err := domain.Canonicalize(args[0])
			if err != nil {
				return err
			}
			reason := db.DisabledReasonManual
			n, err := q.DomainDisable(cmd.Context(), db.DomainDisableParams{Host: host, Reason: &reason})
			if err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("%s not found or already disabled", host)
			}
			fmt.Printf("%s disabled (manual)\n", host)
			return nil
		},
	}
	cmd.Flags().StringVar(&serviceList, "service-list", "",
		"path to a curated host list; each host is disabled with disabled_reason='service'")
	return cmd
}

func enableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <host>",
		Short: "Re-enable a manual/service-disabled host (immediate rescan)",
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
			n, err := db.New(pool).DomainEnable(cmd.Context(), host)
			if err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("%s not found, not disabled, or not manual/service-disabled", host)
			}
			fmt.Printf("%s re-enabled\n", host)
			return nil
		},
	}
}

// statsCmd re-runs the daily stats rollup on demand (06-ingest.md §10.7):
// idempotent, and deliberately without JobDailyTick.
func statsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Stats snapshot maintenance",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "recalc",
		Short: "Re-run today's stats snapshots + counter recomputes (idempotent; no tick lock)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			if err := crawler.RunStatsRollup(cmd.Context(), pool); err != nil {
				return err
			}
			fmt.Println("stats recalculated for today")
			return nil
		},
	})
	return cmd
}
