//go:build integration

// Package pgtest is the shared integration-test harness (10-testing.md §9.1):
// one TimescaleDB container per test binary; Main migrates a template
// database once and each test clones it, so tests never share mutable state.
// All files are integration-tagged — nothing here ships in the binaries.
package pgtest

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // pgx5:// driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/lasseh/whynoipv6/db/migrations"
)

const templateDB = "whynoipv6_template"

var (
	adminDSN string // DSN to the maintenance db ("postgres")
	dbSeq    atomic.Int64
)

// Main is the TestMain body: it boots the container, migrates the template
// database, runs the tests, and terminates the container. Callers:
//
//	func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }
func Main(m *testing.M) int {
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "timescale/timescaledb:latest-pg18",
		tcpostgres.WithDatabase(templateDB),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("integration"),
		// No TimescaleDB background workers in tests: the job scheduler
		// holds a connection per database with jobs, which blocks
		// CREATE DATABASE ... TEMPLATE cloning.
		testcontainers.WithCmdArgs("-c", "timescaledb.max_background_workers=0"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(3*time.Minute)),
	)
	if err != nil {
		log.Printf("start timescaledb container: %v", err)
		return 1
	}
	defer func() { _ = testcontainers.TerminateContainer(ctr) }()

	tmplDSN, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Printf("container dsn: %v", err)
		return 1
	}
	if err := MigrateUp(tmplDSN); err != nil {
		log.Printf("migrate template: %v", err)
		return 1
	}
	adminDSN = replaceDBName(tmplDSN, "postgres")

	return m.Run()
}

// MigrateUp applies the embedded migrations 000001→000003 to dsn.
func MigrateUp(dsn string) error {
	mig, err := NewMigrator(dsn)
	if err != nil {
		return err
	}
	defer func() { _, _ = mig.Close() }()
	if err := mig.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// NewMigrator builds a golang-migrate instance over the embedded SQL files.
func NewMigrator(dsn string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return nil, err
	}
	pgx5 := "pgx5://" + strings.TrimPrefix(dsn, "postgres://")
	return migrate.NewWithSourceInstance("iofs", src, pgx5)
}

func replaceDBName(dsn, name string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		panic(err)
	}
	u.Path = "/" + name
	return u.String()
}

// NewDB clones the migrated template database and returns a pool on the
// clone (10-testing.md §9.1). The clone is dropped on test cleanup.
func NewDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("t_%d", dbSeq.Add(1))

	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, templateDB)); err != nil {
		t.Fatalf("clone template: %v", err)
	}

	pool, err := pgxpool.New(ctx, replaceDBName(adminDSN, name))
	if err != nil {
		t.Fatalf("test pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		admin, err := pgxpool.New(ctx, adminDSN)
		if err != nil {
			return
		}
		defer admin.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return pool
}
