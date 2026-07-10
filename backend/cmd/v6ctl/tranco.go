package main

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/config"
	"github.com/lasseh/whynoipv6/internal/ingest"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

func newPool(cmd *cobra.Command) (*pgxpool.Pool, *config.Config, error) {
	cfg := cfgFromCmd(cmd)
	pool, err := pgxpool.New(cmd.Context(), cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}
	return pool, cfg, nil
}

func trancoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tranco",
		Short: "Tranco top-1M list import",
	}

	var force bool
	importCmd := &cobra.Command{
		Use:   "import",
		Short: "Run one Tranco import attempt (break-glass; the crawler coordinator is the scheduled trigger)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pool, cfg, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			imp := ingest.NewTrancoImporter(pool, ingest.NewHTTPTrancoSource(), ingest.TrancoConfig{
				MinRows:      cfg.Int("tranco.min_rows"),
				MaxDelistPct: cfg.Float("tranco.max_delist_pct"),
			})
			rep, err := imp.Import(cmd.Context(), force)
			if err != nil {
				return err
			}
			fmt.Printf("outcome: %s list_id: %s lines: %d imported: %d delisted: %d rejected: %d duplicates: %d\n",
				rep.Outcome, rep.ListID, rep.LineCount, rep.ImportedCount, rep.Delisted,
				rep.RejectedCount, rep.DuplicateCount)
			if rep.Outcome == ingest.TrancoAborted {
				return fmt.Errorf("import aborted: %s", rep.Note)
			}
			return nil
		},
	}
	importCmd.Flags().BoolVar(&force, "force", false, "bypass the aborted-list short-circuit and the sanity guard")
	cmd.AddCommand(importCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show the 10 most recent tranco_import rows and staleness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pool, cfg, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			q := db.New(pool)
			rows, err := q.TrancoRecentImports(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Println("list_id\tlist_date\timported_at\taborted\tlines\timported\tdelisted\trejected\tduplicates\tnote")
			for i := range rows {
				r := &rows[i]
				note := ""
				if r.Note != nil {
					note = *r.Note
				}
				fmt.Printf("%s\t%s\t%s\t%t\t%s\t%s\t%s\t%s\t%s\t%s\n",
					r.ListID, r.ListDate.Time.Format("2006-01-02"),
					r.ImportedAt.Time.Format(time.RFC3339), r.Aborted,
					fmtPtr(r.LineCount), fmtPtr(r.ImportedCount), fmtPtr(r.Delisted),
					fmtPtr(r.RejectedCount), fmtPtr(r.DuplicateCount), note)
			}
			last, err := q.TrancoLastSuccessAt(cmd.Context())
			if err == nil && last.Valid {
				age := time.Since(last.Time)
				fmt.Printf("hours since last successful import: %.1f (warn threshold %s)\n",
					age.Hours(), cfg.Duration("tranco.stale_warn_after"))
			} else {
				fmt.Println("no successful import recorded yet")
			}
			return nil
		},
	})
	return cmd
}

func fmtPtr(v *int32) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *v)
}
