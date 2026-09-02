package checker

import (
	"bytes"
	"context"
	"crypto/x509"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
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
