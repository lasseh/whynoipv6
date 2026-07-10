//go:build integration

package crawler

import (
	"context"
	"testing"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestLiveCheckEnsureDomain (07 §5.1.5 step 2): created_by='live_check',
// rank NULL, kind via PSL, parent linked ONLY when the registrable parent
// row already exists — never auto-ensured.
func TestLiveCheckEnsureDomain(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	lc := &LiveChecker{Pool: pool, Q: db.New(pool)}

	// Unknown apex: inserted with sentinel attribution + ccTLD probe.
	kind, err := lc.ensureDomain(ctx, "nyapex.no")
	if err != nil || kind != "apex" {
		t.Fatalf("apex: kind=%s err=%v", kind, err)
	}
	var createdBy, country string
	var rank *int32
	var lastReq *string
	if err := pool.QueryRow(ctx, `
		SELECT d.created_by::text, c.code::text, d.rank, d.last_requested_at::text
		FROM domain d JOIN country c ON c.id = d.country_id
		WHERE d.host = 'nyapex.no'`).Scan(&createdBy, &country, &rank, &lastReq); err != nil {
		t.Fatal(err)
	}
	if createdBy != "live_check" || country != "NO" || rank != nil || lastReq == nil {
		t.Errorf("apex row: created_by=%s country=%s rank=%v last_requested_at=%v",
			createdBy, country, rank, lastReq)
	}

	// Subdomain with NO existing parent: parent_id stays NULL — live
	// checks never auto-ensure parents.
	kind, err = lc.ensureDomain(ctx, "www.orphan.se")
	if err != nil || kind != "subdomain" {
		t.Fatalf("subdomain: kind=%s err=%v", kind, err)
	}
	var parentID *int64
	if err := pool.QueryRow(ctx, "SELECT parent_id FROM domain WHERE host = 'www.orphan.se'").Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	if parentID != nil {
		t.Error("parent must not be auto-ensured for live-check subdomains")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM domain WHERE host = 'orphan.se'").Scan(new(int)); err != nil {
		t.Fatal(err)
	}
	var orphanParents int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM domain WHERE host = 'orphan.se'").Scan(&orphanParents)
	if orphanParents != 0 {
		t.Error("registrable parent row must not be created")
	}

	// Subdomain whose parent ALREADY exists links to it.
	seedDue(t, pool, 1) // d1.example
	kind, err = lc.ensureDomain(ctx, "api.d1.example")
	if err != nil || kind != "subdomain" {
		t.Fatalf("linked subdomain: kind=%s err=%v", kind, err)
	}
	var linked *int64
	if err := pool.QueryRow(ctx, "SELECT parent_id FROM domain WHERE host = 'api.d1.example'").Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked == nil {
		t.Error("existing parent must be linked")
	}

	// Idempotent on an existing host: returns its kind, no duplicate.
	if kind, err := lc.ensureDomain(ctx, "nyapex.no"); err != nil || kind != "apex" {
		t.Errorf("re-ensure: kind=%s err=%v", kind, err)
	}
}
