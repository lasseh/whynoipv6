//go:build integration

// Package pgtest is the shared integration-test harness (10-testing.md §9.1):
// one Postgres server for the whole test run, a template database migrated
// once, and one clone of it per test so tests never share mutable state.
// All files are integration-tagged — nothing here ships in the binaries.
//
// The server comes from PGTEST_DSN, which `make test-integration` points at a
// single container it owns. Without it each test binary boots a throwaway
// container of its own — fine for one package, but `go test ./...` would then
// run eight Postgres servers at once, which is what made CI flaky.
package pgtest

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // pgx5:// driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/lasseh/whynoipv6/db/migrations"
)

const (
	// dsnEnv names the server to test against. Set by `make test-integration`.
	dsnEnv = "PGTEST_DSN"

	// templatePrefix names the migrated database every test clones. The
	// embedded migration version is part of the name (see templateDB), so
	// "the template exists" also means "at this version": a checkout that
	// adds a migration gets a fresh template instead of silently cloning a
	// stale schema. Nothing ever connects to it — CREATE DATABASE ...
	// TEMPLATE fails while a session is attached to the source, so all admin
	// work runs against "postgres", and reading the version out of the
	// template is exactly what the name avoids.
	templatePrefix = "whynoipv6_template_v"
	// buildDB is where the template is migrated before being renamed into
	// place, so "templateDB exists" always means "templateDB is fully
	// migrated" — an interrupted build leaves no half-migrated template.
	buildDB = "whynoipv6_template_build"

	// templateLock serializes template creation across the test binaries that
	// `go test ./...` runs in parallel against the shared server. Advisory
	// locks are scoped to the database holding them, so this only serializes
	// because every binary takes it on the "postgres" maintenance database.
	templateLock = 6066001

	// objectInUse is 55006, raised when CREATE DATABASE races a DROP of the
	// same template.
	objectInUse = "55006"

	// shmSize matches compose.yaml: docker's 64m default breaks parallel
	// queries over the 1M-row domain table.
	shmSize = 256 << 20
)

var (
	adminDSN string // DSN to the maintenance db ("postgres")
	dbSeq    atomic.Int64
	runTag   = fmt.Sprintf("%d", os.Getpid()) // clone names are unique per binary
)

// Main is the TestMain body: it resolves the server, migrates the template
// database if no other binary has yet, and runs the tests. Callers:
//
//	func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }
func Main(m *testing.M) int {
	ctx := context.Background()

	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		ctr, err := startContainer(ctx)
		if err != nil {
			log.Printf("start timescaledb container: %v", err)
			return 1
		}
		defer func() { _ = testcontainers.TerminateContainer(ctr) }()
		if dsn, err = ctr.ConnectionString(ctx, "sslmode=disable"); err != nil {
			log.Printf("container dsn: %v", err)
			return 1
		}
	}
	adminDSN = replaceDBName(dsn, "postgres")

	if err := ensureTemplate(ctx); err != nil {
		log.Printf("prepare template: %v", err)
		return 1
	}
	return m.Run()
}

// startContainer boots the fallback server used when PGTEST_DSN is unset.
// Settings are kept in step with the testdb-up target in backend/Makefile.
func startContainer(ctx context.Context) (*tcpostgres.PostgresContainer, error) {
	return tcpostgres.Run(ctx, "timescale/timescaledb:latest-pg18",
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("integration"),
		// No TimescaleDB background workers in tests: the job scheduler
		// holds a connection per database with jobs, which blocks
		// CREATE DATABASE ... TEMPLATE cloning.
		testcontainers.WithCmdArgs(
			"-c", "timescaledb.max_background_workers=0",
			"-c", "max_connections=200",
			"-c", "fsync=off",
			"-c", "synchronous_commit=off",
			"-c", "full_page_writes=off",
		),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
			hc.ShmSize = shmSize
		}),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(3*time.Minute)),
	)
}

// EmbeddedVersion is the highest migration number in db/migrations —
// golang-migrate's version after a clean Up, and the version every clone of
// the template must report.
func EmbeddedVersion() int {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		panic("pgtest: read embedded migrations: " + err.Error())
	}
	highest := 0
	for _, e := range entries {
		digits := e.Name()[:strings.IndexFunc(e.Name(), func(r rune) bool { return r < '0' || r > '9' })]
		n, err := strconv.Atoi(digits)
		if err != nil {
			panic("pgtest: unnumbered migration " + e.Name())
		}
		highest = max(highest, n)
	}
	return highest
}

// templateDB is templatePrefix plus the embedded migration version.
func templateDB() string { return templatePrefix + strconv.Itoa(EmbeddedVersion()) }

// dropStaleTemplates removes templates built from a different migration set.
// Called under templateLock, so no other binary is mid-build; a clone still
// running off a stale template belongs to a tree that no longer exists.
func dropStaleTemplates(ctx context.Context, admin *pgx.Conn) error {
	rows, err := admin.Query(ctx,
		"SELECT datname FROM pg_database WHERE datname LIKE $1 AND datname <> $2",
		templatePrefix+"%", templateDB())
	if err != nil {
		return fmt.Errorf("list templates: %w", err)
	}
	var stale []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		stale = append(stale, name)
	}
	rows.Close()
	for _, name := range stale {
		log.Printf("pgtest: dropping stale template %s (migrations now at %d)", name, EmbeddedVersion())
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			return fmt.Errorf("drop stale template %s: %w", name, err)
		}
	}
	return nil
}

// ensureTemplate creates and migrates templateDB unless it is already there.
// Every test binary calls this against the same server, so the work happens
// once behind templateLock and the result is only published under the template
// name after the migrations succeed.
func ensureTemplate(ctx context.Context) error {
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return fmt.Errorf("connect admin: %w", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	if _, err := admin.Exec(ctx, "SELECT pg_advisory_lock($1)", templateLock); err != nil {
		return fmt.Errorf("lock template: %w", err)
	}
	defer func() {
		_, _ = admin.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", templateLock)
	}()

	if err := dropStaleTemplates(ctx, admin); err != nil {
		return err
	}

	var exists bool
	if err := admin.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", templateDB()).Scan(&exists); err != nil {
		return fmt.Errorf("look up template: %w", err)
	}
	if exists {
		return nil
	}

	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+buildDB+" WITH (FORCE)"); err != nil {
		return fmt.Errorf("drop stale build database: %w", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+buildDB); err != nil {
		return fmt.Errorf("create build database: %w", err)
	}
	if err := MigrateUp(replaceDBName(adminDSN, buildDB)); err != nil {
		return err
	}
	if _, err := admin.Exec(ctx, "ALTER DATABASE "+buildDB+" RENAME TO "+templateDB()); err != nil {
		return fmt.Errorf("publish template: %w", err)
	}
	return nil
}

// MigrateUp applies every embedded migration to dsn.
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

// NewMigrator builds a golang-migrate instance over the embedded SQL files,
// through the same DSN rewrite v6ctl uses.
func NewMigrator(dsn string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return nil, err
	}
	pgx5, err := migrations.DriverURL(dsn)
	if err != nil {
		return nil, err
	}
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
	name := fmt.Sprintf("t_%s_%d", runTag, dbSeq.Add(1))

	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	// Concurrent clones of one template are fine — CREATE DATABASE takes a
	// share lock on the source — but a clone racing another binary's DROP
	// comes back as 55006, so retry briefly before failing the test.
	for attempt := range 5 {
		if _, err = admin.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE "+templateDB()); err == nil {
			break
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != objectInUse {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("clone template: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(replaceDBName(adminDSN, name))
	if err != nil {
		t.Fatalf("parse test dsn: %v", err)
	}
	// One server backs every package `go test ./...` runs in parallel, so keep
	// each test's pool well inside the server's max_connections.
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("test pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		admin, err := pgx.Connect(ctx, adminDSN)
		if err != nil {
			return
		}
		defer func() { _ = admin.Close(ctx) }()
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return pool
}
