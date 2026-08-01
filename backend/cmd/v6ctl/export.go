package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/export"
	"github.com/lasseh/whynoipv6/internal/lock"
	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// exportCmd runs the nightly dataset snapshot export (07 §5.3), serialized
// by the dataset-export advisory lock.
func exportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Export the daily static dataset snapshot (tiers × CSV.gz + Parquet)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := cfgFromCmd(cmd)
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()

			err = lock.TryRun(cmd.Context(), pool, lock.JobDatasetExport, func(ctx context.Context) error {
				generation, _, err := postgres.Generation(ctx, db.New(pool))
				if err != nil {
					return fmt.Errorf("generation: %w", err)
				}
				e := &export.Exporter{Pool: pool, Dir: cfg.DatasetsDir,
					RetentionDays: cfg.Int("datasets.retention_days")}
				if err := e.Run(ctx, generation); err != nil {
					return err
				}
				fmt.Printf("exported snapshot to %s (generation %d)\n", cfg.DatasetsDir, generation)
				return nil
			})
			if errors.Is(err, lock.ErrHeld) {
				return errors.New("another dataset export is running")
			}
			return err
		},
	}
}
