//go:build integration

package crawler

import (
	"context"
	"testing"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestServiceLifecycleVerbs (P2.14 acceptance): confirm flips the domain out
// of the frontier (not claimable); dismiss leaves it untouched and never
// re-flags; manual disable is reversible.
func TestServiceLifecycleVerbs(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	q := db.New(pool)
	seedDue(t, pool, 2) // d1.example, d2.example, both due

	// Flag both as candidates (heuristic output shape).
	mustExec(t, pool, `INSERT INTO service_candidate (domain_id, reasons)
		SELECT id, ARRAY['apex_www_no_record'] FROM domain`)

	// Confirm d1: disabled service → not claimable (claim-query read-back).
	reason := db.DisabledReasonService
	if n, err := q.DomainDisable(ctx, db.DomainDisableParams{Host: "d1.example", Reason: &reason}); err != nil || n != 1 {
		t.Fatalf("disable: n=%d err=%v", n, err)
	}
	if _, err := q.ServiceCandidateResolve(ctx, "d1.example"); err != nil {
		t.Fatal(err)
	}
	f := NewFrontier(pool, FrontierConfig{BatchSize: 10, Order: "rank"})
	batch, err := f.ClaimBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 1 || batch[0].Host != "d2.example" {
		t.Fatalf("claim after confirm = %+v, want only d2.example", batch)
	}

	// Dismiss d2: domain untouched, candidate closed, never re-flagged.
	if n, err := q.ServiceCandidateResolve(ctx, "d2.example"); err != nil || n != 1 {
		t.Fatalf("dismiss: n=%d err=%v", n, err)
	}
	var disabled bool
	if err := pool.QueryRow(ctx, "SELECT disabled FROM domain WHERE host='d2.example'").Scan(&disabled); err != nil {
		t.Fatal(err)
	}
	if disabled {
		t.Error("dismiss must leave the domain untouched")
	}
	if n, err := q.DetectServiceCandidatesApex(ctx); err != nil || n != 0 {
		t.Errorf("re-detection after dismiss inserted %d rows (err=%v), want 0", n, err)
	}
	open, err := q.ServiceCandidateList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Errorf("open candidates = %d, want 0", len(open))
	}

	// Manual disable + enable round-trip.
	manual := db.DisabledReasonManual
	if n, err := q.DomainDisable(ctx, db.DomainDisableParams{Host: "d2.example", Reason: &manual}); err != nil || n != 1 {
		t.Fatalf("manual disable: n=%d err=%v", n, err)
	}
	if n, err := q.DomainEnable(ctx, "d2.example"); err != nil || n != 1 {
		t.Fatalf("enable: n=%d err=%v", n, err)
	}
	var dueNow bool
	if err := pool.QueryRow(ctx,
		"SELECT NOT disabled AND next_check_at <= now() FROM domain WHERE host='d2.example'").Scan(&dueNow); err != nil {
		t.Fatal(err)
	}
	if !dueNow {
		t.Error("re-enabled row must be immediately due")
	}
}
