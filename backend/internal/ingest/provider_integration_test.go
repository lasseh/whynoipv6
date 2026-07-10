//go:build integration

package ingest

import (
	"context"
	"testing"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestProviderRoundTrip covers the P1.13 verbs surface: add → list
// round-trips a suffix set; the stamping writer touches only the pivot
// column; remove leaves stamped domains (self-healing on next commit).
func TestProviderRoundTrip(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	q := db.New(pool)

	if err := ProviderAdd(ctx, q, "Cloudflare", []string{"NS.CLOUDFLARE.COM", ".cloudflare.com"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := ProviderAdd(ctx, q, "Cloudflare", []string{"cloudflare.com", "cdn.cloudflarenet.com"}); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	rows, err := q.ProviderList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || len(rows[0].NsSuffixes) != 3 {
		t.Fatalf("list = %+v, want 1 provider with 3 deduped lowercase suffixes", rows)
	}

	// Seed a domain and stamp it from an observed NS set.
	var domainID int64
	if err := pool.QueryRow(ctx, `INSERT INTO domain (host, kind, created_by, asn_id, country_id, tld)
		VALUES ('stamp.example', 'apex', 'tranco',
		        (SELECT id FROM asn WHERE number=0), (SELECT id FROM country WHERE code='UN'), 'example')
		RETURNING id`).Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	m, err := LoadProviderMapping(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if err := StampDNSProvider(ctx, q, m, domainID, []string{"gina.ns.cloudflare.com"}); err != nil {
		t.Fatal(err)
	}

	// Read-back: pivot set; no scan/changelog rows; no confirmed-status writes.
	var providerID *int64
	var baseStatus *string
	var scans, changelogs int
	if err := pool.QueryRow(ctx, `SELECT d.dns_provider_id, d.base_status::text,
		(SELECT count(*) FROM scan), (SELECT count(*) FROM changelog)
		FROM domain d WHERE d.id=$1`, domainID).
		Scan(&providerID, &baseStatus, &scans, &changelogs); err != nil {
		t.Fatal(err)
	}
	if providerID == nil {
		t.Error("dns_provider_id not stamped")
	}
	if baseStatus != nil || scans != 0 || changelogs != 0 {
		t.Errorf("stamping wrote beyond the pivot: base=%v scans=%d changelog=%d", baseStatus, scans, changelogs)
	}

	// Remove: referencing domains are cleared first (FK), then the row is
	// deleted; they re-stamp on their next scan commit (§6.11 self-heal).
	if _, err := q.ProviderClearDomains(ctx, "Cloudflare"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	n, err := q.ProviderDelete(ctx, "Cloudflare")
	if err != nil || n != 1 {
		t.Fatalf("delete: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, "SELECT dns_provider_id FROM domain WHERE id=$1", domainID).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	if providerID != nil {
		t.Errorf("dns_provider_id = %v after remove, want NULL until next commit", *providerID)
	}
}
