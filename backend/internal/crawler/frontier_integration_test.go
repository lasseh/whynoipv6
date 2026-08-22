//go:build integration

package crawler

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

func seedDue(t *testing.T, pool *pgxpool.Pool, n int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO domain (host, kind, rank, created_by, asn_id, country_id, tld, next_check_at)
		SELECT 'd' || g || '.example', 'apex', g, 'tranco',
		       (SELECT id FROM asn WHERE number = 0),
		       (SELECT id FROM country WHERE code = 'UN'),
		       'example', now() - interval '1 minute'
		FROM generate_series(1, $1::int) g`, n)
	if err != nil {
		t.Fatal(err)
	}
}

// TestClaimAtomicity (04 §17.2): two concurrent claimers over an overlapping
// due set never return the same row within a lease window; a stale lease
// (>30 min) is re-claimed.
func TestClaimAtomicity(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	seedDue(t, pool, 500)

	cfg := FrontierConfig{BatchSize: 50, Order: "rank"}
	f1 := NewFrontier(pool, cfg)
	f2 := NewFrontier(pool, cfg)

	seen := map[int64]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	claim := func(f *Frontier) {
		defer wg.Done()
		for range 5 { // 5 batches × 50 = 250 rows per claimer
			batch, err := f.ClaimBatch(ctx)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			mu.Lock()
			for _, d := range batch {
				seen[d.ID]++
			}
			mu.Unlock()
		}
	}
	wg.Add(2)
	go claim(f1)
	go claim(f2)
	wg.Wait()

	if len(seen) != 500 {
		t.Errorf("claimed %d distinct rows, want 500", len(seen))
	}
	for id, n := range seen {
		if n > 1 {
			t.Fatalf("domain %d claimed %d times within one lease window", id, n)
		}
	}

	// Snapshot shape: rank-ordered first batch carried the dims map.
	batch, err := f1.ClaimBatch(ctx)
	if err != nil || len(batch) != 0 {
		t.Errorf("frontier should be drained: n=%d err=%v", len(batch), err)
	}

	// Stale lease (>30 min) is re-claimed; fresh one is not.
	if _, err := pool.Exec(ctx,
		"UPDATE domain SET claimed_at = now() - interval '31 minutes' WHERE rank <= 10"); err != nil {
		t.Fatal(err)
	}
	batch, err = f1.ClaimBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 10 {
		t.Errorf("re-claimed %d stale-lease rows, want 10", len(batch))
	}
	for _, d := range batch {
		if d.Rank == nil || *d.Rank > 10 {
			t.Errorf("unexpected re-claimed row: %+v", d)
		}
		if d.ClaimedAt.IsZero() || time.Since(d.ClaimedAt) > time.Minute {
			t.Errorf("lease token not refreshed: %v", d.ClaimedAt)
		}
		if len(d.Dims) != 6 {
			t.Errorf("snapshot dims = %d groups, want 6", len(d.Dims))
		}
	}
}

// TestClaimLoopDispatch: the Run loop dispatches every claimed row to the
// bounded slot pool and drains cleanly on cancel.
func TestClaimLoopDispatch(t *testing.T) {
	pool := pgtest.NewDB(t)
	seedDue(t, pool, 120)

	f := NewFrontier(pool, FrontierConfig{
		BatchSize: 40, Order: "rank", EmptyPoll: 50 * time.Millisecond,
		WorkerSlots: 8, RetryInterval: 50 * time.Millisecond,
	})
	var mu sync.Mutex
	processed := map[int64]bool{}
	donech := make(chan struct{})
	f.Process = func(_ context.Context, d ClaimedDomain) {
		mu.Lock()
		processed[d.ID] = true
		n := len(processed)
		mu.Unlock()
		if n == 120 {
			close(donech)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	loopDone := make(chan struct{})
	go func() { f.Run(ctx, ctx); close(loopDone) }()

	select {
	case <-donech:
	case <-time.After(20 * time.Second):
		t.Fatal("claim loop did not process all 120 rows")
	}
	cancel()
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not drain slots after cancel")
	}
}
