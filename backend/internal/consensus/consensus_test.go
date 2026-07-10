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
}

func (f *fakeProvider) set(b string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.behavior = b
}

func (f *fakeProvider) handle(w dns.ResponseWriter, r *dns.Msg) {
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
	a := func(ip string) *dns.A {
		return &dns.A{
			Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   net.ParseIP(ip),
		}
	}
	switch b {
	case "exists":
		if q.Qtype == dns.TypeAAAA {
			m.Answer = append(m.Answer, aaaa("2001:db8::1"), aaaa("2001:db8::2"))
		} else if q.Qtype == dns.TypeA {
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
	perProviderBudget = 150 * time.Millisecond // shrink timeout rows
	t.Cleanup(func() { perProviderBudget = 4 * time.Second })

	h := &harness{cf: startFake(t), go_: startFake(t), q9: startFake(t), bulk: startFake(t)}
	var alerts []string
	var mu sync.Mutex
	h.alerts = &alerts
	h.r = New(Config{
		PerProviderQPS: 1000,
		FastLane:       FastLaneConfig{NondefinitiveRate: 0.05, Window: 15 * time.Minute, MinSamples: 40, RecoverBelow: 0.02},
		Provider:       ProviderConfig{FailureRate: 0.50, Window: 15 * time.Minute, MinSamples: 20, RecoveryProbes: 3},
	}, checker.NewResolver([]string{h.bulk.addr}),
		func(_ context.Context, msg string) { mu.Lock(); alerts = append(alerts, msg); mu.Unlock() },
		slog.Default())
	t.Cleanup(h.r.Close)

	// Point the pinned providers at the fakes (in-package test override).
	h.r.providers[0].res = checker.NewResolver([]string{h.cf.addr})
	h.r.providers[1].res = checker.NewResolver([]string{h.go_.addr})
	h.r.providers[2].res = checker.NewResolver([]string{h.q9.addr})
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

// TestConditionalALookup (10-testing §3.2): the A lookup fires only on a
// NOERROR-empty quorum, classifying via the bulk resolver.
func TestConditionalALookup(t *testing.T) {
	h := newHarness(t)

	// a_present: bulk answers an A record.
	h.bulk.set("a_present")
	h.script("empty", "empty", "empty")
	ans, err := h.r.LookupAAAA(context.Background(), "probe.example")
	if err != nil {
		t.Fatal(err)
	}
	if ans.AOutcome != "a_present" {
		t.Errorf("AOutcome = %q, want a_present", ans.AOutcome)
	}

	h.bulk.set("empty")
	ans, _ = h.r.LookupAAAA(context.Background(), "probe.example")
	if ans.AOutcome != "a_absent" {
		t.Errorf("AOutcome = %q, want a_absent", ans.AOutcome)
	}

	h.bulk.set("nxdomain")
	ans, _ = h.r.LookupAAAA(context.Background(), "probe.example")
	if ans.AOutcome != "a_absent" {
		t.Errorf("A-NXDOMAIN AOutcome = %q, want a_absent (domain's favor)", ans.AOutcome)
	}

	h.bulk.set("servfail")
	ans, _ = h.r.LookupAAAA(context.Background(), "probe.example")
	if ans.AOutcome != "a_error" {
		t.Errorf("A-SERVFAIL AOutcome = %q, want a_error", ans.AOutcome)
	}

	// No A lookup on an exists quorum.
	h.script("exists", "exists", "exists")
	ans, _ = h.r.LookupAAAA(context.Background(), "probe.example")
	if ans.AOutcome != "" {
		t.Errorf("AOutcome = %q on exists quorum, want empty", ans.AOutcome)
	}
}

// TestConditionalCDLookup (10-testing / 02 §2.7b): all-SERVFAIL triggers the
// CD=1 rescue with cd_present / cd_empty / cd_fail shapes.
func TestConditionalCDLookup(t *testing.T) {
	h := newHarness(t)
	h.script("servfail", "servfail", "servfail")

	// cd_present: bulk (CD=1) answers routable AAAA.
	h.bulk.set("exists")
	ans, err := h.r.LookupAAAA(context.Background(), "broken-dnssec.example")
	if err != nil {
		t.Fatalf("cd_present must return nil error, got %v", err)
	}
	if ans.CDOutcome != "cd_present" || len(ans.IPs) == 0 || ans.Rcode != "NOERROR" {
		t.Errorf("cd_present shape = %+v", ans)
	}

	// cd_empty: NOERROR no AAAA → conditional A lookup runs.
	h.bulk.set("empty")
	ans, err = h.r.LookupAAAA(context.Background(), "broken-dnssec.example")
	if err != nil {
		t.Fatalf("cd_empty must return nil error, got %v", err)
	}
	if ans.CDOutcome != "cd_empty" || ans.AOutcome != "a_absent" {
		t.Errorf("cd_empty shape = %+v", ans)
	}

	// cd_fail: bulk also SERVFAILs → plain error survives.
	h.bulk.set("servfail")
	ans, err = h.r.LookupAAAA(context.Background(), "broken-dnssec.example")
	if err == nil || errors.Is(err, checker.ErrQuorumInconsistent) {
		t.Fatalf("cd_fail must keep the plain error, got %v", err)
	}
	if ans.CDOutcome != "cd_fail" {
		t.Errorf("CDOutcome = %q, want cd_fail", ans.CDOutcome)
	}

	// Timeouts do NOT qualify for the rescue.
	h.script("timeout", "timeout", "timeout")
	h.bulk.set("exists")
	ans, err = h.r.LookupAAAA(context.Background(), "dark.example")
	if err == nil {
		t.Fatal("all-timeout must stay a plain error (no CD rescue)")
	}
	if ans.CDOutcome != "" {
		t.Errorf("CDOutcome = %q on all-timeout, want empty", ans.CDOutcome)
	}
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
	h.script("exists", "empty", "nxdomain") // inconsistent = non-definitive
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
	if got := h2.r.DroppedProvider(); got != "quad9" {
		t.Fatalf("dropped = %q, want quad9", got)
	}

	// A second failing provider is never dropped.
	h2.go_.set("timeout")
	h2.script("exists", "timeout", "timeout")
	for range 25 {
		_, _ = h2.r.LookupAAAA(context.Background(), "g8dark.example")
	}
	h2.r.evaluateBreakers()
	if got := h2.r.DroppedProvider(); got != "quad9" {
		t.Errorf("dropped = %q after second failure, want quad9 only (never a 2nd)", got)
	}

	// 2-of-2 degraded mode: agreement string is 2of2.
	h2.go_.set("exists")
	h2.script("exists", "exists", "exists")
	ans, err := h2.r.LookupAAAA(context.Background(), "degraded.example")
	if err != nil {
		t.Fatal(err)
	}
	if ans.Quorum.Agreement != "2of2" {
		t.Errorf("degraded agreement = %s, want 2of2", ans.Quorum.Agreement)
	}

	// Canary: three consecutive valid probes restore the provider.
	h2.q9.set("exists")
	for range 3 {
		h2.r.runCanary()
	}
	if got := h2.r.DroppedProvider(); got != "" {
		t.Errorf("dropped = %q after 3 canary passes, want restored", got)
	}
}
