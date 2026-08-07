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
	if jobs != 11 {
		t.Errorf("policy jobs = %d, want 11 (5 columnstore + 4 retention + 2 cagg refresh)", jobs)
	}

	// Two continuous aggregates, and they are not interchangeable:
	// scan_daily_adoption is measurement-flavored and never served (07 §4.10,
	// OPEN-5), changelog_daily is confirmed_state and is public via
	// /stats/changes. Real-time aggregation differs for the same reason —
	// the changelog surface must show a transition committed a minute ago.
	var caggs []string
	rows, err := pool.Query(ctx,
		"SELECT view_name FROM timescaledb_information.continuous_aggregates ORDER BY view_name")
	if err != nil {
		t.Fatalf("caggs: %v", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		caggs = append(caggs, name)
	}
	rows.Close()
	if len(caggs) != 2 || caggs[0] != "changelog_daily" || caggs[1] != "scan_daily_adoption" {
		t.Errorf("caggs = %v, want [changelog_daily scan_daily_adoption]", caggs)
	}

	var realtime bool
	if err := pool.QueryRow(ctx,
		`SELECT NOT materialized_only FROM timescaledb_information.continuous_aggregates
		 WHERE view_name = 'changelog_daily'`).Scan(&realtime); err != nil {
		t.Fatalf("changelog_daily materialized_only: %v", err)
	}
	if !realtime {
		t.Error("changelog_daily must keep real-time aggregation on: its ETag follows max(changelog.ts), so a just-committed transition has to be visible")
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
	if err != nil || dirty || v != 10 {
		t.Errorf("version after down/up = %d dirty=%t err=%v, want 10 clean", v, dirty, err)
	}
}
