//go:build integration

package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestPolicyConfig pins every Timescale policy's interval against the value
// its migration declares. The test server runs with
// timescaledb.max_background_workers=0, so nothing ever executes these on
// its own and TestMigrations only counted them: a typo'd `after => INTERVAL
// '9 days'` for 90 would have shipped and surfaced ten days later as
// decompression cost on the crawler's writes.
func TestPolicyConfig(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	type policy struct{ proc, table, key, want string }
	want := []policy{
		{"policy_compression", "scan", "compress_after", "14 days"},
		{"policy_retention", "scan", "drop_after", "2 years"},
		{"policy_compression", "scan_detail", "compress_after", "3 days"},
		{"policy_retention", "scan_detail", "drop_after", "90 days"},
		{"policy_compression", "changelog", "compress_after", "60 days"},
		{"policy_retention", "crawler_metrics", "drop_after", "90 days"},
		{"policy_retention", "unbound_stats", "drop_after", "30 days"},
		{"policy_compression", "stats_asn_daily", "compress_after", "180 days"},
		{"policy_refresh_continuous_aggregate", "scan_daily_adoption", "start_offset", "3 days"},
		{"policy_compression", "scan_daily_adoption", "compress_after", "90 days"},
		{"policy_refresh_continuous_aggregate", "changelog_daily", "start_offset", "90 days"},
	}
	for _, p := range want {
		t.Run(p.table+"/"+p.key, func(t *testing.T) {
			var got string
			if err := pool.QueryRow(ctx, `SELECT config->>$3 FROM timescaledb_information.jobs
				WHERE proc_name = $1 AND hypertable_name = $2`, p.proc, p.table, p.key).Scan(&got); err != nil {
				t.Fatalf("%s on %s: %v", p.proc, p.table, err)
			}
			if got != p.want {
				t.Errorf("%s.%s = %q, want %q", p.table, p.key, got, p.want)
			}
		})
	}

	// Both cagg refreshes end one hour back, so a bucket is materialized
	// only once it can no longer change.
	var ends int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM timescaledb_information.jobs
		WHERE proc_name = 'policy_refresh_continuous_aggregate'
		  AND config->>'end_offset' = '01:00:00'`).Scan(&ends); err != nil {
		t.Fatal(err)
	}
	if ends != 2 {
		t.Errorf("cagg refreshes with a 1h end_offset = %d, want 2", ends)
	}
}

// TestPolicyExecution runs the columnstore and retention policies for
// scan_detail with CALL run_job, which works with the job scheduler off, and
// asserts what they did: a chunk past the compression boundary is
// compressed, a chunk inside it is not, and a chunk past the retention
// boundary is dropped. Without this the boundaries are only ever read.
func TestPolicyExecution(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		INSERT INTO domain (host, kind, created_by, asn_id, country_id, tld, classification)
		VALUES ('policy.example', 'apex', 'tranco',
		        (SELECT id FROM asn WHERE number = 0), (SELECT id FROM country WHERE code = 'UN'),
		        'example', 'unknown')`); err != nil {
		t.Fatal(err)
	}
	// scan_detail chunks are one day wide: compress after 3 days, drop after
	// 90. One row on each side of both boundaries.
	for _, age := range []string{"1 hour", "10 days", "200 days"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO scan_detail (domain_id, ts, details, duration_ms)
			SELECT id, now() - $1::interval, '{"results":{}}'::jsonb, 5 FROM domain
			WHERE host = 'policy.example'`, age); err != nil {
			t.Fatalf("seed %s: %v", age, err)
		}
	}

	runPolicy(t, pool, "policy_compression", "scan_detail")
	fresh, old := chunkCompression(t, pool)
	if fresh != 0 {
		t.Errorf("%d chunk(s) inside the 3-day window compressed", fresh)
	}
	if old == 0 {
		t.Error("no chunk past the 3-day window was compressed")
	}

	before := countRows(t, pool, "SELECT count(*) FROM scan_detail")
	runPolicy(t, pool, "policy_retention", "scan_detail")
	after := countRows(t, pool, "SELECT count(*) FROM scan_detail")
	if before != 3 {
		t.Fatalf("seeded rows = %d, want 3", before)
	}
	if after != 2 {
		t.Errorf("rows after retention = %d, want 2 (the 200-day row dropped)", after)
	}
}

// runPolicy executes one policy job by id. CALL run_job runs the policy
// inline, which is what makes this testable with max_background_workers=0.
func runPolicy(t *testing.T, pool *pgxpool.Pool, proc, table string) {
	t.Helper()
	ctx := context.Background()
	var jobID int
	if err := pool.QueryRow(ctx, `SELECT job_id FROM timescaledb_information.jobs
		WHERE proc_name = $1 AND hypertable_name = $2`, proc, table).Scan(&jobID); err != nil {
		t.Fatalf("find %s on %s: %v", proc, table, err)
	}
	if _, err := pool.Exec(ctx, "CALL run_job($1)", jobID); err != nil {
		t.Fatalf("run %s on %s: %v", proc, table, err)
	}
}

// chunkCompression splits scan_detail's chunks by the 3-day boundary and
// reports how many on each side are compressed.
func chunkCompression(t *testing.T, pool *pgxpool.Pool) (fresh, old int) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), `SELECT
		count(*) FILTER (WHERE is_compressed AND range_end  > now() - interval '3 days'),
		count(*) FILTER (WHERE is_compressed AND range_end <= now() - interval '3 days')
		FROM timescaledb_information.chunks WHERE hypertable_name = 'scan_detail'`).
		Scan(&fresh, &old); err != nil {
		t.Fatal(err)
	}
	return fresh, old
}

func countRows(t *testing.T, pool *pgxpool.Pool, q string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
