//go:build integration

package crawler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestShutdown (04 §17.8): cancelling the claim context mid-batch drains the
// in-flight domains (each commits fully), writes the is_final metrics row,
// and leaves every processed row without a lease; expired leases are
// reclaimed by a restarted claimer.
func TestShutdown(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	seedDue(t, pool, 40)

	committer := NewCommitter(pool, testCommitCfg(false))
	metrics := NewMetrics(pool, uuid.New(), "shutdown-test:1")

	rootCtx := context.Background()
	f := NewFrontier(pool, FrontierConfig{
		BatchSize: 40, Order: "rank", EmptyPoll: 50 * time.Millisecond, WorkerSlots: 4,
	})
	f.Process = func(_ context.Context, d ClaimedDomain) {
		time.Sleep(120 * time.Millisecond) // simulate scan wall time
		obs := stableObs(domain.DimBase, domain.ObsSupported)
		res, err := committer.Commit(rootCtx, &CommitInput{
			Snapshot: d, Obs: obs,
			Attribution: &Attribution{AsnID: d.AsnID, CountryID: d.CountryID},
			Details:     []byte(`{}`), DurationMS: 120, T: time.Now().UTC(),
		})
		metrics.RecordScan(rootCtx, &obs, false, res, err, 120*time.Millisecond)
	}

	claimCtx, stopClaim := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { f.Run(claimCtx); close(done) }()

	// SIGTERM analog mid-batch: stop claiming while workers are busy.
	time.Sleep(400 * time.Millisecond)
	stopClaim()
	select {
	case <-done:
	case <-time.After(drainDeadline()):
		t.Fatal("drain exceeded the budget")
	}
	metrics.Checkpoint(ctx, true) // §14 step 3: the final row

	// Every row is either fully committed (status set, lease cleared) or
	// untouched-but-claimed (undispatched; expires via the 30-min lease).
	var halfWritten, committedFresh int
	if err := pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE base_status IS NOT NULL AND claimed_at IS NOT NULL),
		count(*) FILTER (WHERE base_status IS NOT NULL AND claimed_at IS NULL)
		FROM domain`).Scan(&halfWritten, &committedFresh); err != nil {
		t.Fatal(err)
	}
	if halfWritten != 0 {
		t.Errorf("%d rows committed but still leased (half-written state)", halfWritten)
	}
	if committedFresh == 0 {
		t.Error("no rows committed before the drain — test did not exercise in-flight work")
	}
	var scans int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM scan").Scan(&scans); err != nil {
		t.Fatal(err)
	}
	if scans != committedFresh {
		t.Errorf("scan rows = %d, committed rows = %d — must match exactly", scans, committedFresh)
	}

	var finalRows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM crawler_metrics WHERE is_final").Scan(&finalRows); err != nil {
		t.Fatal(err)
	}
	if finalRows != 1 {
		t.Errorf("is_final rows = %d, want 1", finalRows)
	}

	// Restart: age the leftover leases; a fresh claimer reclaims them.
	var leased int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM domain WHERE claimed_at IS NOT NULL").Scan(&leased); err != nil {
		t.Fatal(err)
	}
	if leased > 0 {
		mustExec(t, pool, "UPDATE domain SET claimed_at = claimed_at - interval '31 minutes' WHERE claimed_at IS NOT NULL")
		batch, err := NewFrontier(pool, FrontierConfig{BatchSize: 50, Order: "rank"}).ClaimBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch) != leased {
			t.Errorf("restarted process reclaimed %d rows, want %d", len(batch), leased)
		}
	}
}

// drainDeadline mirrors cmd/crawler's drainBudget with test margin.
func drainDeadline() time.Duration { return 10 * time.Second }
