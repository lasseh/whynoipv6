package checker

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These three checks could not be pointed at the loopback harness before:
// they built their transports inline with a hardcoded "443" and no rootCAs,
// so their network paths had no tests at all. Routing them through the probe
// gave them the same port/rootCAs construction state http_ipv6 and
// https_ipv6 already had.

func TestLatencyIPv6MeasuresOverLoopback(t *testing.T) {
	port, roots := newV6TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	c := &LatencyIPv6{dialer: loopbackDialer(t, exampleZone(t)), port: port, rootCAs: roots}

	res, err := c.Check(context.Background(), "example.com", KindApex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusSupported {
		t.Fatalf("status = %s, want supported", res.Status)
	}
	d, ok := res.Detail.(*LatencyDetail)
	if !ok {
		t.Fatalf("detail type = %T", res.Detail)
	}
	if d.Address != "::1" {
		t.Errorf("address = %q, want ::1", d.Address)
	}
	if d.AvgMS == nil {
		t.Fatal("avg_ms is nil")
	}
	// Three requests, highest discarded, remaining two averaged.
	if len(d.Measurements) != 3 {
		t.Errorf("measurements = %d, want 3", len(d.Measurements))
	}
}

func TestLatencyIPv6ConnRefused(t *testing.T) {
	c := &LatencyIPv6{dialer: loopbackDialer(t, exampleZone(t)), port: closedV6Port(t)}

	res, err := c.Check(context.Background(), "example.com", KindApex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status == StatusSupported {
		t.Fatalf("a refused dial reported supported: %+v", res.Detail)
	}
}

func TestResourceDiscoveryFindsExternalHosts(t *testing.T) {
	const page = `<html><head>
	  <link rel="stylesheet" href="https://cdn.example.net/app.css">
	  <script src="https://static.example.org/app.js"></script>
	  <img src="/local.png">
	</head><body></body></html>`
	port, roots := newV6TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	c := &ResourceDiscovery{dialer: loopbackDialer(t, exampleZone(t)), port: port, rootCAs: roots}

	res, err := c.Check(context.Background(), "example.com", KindApex)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := res.Detail.(*ResourceDiscoveryDetail)
	if !ok {
		t.Fatalf("detail type = %T", res.Detail)
	}
	got := strings.Join(d.Hosts, ",")
	for _, want := range []string{"cdn.example.net", "static.example.org"} {
		if !strings.Contains(got, want) {
			t.Errorf("hosts %q missing %s", got, want)
		}
	}
	// Same-origin assets are not external resources.
	if strings.Contains(got, "example.com") {
		t.Errorf("hosts %q includes the page's own domain", got)
	}
}

// siteTLSServer is newV6TLSServer with a certificate valid for every name
// given, not just example.com. The shared httptest certificate carries only
// example.com, so an apex→www hop — which pins SNI to www.example.com —
// cannot verify against it.
func siteTLSServer(t *testing.T, handler http.Handler, names ...string) (port string, roots *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: names[0]},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              names,
		IPAddresses:           []net.IP{net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ln := v6Listener(t)
	srv := &httptest.Server{
		Listener: ln,
		Config:   &http.Server{Handler: handler},
		TLS:      &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	roots = x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	return lnPort(t, ln), roots
}

// TestResourceDiscoveryFollowsApexToWWW is review issue 01's finding 09-0.
// An apex that 301s to www is the dominant apex configuration; refusing to
// follow it left discovery parsing the 3xx boilerplate, reporting zero
// hosts, and folding a vacuous not_applicable that unlocked saint and
// ipv6_only over a page never read.
func TestResourceDiscoveryFollowsApexToWWW(t *testing.T) {
	const page = `<html><head>
	  <link rel="stylesheet" href="https://cdn.example.net/app.css">
	  <script src="https://static.example.org/app.js"></script>
	</head><body></body></html>`
	port, roots := siteTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(hostOnly(r.Host), "example.com") {
			http.Redirect(w, r, "https://www.example.com/", http.StatusMovedPermanently)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}), "example.com", "www.example.com")

	z := newZone(t,
		"example.com. 3600 IN AAAA ::1",
		"www.example.com. 3600 IN AAAA ::1",
	)
	c := &ResourceDiscovery{dialer: loopbackDialer(t, z), port: port, rootCAs: roots}

	res, err := c.Check(context.Background(), "example.com", KindApex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusSupported {
		t.Fatalf("status = %s, want supported: %+v", res.Status, res.Detail)
	}
	d, ok := res.Detail.(*ResourceDiscoveryDetail)
	if !ok {
		t.Fatalf("detail type = %T", res.Detail)
	}
	got := strings.Join(d.Hosts, ",")
	for _, want := range []string{"cdn.example.net", "static.example.org"} {
		if !strings.Contains(got, want) {
			t.Errorf("hosts %q missing %s — the www body was never parsed", got, want)
		}
	}
}

// TestResourceDiscoveryKeepsCrossSiteRedirects: the in-scope rule is the
// apex and its subdomains, not "any redirect". A hop to another site would
// be fetched on the wrong vhost, so the last response stands and discovery
// reports what the apex itself served.
func TestResourceDiscoveryKeepsCrossSiteRedirects(t *testing.T) {
	port, roots := siteTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(hostOnly(r.Host), "example.com") {
			http.Redirect(w, r, "https://elsewhere.example/", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><script src="https://leaked.example/x.js"></script></head></html>`))
	}), "example.com", "elsewhere.example")

	z := newZone(t,
		"example.com. 3600 IN AAAA ::1",
		"elsewhere.example. 3600 IN AAAA ::1",
	)
	c := &ResourceDiscovery{dialer: loopbackDialer(t, z), port: port, rootCAs: roots}

	res, err := c.Check(context.Background(), "example.com", KindApex)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := res.Detail.(*ResourceDiscoveryDetail)
	if !ok {
		t.Fatalf("detail type = %T", res.Detail)
	}
	if slices.Contains(d.Hosts, "leaked.example") {
		t.Errorf("hosts = %v: followed a redirect off the site", d.Hosts)
	}
}

// TestResourceDiscoveryNeedsAAAAOnTheHop pins the rule the decision on
// review issue 01 calls out: every hop is over IPv6 or not at all. A
// v4-only www is not fetched over v4 to make up for it — the last response
// stands, and the www dimension tells that story instead.
func TestResourceDiscoveryNeedsAAAAOnTheHop(t *testing.T) {
	port, roots := siteTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(hostOnly(r.Host), "example.com") {
			http.Redirect(w, r, "https://www.example.com/", http.StatusMovedPermanently)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><script src="https://v4only.example/x.js"></script></head></html>`))
	}), "example.com", "www.example.com")

	// www has an A record only — no AAAA to hop to.
	z := newZone(t,
		"example.com. 3600 IN AAAA ::1",
		"www.example.com. 3600 IN A 127.0.0.1",
	)
	c := &ResourceDiscovery{dialer: loopbackDialer(t, z), port: port, rootCAs: roots}

	res, err := c.Check(context.Background(), "example.com", KindApex)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := res.Detail.(*ResourceDiscoveryDetail)
	if !ok {
		t.Fatalf("detail type = %T", res.Detail)
	}
	if slices.Contains(d.Hosts, "v4only.example") {
		t.Errorf("hosts = %v: fetched the www page over IPv4", d.Hosts)
	}
}

// hostOnly strips any :port from a Host header.
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// dualStackTLSServer binds both loopback families, so a check that dials
// v4 and v6 separately reaches the same server on each.
func dualStackTLSServer(t *testing.T, handler http.Handler) (port string, roots *x509.CertPool) {
	t.Helper()
	ln, err := net.Listen("tcp", ":0") // dual-stack: accepts ::1 and 127.0.0.1
	if err != nil {
		t.Skipf("dual-stack loopback unavailable: %v", err)
	}
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: handler}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	roots = x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	return lnPort(t, ln), roots
}

func TestResponseParityComparesBothFamilies(t *testing.T) {
	body := strings.Repeat("x", 128)
	port, roots := dualStackTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	// One server on both families, so the v4 and v6 fetches must agree.
	z := newZone(t, "example.com. 3600 IN AAAA ::1", "example.com. 3600 IN A 127.0.0.1")
	c := &ResponseParity{dialer: loopbackDialer(t, z), port: port, rootCAs: roots}

	res, err := c.Check(context.Background(), "example.com", KindApex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusSupported {
		t.Fatalf("identical responses reported %s: %+v", res.Status, res.Detail)
	}
}

// TestResponseParityMeasuresTheBytesItRead covers review issue 19: the two
// families must be measured the same way. The same 2 MiB page is served
// with an explicit Content-Length to v4 and chunked to v6 — a CDN edge on
// one family and an identity origin on the other. Reading resp.ContentLength
// when it is set (uncapped, 2 MiB) and len(body) when it is not (capped at
// 1 MiB) made these identical pages differ by 50% and report partial on the
// public parity dimension.
func TestResponseParityMeasuresTheBytesItRead(t *testing.T) {
	const size = 2 << 20 // twice maxBodySize, so the cap actually bites
	page := bytes.Repeat([]byte("x"), size)
	port, roots := dualStackTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if remoteIsV4(t, r.RemoteAddr) {
			// Identity origin: the header carries the full, uncapped size.
			w.Header().Set("Content-Length", strconv.Itoa(size))
		}
		// No Content-Length on the v6 leg: Go chunks it and the client
		// reports ContentLength -1.
		_, _ = w.Write(page)
	}))
	z := newZone(t, "example.com. 3600 IN AAAA ::1", "example.com. 3600 IN A 127.0.0.1")
	c := &ResponseParity{dialer: loopbackDialer(t, z), port: port, rootCAs: roots}

	res, err := c.Check(context.Background(), "example.com", KindApex)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := res.Detail.(*ParityDetail)
	if !ok {
		t.Fatalf("detail type = %T", res.Detail)
	}
	if d.IPv4 == nil || d.IPv6 == nil {
		t.Fatalf("one family did not fetch: %+v", d)
	}
	if d.IPv4.ContentLength != maxBodySize || d.IPv6.ContentLength != maxBodySize {
		t.Errorf("lengths = v4 %d, v6 %d; both must be the %d bytes actually read",
			d.IPv4.ContentLength, d.IPv6.ContentLength, maxBodySize)
	}
	if res.Status != StatusSupported {
		diff := math.NaN()
		if d.ContentLengthDiffPct != nil {
			diff = *d.ContentLengthDiffPct
		}
		t.Errorf("status = %s (diff %.1f%%), want supported: the pages are identical",
			res.Status, diff)
	}
}

// remoteIsV4 reports whether a RemoteAddr reached the dual-stack listener
// over IPv4; a v4 connection can arrive v4-mapped, so unmap before asking.
func remoteIsV4(t *testing.T, remoteAddr string) bool {
	t.Helper()
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		t.Errorf("remote addr %q: %v", remoteAddr, err)
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		t.Errorf("remote host %q: %v", host, err)
		return false
	}
	return addr.Unmap().Is4()
}

// The probe's redirect policy is exact: sameHostRedirect(domain, n) follows
// at most n hops and never leaves the domain.
func TestSameHostRedirectPolicy(t *testing.T) {
	policy := sameHostRedirect("example.com", 2)
	req := func(host string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, "https://"+host+"/", http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	// via[0] is the original request; the chain grows by one per hop.
	via := func(n int) []*http.Request {
		v := make([]*http.Request, n)
		for i := range v {
			v[i] = req("example.com")
		}
		return v
	}

	if err := policy(req("example.com"), via(1)); err != nil {
		t.Errorf("first hop refused: %v", err)
	}
	if err := policy(req("example.com"), via(2)); err != nil {
		t.Errorf("second hop refused: %v", err)
	}
	if err := policy(req("example.com"), via(3)); err == nil {
		t.Error("third hop followed past the two-hop ceiling")
	}
	if err := policy(req("evil.example.net"), via(1)); err == nil {
		t.Error("followed a redirect off the original domain")
	}
	// Same host but another origin: the transport is pinned to ip:443 with
	// TLS, so a scheme or port change cannot be honoured.
	plain, _ := http.NewRequest(http.MethodGet, "http://example.com/", http.NoBody)
	if err := policy(plain, via(1)); err == nil {
		t.Error("followed an https→http redirect onto the pinned TLS port")
	}
	other, _ := http.NewRequest(http.MethodGet, "https://example.com:8443/", http.NoBody)
	if err := policy(other, via(1)); err == nil {
		t.Error("followed a redirect to another port on the pinned transport")
	}
}

// Every probe dial runs the SSRF blocklist, so a check cannot reach a
// reserved address even if it forgets its own ValidateIP call. Production
// NewSafeDialer carries the full list; the loopback test adapter carries an
// empty one, which is why ::1 is reachable in the tests above.
func TestProbeDialEnforcesBlocklist(t *testing.T) {
	p := probe{dialer: NewSafeDialer(NewResolver([]string{"[::1]:53"})), port: "443"}

	_, err := p.dial(context.Background(), net.ParseIP("::1"), "tcp6", "443")
	if err == nil {
		t.Fatal("dialed a loopback address through the production blocklist")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error = %v, want a blocklist rejection", err)
	}
}

// TestResponseParityWalksTheAddressRotation is review issue 63. The check
// tried v6IPs[0] and stopped, so a site announcing several AAAAs with a dead
// first edge earned a definitive `unsupported` on the dimension that feeds
// broken_v6 and ipv6_only — while every browser reached it on the next
// address via Happy Eyeballs. Three counted scans hitting the same first
// address confirmed it and the site was publicly flagged as broken.
func TestResponseParityWalksTheAddressRotation(t *testing.T) {
	body := strings.Repeat("x", 128)
	port, roots := dualStackTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))

	t.Run("blocked first address is skipped, not fatal", func(t *testing.T) {
		z := newZone(t,
			"example.com. 3600 IN AAAA 2001:db8::dead",
			"example.com. 3600 IN AAAA ::1",
			"example.com. 3600 IN A 127.0.0.1")
		d := loopbackDialer(t, z)
		// Block only the first address, as the SSRF list would for one edge
		// that happens to sit in a reserved range.
		_, blocked, err := net.ParseCIDR("2001:db8::dead/128")
		if err != nil {
			t.Fatal(err)
		}
		d.blockedV6 = []*net.IPNet{blocked}
		c := &ResponseParity{dialer: d, port: port, rootCAs: roots}

		res, err := c.Check(context.Background(), "example.com", KindApex)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != StatusSupported {
			t.Fatalf("a blocked first address gave %s: %+v", res.Status, res.Detail)
		}
		det, ok := res.Detail.(*ParityDetail)
		if !ok || det.IPv6 == nil {
			t.Fatalf("detail = %+v", res.Detail)
		}
		if det.IPv6.Address != "::1" {
			t.Errorf("fetched over %s, want the second address ::1", det.IPv6.Address)
		}
	})

	t.Run("unreachable first address is walked past", func(t *testing.T) {
		// 100::/64 is the RFC 6666 discard prefix: routers drop it, so the
		// dial fails rather than hanging on a live host that never answers.
		z := newZone(t,
			"example.com. 3600 IN AAAA 100::1",
			"example.com. 3600 IN AAAA ::1",
			"example.com. 3600 IN A 127.0.0.1")
		d := loopbackDialer(t, z)
		// The discard prefix is dropped rather than refused, so the dial
		// hangs until it times out. Shorten that here: the walk is what is
		// under test, not how long the crawler is willing to wait.
		d.dialer = &net.Dialer{Timeout: 200 * time.Millisecond}
		c := &ResponseParity{dialer: d, port: port, rootCAs: roots}

		res, err := c.Check(context.Background(), "example.com", KindApex)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != StatusSupported {
			t.Fatalf("a dead first address gave %s: %+v", res.Status, res.Detail)
		}
		det, ok := res.Detail.(*ParityDetail)
		if !ok || det.IPv6 == nil {
			t.Fatalf("detail = %+v", res.Detail)
		}
		if det.IPv6.Address != "::1" {
			t.Errorf("fetched over %s, want the second address ::1", det.IPv6.Address)
		}
	})

	t.Run("every address blocked stays an error", func(t *testing.T) {
		z := newZone(t,
			"example.com. 3600 IN AAAA 2001:db8::dead",
			"example.com. 3600 IN A 127.0.0.1")
		d := loopbackDialer(t, z)
		_, blocked, err := net.ParseCIDR("2001:db8::/32")
		if err != nil {
			t.Fatal(err)
		}
		d.blockedV6 = []*net.IPNet{blocked}
		c := &ResponseParity{dialer: d, port: port, rootCAs: roots}

		res, err := c.Check(context.Background(), "example.com", KindApex)
		if err != nil {
			t.Fatal(err)
		}
		// Nothing was probed, so this is our refusal rather than the site's
		// failure: error, never a definitive unsupported.
		if res.Status != StatusError {
			t.Errorf("all-blocked gave %s, want error", res.Status)
		}
	})
}

// TestResponseParityDefersOnATimeout applies issue 63's lesson to the verdict
// rather than the walk. A timeout is our clock running out, not the site's
// answer, and every other check here already says so — dial.go's isTimeout
// branch, tls_ipv6, smtp_ipv6. fetchAny made it matter more: up to
// maxAddressAttempts dials per family at the dialer's 10s against this
// check's 20s, so one dead IPv4 address plus one slow IPv6 address is exactly
// the budget and the second half of the check never gets to run.
func TestResponseParityDefersOnATimeout(t *testing.T) {
	body := strings.Repeat("x", 64)

	t.Run("a timed-out IPv6 fetch defers", func(t *testing.T) {
		port, roots := dualStackTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		// The RFC 6666 discard prefix again, and the only AAAA this time, so
		// the walk has nothing left to try and the check has to rule on a
		// timeout.
		z := newZone(t,
			"example.com. 3600 IN AAAA 100::1",
			"example.com. 3600 IN A 127.0.0.1")
		d := loopbackDialer(t, z)
		d.dialer = &net.Dialer{Timeout: 200 * time.Millisecond}
		c := &ResponseParity{dialer: d, port: port, rootCAs: roots}

		res, err := c.Check(context.Background(), "example.com", KindApex)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != StatusError {
			t.Fatalf("a timed-out IPv6 fetch gave %s, want error: %+v", res.Status, res.Detail)
		}
	})

	t.Run("a refused IPv6 fetch stays unsupported", func(t *testing.T) {
		// IPv4-only listener, so ::1 on the same port is refused outright.
		// The host answered, and what it said was no — that is evidence, and
		// the deferral above must not swallow it.
		ln, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Skipf("IPv4 loopback unavailable: %v", err)
		}
		srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) },
		)}}
		srv.StartTLS()
		t.Cleanup(srv.Close)
		roots := x509.NewCertPool()
		roots.AddCert(srv.Certificate())

		z := newZone(t,
			"example.com. 3600 IN AAAA ::1",
			"example.com. 3600 IN A 127.0.0.1")
		c := &ResponseParity{dialer: loopbackDialer(t, z), port: lnPort(t, ln), rootCAs: roots}

		res, err := c.Check(context.Background(), "example.com", KindApex)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != StatusUnsupported {
			t.Fatalf("a refused IPv6 fetch gave %s, want unsupported: %+v", res.Status, res.Detail)
		}
	})
}
