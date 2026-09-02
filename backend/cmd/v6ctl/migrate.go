package main

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // pgx5:// database driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/db/migrations"
	"github.com/lasseh/whynoipv6/internal/config"
)

// newMigrator builds a golang-migrate instance over the embedded SQL files.
// The DATABASE_URL is rewritten to the golang-migrate pgx/v5 driver URL
// (pgx5://, pgxpool-only pool_* keys dropped) — no second config key
// (05-schema.md §2.1).
func newMigrator(cfg *config.Config) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return nil, fmt.Errorf("migrations source: %w", err)
	}
	url, err := migrations.DriverURL(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("migrate init: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, url)
	if err != nil {
		return nil, fmt.Errorf("migrate init: %w", err)
	}
	return m, nil
}

func migrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Manage database schema migrations (forward-only in production)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := newMigrator(cfgFromCmd(cmd))
			if err != nil {
				return err
			}
			defer func() { _, _ = m.Close() }()
			if err := m.Up(); err != nil {
				if errors.Is(err, migrate.ErrNoChange) {
					fmt.Println("already up to date")
					return nil
				}
				return fmt.Errorf("migrate up: %w", err)
			}
			v, dirty, err := m.Version()
			if err != nil {
				return fmt.Errorf("migrate up: version: %w", err)
			}
			fmt.Printf("migrated to version %d dirty: %t\n", v, dirty)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print current migration version and dirty flag",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := newMigrator(cfgFromCmd(cmd))
			if err != nil {
				return err
			}
			defer func() { _, _ = m.Close() }()
			v, dirty, err := m.Version()
			if err != nil {
				if errors.Is(err, migrate.ErrNilVersion) {
					fmt.Println("version: none (no migrations applied)")
					return nil
				}
				return fmt.Errorf("migrate version: %w", err)
			}
			fmt.Printf("version: %d dirty: %t\n", v, dirty)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "force <version>",
		Short: "Set the migration version after manual repair of a dirty state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("force: version must be an integer: %w", err)
			}
			fmt.Println("WARNING: force overrides the recorded schema version without running migrations; operator-only, use after manual repair of a dirty state")
			m, err := newMigrator(cfgFromCmd(cmd))
			if err != nil {
				return err
			}
			defer func() { _, _ = m.Close() }()
			if err := m.Force(v); err != nil {
				return fmt.Errorf("migrate force: %w", err)
			}
			fmt.Printf("forced version %d\n", v)
			return nil
		},
	})

	// There is deliberately no `migrate down` verb: production is
	// forward-only; the down files exist for dev/integration use via the
	// library API (05-schema.md §2.1).
	return cmd
}
