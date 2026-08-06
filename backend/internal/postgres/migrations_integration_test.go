//go:build integration

package postgres

import (
	"context"
	"testing"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestHarnessBoot is the P1.10 smoke test: the container boots, migrations
// 000001→000003 are applied, and one SELECT runs against the migrated schema.
func TestHarnessBoot(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM country").Scan(&n); err != nil {
		t.Fatalf("select: %v", err)
	}
	if n != 251 {
		t.Errorf("country rows = %d, want 251", n)
	}
}

// TestMigrations covers 10-testing.md §9.2: hypertables, policy jobs, seeds,
// and the Round-3.0 constraint negatives.
func TestMigrations(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	var hypertables int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM timescaledb_information.hypertables").Scan(&hypertables); err != nil {
		t.Fatalf("hypertables: %v", err)
	}
	if hypertables != 6 {
		t.Errorf("hypertables = %d, want 6", hypertables)
	}

	var jobs int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM timescaledb_information.jobs
		 WHERE proc_name IN ('policy_compression','policy_retention','policy_refresh_continuous_aggregate')`).
		Scan(&jobs); err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if jobs != 10 {
		t.Errorf("policy jobs = %d, want 10 (5 columnstore + 4 retention + 1 cagg refresh)", jobs)
	}

	var cagg string
	if err := pool.QueryRow(ctx,
		"SELECT view_name FROM timescaledb_information.continuous_aggregates").Scan(&cagg); err != nil {
		t.Fatalf("cagg: %v", err)
	}
	if cagg != "scan_daily_adoption" {
		t.Errorf("cagg = %q, want scan_daily_adoption", cagg)
	}

	var countries, sentinelASN, sentinelCountry, statsRows int
	if err := pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM country),
		        (SELECT count(*) FROM asn WHERE number = 0),
		        (SELECT count(*) FROM country WHERE code = 'UN'),
		        (SELECT count(*) FROM stats_global_daily)`).
		Scan(&countries, &sentinelASN, &sentinelCountry, &statsRows); err != nil {
		t.Fatalf("seeds: %v", err)
	}
	if countries != 251 || sentinelASN != 1 || sentinelCountry != 1 || statsRows != 1 {
		t.Errorf("seeds = %d/%d/%d/%d, want 251/1/1/1", countries, sentinelASN, sentinelCountry, statsRows)
	}

	// Constraint negatives (§9.2 item 5).
	if _, err := pool.Exec(ctx,
		"INSERT INTO changelog (domain_id, field, old_value, new_value) VALUES (1, 'base', NULL, 'supported')"); err == nil {
		t.Error("changelog insert with NULL old_value succeeded, want NOT NULL violation")
	}
	if _, err := pool.Exec(ctx, "SELECT 'import'::created_by"); err == nil {
		t.Error("created_by 'import' accepted, want invalid enum value")
	}
}

// TestMigrateDownUp covers §9.2 item 4: down to 0 then up again is green.
func TestMigrateDownUp(t *testing.T) {
	pool := pgtest.NewDB(t)
	dsn := pool.Config().ConnString()
	pool.Close()

	mig, err := pgtest.NewMigrator(dsn)
	if err != nil {
		t.Fatalf("migrator: %v", err)
	}
	defer func() { _, _ = mig.Close() }()
	if err := mig.Down(); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if err := mig.Up(); err != nil {
		t.Fatalf("migrate up after down: %v", err)
	}
	v, dirty, err := mig.Version()
	if err != nil || dirty || v != 7 {
		t.Errorf("version after down/up = %d dirty=%t err=%v, want 7 clean", v, dirty, err)
	}
}
