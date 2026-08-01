//go:build integration

package crawler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestLeaseFenceChaos (P2.G3; 03 §17.4/§17.6): a worker stalls mid-batch,
// its leases expire, another process reclaims and commits; the stalled
// worker's late commits are all fenced — no double changelog rows, no
// orphaned fresh leases, and the lease_lost counter accounts for every
// in-flight domain of the killed worker.
func TestLeaseFenceChaos(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	const n = 10

	// Domains with a mature pending candidate: the next counted definitive
	// `unsupported` flips base and writes a changelog row.
	mustExec(t, pool, `
		INSERT INTO domain (host, kind, rank, created_by, asn_id, country_id, tld, next_check_at,
		                    base_status, base_pending, base_pending_count, base_since, last_counted_at,
		                    classification)
		SELECT 'chaos' || g || '.example', 'apex', g, 'tranco',
		       (SELECT id FROM asn WHERE number=0), (SELECT id FROM country WHERE code='UN'), 'example',
		       now() - interval '1 minute',
		       'supported', 'unsupported', 1, now() - interval '3 days', now() - interval '2 days',
		       'partial'
		FROM generate_series(1, $1::int) g`, n)

	// Worker A claims the whole batch, then "stalls" (no commits).
	workerA := NewFrontier(pool, FrontierConfig{BatchSize: n, Order: "rank"})
	batchA, err := workerA.ClaimBatch(ctx)
	if err != nil || len(batchA) != n {
		t.Fatalf("worker A claim: n=%d err=%v", len(batchA), err)
	}

	// The lease window passes; worker B reclaims and commits everything.
	mustExec(t, pool, "UPDATE domain SET claimed_at = claimed_at - interval '31 minutes'")
	workerB := NewFrontier(pool, FrontierConfig{BatchSize: n, Order: "rank"})
	batchB, err := workerB.ClaimBatch(ctx)
	if err != nil || len(batchB) != n {
		t.Fatalf("worker B reclaim: n=%d err=%v", len(batchB), err)
	}
	committer := NewCommitter(pool, testCommitCfg(false))
	obs := stableObs(domain.DimBase, domain.ObsUnsupported)
	for _, d := range batchB {
		res, err := committer.Commit(ctx, &CommitInput{
			Snapshot: d, Obs: obs,
			Attribution: &Attribution{AsnID: d.AsnID, CountryID: d.CountryID},
			Details:     []byte(`{}`), DurationMS: 1, T: time.Now().UTC(),
		})
		if err != nil || res.LeaseLost {
			t.Fatalf("worker B commit: %+v err=%v", res, err)
		}
		if len(res.Transitions) != 1 {
			t.Fatalf("worker B expected a base flip, got %+v", res.Transitions)
		}
	}

	// Worker A wakes up and tries to commit its stale batch: every unit is
	// fenced and writes nothing.
	for _, d := range batchA {
		res, err := committer.Commit(ctx, &CommitInput{
			Snapshot: d, Obs: obs,
			Attribution: &Attribution{AsnID: d.AsnID, CountryID: d.CountryID},
			Details:     []byte(`{}`), DurationMS: 1, T: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("worker A late commit errored (should be a clean fence): %v", err)
		}
		if !res.LeaseLost {
			t.Fatal("worker A's stale commit landed — fence broken")
		}
	}

	// No double changelog: exactly one row per (domain_id, field).
	var dupes, total, scans int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM (SELECT domain_id, field FROM changelog
		   GROUP BY domain_id, field HAVING count(*) > 1) d),
		(SELECT count(*) FROM changelog),
		(SELECT count(*) FROM scan)`).Scan(&dupes, &total, &scans); err != nil {
		t.Fatal(err)
	}
	if dupes != 0 || total != n || scans != n {
		t.Errorf("changelog dupes=%d total=%d scans=%d, want 0/%d/%d", dupes, total, scans, n, n)
	}

	// No orphaned fresh lease remains.
	var fresh int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM domain WHERE claimed_at IS NOT NULL").Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	if fresh != 0 {
		t.Errorf("%d rows still leased after both workers finished", fresh)
	}
}

// TestRankedListPlanAvoidsDueIndex (P2.G4): no ranked-list read query plan
// may use idx_domain_due (05-schema §1.8 corollary).
func TestRankedListPlanAvoidsDueIndex(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	seedDue(t, pool, 5000)
	mustExec(t, pool, "UPDATE domain SET classification='hero' WHERE rank % 3 = 0")
	mustExec(t, pool, "VACUUM ANALYZE domain")

	rows, err := pool.Query(ctx, `EXPLAIN (FORMAT TEXT)
		SELECT id, host, rank FROM domain
		WHERE classification = 'hero' AND rank IS NOT NULL AND NOT disabled
		ORDER BY rank ASC LIMIT 50`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	full := ""
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		full += line + "\n"
	}
	if strings.Contains(full, "idx_domain_due") {
		t.Errorf("ranked-list read uses idx_domain_due:\n%s", full)
	}
	if !strings.Contains(full, "idx_domain_heroes") {
		t.Logf("note: hero list plan does not use idx_domain_heroes (small table may prefer other paths):\n%s", full)
	}
}
