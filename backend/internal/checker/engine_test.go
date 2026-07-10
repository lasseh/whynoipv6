package checker

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// startFakeDNS runs a scripted DNS server on 127.0.0.1:0 (UDP) and returns
// its address. The default handler answers NOERROR with no records.
func startFakeDNS(t *testing.T, handler dns.HandlerFunc) string {
	t.Helper()
	lc := &net.ListenConfig{}
	pc, err := lc.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &dns.Server{PacketConn: pc, Handler: handler}
	go func() { _ = srv.ActivateAndServe() }()
	t.Cleanup(func() { _ = srv.Shutdown() })
	return pc.LocalAddr().String()
}

func emptyNoError(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	_ = w.WriteMsg(m)
}

// fakeSeam scripts the consensus AAAAResolver and counts lookups per name.
type fakeSeam struct {
	mu      sync.Mutex
	answers map[string]AAAAAnswer
	calls   map[string]int
}

func newFakeSeam() *fakeSeam {
	return &fakeSeam{answers: map[string]AAAAAnswer{}, calls: map[string]int{}}
}

func (f *fakeSeam) LookupAAAA(_ context.Context, name string) (AAAAAnswer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[name]++
	if a, ok := f.answers[name]; ok {
		return a, nil
	}
	return AAAAAnswer{Rcode: "NOERROR"}, nil // NOERROR-empty
}

func (f *fakeSeam) callCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

func testRunner(t *testing.T, seam AAAAResolver, resources bool) *Runner {
	t.Helper()
	addr := startFakeDNS(t, emptyNoError)
	dialer := NewSafeDialer(NewResolver([]string{addr}))
	return NewRunner(Config{MaxNSLookups: 4, MaxMXLookups: 5, EnableResourceDiscovery: resources},
		seam, dialer, slog.Default())
}

// TestRunnerNoAAAA (01 §14.3): a host with no AAAA anywhere runs network I/O
// for phase-1 only; all eight phase-2 checks land not_applicable with the
// exact skip reasons, latency_ipv4 among them.
func TestRunnerNoAAAA(t *testing.T) {
	r := testRunner(t, newFakeSeam(), false)
	res := r.Run(context.Background(), "v4only.example", KindApex)

	if len(res.Results) != 14 {
		t.Errorf("results = %d checks, want 14 (resource_discovery not registered)", len(res.Results))
	}
	naWithReason := map[string]string{
		"http_ipv6":            reasonNoAAAARecord,
		"https_ipv6":           reasonNoAAAARecord,
		"tls_ipv6":             reasonNoAAAARecord,
		"http_response_parity": reasonNoAAAARecord,
		"latency_ipv4":         reasonNoAAAARecord,
		"latency_ipv6":         reasonNoAAAARecord,
		"dns_ptr_ipv6":         reasonNoAAAARecord,
		"smtp_ipv6":            reasonNoMXWithAAAA,
	}
	for name, wantReason := range naWithReason {
		got, ok := res.Results[name]
		if !ok {
			t.Errorf("%s missing from results", name)
			continue
		}
		if got.Status != StatusNotApplicable {
			t.Errorf("%s = %s, want not_applicable", name, got.Status)
		}
		if reason := got.Details["reason"]; reason != wantReason {
			t.Errorf("%s reason = %v, want %q", name, reason, wantReason)
		}
		if got.Latency != 0 {
			t.Errorf("%s latency = %v, want 0 (skipped, no I/O)", name, got.Latency)
		}
	}
	if res.Results["dns_aaaa_base"].Status != StatusUnsupported {
		t.Errorf("dns_aaaa_base = %s, want unsupported (NOERROR-empty)", res.Results["dns_aaaa_base"].Status)
	}
}

// TestRunnerSubdomain (01 §14.4): dns_aaaa_www is forced not_applicable with
// no DNS query; a no-MX subdomain yields not_applicable without the
// implicit-MX fallback.
func TestRunnerSubdomain(t *testing.T) {
	seam := newFakeSeam()
	seam.answers["api.dnb.no"] = AAAAAnswer{Rcode: "NOERROR"} // NOERROR-empty
	r := testRunner(t, seam, false)

	res := r.Run(context.Background(), "api.dnb.no", KindSubdomain)

	www := res.Results["dns_aaaa_www"]
	if www.Status != StatusNotApplicable {
		t.Errorf("www = %s, want not_applicable", www.Status)
	}
	if got := www.Details["reason"]; got != "subdomain entity: www check not applicable" {
		t.Errorf("www reason = %v", got)
	}
	if n := seam.callCount("www.api.dnb.no"); n != 0 {
		t.Errorf("www AAAA queried %d times, want 0", n)
	}

	mx := res.Results["dns_mx_ipv6"]
	if mx.Status != StatusNotApplicable {
		t.Errorf("mx = %s, want not_applicable", mx.Status)
	}
	if got := mx.Details["reason"]; got != "no explicit MX records (subdomain entity)" {
		t.Errorf("mx reason = %v (implicit-MX fallback must be skipped)", got)
	}
}

// panicChecker always panics.
type panicChecker struct{}

func (panicChecker) Name() string { return "panic_check" }
func (panicChecker) Check(context.Context, string, Kind) (Result, error) {
	panic("boom")
}

// TestCheckPanicIsolation (01 §14.5): a panicking check yields error for
// itself; every other check is unaffected and the process survives.
func TestCheckPanicIsolation(t *testing.T) {
	r := testRunner(t, newFakeSeam(), false)
	r.Register(panicChecker{})

	res := r.Run(context.Background(), "v4only.example", KindApex)

	p := res.Results["panic_check"]
	if p.Status != StatusError {
		t.Fatalf("panic check = %s, want error", p.Status)
	}
	if msg, _ := p.Details["error"].(string); !strings.Contains(msg, "internal error: boom") {
		t.Errorf("panic detail = %v", p.Details)
	}
	if res.Results["dns_aaaa_base"].Status != StatusUnsupported {
		t.Errorf("sibling check affected by panic: %v", res.Results["dns_aaaa_base"])
	}
}

// TestHTTPErrorTypes (01 §14.6): the terminal classification helpers produce
// the exact error_type inputs for refused / timeout / bad-cert / other.
func TestHTTPErrorTypes(t *testing.T) {
	refused := &net.OpError{Op: "dial", Err: func() *syscall.Errno { e := syscall.ECONNREFUSED; return &e }()}
	if !isConnRefused(refused) {
		t.Error("ECONNREFUSED not classified connection_refused")
	}
	if !isTimeout(context.DeadlineExceeded) {
		t.Error("DeadlineExceeded not classified timeout")
	}
	certErr := &tls.CertificateVerificationError{Err: errors.New("x509: certificate signed by unknown authority")}
	if !isTLSError(certErr) {
		t.Error("CertificateVerificationError not classified certificate_error")
	}
	if isConnRefused(errors.New("other")) || isTimeout(errors.New("other")) || isTLSError(errors.New("other")) {
		t.Error("generic error must classify as unknown")
	}
}

// TestResourceDiscovery (01 §14.7): full deduped external-host list,
// first-seen order, <base href> honored, own host + subdomains excluded,
// data:/javascript: ignored.
func TestResourceDiscovery(t *testing.T) {
	page := `<!doctype html><html><head>
<base href="https://cdn.base.example/assets/">
<link href="style.css" rel="stylesheet">
<link href="https://fonts.example/font.woff2" rel="preload">
<script src="https://js.example/app.js"></script>
<script src="https://js.example/app2.js"></script>
<script src="data:text/javascript,void(0)"></script>
<img src="javascript:alert(1)">
<img src="//img.example/logo.png">
<img src="https://own.example/self.png">
<img src="https://static.own.example/sub.png">
</head><body></body></html>`

	pageURL, _ := url.Parse("https://own.example/")
	hosts := extractExternalHosts([]byte(page), pageURL, "own.example")

	want := []string{"cdn.base.example", "fonts.example", "js.example", "img.example"}
	if len(hosts) != len(want) {
		t.Fatalf("hosts = %v, want %v", hosts, want)
	}
	for i := range want {
		if hosts[i] != want[i] {
			t.Errorf("hosts[%d] = %s, want %s (first-seen order)", i, hosts[i], want[i])
		}
	}
}

// TestPreflightFreshness (01 §14.9): PassedWithin flips false exactly
// PreflightFreshness after the last pass with no probes in between.
func TestPreflightFreshness(t *testing.T) {
	p := &Preflight{logger: slog.Default()}
	if p.PassedWithin(PreflightFreshness) {
		t.Error("never-passed preflight reports fresh")
	}
	if !p.LastPass().IsZero() {
		t.Error("LastPass should be zero before any pass")
	}

	p.lastPass.Store(time.Now().Add(-PreflightFreshness + 2*time.Second).UnixNano())
	if !p.PassedWithin(PreflightFreshness) {
		t.Error("pass 4m58s ago should be fresh")
	}
	p.lastPass.Store(time.Now().Add(-PreflightFreshness - time.Second).UnixNano())
	if p.PassedWithin(PreflightFreshness) {
		t.Error("pass 5m01s ago should be stale")
	}
}
