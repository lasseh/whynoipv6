//go:build integration

package crawler

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miekg/dns"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
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

// sweepDNS is a scripted loopback resolver for the sweep: one behaviour at
// a time, answering whatever name it is asked. It is what lets the test
// drive sweepHost — and with it ResourceSweeper.lookup, the 06 §5.3 answer
// table — instead of re-implementing the confirmation machine beside it.
type sweepDNS struct {
	mu       sync.Mutex
	behavior string
	addr     string
}

func (f *sweepDNS) set(b string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.behavior = b
}

func (f *sweepDNS) handle(w dns.ResponseWriter, r *dns.Msg) {
	f.mu.Lock()
	b := f.behavior
	f.mu.Unlock()

	m := new(dns.Msg)
	m.SetReply(r)
	q := r.Question[0]
	aaaa := func(ip string) *dns.AAAA {
		return &dns.AAAA{
			Hdr:  dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 300},
			AAAA: net.ParseIP(ip),
		}
	}
	switch b {
	case "routable":
		// Must be genuinely routable: since review issue 15 the routable
		// filter rejects everything the SSRF blocklist does, and the
		// documentation range 2001:db8::/32 is on that list.
		m.Answer = append(m.Answer, aaaa("2606:4700::1"))
	case "loopback": // NOERROR with a non-routable address only
		m.Answer = append(m.Answer, aaaa("::1"))
	case "empty": // NOERROR, no AAAA
	case "nxdomain":
		m.SetRcode(r, dns.RcodeNameError)
	case "servfail":
		m.SetRcode(r, dns.RcodeServerFailure)
	case "timeout":
		return // no reply at all
	}
	_ = w.WriteMsg(m)
}

func startSweepDNS(t *testing.T) *sweepDNS {
	t.Helper()
	f := &sweepDNS{behavior: "empty"}
	lc := &net.ListenConfig{}
	pc, err := lc.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(f.handle)}
	go func() { _ = srv.ActivateAndServe() }()
	t.Cleanup(func() { _ = srv.Shutdown() })
	f.addr = pc.LocalAddr().String()
	return f
}

// TestResourceSweepMachine (P5.1 / 06 §5.4): claim bumps the schedule (the
// lease), the N=2 confirmation machine holds, non-definitive touches
// nothing. Driven through sweepHost against a scripted resolver, so the
// §5.3 answer table (NXDOMAIN → no_record, NOERROR-nonroutable →
// unsupported, SERVFAIL/timeout → non-definitive) is executed rather than
// mirrored: a test that copies the machine passes every mutation of it.
func TestResourceSweepMachine(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		INSERT INTO resource_host (host, dependent_count, next_check_at)
		VALUES ('r1.example.net', 1, now() - interval '1 minute'),
		       ('r0.example.net', 0, now() - interval '1 minute')`); err != nil {
		t.Fatal(err)
	}

	fake := startSweepDNS(t)
	bulk := checker.NewResolver([]string{fake.addr})
	// The timeout rows would otherwise cost 2 × 5s of dnsTimeout each.
	bulk.SetAttemptTimeout(200 * time.Millisecond)
	s := &ResourceSweeper{Pool: pool, Bulk: bulk}

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

	hostID := batch[0].ID
	assertHost := func(t *testing.T, wantStatus, wantPending string, wantCount int) {
		t.Helper()
		var st, pd *string
		var n int16
		if err := pool.QueryRow(ctx,
			"SELECT aaaa_status::text, aaaa_pending::text, aaaa_pending_count FROM resource_host WHERE id=$1",
			hostID).Scan(&st, &pd, &n); err != nil {
			t.Fatal(err)
		}
		got := fmt.Sprintf("%v/%v/%d", deref(st), deref(pd), n)
		want := fmt.Sprintf("%s/%s/%d", wantStatus, wantPending, wantCount)
		if got != want {
			t.Errorf("host state = %s, want %s", got, want)
		}
	}

	// One sweep pass: make the row due again, re-claim it (which is where
	// the machine's prior state comes from — sweepHost reads the claimed
	// row, never a carried-over struct), and sweep it against the scripted
	// answer. The re-claim also re-arms the 2h lease.
	sweep := func(t *testing.T, answer string) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			"UPDATE resource_host SET next_check_at = now() - interval '1 minute' WHERE id=$1", hostID); err != nil {
			t.Fatal(err)
		}
		claimed, err := s.claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(claimed) != 1 || claimed[0].ID != hostID {
			t.Fatalf("re-claim = %+v", claimed)
		}
		fake.set(answer)
		s.sweepHost(ctx, &claimed[0])
	}

	steps := []struct {
		name                    string
		answer                  string
		wantStatus, wantPending string
		wantCount               int
	}{
		{"NOERROR with only a non-routable address bootstraps unsupported", "loopback", "unsupported", "<nil>", 0},
		{"a routable address is a candidate, not a flip", "routable", "unsupported", "supported", 1},
		{"the second consecutive sighting flips (N=2)", "routable", "supported", "<nil>", 0},
		{"NXDOMAIN is no_record, and a new candidate", "nxdomain", "supported", "no_record", 1},
		{"agreement with the confirmed value clears the candidate", "routable", "supported", "<nil>", 0},
		{"NOERROR with no AAAA at all is unsupported", "empty", "supported", "unsupported", 1},
	}
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			sweep(t, st.answer)
			assertHost(t, st.wantStatus, st.wantPending, st.wantCount)
		})
	}

	// Non-definitive answers touch nothing at all — not the columns, and
	// not the schedule: the row keeps the claim's +2h lease so the next
	// sweep retries it soon, rather than the +24h a commit would set.
	for _, answer := range []string{"servfail", "timeout"} {
		t.Run("non-definitive: "+answer, func(t *testing.T) {
			sweep(t, answer)
			assertHost(t, "supported", "unsupported", 1)
			var lease bool
			if err := pool.QueryRow(ctx,
				"SELECT next_check_at < now() + interval '3 hours' FROM resource_host WHERE id=$1",
				hostID).Scan(&lease); err != nil {
				t.Fatal(err)
			}
			if !lease {
				t.Error("a non-definitive sweep must leave the claim's 2h lease, not commit a 24h schedule")
			}
		})
	}

	// Review issue 30: a resolver that accepts the query and never answers
	// must cost one host's turn, not a sweep goroutine for the life of the
	// process. sweepLookupBudget bounds it; the row is left exactly as a
	// non-definitive answer leaves it, so the claim's 2h lease still stands
	// and the host is retried soon.
	t.Run("a silent resolver is bounded by the lookup budget", func(t *testing.T) {
		// No per-attempt shortening here: this asserts the budget itself,
		// so the resolver's own retries have to be the thing it cuts off.
		slow := &ResourceSweeper{Pool: pool, Bulk: checker.NewResolver([]string{fake.addr})}
		fake.set("timeout")

		// The subtests above left the row on their claim's 2h lease.
		if _, err := pool.Exec(ctx,
			"UPDATE resource_host SET next_check_at = now() - interval '1 minute' WHERE id=$1",
			hostID); err != nil {
			t.Fatal(err)
		}
		batch, err := slow.claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch) != 1 {
			t.Fatalf("claim = %+v, want the one dependent host", batch)
		}

		start := time.Now()
		slow.sweepHost(ctx, &batch[0])
		elapsed := time.Since(start)

		if elapsed > 2*sweepLookupBudget {
			t.Errorf("a silent resolver held the sweep for %s, budget is %s", elapsed, sweepLookupBudget)
		}
		assertHost(t, "supported", "unsupported", 1)
	})
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

// TestResourceSweepClaimBacksOff is review issue 56. A non-definitive sweep
// writes nothing, so the claim's flat 2h bump was the whole schedule for a
// host that never answers: 12 lookups a day against 1 for a healthy host,
// each silent one costing the sweeper's single sequential goroutine the full
// 3s sweepLookupBudget.
//
// The backoff needs no new column. ResourceSweepCommit is the only writer of
// last_checked_at and runs only on a definitive outcome, so the column
// already means "when we last learned anything": NULL is never-resolved,
// stale is resolved-once-and-stuck.
func TestResourceSweepClaimBacksOff(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		INSERT INTO resource_host (host, dependent_count, next_check_at, last_checked_at)
		VALUES ('never.example.net', 1, now() - interval '1 minute', NULL),
		       ('stale.example.net', 1, now() - interval '1 minute', now() - interval '10 days'),
		       ('fresh.example.net', 1, now() - interval '1 minute', now() - interval '1 hour')`); err != nil {
		t.Fatal(err)
	}

	s := &ResourceSweeper{Pool: pool}
	batch, err := s.claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 3 {
		t.Fatalf("claimed %d hosts, want 3", len(batch))
	}

	for host, want := range map[string]time.Duration{
		// Never resolved: 4 lookups a day, not 12. Not the full 24h — a host
		// resource discovery found minutes ago whose first lookup failed
		// should not wait a day for its second.
		"never.example.net": 6 * time.Hour,
		// Resolved once, nothing since: it is stuck, so cost it exactly what
		// a healthy host costs.
		"stale.example.net": 24 * time.Hour,
		// Recently healthy: keep the quick retry, which is the one case where
		// retrying soon is worth anything.
		"fresh.example.net": 2 * time.Hour,
	} {
		var gap time.Duration
		var secs float64
		if err := pool.QueryRow(ctx,
			`SELECT extract(epoch FROM next_check_at - now()) FROM resource_host WHERE host=$1`,
			host).Scan(&secs); err != nil {
			t.Fatal(err)
		}
		gap = time.Duration(secs) * time.Second
		if gap < want-time.Minute || gap > want+time.Minute {
			t.Errorf("%s: next_check_at is %v out, want ~%v", host, gap.Round(time.Minute), want)
		}
	}

	// A stuck host that starts answering must fall back to the normal
	// cadence on its own — the backoff reads last_checked_at, and the commit
	// writes it.
	var stuckID int64
	if err := pool.QueryRow(ctx,
		`SELECT id FROM resource_host WHERE host='stale.example.net'`).Scan(&stuckID); err != nil {
		t.Fatal(err)
	}
	supported := domain.StatusSupported
	if err := db.New(pool).ResourceSweepCommit(ctx, db.ResourceSweepCommitParams{
		ID: stuckID, AaaaStatus: statusDB(&supported),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE resource_host SET next_check_at = now() - interval '1 minute' WHERE id=$1`,
		stuckID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.claim(ctx); err != nil {
		t.Fatal(err)
	}
	var secs float64
	if err := pool.QueryRow(ctx,
		`SELECT extract(epoch FROM next_check_at - now()) FROM resource_host WHERE id=$1`,
		stuckID).Scan(&secs); err != nil {
		t.Fatal(err)
	}
	if gap := time.Duration(secs) * time.Second; gap > 3*time.Hour {
		t.Errorf("a recovered host is still backed off %v, want the 2h retry", gap.Round(time.Minute))
	}
}
