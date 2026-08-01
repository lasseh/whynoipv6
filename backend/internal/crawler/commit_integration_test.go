//go:build integration

package crawler

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// claimOne seeds a due domain and claims it, returning the snapshot.
func claimOne(t *testing.T, pool *pgxpool.Pool) ClaimedDomain {
	t.Helper()
	seedDue(t, pool, 1)
	f := NewFrontier(pool, FrontierConfig{BatchSize: 10, Order: "rank"})
	batch, err := f.ClaimBatch(context.Background())
	if err != nil || len(batch) != 1 {
		t.Fatalf("claim: n=%d err=%v", len(batch), err)
	}
	return batch[0]
}

// TestCommitTxn (10-testing §10): the single-round-trip batch write lands
// domain state + scan + scan_detail atomically against real DDL; the lease
// fence discards a stale worker's whole unit; re-commit is idempotent.
func TestCommitTxn(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	c := NewCommitter(pool, testCommitCfg(false))

	snap := claimOne(t, pool)
	obs := stableObs(domain.DimBase, domain.ObsSupported)
	tt := time.Now().UTC().Truncate(time.Microsecond)

	res, err := c.Commit(ctx, &CommitInput{
		Snapshot: snap, Obs: obs,
		Attribution: &Attribution{AsnID: snap.AsnID, CountryID: snap.CountryID},
		Details:     []byte(`{"results":{}}`), DurationMS: 1234, T: tt,
	})
	if err != nil || res.LeaseLost {
		t.Fatalf("commit: %+v err=%v", res, err)
	}
	if res.Bootstraps == 0 {
		t.Error("first commit should bootstrap")
	}

	var baseStatus, class string
	var scans, details, changelogs int
	var claimedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT base_status::text, classification::text, claimed_at,
		(SELECT count(*) FROM scan), (SELECT count(*) FROM scan_detail), (SELECT count(*) FROM changelog)
		FROM domain WHERE id=$1`, snap.ID).
		Scan(&baseStatus, &class, &claimedAt, &scans, &details, &changelogs); err != nil {
		t.Fatal(err)
	}
	if baseStatus != "supported" || class != "hero" || claimedAt != nil {
		t.Errorf("domain state = %s/%s claimed=%v", baseStatus, class, claimedAt)
	}
	if scans != 1 || details != 1 || changelogs != 0 {
		t.Errorf("rows scan/detail/changelog = %d/%d/%d, want 1/1/0", scans, details, changelogs)
	}

	// Idempotent re-commit (03 §17.6): the lease is now NULL, so the fenced
	// UPDATE matches 0 rows and NOTHING lands — no duplicate scan rows.
	res, err = c.Commit(ctx, &CommitInput{
		Snapshot: snap, Obs: obs,
		Attribution: &Attribution{AsnID: snap.AsnID, CountryID: snap.CountryID},
		Details:     []byte(`{"results":{}}`), DurationMS: 1234, T: tt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.LeaseLost {
		t.Fatal("re-commit with a consumed lease must be lease-lost")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM scan").Scan(&scans); err != nil {
		t.Fatal(err)
	}
	if scans != 1 {
		t.Errorf("scan rows after lease-lost re-commit = %d, want 1 (nothing written)", scans)
	}
}

// TestCommitTxnFence: a reclaimed row discards the loser's whole transaction
// — no domain state, no scan, no changelog from the stale worker.
func TestCommitTxnFence(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	c := NewCommitter(pool, testCommitCfg(false))

	snap := claimOne(t, pool)

	// Simulate a reclaim: another worker re-stamps the lease.
	if _, err := pool.Exec(ctx,
		"UPDATE domain SET claimed_at = now() + interval '1 second' WHERE id=$1", snap.ID); err != nil {
		t.Fatal(err)
	}

	res, err := c.Commit(ctx, &CommitInput{
		Snapshot: snap, Obs: stableObs(domain.DimBase, domain.ObsSupported),
		Attribution: &Attribution{AsnID: snap.AsnID, CountryID: snap.CountryID},
		Details:     []byte(`{}`), DurationMS: 1, T: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.LeaseLost {
		t.Fatal("stale lease must be fenced")
	}
	var baseStatus *string
	var scans int
	if err := pool.QueryRow(ctx,
		"SELECT base_status::text, (SELECT count(*) FROM scan) FROM domain WHERE id=$1", snap.ID).
		Scan(&baseStatus, &scans); err != nil {
		t.Fatal(err)
	}
	if baseStatus != nil || scans != 0 {
		t.Errorf("fenced commit leaked: base=%v scans=%d", baseStatus, scans)
	}
}

// TestCommitTxnChangelog: a confirmed flip writes exactly one changelog row
// joined to a scan row with the same (domain_id, ts).
func TestCommitTxnChangelog(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	c := NewCommitter(pool, testCommitCfg(false))

	snap := claimOne(t, pool)
	commit := func(dt time.Duration, o domain.Observation) {
		t.Helper()
		// Re-claim before each commit (the lease was consumed).
		if _, err := pool.Exec(ctx,
			"UPDATE domain SET next_check_at = now() - interval '1 minute', claimed_at = NULL WHERE id=$1", snap.ID); err != nil {
			t.Fatal(err)
		}
		f := NewFrontier(pool, FrontierConfig{BatchSize: 10, Order: "rank"})
		batch, err := f.ClaimBatch(ctx)
		if err != nil || len(batch) != 1 {
			t.Fatalf("re-claim: n=%d err=%v", len(batch), err)
		}
		res, err := c.Commit(ctx, &CommitInput{
			Snapshot: batch[0], Obs: stableObs(domain.DimBase, o),
			Attribution: &Attribution{AsnID: snap.AsnID, CountryID: snap.CountryID},
			Details:     []byte(`{}`), DurationMS: 1,
			T: time.Now().UTC().Add(dt),
		})
		if err != nil || res.LeaseLost {
			t.Fatalf("commit: %+v err=%v", res, err)
		}
	}

	commit(0, domain.ObsSupported)              // bootstrap
	commit(24*time.Hour, domain.ObsUnsupported) // pending 1
	commit(48*time.Hour, domain.ObsUnsupported) // flip → changelog

	var n int
	var joined bool
	if err := pool.QueryRow(ctx, `SELECT count(*),
		bool_and(EXISTS (SELECT 1 FROM scan s WHERE s.domain_id = c.domain_id AND s.ts = c.ts))
		FROM changelog c`).Scan(&n, &joined); err != nil {
		t.Fatal(err)
	}
	if n != 1 || !joined {
		t.Errorf("changelog rows = %d joined=%t, want exactly 1 row joining its scan", n, joined)
	}
	var old, newV string
	if err := pool.QueryRow(ctx, "SELECT old_value::text, new_value::text FROM changelog").Scan(&old, &newV); err != nil {
		t.Fatal(err)
	}
	if old != "supported" || newV != "unsupported" {
		t.Errorf("changelog = %s→%s", old, newV)
	}
}

// TestCommitPivots: the provider pivots ride the fenced UPDATE — stamped
// with the unit, untouched on deferred scans, discarded with a lost lease.
func TestCommitPivots(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	c := NewCommitter(pool, testCommitCfg(false))

	var providerID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO dns_provider (name, ns_suffixes) VALUES ('Cloudflare', '{ns.cloudflare.com}')
		 RETURNING id`).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	tag := "cloudflare"

	snap := claimOne(t, pool)
	f := NewFrontier(pool, FrontierConfig{BatchSize: 10, Order: "rank"})
	reclaim := func() ClaimedDomain {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`UPDATE domain SET next_check_at = now() - interval '1 minute', claimed_at = NULL
			 WHERE id=$1`, snap.ID); err != nil {
			t.Fatal(err)
		}
		batch, err := f.ClaimBatch(ctx)
		if err != nil || len(batch) != 1 {
			t.Fatalf("reclaim: n=%d err=%v", len(batch), err)
		}
		return batch[0]
	}
	pivotState := func() (prov *int64, hosting *string) {
		t.Helper()
		if err := pool.QueryRow(ctx,
			`SELECT dns_provider_id, hosting_provider FROM domain WHERE id=$1`, snap.ID).
			Scan(&prov, &hosting); err != nil {
			t.Fatal(err)
		}
		return prov, hosting
	}

	// Definitive scan with pivots: both columns land with the unit.
	res, err := c.Commit(ctx, &CommitInput{
		Snapshot: snap, Obs: stableObs(domain.DimBase, domain.ObsSupported),
		Pivots:  &Pivots{StampDNS: true, DNSProvider: &providerID, Hosting: &tag},
		Details: []byte(`{"results":{}}`), T: time.Now().UTC(),
	})
	if err != nil || res.LeaseLost {
		t.Fatalf("commit: %+v err=%v", res, err)
	}
	prov, hosting := pivotState()
	if prov == nil || *prov != providerID || hosting == nil || *hosting != tag {
		t.Fatalf("pivots = (%v, %v), want (%d, %q)", prov, hosting, providerID, tag)
	}

	// Deferred scan (nil Pivots): both columns stay untouched.
	snap2 := reclaim()
	if res, err = c.Commit(ctx, &CommitInput{
		Snapshot: snap2, Obs: stableObs(domain.DimBase, domain.ObsSupported),
		Details:  []byte(`{"results":{}}`), T: time.Now().UTC(),
	}); err != nil || res.LeaseLost {
		t.Fatalf("deferred commit: %+v err=%v", res, err)
	}
	if prov, hosting = pivotState(); prov == nil || hosting == nil {
		t.Fatal("deferred scan touched the pivots")
	}

	// Lost lease: a clearing stamp is discarded with the rest of the unit.
	snap3 := reclaim()
	if _, err := pool.Exec(ctx,
		"UPDATE domain SET claimed_at = now() + interval '1 second' WHERE id=$1", snap.ID); err != nil {
		t.Fatal(err)
	}
	if res, err = c.Commit(ctx, &CommitInput{
		Snapshot: snap3, Obs: stableObs(domain.DimBase, domain.ObsSupported),
		Pivots:   &Pivots{StampDNS: true, DNSProvider: nil, Hosting: nil},
		Details:  []byte(`{"results":{}}`), T: time.Now().UTC(),
	}); err != nil || !res.LeaseLost {
		t.Fatalf("stale commit: %+v err=%v", res, err)
	}
	if prov, hosting = pivotState(); prov == nil || hosting == nil {
		t.Fatal("fenced commit cleared the pivots")
	}
}
