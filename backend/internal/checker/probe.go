package checker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
)

// probe is the one pinned-IPv6 fetch in the package. Every check that dials
// an address it resolved itself goes through here, and every dial it makes
// runs SafeDialer.DialContext — so an address the SSRF blocklist rejects is
// unreachable by construction rather than by each check remembering to call
// ValidateIP first.
//
// port and rootCAs are the construction state that makes a check testable:
// with them the package's loopback TLS server can stand in for the real
// internet. Checks that omitted them could not be pointed at the harness at
// all, which is why their network paths had no tests.
type probe struct {
	dialer  *SafeDialer
	port    string         // internal seam: "443"/"80" in production
	rootCAs *x509.CertPool // internal seam: nil = system roots
}

// probeOptions is what genuinely differs between the checks; everything
// else (pinning, SSRF validation, keepalive policy, TLS floor) is fixed.
type probeOptions struct {
	// TLS wraps the connection and pins SNI to the domain.
	TLS bool
	// Network is "tcp6" unless a check deliberately dials v4 for a
	// comparison (response_parity); empty means tcp6.
	Network string
	// Redirect is the client's CheckRedirect. Nil never follows.
	Redirect func(req *http.Request, via []*http.Request) error
}

// network resolves the address family, defaulting to IPv6.
func (o probeOptions) network() string {
	if o.Network == "" {
		return "tcp6"
	}
	return o.Network
}

// dial opens one validated connection. tls_ipv6 and smtp_ipv6 speak their
// own protocols over it rather than HTTP.
func (p probe) dial(ctx context.Context, ip net.IP, network, port string) (net.Conn, error) {
	return p.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
}

// transport builds the pinned transport: every dial ignores the address the
// http stack asks for and uses the one IP the check resolved and the port
// this probe was constructed with.
func (p probe) transport(ip net.IP, domain string, o probeOptions) *http.Transport {
	addr := net.JoinHostPort(ip.String(), p.port)
	t := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return p.dialer.DialContext(ctx, o.network(), addr)
		},
		DisableKeepAlives: true,
	}
	if o.TLS {
		t.TLSClientConfig = &tls.Config{
			ServerName: domain,
			MinVersion: tls.VersionTLS12,
			RootCAs:    p.rootCAs,
		}
	}
	return t
}

// client builds the pinned HTTP client for one attempt.
func (p probe) client(ip net.IP, domain string, o probeOptions) *http.Client {
	redirect := o.Redirect
	if redirect == nil {
		redirect = neverRedirect
	}
	return &http.Client{Transport: p.transport(ip, domain, o), CheckRedirect: redirect}
}

// get issues the probe's one request shape: GET /, the bot User-Agent, and
// the caller's context.
func (p probe) get(ctx context.Context, ip net.IP, domain, scheme string, o probeOptions) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+domain+"/", http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	return p.client(ip, domain, o).Do(req)
}

// neverRedirect is the default policy: report the redirect as the response
// rather than following it.
func neverRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// sameHostRedirect follows at most maxHops redirects, and only while they
// stay on the original origin: same host, same scheme, same (or default)
// port. The transport dials the pinned original IP:port with the original
// SNI, so a cross-host redirect would fetch the wrong vhost's content, and
// an https→http or :8443 redirect would put plaintext or the wrong Host on
// the pinned TLS port.
//
// via holds the requests already made, so on the Nth redirect len(via) is
// N: following while len(via) <= maxHops permits exactly maxHops hops.
func sameHostRedirect(domain string, maxHops int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) > maxHops || req.URL.Hostname() != domain || len(via) == 0 {
			return http.ErrUseLastResponse
		}
		first := via[0].URL
		if req.URL.Scheme != first.Scheme || req.URL.Port() != first.Port() {
			return http.ErrUseLastResponse
		}
		return nil
	}
}
