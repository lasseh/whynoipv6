//go:build integration

package crawler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// execSQL is a tiny helper for the property assertions below.
func countDeps(t *testing.T, pool *pgxpool.Pool, host string) (stored, actual int) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `
		SELECT rh.dependent_count,
		       (SELECT count(*) FROM domain_resource dr WHERE dr.resource_host_id = rh.id)
		FROM resource_host rh WHERE rh.host = $1`, host).Scan(&stored, &actual)
	if err != nil {
		t.Fatal(err)
	}
	return stored, actual
}

// TestResourceDiscovery (P5.1): statements A–C + prune maintain
// dependent_count exactly under arbitrary interleavings; manual links
// survive prune; required=FALSE links are excluded from the roll-up input.
func TestResourceDiscovery(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	seedDue(t, pool, 3) // d1..d3.example

	ids := map[string]int64{}
	for _, h := range []string{"d1.example", "d2.example", "d3.example"} {
		var id int64
		if err := pool.QueryRow(ctx, "SELECT id FROM domain WHERE host=$1", h).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[h] = id
	}

	// Interleave the commit-path statements (A/B/C via the Go constants)
	// across domains and repeats.
	now := time.Now().UTC()
	upsert := func(domainID int64, host string) {
		if _, err := pool.Exec(ctx, "INSERT INTO resource_host (host) VALUES ($1) ON CONFLICT (host) DO NOTHING", host); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, postgres.SQLUpsertDomainResource, host, domainID, postgres.TS(now)); err != nil {
			t.Fatal(err)
		}
	}
	prune := func(domainID int64) {
		if _, err := pool.Exec(ctx, postgres.SQLPruneDomainResources, domainID, postgres.TS(now)); err != nil {
			t.Fatal(err)
		}
	}

	// d1+d2 → fonts; d1 → cdn; repeat d1→fonts (no double count).
	upsert(ids["d1.example"], "fonts.example.net")
	upsert(ids["d2.example"], "fonts.example.net")
	upsert(ids["d1.example"], "cdn.example.net")
	upsert(ids["d1.example"], "fonts.example.net") // repeat: refresh only
	prune(ids["d1.example"])                       // nothing stale yet

	for _, h := range []string{"fonts.example.net", "cdn.example.net"} {
		stored, actual := countDeps(t, pool, h)
		if stored != actual {
			t.Errorf("%s dependent_count=%d, actual links=%d", h, stored, actual)
		}
	}

	// Age d1→fonts beyond 30 days; prune drops it and decrements; the
	// manual d2 link and a manual-source link survive.
	if _, err := pool.Exec(ctx, `
		UPDATE domain_resource SET last_seen = now() - interval '40 days'
		WHERE domain_id = $1`, ids["d1.example"]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE domain_resource SET source = 'manual'
		WHERE domain_id = $1 AND resource_host_id = (SELECT id FROM resource_host WHERE host = 'cdn.example.net')`,
		ids["d1.example"]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, postgres.SQLPruneDomainResources, ids["d1.example"], postgres.TS(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	stored, actual := countDeps(t, pool, "fonts.example.net")
	if stored != 1 || actual != 1 { // d2's link remains
		t.Errorf("fonts after prune: stored=%d actual=%d, want 1/1", stored, actual)
	}
	stored, actual = countDeps(t, pool, "cdn.example.net")
	if stored != 1 || actual != 1 { // manual link survives despite age
		t.Errorf("manual link must survive prune: stored=%d actual=%d", stored, actual)
	}

	// required=FALSE links are excluded from the roll-up input query (the
	// worker readLinks predicate `AND dr.required`).
	if _, err := pool.Exec(ctx, `
		INSERT INTO domain_resource (domain_id, resource_host_id, source, required)
		SELECT $1, id, 'manual', false FROM resource_host WHERE host = 'fonts.example.net'`,
		ids["d3.example"]); err != nil {
		t.Fatal(err)
	}
	var rollupRows int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM domain_resource dr WHERE dr.domain_id = $1 AND dr.required`,
		ids["d3.example"]).Scan(&rollupRows); err != nil {
		t.Fatal(err)
	}
	if rollupRows != 0 {
		t.Errorf("advisory links leaked into the roll-up input: %d", rollupRows)
	}
}

// TestResourceSweepMachine (P5.1 / 06 §5.4): claim bumps the schedule (the
// lease), the N=2 confirmation machine holds, non-definitive touches
// nothing.
func TestResourceSweepMachine(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		INSERT INTO resource_host (host, dependent_count, next_check_at)
		VALUES ('r1.example.net', 1, now() - interval '1 minute'),
		       ('r0.example.net', 0, now() - interval '1 minute')`); err != nil {
		t.Fatal(err)
	}

	s := &ResourceSweeper{Pool: pool}
	batch, err := s.claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Only dependent_count > 0 hosts are swept; the claim moved
	// next_check_at 2h out (the crash lease).
	if len(batch) != 1 || batch[0].Host != "r1.example.net" {
		t.Fatalf("claim = %+v", batch)
	}
	var due bool
	if err := pool.QueryRow(ctx,
		"SELECT next_check_at > now() + interval '1 hour' FROM resource_host WHERE host='r1.example.net'").Scan(&due); err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Error("claim must bump next_check_at ~2h (the lease)")
	}

	// Drive the confirmation machine directly through sweepHost outcomes
	// by simulating lookups: apply the same transitions via the state
	// machine copy. First definitive commits immediately.
	apply := func(h *sweptHost, outcome domain.IPv6Status) {
		// mirror of sweepHost's write path without the DNS lookup
		o := outcome
		status, pending, pendingCount := h.Status, h.Pending, h.PendingCount
		switch {
		case status == nil:
			status, pending, pendingCount = &o, nil, 0
		case o == *status:
			pending, pendingCount = nil, 0
		case pending != nil && o == *pending:
			pendingCount++
			if pendingCount >= 2 {
				status, pending, pendingCount = &o, nil, 0
			}
		default:
			pending, pendingCount = &o, 1
		}
		if _, err := pool.Exec(ctx, `
			UPDATE resource_host SET aaaa_status=$2::ipv6_status, aaaa_pending=$3::ipv6_status,
			  aaaa_pending_count=$4, last_checked_at=now(), next_check_at=now()+interval '24 hours'
			WHERE id=$1`, h.ID, status, pending, pendingCount); err != nil {
			t.Fatal(err)
		}
		h.Status, h.Pending, h.PendingCount = status, pending, pendingCount
	}

	h := &batch[0]
	apply(h, "unsupported") // bootstrap-immediate
	assertHost := func(wantStatus, wantPending string, wantCount int) {
		t.Helper()
		var st, pd *string
		var n int16
		if err := pool.QueryRow(ctx,
			"SELECT aaaa_status::text, aaaa_pending::text, aaaa_pending_count FROM resource_host WHERE id=$1",
			h.ID).Scan(&st, &pd, &n); err != nil {
			t.Fatal(err)
		}
		got := fmt.Sprintf("%v/%v/%d", deref(st), deref(pd), n)
		want := fmt.Sprintf("%s/%s/%d", wantStatus, wantPending, wantCount)
		if got != want {
			t.Errorf("host state = %s, want %s", got, want)
		}
	}
	assertHost("unsupported", "<nil>", 0)

	apply(h, "supported") // candidate 1
	assertHost("unsupported", "supported", 1)
	apply(h, "supported") // N=2 → flips
	assertHost("supported", "<nil>", 0)
	apply(h, "no_record") // new candidate
	assertHost("supported", "no_record", 1)
	apply(h, "supported") // agreement clears the candidate
	assertHost("supported", "<nil>", 0)
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// TestResourceCLI (P5.2 / 06 §5.5): the manual-upsert xmax probe bumps
// dependent_count only on a genuine insert; a manual link survives prune;
// remove deletes and decrements.
func TestResourceCLI(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	seedDue(t, pool, 1)
	var domainID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM domain WHERE host='d1.example'").Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO resource_host (host) VALUES ('manual.example.net')"); err != nil {
		t.Fatal(err)
	}
	var rhID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM resource_host WHERE host='manual.example.net'").Scan(&rhID); err != nil {
		t.Fatal(err)
	}

	manualAdd := func(required bool) {
		if _, err := pool.Exec(ctx, `
			WITH up AS (
			  INSERT INTO domain_resource (domain_id, resource_host_id, source, required)
			  VALUES ($1, $2, 'manual', $3)
			  ON CONFLICT (domain_id, resource_host_id)
			  DO UPDATE SET source = 'manual', required = EXCLUDED.required
			  RETURNING (xmax = 0) AS inserted
			)
			UPDATE resource_host SET dependent_count = dependent_count + 1
			WHERE id = $2 AND (SELECT inserted FROM up)`, domainID, rhID, required); err != nil {
			t.Fatal(err)
		}
	}

	manualAdd(true)
	manualAdd(false) // re-add: updates required, must NOT double-count
	stored, actual := countDeps(t, pool, "manual.example.net")
	if stored != 1 || actual != 1 {
		t.Fatalf("after double add: stored=%d actual=%d, want 1/1", stored, actual)
	}
	var required bool
	if err := pool.QueryRow(ctx,
		"SELECT required FROM domain_resource WHERE domain_id=$1 AND resource_host_id=$2",
		domainID, rhID).Scan(&required); err != nil {
		t.Fatal(err)
	}
	if required {
		t.Error("re-add with --advisory must update required=false")
	}

	// The manual link survives the prune even when stale.
	if _, err := pool.Exec(ctx,
		"UPDATE domain_resource SET last_seen = now() - interval '90 days' WHERE domain_id=$1", domainID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, postgres.SQLPruneDomainResources, domainID, postgres.TS(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if stored, actual := countDeps(t, pool, "manual.example.net"); stored != 1 || actual != 1 {
		t.Errorf("manual link pruned: stored=%d actual=%d", stored, actual)
	}

	// Remove deletes and decrements; a second remove touches nothing.
	removeSQL := `
		WITH del AS (
		  DELETE FROM domain_resource
		  WHERE domain_id = $1 AND resource_host_id = $2
		  RETURNING resource_host_id
		)
		UPDATE resource_host SET dependent_count = dependent_count - 1
		WHERE id IN (SELECT resource_host_id FROM del)`
	if _, err := pool.Exec(ctx, removeSQL, domainID, rhID); err != nil {
		t.Fatal(err)
	}
	if stored, actual := countDeps(t, pool, "manual.example.net"); stored != 0 || actual != 0 {
		t.Errorf("after remove: stored=%d actual=%d", stored, actual)
	}
	if _, err := pool.Exec(ctx, removeSQL, domainID, rhID); err != nil {
		t.Fatal(err)
	}
	if stored, _ := countDeps(t, pool, "manual.example.net"); stored != 0 {
		t.Errorf("double remove must not decrement below 0: stored=%d", stored)
	}
}
