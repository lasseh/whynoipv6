package checker

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/miekg/dns"

	"golang.org/x/net/publicsuffix"
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
	r := testRunner(t, newScriptAAAA(), false)
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
		if got.Detail == nil || got.Detail.common().Reason != wantReason {
			t.Errorf("%s reason = %v, want %q", name, got.Detail, wantReason)
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
	seam := newScriptAAAA()
	seam.ans["api.dnb.no"] = AAAAAnswer{Rcode: "NOERROR"} // NOERROR-empty
	r := testRunner(t, seam, false)

	res := r.Run(context.Background(), "api.dnb.no", KindSubdomain)

	www := res.Results["dns_aaaa_www"]
	if www.Status != StatusNotApplicable {
		t.Errorf("www = %s, want not_applicable", www.Status)
	}
	if www.Detail == nil || www.Detail.common().Reason != "subdomain entity: www check not applicable" {
		t.Errorf("www reason = %v", www.Detail)
	}
	if n := seam.callCount("www.api.dnb.no"); n != 0 {
		t.Errorf("www AAAA queried %d times, want 0", n)
	}

	mx := res.Results["dns_mx_ipv6"]
	if mx.Status != StatusNotApplicable {
		t.Errorf("mx = %s, want not_applicable", mx.Status)
	}
	if _, mxD, ok := res.MX(); !ok || mxD.Reason != "no explicit MX records (subdomain entity)" {
		t.Errorf("mx reason = %v (implicit-MX fallback must be skipped)", mxD)
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
	r := testRunner(t, newScriptAAAA(), false)
	r.Register(panicChecker{})

	res := r.Run(context.Background(), "v4only.example", KindApex)

	p := res.Results["panic_check"]
	if p.Status != StatusError {
		t.Fatalf("panic check = %s, want error", p.Status)
	}
	if p.Detail == nil || !strings.Contains(p.Detail.common().Error, "internal error: boom") {
		t.Errorf("panic detail = %v", p.Detail)
	}
	if res.Results["dns_aaaa_base"].Status != StatusUnsupported {
		t.Errorf("sibling check affected by panic: %v", res.Results["dns_aaaa_base"])
	}
}

// TestHTTPErrorTypes (01 §14.6): the terminal classification helpers produce
// the exact error_type inputs for refused / timeout / bad-cert / other.
func TestHTTPErrorTypes(t *testing.T) {
	// The realistic dial chain: *net.OpError → *os.SyscallError → syscall.Errno.
	refused := &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
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
	// A plaintext listener on the TLS port: crypto/tls returns the
	// RecordHeaderError by value, and net/http substitutes ErrSchemeMismatch
	// when the record starts with "HTTP/". Both arrive wrapped in *url.Error.
	plain := &url.Error{Op: "Get", URL: "https://example.com/", Err: tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}}
	if !isTLSError(plain) {
		t.Error("tls.RecordHeaderError (value) not classified certificate_error")
	}
	mismatch := &url.Error{Op: "Get", URL: "https://example.com/", Err: http.ErrSchemeMismatch}
	if !isTLSError(mismatch) {
		t.Error("http.ErrSchemeMismatch not classified certificate_error")
	}
	if isConnRefused(errors.New("other")) || isTimeout(errors.New("other")) || isTLSError(errors.New("other")) {
		t.Error("generic error must classify as unknown")
	}
}

// TestResourceDiscovery (01 §14.7): full deduped external-host list,
// first-seen order, <base href> honored, own host + subdomains excluded,
// data:/javascript: ignored.
//
// Review issue 21 (01 §11.9 erratum) adds the rel filter and the two
// sources that were being missed: only fetched <link> rels count, and
// srcset/poster targets do.
func TestResourceDiscovery(t *testing.T) {
	page := `<!doctype html><html><head>
<base href="https://cdn.base.example/assets/">
<link href="style.css" rel="stylesheet">
<link href="https://fonts.example/font.woff2" rel="preload">
<link rel="canonical" href="https://canonical.example/">
<link rel="alternate" hreflang="sv" href="https://sibling.example/">
<link rel="alternate" type="application/rss+xml" href="https://feeds.example/rss">
<link rel="dns-prefetch" href="https://dnshint.example/">
<link rel="preconnect" href="https://preconnect.example/">
<link rel="me" href="https://social.example/@me">
<link href="https://norel.example/x.css">
<link rel="shortcut icon" href="https://icons.example/favicon.ico">
<script src="https://js.example/app.js"></script>
<script src="https://js.example/app2.js"></script>
<script src="data:text/javascript,void(0)"></script>
<img src="javascript:alert(1)">
<img src="//img.example/logo.png">
<img src="https://own.example/self.png">
<img src="https://static.own.example/sub.png">
<img srcset="https://srcset.example/a.png 1x, https://srcset2.example/b.png 2x">
<video poster="https://poster.example/still.jpg"></video>
</head><body></body></html>`

	pageURL, _ := url.Parse("https://own.example/")
	hosts := extractExternalHosts([]byte(page), pageURL, "own.example")

	want := []string{
		"cdn.base.example", "fonts.example", "icons.example", "js.example",
		"img.example", "srcset.example", "srcset2.example", "poster.example",
	}
	if len(hosts) != len(want) {
		t.Fatalf("hosts = %v, want %v", hosts, want)
	}
	for i := range want {
		if hosts[i] != want[i] {
			t.Errorf("hosts[%d] = %s, want %s (first-seen order)", i, hosts[i], want[i])
		}
	}
	// Named explicitly: each of these once made a domain resources_v4only
	// for a host the rendered page never fetches.
	for _, excluded := range []string{
		"canonical.example", "sibling.example", "feeds.example",
		"dnshint.example", "preconnect.example", "social.example", "norel.example",
	} {
		if slices.Contains(hosts, excluded) {
			t.Errorf("%s is not a render-time dependency but was discovered", excluded)
		}
	}
}

// TestResourceDiscoverySchemeFilter (review issue 30): only what a browser
// fetches over the network counts. The old denylist named data: and
// javascript: and let everything else through; an allowlist does not need
// extending for the next scheme.
func TestResourceDiscoverySchemeFilter(t *testing.T) {
	page := `<html><head>
<script src="https://ok-https.example/a.js"></script>
<script src="http://ok-http.example/b.js"></script>
<img src="//protocol-relative.example/c.png">
<img src="data:image/png;base64,AAAA">
<img src="javascript:alert(1)">
<a-tag><img src="mailto:someone@mail.example"></a-tag>
<img src="tel:+4712345678">
<img src="blob:https://blob.example/1234">
<iframe src="ws://socket.example/live"></iframe>
<iframe src="chrome-extension://abcdef/panel.html"></iframe>
<iframe src="ftp://files.example/x.zip"></iframe>
</head><body></body></html>`

	pageURL, _ := url.Parse("https://own.example/")
	hosts := extractExternalHosts([]byte(page), pageURL, "own.example")

	want := []string{"ok-https.example", "ok-http.example", "protocol-relative.example"}
	if len(hosts) != len(want) {
		t.Fatalf("hosts = %v, want exactly %v", hosts, want)
	}
	for i := range want {
		if hosts[i] != want[i] {
			t.Errorf("hosts[%d] = %s, want %s", i, hosts[i], want[i])
		}
	}
}

// TestResourceDiscoveryPerSiteCap (review issue 30): one registrable domain
// cannot fill the whole required-host budget. A page that shards assets, or
// a tracker minting a per-visit subdomain, otherwise crowds out genuinely
// distinct dependencies and mints a registry row per sighting.
func TestResourceDiscoveryPerSiteCap(t *testing.T) {
	var b strings.Builder
	b.WriteString("<html><head>")
	for i := range 9 {
		fmt.Fprintf(&b, `<script src="https://shard%d.cdn.example/app.js"></script>`, i)
	}
	// A second registrable domain gets its own budget, and co.uk is a
	// multi-label suffix — the cap must key on example.co.uk, not co.uk.
	for i := range 9 {
		fmt.Fprintf(&b, `<script src="https://n%d.other.co.uk/x.js"></script>`, i)
	}
	b.WriteString(`<script src="https://only.one.example/z.js"></script>`)
	b.WriteString("</head></html>")

	pageURL, _ := url.Parse("https://own.example/")
	hosts := extractExternalHosts([]byte(b.String()), pageURL, "own.example")

	perSite := map[string]int{}
	for _, h := range hosts {
		site, err := publicsuffix.EffectiveTLDPlusOne(h)
		if err != nil {
			t.Fatalf("%s: %v", h, err)
		}
		perSite[site]++
	}
	for site, n := range perSite {
		if n > maxHostsPerSite {
			t.Errorf("%s contributed %d hosts, cap is %d", site, n, maxHostsPerSite)
		}
	}
	if perSite["cdn.example"] != maxHostsPerSite {
		t.Errorf("cdn.example = %d hosts, want the full %d", perSite["cdn.example"], maxHostsPerSite)
	}
	if perSite["other.co.uk"] != maxHostsPerSite {
		t.Errorf("other.co.uk = %d hosts, want its own %d — the cap must key on the "+
			"registrable domain, not the public suffix", perSite["other.co.uk"], maxHostsPerSite)
	}
	if perSite["one.example"] != 1 {
		t.Errorf("one.example = %d, want 1: a small site must not be starved by a shardy one",
			perSite["one.example"])
	}
}

// TestPreflightFreshness (01 §14.9): the freshness window closes exactly
// PreflightFreshness after the last pass with no probes in between.
//
// Asserted through LastPass, which is the production gate: the mapper
// compares its preflightPassedAt input against preflightFreshness itself.
// PassedWithin used to wrap this and nothing but this test called it
// (review issue 48, 01 §12 erratum).
func TestPreflightFreshness(t *testing.T) {
	fresh := func(p *Preflight) bool {
		last := p.LastPass()
		return !last.IsZero() && time.Since(last) < PreflightFreshness
	}

	p := &Preflight{logger: slog.Default()}
	if fresh(p) {
		t.Error("never-passed preflight reports fresh")
	}
	if !p.LastPass().IsZero() {
		t.Error("LastPass should be zero before any pass")
	}

	p.lastPass.Store(time.Now().Add(-PreflightFreshness + 2*time.Second).UnixNano())
	if !fresh(p) {
		t.Error("pass 4m58s ago should be fresh")
	}
	p.lastPass.Store(time.Now().Add(-PreflightFreshness - time.Second).UnixNano())
	if fresh(p) {
		t.Error("pass 5m01s ago should be stale")
	}
}

// neverRanChecker records whether Check was reached.
type neverRanChecker struct{ ran *bool }

func (neverRanChecker) Name() string { return "never_ran" }
func (c neverRanChecker) Check(context.Context, string, Kind) (Result, error) {
	*c.ran = true
	return Result{Status: StatusSupported}, nil
}

// TestStarvedCheckIsLogged is review issue 68, which asked which of the nine
// phase-2 checks starves at the 90s domainTimeout under concurrencyLimit=6.
//
// The arithmetic says none can. Worst case every check runs to its own
// declared timeout: phase 1 is 6 checks under a limit of 6 — 15/15/25/30/20/5s
// all in parallel, so 30s — and phase 2 is 9 under 6, durations
// 30/30/30/20/15/15/10/10/10. The worst ordering starts the three 30s checks
// last, at t=10 when the first short slot frees, finishing at 40s. 30 + 40 =
// 70s against a 90s budget.
//
// So this is a guard, not a fix. The runner already detected the condition
// and said nothing: it stored "scan cancelled" and moved on, and the
// scan_detail payload keeps only the status, so nothing downstream could name
// the check that lost. If the arithmetic is ever wrong — a per-check timeout
// raised, a tenth phase-2 check added — production now says which one.
func TestStarvedCheckIsLogged(t *testing.T) {
	var buf bytes.Buffer
	r := &Runner{logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the domain budget expired while this check waited for a slot

	ran := false
	var results sync.Map
	r.runCheck(ctx, "starved.example", KindApex, neverRanChecker{ran: &ran}, &results)

	if ran {
		t.Error("a check whose context is already done must not run")
	}
	got, ok := results.Load("never_ran")
	if !ok {
		t.Fatal("no result stored for the starved check")
	}
	res, ok := got.(Result)
	if !ok || res.Status != StatusError {
		t.Errorf("starved check = %+v, want error", got)
	}

	logged := buf.String()
	if !strings.Contains(logged, "check starved") || !strings.Contains(logged, "never_ran") {
		t.Errorf("starvation logged %q, want a warn naming the check", logged)
	}
}
