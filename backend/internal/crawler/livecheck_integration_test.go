//go:build integration

package crawler

import (
	"context"
	"testing"

	"github.com/lasseh/whynoipv6/internal/geoip"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestLiveCheckEnsureDomain (07 §5.1.5 step 2): created_by='live_check',
// rank NULL, kind via PSL, parent linked ONLY when the registrable parent
// row already exists — never auto-ensured.
func TestLiveCheckEnsureDomain(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	q := db.New(pool)
	countries, err := geoip.LoadCountryMap(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	lc := &LiveChecker{Pool: pool, Q: q, Countries: countries}

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

	// Subdomain whose parent ALREADY exists links to it (nyapex.no was
	// created above).
	kind, err = lc.ensureDomain(ctx, "api.nyapex.no")
	if err != nil || kind != "subdomain" {
		t.Fatalf("linked subdomain: kind=%s err=%v", kind, err)
	}
	var linked *int64
	if err := pool.QueryRow(ctx, "SELECT parent_id FROM domain WHERE host = 'api.nyapex.no'").Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked == nil {
		t.Error("existing parent must be linked")
	}

	// Insert-time attribution is the 06 §6.5 helper's, not a second copy of
	// the rule: sentinel ASN, and the country the in-memory map derives —
	// ccTLD where there is one, sentinel otherwise.
	for _, host := range []string{"nyapex.no", "attr.example.com"} {
		if _, err := lc.ensureDomain(ctx, host); err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		var asnID, countryID int32
		if err := pool.QueryRow(ctx,
			"SELECT asn_id, country_id FROM domain WHERE host = $1", host).
			Scan(&asnID, &countryID); err != nil {
			t.Fatal(err)
		}
		if asnID != countries.SentinelASN {
			t.Errorf("%s asn_id = %d, want the sentinel %d", host, asnID, countries.SentinelASN)
		}
		if want := countries.InsertCountryID(host); countryID != want {
			t.Errorf("%s country_id = %d, want %d from the country map", host, countryID, want)
		}
	}

	// An unknown TLD is rejected — no wildcard PSL rule (06 §4.2).
	if _, err := lc.ensureDomain(ctx, "host.unknowntld999"); err == nil {
		t.Error("unknown TLD must fail PSL evaluation")
	}

	// Idempotent on an existing host: returns its kind, no duplicate.
	if kind, err := lc.ensureDomain(ctx, "nyapex.no"); err != nil || kind != "apex" {
		t.Errorf("re-ensure: kind=%s err=%v", kind, err)
	}
}
