//go:build integration

package crawler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestMetrics (04 §17.9): checkpoint rows land every checkpointEvery domains
// and within the idle window; succeeded+failed=processed on every row; a
// forced lease-fence abort shows in failed and dim_counters.lease_lost.
func TestMetrics(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	m := NewMetrics(pool, uuid.New(), "test:1")

	obs := stableObs(domain.DimBase, domain.ObsSupported)

	// 999 successes + 1 lease-lost = exactly one checkpointEvery boundary;
	// ~2% slow scans so the 700 ms bucket carries p99.
	for range checkpointEvery - 21 {
		m.RecordScan(ctx, &obs, false, CommitResult{}, nil, 50*time.Millisecond)
	}
	for range 20 {
		m.RecordScan(ctx, &obs, false, CommitResult{}, nil, 700*time.Millisecond)
	}
	m.RecordScan(ctx, &obs, false, CommitResult{LeaseLost: true}, nil, 700*time.Millisecond)

	var processed, succeeded, failed int32
	var counters []byte
	var p50, p99 int32
	if err := pool.QueryRow(ctx, `SELECT processed, succeeded, failed, dim_counters, p50_ms, p99_ms
		FROM crawler_metrics ORDER BY ts DESC LIMIT 1`).
		Scan(&processed, &succeeded, &failed, &counters, &p50, &p99); err != nil {
		t.Fatalf("no checkpoint row after %d scans: %v", checkpointEvery, err)
	}
	if processed != checkpointEvery || succeeded+failed != processed || failed != 1 {
		t.Errorf("row = %d/%d/%d, want processed=%d succeeded+failed=processed failed=1",
			processed, succeeded, failed, checkpointEvery)
	}
	var dim map[string]any
	if err := json.Unmarshal(counters, &dim); err != nil {
		t.Fatal(err)
	}
	if got, _ := dim["lease_lost"].(float64); got != 1 {
		t.Errorf("dim_counters.lease_lost = %v, want 1", dim["lease_lost"])
	}
	if base, _ := dim["base"].(map[string]any); base["supported"] != float64(checkpointEvery) {
		t.Errorf("dim_counters.base = %v", dim["base"])
	}
	if p50 < 32 || p50 > 128 {
		t.Errorf("p50 = %d ms, want ≈50 (log-bucket estimate)", p50)
	}
	if p99 < 256 {
		t.Errorf("p99 = %d ms, want ≥512-bucket (one 700 ms outlier)", p99)
	}

	// Idle checkpoint rule: with a shrunken idle window, a row lands with
	// processed=0 while nothing is scanned.
	old := idleCheckpointAfter
	idleCheckpointAfter = 100 * time.Millisecond
	defer func() { idleCheckpointAfter = old }()
	// The idle loop ticks every 30s; call the check directly like it would.
	time.Sleep(150 * time.Millisecond)
	m.Checkpoint(ctx, false)

	var idleProcessed int32
	var isFinal bool
	if err := pool.QueryRow(ctx, `SELECT processed, is_final FROM crawler_metrics
		ORDER BY ts DESC LIMIT 1`).Scan(&idleProcessed, &isFinal); err != nil {
		t.Fatal(err)
	}
	if idleProcessed != 0 || isFinal {
		t.Errorf("idle row = processed=%d final=%t, want 0/false", idleProcessed, isFinal)
	}

	// Final shutdown row.
	m.RecordScan(ctx, &obs, true, CommitResult{}, nil, 10*time.Millisecond)
	m.Checkpoint(ctx, true)
	var finalProcessed int32
	var unresolvable []byte
	if err := pool.QueryRow(ctx, `SELECT processed, dim_counters FROM crawler_metrics
		WHERE is_final ORDER BY ts DESC LIMIT 1`).Scan(&finalProcessed, &unresolvable); err != nil {
		t.Fatal(err)
	}
	if finalProcessed != 1 {
		t.Errorf("final row processed = %d, want 1 (deltas, not cumulative)", finalProcessed)
	}
	var fin map[string]any
	_ = json.Unmarshal(unresolvable, &fin)
	if got, _ := fin["unresolvable"].(float64); got != 1 {
		t.Errorf("final dim_counters.unresolvable = %v, want 1", fin["unresolvable"])
	}
}
