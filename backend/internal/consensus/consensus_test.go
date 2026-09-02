package consensus

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/lasseh/whynoipv6/internal/checker"
)

// Scriptable fake DNS provider. Behaviors: "exists", "exists2" (different
// record set), "empty", "nxdomain", "servfail", "refused", "timeout",
// "loopback" (answers only ::1).
type fakeProvider struct {
	mu       sync.Mutex
	behavior string
	addr     string

	// Wire-query counters, per 10-testing §3.2/§3.2b's "issued?" column.
	// They count what reaches the socket, so a QueryWithRetry that retries
	// on SERVFAIL or a timeout shows as 2 — which is the behaviour worth
	// pinning as well.
	aQueries, cdQueries, aaaaQueries int
}

func (f *fakeProvider) set(b string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.behavior = b
}

// resetCounts zeroes the wire counters before a scripted row.
func (f *fakeProvider) resetCounts() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.aQueries, f.cdQueries, f.aaaaQueries = 0, 0, 0
}

func (f *fakeProvider) counts() (a, cd, aaaa int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.aQueries, f.cdQueries, f.aaaaQueries
}

func (f *fakeProvider) handle(w dns.ResponseWriter, r *dns.Msg) {
	q := r.Question[0]
	f.mu.Lock()
	b := f.behavior
	switch {
	case q.Qtype == dns.TypeA:
		f.aQueries++
	case q.Qtype == dns.TypeAAAA && r.CheckingDisabled:
		f.cdQueries++
	case q.Qtype == dns.TypeAAAA:
		f.aaaaQueries++
	}
	f.mu.Unlock()

	m := new(dns.Msg)
	m.SetReply(r)
	aaaa := func(ip string) *dns.AAAA {
		return &dns.AAAA{
			Hdr:  dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 300},
			AAAA: net.ParseIP(ip),
		}
	}
	a := func(ip string) *dns.A {
		return &dns.A{
			Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   net.ParseIP(ip),
		}
	}
	switch b {
	case "exists":
		switch q.Qtype {
		case dns.TypeAAAA:
			m.Answer = append(m.Answer, aaaa("2001:db8::1"), aaaa("2001:db8::2"))
		case dns.TypeA:
			m.Answer = append(m.Answer, a("192.0.2.1"))
		}
	case "exists2":
		m.Answer = append(m.Answer, aaaa("2001:db8::99"))
	case "a_present":
		if q.Qtype == dns.TypeA {
			m.Answer = append(m.Answer, a("192.0.2.7"))
		}
	case "empty":
		// NOERROR, no records.
	case "nxdomain":
		m.SetRcode(r, dns.RcodeNameError)
	case "servfail":
		m.SetRcode(r, dns.RcodeServerFailure)
	case "refused":
		m.SetRcode(r, dns.RcodeRefused)
	case "loopback":
		m.Answer = append(m.Answer, aaaa("::1"))
	case "timeout":
		return // no reply
	}
	_ = w.WriteMsg(m)
}

func startFake(t *testing.T) *fakeProvider {
	t.Helper()
	f := &fakeProvider{behavior: "empty"}
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

type harness struct {
	r           *Resolver
	cf, go_, q9 *fakeProvider
	bulk        *fakeProvider
	alerts      *[]string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	saved := perProviderBudget
	perProviderBudget = 150 * time.Millisecond // shrink timeout rows
	t.Cleanup(func() { perProviderBudget = saved })

	return newHarnessQPS(t, 1000)
}

// newHarnessQPS is newHarness with the per-provider token bucket set.
func newHarnessQPS(t *testing.T, qps int) *harness {
	t.Helper()
	h := &harness{cf: startFake(t), go_: startFake(t), q9: startFake(t), bulk: startFake(t)}
	var alerts []string
	var mu sync.Mutex
	h.alerts = &alerts

	// The bulk resolver has no per-attempt cap in production; shrink it
	// here so the scripted timeout rows do not cost 2 × dnsTimeout.
	bulk := checker.NewResolver([]string{h.bulk.addr})
	bulk.SetAttemptTimeout(150 * time.Millisecond)

	// Through the production constructor with the provider table injected,
	// so the providers keep New's SetAttemptTimeout(perAttemptTimeout);
	// replacing providerState.res afterwards silently dropped it.
	h.r = newWithProviders(Config{
		PerProviderQPS: qps,
		FastLane:       FastLaneConfig{NondefinitiveRate: 0.05, Window: 15 * time.Minute, MinSamples: 40, RecoverBelow: 0.02},
		Provider:       ProviderConfig{FailureRate: 0.50, Window: 15 * time.Minute, MinSamples: 20, RecoveryProbes: 3},
	}, []providerDef{
		{name: providerCloudflare, upstreams: []string{h.cf.addr}},
		{name: providerGoogle, upstreams: []string{h.go_.addr}},
		{name: providerQuad9, upstreams: []string{h.q9.addr}},
	}, bulk,
		func(_ context.Context, msg string) { mu.Lock(); alerts = append(alerts, msg); mu.Unlock() },
		slog.Default())
	t.Cleanup(h.r.Close)
	return h
}

func (h *harness) script(cf, goo, q9 string) {
	h.cf.set(cf)
	h.go_.set(goo)
	h.q9.set(q9)
}

// TestQuorumTruthTable reproduces the 10-testing §3.1 permutation table.
func TestQuorumTruthTable(t *testing.T) {
	h := newHarness(t)

	type want struct {
		result    string // "exists"|"empty"|"nxdomain"|"inconsistent"|"error"
		agreement string
		disagreed bool
	}
	rows := []struct {
		name        string
		cf, go_, q9 string
		want        want
	}{
		{"3of3_exists", "exists", "exists", "exists", want{"exists", "3of3", false}},
		{"3of3_empty", "empty", "empty", "empty", want{"empty", "3of3", false}},
		{"3of3_nxdomain", "nxdomain", "nxdomain", "nxdomain", want{"nxdomain", "3of3", false}},
		{"2of3_exists_over_empty", "exists", "exists", "empty", want{"exists", "2of3", true}},
		{"2of3_exists_over_nx", "exists", "exists", "nxdomain", want{"exists", "2of3", true}},
		{"2of3_empty_over_exists", "empty", "empty", "exists", want{"empty", "2of3", true}},
		{"2of3_empty_over_nx", "empty", "empty", "nxdomain", want{"empty", "2of3", true}},
		{"2of3_nx_over_exists", "nxdomain", "nxdomain", "exists", want{"nxdomain", "2of3", true}},
		{"2of3_nx_over_empty", "nxdomain", "nxdomain", "empty", want{"nxdomain", "2of3", true}},
		{"2plus1nonanswer_exists", "exists", "exists", "timeout", want{"exists", "2of3", false}},
		{"2plus1nonanswer_empty", "empty", "empty", "timeout", want{"empty", "2of3", false}},
		{"2plus1nonanswer_nx", "nxdomain", "nxdomain", "timeout", want{"nxdomain", "2of3", false}},
		{"servfail_is_nonanswer", "exists", "exists", "servfail", want{"exists", "2of3", false}},
		{"refused_is_nonanswer", "empty", "empty", "refused", want{"empty", "2of3", false}},
		{"noquorum_1_1_1", "exists", "empty", "nxdomain", want{"inconsistent", "0of3", false}},
		{"noquorum_two_disagree", "exists", "empty", "timeout", want{"inconsistent", "0of3", false}},
		{"noquorum_exists_nx_N", "exists", "nxdomain", "timeout", want{"inconsistent", "0of3", false}},
		{"noquorum_empty_nx_N", "empty", "nxdomain", "timeout", want{"inconsistent", "0of3", false}},
		{"error_1valid_exists", "exists", "timeout", "timeout", want{"error", "0of3", false}},
		{"error_1valid_empty", "empty", "timeout", "timeout", want{"error", "0of3", false}},
		{"error_1valid_nx", "nxdomain", "timeout", "timeout", want{"error", "0of3", false}},
		{"error_0valid", "timeout", "timeout", "timeout", want{"error", "0of3", false}},
		{"nonroutable_reduces_to_empty", "loopback", "empty", "empty", want{"empty", "3of3", false}},
	}

	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			h.script(tc.cf, tc.go_, tc.q9)
			ans, err := h.r.LookupAAAA(context.Background(), "probe.example")

			switch tc.want.result {
			case "inconsistent":
				if !errors.Is(err, checker.ErrQuorumInconsistent) {
					t.Fatalf("err = %v, want ErrQuorumInconsistent", err)
				}
			case "error":
				if err == nil || errors.Is(err, checker.ErrQuorumInconsistent) {
					t.Fatalf("err = %v, want plain error", err)
				}
			case "exists":
				if err != nil || len(ans.IPs) == 0 {
					t.Fatalf("ans=%+v err=%v, want exists", ans, err)
				}
			case "empty":
				if err != nil || len(ans.IPs) != 0 || ans.Rcode != "NOERROR" {
					t.Fatalf("ans=%+v err=%v, want NOERROR-empty", ans, err)
				}
			case "nxdomain":
				if err != nil || ans.Rcode != "NXDOMAIN" {
					t.Fatalf("ans=%+v err=%v, want NXDOMAIN", ans, err)
				}
			}
			if ans.Quorum == nil {
				t.Fatal("QuorumInfo missing")
			}
			if ans.Quorum.Agreement != tc.want.agreement {
				t.Errorf("agreement = %s, want %s", ans.Quorum.Agreement, tc.want.agreement)
			}
			if tc.want.result == "exists" || tc.want.result == "empty" || tc.want.result == "nxdomain" {
				if ans.Quorum.Disagreed != tc.want.disagreed {
					t.Errorf("disagreed = %t, want %t", ans.Quorum.Disagreed, tc.want.disagreed)
				}
			}
		})
	}
}

// TestNonAnswerClassification: SERVFAIL/REFUSED never classify as empty and
// the raw rcodes are observable in QuorumInfo (10-testing §3.1 assertions).
func TestNonAnswerClassification(t *testing.T) {
	h := newHarness(t)
	h.script("empty", "empty", "servfail")
	ans, err := h.r.LookupAAAA(context.Background(), "probe.example")
	if err != nil {
		t.Fatal(err)
	}
	if got := ans.Quorum.PerResolver["quad9"]; got != "error" {
		t.Errorf("quad9 symbol = %s, want error (SERVFAIL is never a vote)", got)
	}
	if got := ans.Quorum.Rcodes["quad9"]; got != "SERVFAIL" {
		t.Errorf("quad9 rcode = %q, want SERVFAIL", got)
	}

	h.script("empty", "empty", "refused")
	ans, err = h.r.LookupAAAA(context.Background(), "probe.example")
	if err != nil {
		t.Fatal(err)
	}
	if got := ans.Quorum.PerResolver["quad9"]; got != "error" {
		t.Errorf("quad9 symbol = %s, want error (REFUSED is never empty)", got)
	}
	if got := ans.Quorum.Rcodes["quad9"]; got != "REFUSED" {
		t.Errorf("quad9 rcode = %q, want REFUSED", got)
	}

	// Timeout keeps a split symbol + empty rcode for diagnostics.
	h.script("exists", "exists", "timeout")
	ans, err = h.r.LookupAAAA(context.Background(), "probe.example")
	if err != nil {
		t.Fatal(err)
	}
	if got := ans.Quorum.PerResolver["quad9"]; got != "timeout" {
		t.Errorf("quad9 symbol = %s, want timeout (kept split from error)", got)
	}
	if got := ans.Quorum.Rcodes["quad9"]; got != "" {
		t.Errorf("quad9 rcode = %q, want empty (no DNS response)", got)
	}
}

// TestQuorumByteIdentical: on a 2-1 exists split with different record sets,
// the returned answer equals the FIRST agreeing provider's set exactly —
// never a merge (10-testing §3.1).
func TestQuorumByteIdentical(t *testing.T) {
	h := newHarness(t)
	h.script("exists", "exists2", "empty")
	ans, err := h.r.LookupAAAA(context.Background(), "probe.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(ans.IPs) != 2 || ans.IPs[0].String() != "2001:db8::1" || ans.IPs[1].String() != "2001:db8::2" {
		t.Errorf("answer = %v, want cloudflare's exact 2-record set", ans.IPs)
	}
}

// TestConditionalALookup is 10-testing §3.2's table. The "A query issued?"
// column is the point: the bulk A lookup runs iff the AAAA quorum symbol is
// `empty`, so every non-empty quorum asserts a zero counter. Without that,
// moving the guard so classifyA also runs on nxdomain quorums costs an
// extra bulk query per dead domain and no test notices.
func TestConditionalALookup(t *testing.T) {
	h := newHarness(t)

	rows := []struct {
		name        string
		cf, go_, q9 string // the AAAA quorum
		a           string // the scripted bulk A answer
		wantOutcome string
		wantACalls  int // wire queries; 2 where QueryWithRetry retries
	}{
		{"a_present", "empty", "empty", "empty", "a_present", "a_present", 1},
		{"a_absent_noerror", "empty", "empty", "empty", "empty", "a_absent", 1},
		{"a_absent_nxdomain", "empty", "empty", "empty", "nxdomain", "a_absent", 1},
		{"a_error_servfail", "empty", "empty", "empty", "servfail", "a_error", 2},
		{"a_error_timeout", "empty", "empty", "empty", "timeout", "a_error", 2},
		{"no_a_on_exists", "exists", "exists", "exists", "a_present", "", 0},
		{"no_a_on_nxdomain", "nxdomain", "nxdomain", "nxdomain", "a_present", "", 0},
		{"no_a_on_error", "timeout", "timeout", "timeout", "a_present", "", 0},
		{"no_a_on_inconsistent", "exists", "empty", "nxdomain", "a_present", "", 0},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			h.script(tc.cf, tc.go_, tc.q9)
			h.bulk.set(tc.a)
			h.bulk.resetCounts()
			ans, _ := h.r.LookupAAAA(context.Background(), "probe.example")
			if ans.AOutcome != tc.wantOutcome {
				t.Errorf("AOutcome = %q, want %q", ans.AOutcome, tc.wantOutcome)
			}
			if a, cd, _ := h.bulk.counts(); a != tc.wantACalls || cd != 0 {
				t.Errorf("bulk calls = %d A / %d CD, want %d A / 0 CD", a, cd, tc.wantACalls)
			}
		})
	}
}

// TestConditionalCDLookup is 10-testing §3.2b's table (02 §2.7b). The CD=1
// re-query runs iff no quorum was reached AND at least two providers
// returned an explicit SERVFAIL/REFUSED with no other rcode among the
// answers. A timeout does not disqualify, it just does not count toward the
// two; a reached quorum never gets there. Counters assert the "CD=1
// issued?" column, including the +1 A on the cd_empty rows.
func TestConditionalCDLookup(t *testing.T) {
	h := newHarness(t)

	rows := []struct {
		name        string
		cf, go_, q9 string
		cd          string // the scripted bulk answer, used for both CD=1 and A
		wantCD      string
		wantA       string
		wantErr     string // "", "plain", "inconsistent"
		wantCDCalls int
		wantACalls  int
	}{
		{"cd_present", "servfail", "servfail", "servfail", "exists", "cd_present", "", "", 1, 0},
		{"cd_present_refused", "refused", "servfail", "refused", "exists", "cd_present", "", "", 1, 0},
		{"cd_empty_apresent", "servfail", "servfail", "servfail", "a_present", "cd_empty", "a_present", "", 1, 1},
		{"cd_empty_aabsent", "servfail", "servfail", "servfail", "empty", "cd_empty", "a_absent", "", 1, 1},
		{"cd_fail_servfail", "servfail", "servfail", "servfail", "servfail", "cd_fail", "", "plain", 2, 0},
		// The two-answer rule (review issue 16, 02 §2.7b erratum). One
		// SERVFAIL among timeouts is one provider's hiccup, not a broken
		// zone: no rescue, and the lookup stays a non-definitive error.
		// This is 10-testing §3.2b's cd_notrun_timeout row.
		{"cd_notrun_timeout", "timeout", "timeout", "servfail", "exists", "", "", "plain", 0, 0},
		// Two explicit answers agree, the third is silent → rescue runs.
		{"cd_runs_on_two_servfail_plus_timeout", "timeout", "servfail", "servfail", "exists", "cd_present", "", "", 1, 0},
		// Two explicit SERVFAIL/REFUSED but a third provider answered
		// NOERROR: some other rcode is present, so the signature is broken
		// and no rescue runs. One valid answer is short of quorum, so the
		// lookup is a plain error — no CD=1, and no conditional A either.
		{"cd_notrun_mixed_rcode", "refused", "servfail", "empty", "a_present", "", "", "plain", 0, 0},
		{"cd_notrun_exists", "exists", "exists", "servfail", "exists", "", "", "", 0, 0},
		{"cd_notrun_empty", "empty", "empty", "empty", "a_present", "", "a_present", "", 0, 1},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			h.script(tc.cf, tc.go_, tc.q9)
			h.bulk.set(tc.cd)
			h.bulk.resetCounts()
			ans, err := h.r.LookupAAAA(context.Background(), "probe.example")

			switch tc.wantErr {
			case "":
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
			case "plain":
				if err == nil || errors.Is(err, checker.ErrQuorumInconsistent) {
					t.Fatalf("err = %v, want a plain error", err)
				}
			case "inconsistent":
				if !errors.Is(err, checker.ErrQuorumInconsistent) {
					t.Fatalf("err = %v, want ErrQuorumInconsistent", err)
				}
			}
			if ans.CDOutcome != tc.wantCD {
				t.Errorf("CDOutcome = %q, want %q", ans.CDOutcome, tc.wantCD)
			}
			if ans.AOutcome != tc.wantA {
				t.Errorf("AOutcome = %q, want %q", ans.AOutcome, tc.wantA)
			}
			if a, cd, _ := h.bulk.counts(); cd != tc.wantCDCalls || a != tc.wantACalls {
				t.Errorf("bulk calls = %d CD / %d A, want %d CD / %d A",
					cd, a, tc.wantCDCalls, tc.wantACalls)
			}
			if tc.wantCD == "cd_present" && (len(ans.IPs) == 0 || ans.Rcode != "NOERROR") {
				t.Errorf("cd_present must credit a supported base: %+v", ans)
			}
		})
	}
}

// droppedProvider reads the breaker's dropped-provider name under the lock.
func droppedProvider(r *Resolver) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}

// TestBreakers: the fast-lane breaker opens/closes on the sampled rate; the
// provider breaker drops at most one provider and the canary restores it
// (10-testing §3 / 02 §8.5).
func TestBreakers(t *testing.T) {
	h := newHarness(t)

	// Fast lane: min_samples reached with >5% non-definitive (test config
	// shrinks min_samples to 40; production values are exercised by the
	// same code path).
	h.script("exists", "exists", "exists")
	for range 36 {
		_, _ = h.r.LookupAAAA(context.Background(), "ok.example")
	}
	h.script("exists", "empty", "nxdomain") // a non-definitive outcome: no quorum
	for range 4 {
		_, _ = h.r.LookupAAAA(context.Background(), "flappy.example")
	}
	if !h.r.FastLaneSuppressed() {
		t.Error("fast-lane breaker should be open at 10% non-definitive")
	}

	// Provider breaker: quad9 blackholes past min_samples.
	h2 := newHarness(t)
	h2.script("exists", "exists", "timeout")
	for range 25 {
		_, _ = h2.r.LookupAAAA(context.Background(), "q9dark.example")
	}
	h2.r.evaluateBreakers()
	if got := droppedProvider(h2.r); got != "quad9" {
		t.Fatalf("dropped = %q, want quad9", got)
	}

	// A second failing provider is never dropped.
	h2.go_.set("timeout")
	h2.script("exists", "timeout", "timeout")
	for range 25 {
		_, _ = h2.r.LookupAAAA(context.Background(), "g8dark.example")
	}
	h2.r.evaluateBreakers()
	if got := droppedProvider(h2.r); got != "quad9" {
		t.Errorf("dropped = %q after second failure, want quad9 only (never a 2nd)", got)
	}

	// 2-of-2 degraded mode scores all three §3.3 shapes. With one provider
	// out the quorum cannot degrade further, so a disagreement is
	// inconsistent and a single non-answer is a plain error — the two rows
	// that make dropping a provider safe rather than silently lossy.
	h2.go_.set("exists")
	degraded := []struct {
		name    string
		cf, go_ string
		want    string // "exists"|"inconsistent"|"error"
	}{
		{"both agree", "exists", "exists", "exists"},
		{"disagreement has no majority left", "exists", "empty", "inconsistent"},
		{"one non-answer leaves one vote", "exists", "timeout", "error"},
	}
	for _, tc := range degraded {
		t.Run("degraded/"+tc.name, func(t *testing.T) {
			h2.script(tc.cf, tc.go_, "exists") // quad9 is dropped; its script is ignored
			ans, err := h2.r.LookupAAAA(context.Background(), "degraded.example")
			switch tc.want {
			case "exists":
				if err != nil || len(ans.IPs) == 0 {
					t.Fatalf("ans=%+v err=%v, want exists", ans, err)
				}
				if ans.Quorum.Agreement != "2of2" {
					t.Errorf("degraded agreement = %s, want 2of2", ans.Quorum.Agreement)
				}
			case "inconsistent":
				if !errors.Is(err, checker.ErrQuorumInconsistent) {
					t.Fatalf("err = %v, want ErrQuorumInconsistent", err)
				}
			case "error":
				if err == nil || errors.Is(err, checker.ErrQuorumInconsistent) {
					t.Fatalf("err = %v, want a plain error", err)
				}
			}
		})
	}
	h2.script("exists", "exists", "exists")

	// Canary: three consecutive valid probes restore the provider.
	h2.q9.set("exists")
	for range 3 {
		h2.r.runCanary()
	}
	if got := droppedProvider(h2.r); got != "" {
		t.Errorf("dropped = %q after 3 canary passes, want restored", got)
	}
}

// TestRescueFitsTheCheckBudget pins the arithmetic 01 §11.1's Decision
// states. The dns_aaaa_base/www budget has to cover the quorum fan-out plus
// both bulk lookups the §2.7b rescue chains — the CD=1 re-query, then
// classifyA. It did not when §2.7b landed (4s + 10s + 10s against 15s), so a
// slow CD answer starved classifyA into a_error and a cd_empty rescue came
// out as error. This is a budget nobody re-derives by hand; the test does.
func TestRescueFitsTheCheckBudget(t *testing.T) {
	worst := perProviderBudget + 2*checker.BulkQueryBudget
	if checker.AAAACheckTimeout < worst {
		t.Errorf("AAAA check budget %s < rescue worst case %s "+
			"(fan-out %s + CD %s + conditional A %s)",
			checker.AAAACheckTimeout, worst,
			perProviderBudget, checker.BulkQueryBudget, checker.BulkQueryBudget)
	}
}

// TestCloseCancelsInFlightMaintenance: Close must reach the work already
// running on the maintenance goroutine, not just stop the next tick. A
// blackholed ops webhook makes evaluateBreakers sit on up to four sequential
// 10s POSTs; when that coincides with SIGTERM, Close used to block for all
// of them and systemd's TimeoutStopSec killed the crawler mid-shutdown.
func TestCloseCancelsInFlightMaintenance(t *testing.T) {
	h := newHarness(t)

	entered := make(chan struct{})
	alertCtxErr := make(chan error, 1)
	h.r.alert = func(ctx context.Context, _ string) {
		close(entered)
		<-ctx.Done()
		alertCtxErr <- ctx.Err()
	}

	// An open fast lane over an idle window closes on the next evaluation
	// and alerts — the first of the four POSTs.
	h.r.mu.Lock()
	h.r.fastOpen = true
	h.r.mu.Unlock()

	done := make(chan struct{})
	go func() { defer close(done); h.r.evaluateBreakers() }()
	<-entered

	h.r.Close()
	select {
	case err := <-alertCtxErr:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("alert context ended with %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel the in-flight alert")
	}
	<-done

	// The harness Cleanup calls Close again: cancelling twice is fine, where
	// closing the old stop channel twice would have panicked.
}

// TestFastLaneCloses covers the other half of the fast-lane breaker (02
// §2.9): it reopens the fast lane once the rate holds below recover_below.
// breaker.go's close arm had no test at all, so a lane that opened stayed
// open for the life of the process as far as the suite was concerned.
func TestFastLaneCloses(t *testing.T) {
	t.Run("an idle window counts as recovered", func(t *testing.T) {
		h := newHarness(t)
		h.r.mu.Lock()
		h.r.fastOpen = true
		h.r.mu.Unlock()
		h.r.evaluateBreakers() // no samples at all in the window
		if h.r.FastLaneSuppressed() {
			t.Error("fast lane still open over an empty window")
		}
	})

	t.Run("closes once the rate falls below recover_below", func(t *testing.T) {
		h := newHarness(t)
		// Open it: 36 clean + 4 non-definitive is 10% over min_samples 40.
		h.script("exists", "exists", "exists")
		for range 36 {
			_, _ = h.r.LookupAAAA(context.Background(), "ok.example")
		}
		h.script("exists", "empty", "nxdomain")
		for range 4 {
			_, _ = h.r.LookupAAAA(context.Background(), "flappy.example")
		}
		if !h.r.FastLaneSuppressed() {
			t.Fatal("fast lane should be open at 10% non-definitive")
		}

		// The same 4 bad samples are still in the 15-minute window, so the
		// lane only closes once enough clean traffic drags the rate under
		// recover_below (0.02): 4/250 = 1.6%.
		h.script("exists", "exists", "exists")
		for range 210 {
			_, _ = h.r.LookupAAAA(context.Background(), "ok.example")
		}
		h.r.evaluateBreakers()
		if h.r.FastLaneSuppressed() {
			t.Error("fast lane still open at 1.6% non-definitive, below recover_below")
		}
	})
}

// TestPerProviderRateLimit (10-testing §3.3): the token bucket blocks rather
// than erroring. At 1 qps the bucket starts full, so the first fan-out is
// free and the second waits ~1s for a refill — nothing is dropped and no
// lookup fails.
func TestPerProviderRateLimit(t *testing.T) {
	h := newHarnessQPS(t, 1)
	h.script("exists", "exists", "exists")

	start := time.Now()
	for range 2 {
		if _, err := h.r.LookupAAAA(context.Background(), "slow.example"); err != nil {
			t.Fatalf("the limiter must block, never fail the lookup: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
		t.Errorf("two lookups at 1 qps took %s; the second did not wait for a token", elapsed)
	}
}
