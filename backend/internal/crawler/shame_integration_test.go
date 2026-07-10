//go:build integration

package crawler

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestShameCLI (P4.11): add rejects non-apex/rank-NULL/disabled hosts, is
// idempotent, writes no changelog; list computes visibility; remove deletes.
func TestShameCLI(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	q := db.New(pool)
	seedDue(t, pool, 2) // d1/d2.example: ranked apexes

	mustExec(t, pool, "UPDATE domain SET classification='sinner' WHERE host='d1.example'")
	mustExec(t, pool, `INSERT INTO domain (host, kind, created_by, asn_id, country_id, tld)
		VALUES ('unranked.example', 'apex', 'campaign',
		        (SELECT id FROM asn WHERE number=0), (SELECT id FROM country WHERE code='UN'), 'example')`)
	mustExec(t, pool, "UPDATE domain SET disabled=true, disabled_reason='manual' WHERE host='d2.example'")

	// Eligibility: rank-NULL and disabled hosts are rejected.
	if _, err := q.ShameEligibleDomain(ctx, "unranked.example"); !errors.Is(err, pgx.ErrNoRows) {
		t.Error("rank-NULL host must be ineligible")
	}
	if _, err := q.ShameEligibleDomain(ctx, "d2.example"); !errors.Is(err, pgx.ErrNoRows) {
		t.Error("disabled host must be ineligible")
	}
	if _, err := q.ShameEligibleDomain(ctx, "missing.example"); !errors.Is(err, pgx.ErrNoRows) {
		t.Error("unknown host must be ineligible")
	}

	// Add + idempotent re-add (reason updated, added_at preserved).
	row, err := q.ShameEligibleDomain(ctx, "d1.example")
	if err != nil {
		t.Fatal(err)
	}
	r1 := "legacy bank"
	if err := q.ShameUpsert(ctx, db.ShameUpsertParams{DomainID: row.ID, Reason: &r1}); err != nil {
		t.Fatal(err)
	}
	var added1 string
	if err := pool.QueryRow(ctx, "SELECT added_at::text FROM top_shame").Scan(&added1); err != nil {
		t.Fatal(err)
	}
	r2 := "still no AAAA"
	if err := q.ShameUpsert(ctx, db.ShameUpsertParams{DomainID: row.ID, Reason: &r2}); err != nil {
		t.Fatal(err)
	}
	var n int
	var reason, added2 string
	if err := pool.QueryRow(ctx,
		"SELECT count(*) OVER (), reason, added_at::text FROM top_shame").Scan(&n, &reason, &added2); err != nil {
		t.Fatal(err)
	}
	if n != 1 || reason != r2 || added1 != added2 {
		t.Errorf("re-add: n=%d reason=%q added %s→%s (added_at must be preserved)", n, reason, added1, added2)
	}
	var changelogs int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM changelog").Scan(&changelogs); err != nil {
		t.Fatal(err)
	}
	if changelogs != 0 {
		t.Error("shame edits must write no changelog rows")
	}

	// List computes visibility (sinner + publicly ranked = visible).
	list, err := q.ShameList(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v n=%d", err, len(list))
	}
	if list[0].Visible == nil || !*list[0].Visible {
		t.Errorf("d1 (sinner, ranked) should be visible: %+v", list[0])
	}

	// Remove deletes the row.
	if n, err := q.ShameRemove(ctx, "d1.example"); err != nil || n != 1 {
		t.Fatalf("remove: n=%d err=%v", n, err)
	}
	if n, err := q.ShameRemove(ctx, "d1.example"); err != nil || n != 0 {
		t.Fatalf("second remove: n=%d err=%v (exit 0, 'not on the shame list')", n, err)
	}
}
