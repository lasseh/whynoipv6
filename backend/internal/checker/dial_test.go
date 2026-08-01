package checker

import (
	"bufio"
	"context"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The dialing checks produce the error_type evidence (connection_refused,
// timeout, certificate_error) that observe.composeConn keys off to decide
// conn=unsupported — the value that turns a domain into a sinner. These
// tests run the production check paths against real servers on the IPv6
// loopback: the fake zone answers AAAA ::1, and loopbackDialer is the test
// adapter at the SafeDialer seam (empty blocklist so ::1 is reachable;
// production NewSafeDialer keeps the full SSRF list).

func loopbackDialer(t *testing.T, z *fakeZone) *SafeDialer {
	t.Helper()
	addr := startFakeDNS(t, z.handler)
	return &SafeDialer{
		resolver: NewResolver([]string{addr}),
		dialer:   &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second},
	}
}

// v6Listener binds the IPv6 loopback, skipping on hosts without one.
func v6Listener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	return ln
}

func lnPort(t *testing.T, ln net.Listener) string {
	t.Helper()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

// closedV6Port reserves a loopback port and closes it, so a dial gets
// ECONNREFUSED.
func closedV6Port(t *testing.T) string {
	t.Helper()
	ln := v6Listener(t)
	port := lnPort(t, ln)
	_ = ln.Close()
	return port
}

func newV6Server(t *testing.T, handler http.Handler) (srv *httptest.Server, port string) {
	t.Helper()
	ln := v6Listener(t)
	srv = &httptest.Server{Listener: ln, Config: &http.Server{Handler: handler}}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, lnPort(t, ln)
}

func newV6TLSServer(t *testing.T, handler http.Handler) (port string, roots *x509.CertPool) {
	t.Helper()
	ln := v6Listener(t)
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: handler}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	roots = x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	return lnPort(t, ln), roots
}

// exampleZone answers AAAA ::1 for example.com — the one name the httptest
// certificate is valid for.
func exampleZone(t *testing.T) *fakeZone {
	t.Helper()
	return newZone(t, "example.com. 3600 IN AAAA ::1")
}

func httpDetail(t *testing.T, res Result) *HTTPDetail {
	t.Helper()
	d, ok := res.Detail.(*HTTPDetail)
	if !ok {
		t.Fatalf("detail = %T, want *HTTPDetail", res.Detail)
	}
	return d
}

func TestHTTPIPv6Supported(t *testing.T) {
	_, port := newV6Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "fake/1.0")
		w.WriteHeader(http.StatusOK)
	}))
	c := &HTTPIPv6{dialer: loopbackDialer(t, exampleZone(t)), port: port}

	res, err := c.Check(context.Background(), "example.com", KindApex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusSupported {
		t.Fatalf("status = %s, want supported", res.Status)
	}
	d := httpDetail(t, res)
	if d.Address != "::1" || d.StatusCode != http.StatusOK || d.Server != "fake/1.0" {
		t.Errorf("detail = %+v", d)
	}
}

func TestHTTPIPv6ConnRefused(t *testing.T) {
	c := &HTTPIPv6{dialer: loopbackDialer(t, exampleZone(t)), port: closedV6Port(t)}

	res, err := c.Check(context.Background(), "example.com", KindApex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusUnsupported {
		t.Fatalf("status = %s, want unsupported", res.Status)
	}
	d := httpDetail(t, res)
	if d.ErrorType != ErrTypeConnRefused || d.Error != errConnRefused {
		t.Errorf("error_type = %q, error = %q", d.ErrorType, d.Error)
	}
}

func TestHTTPSIPv6CertificateError(t *testing.T) {
	// The httptest certificate is self-signed: without injected roots the
	// handshake fails verification — the certificate_error evidence.
	port, _ := newV6TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	c := &HTTPSIPv6{dialer: loopbackDialer(t, exampleZone(t)), port: port}

	res, err := c.Check(context.Background(), "example.com", KindApex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusUnsupported {
		t.Fatalf("status = %s, want unsupported", res.Status)
	}
	if d := httpDetail(t, res); d.ErrorType != ErrTypeCertificate {
		t.Errorf("error_type = %q, want %q (error: %s)", d.ErrorType, ErrTypeCertificate, d.Error)
	}
}

func TestHTTPSIPv6Supported(t *testing.T) {
	port, roots := newV6TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	c := &HTTPSIPv6{dialer: loopbackDialer(t, exampleZone(t)), port: port, rootCAs: roots}

	res, err := c.Check(context.Background(), "example.com", KindApex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusSupported {
		t.Fatalf("status = %s, want supported", res.Status)
	}
	d := httpDetail(t, res)
	if d.Address != "::1" || d.TLSVersion == "" {
		t.Errorf("detail = %+v", d)
	}
}

func TestHTTPSIPv6ConnRefused(t *testing.T) {
	c := &HTTPSIPv6{dialer: loopbackDialer(t, exampleZone(t)), port: closedV6Port(t)}

	res, err := c.Check(context.Background(), "example.com", KindApex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusUnsupported {
		t.Fatalf("status = %s, want unsupported", res.Status)
	}
	if d := httpDetail(t, res); d.ErrorType != ErrTypeConnRefused {
		t.Errorf("error_type = %q", d.ErrorType)
	}
}

// TestDialOverAAAA covers the prologue branches no real server exercises:
// the transient resolver failure, the no-AAAA unsupported, the SSRF block,
// and the terminal-error classification table.
func TestDialOverAAAA(t *testing.T) {
	t.Run("resolver failure is transient", func(t *testing.T) {
		z := exampleZone(t)
		z.servfail["example.com."] = true
		d := loopbackDialer(t, z)
		res, _ := dialOverAAAA(context.Background(), d, "example.com", time.Now(), false,
			func(context.Context, net.IP) (Result, error) {
				t.Fatal("try ran despite resolver failure")
				return Result{}, nil
			})
		if res.Status != StatusError {
			t.Fatalf("status = %s, want error", res.Status)
		}
		if d := httpDetail(t, res); d.ErrorType != "" {
			t.Errorf("transient failure must not carry an error_type, got %q", d.ErrorType)
		}
	})

	t.Run("no AAAA is unsupported", func(t *testing.T) {
		d := loopbackDialer(t, newZone(t)) // empty zone: NOERROR, no records
		res, _ := dialOverAAAA(context.Background(), d, "example.com", time.Now(), false,
			func(context.Context, net.IP) (Result, error) {
				t.Fatal("try ran despite empty AAAA")
				return Result{}, nil
			})
		if res.Status != StatusUnsupported {
			t.Fatalf("status = %s, want unsupported", res.Status)
		}
		if d := httpDetail(t, res); d.Reason != errNoAAAARecord {
			t.Errorf("reason = %q", d.Reason)
		}
	})

	t.Run("blocked address never dials", func(t *testing.T) {
		// The production blocklist rejects ::1 — the SSRF gate holds even
		// when the zone points a name at loopback.
		addr := startFakeDNS(t, exampleZone(t).handler)
		d := NewSafeDialer(NewResolver([]string{addr}))
		res, _ := dialOverAAAA(context.Background(), d, "example.com", time.Now(), false,
			func(context.Context, net.IP) (Result, error) {
				t.Fatal("try ran against a blocked address")
				return Result{}, nil
			})
		if res.Status != StatusError {
			t.Fatalf("status = %s, want error", res.Status)
		}
		if d := httpDetail(t, res); d.Error != errAddrBlocked {
			t.Errorf("error = %q", d.Error)
		}
	})

	t.Run("timeout classifies as error", func(t *testing.T) {
		d := loopbackDialer(t, exampleZone(t))
		res, _ := dialOverAAAA(context.Background(), d, "example.com", time.Now(), false,
			func(context.Context, net.IP) (Result, error) {
				return Result{}, context.DeadlineExceeded
			})
		if res.Status != StatusError {
			t.Fatalf("status = %s, want error", res.Status)
		}
		if d := httpDetail(t, res); d.ErrorType != ErrTypeTimeout {
			t.Errorf("error_type = %q", d.ErrorType)
		}
	})

	t.Run("tls error without withTLS stays unknown", func(t *testing.T) {
		// http_ipv6 has no certificate branch (no TLS on port 80): a
		// tls-shaped terminal error must classify unknown/error, never
		// certificate/unsupported.
		d := loopbackDialer(t, exampleZone(t))
		res, _ := dialOverAAAA(context.Background(), d, "example.com", time.Now(), false,
			func(context.Context, net.IP) (Result, error) {
				return Result{}, &net.OpError{Op: "read", Err: fmt.Errorf("tls: bad certificate")}
			})
		if res.Status != StatusError {
			t.Fatalf("status = %s, want error", res.Status)
		}
		if d := httpDetail(t, res); d.ErrorType != ErrTypeUnknown {
			t.Errorf("error_type = %q", d.ErrorType)
		}
	})
}

func TestTLSIPv6(t *testing.T) {
	t.Run("untrusted chain is unsupported", func(t *testing.T) {
		port, _ := newV6TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		c := &TLSIPv6{dialer: loopbackDialer(t, exampleZone(t)), port: port}
		res, err := c.Check(context.Background(), "example.com", KindApex)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != StatusUnsupported {
			t.Fatalf("status = %s, want unsupported", res.Status)
		}
		d := res.Detail.(*TLSDetail)
		if d.Valid == nil || *d.Valid || !strings.Contains(d.Error, "TLS handshake failed") {
			t.Errorf("detail = %+v", d)
		}
	})

	t.Run("valid chain is supported", func(t *testing.T) {
		port, roots := newV6TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		c := &TLSIPv6{dialer: loopbackDialer(t, exampleZone(t)), port: port, rootCAs: roots}
		res, err := c.Check(context.Background(), "example.com", KindApex)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != StatusSupported {
			t.Fatalf("status = %s, want supported (error: %v)", res.Status, res.Detail.common().Error)
		}
		d := res.Detail.(*TLSDetail)
		if d.Valid == nil || !*d.Valid || d.TLSVersion == "" || d.CipherSuite == "" {
			t.Errorf("detail = %+v", d)
		}
	})
}

// startFakeSMTP speaks just enough SMTP on the IPv6 loopback: banner, EHLO
// with STARTTLS advertised, QUIT.
func startFakeSMTP(t *testing.T) string {
	t.Helper()
	ln := v6Listener(t)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_, _ = fmt.Fprintf(c, "220 mx.mailer.test ESMTP ready\r\n")
				br := bufio.NewReader(c)
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						_, _ = fmt.Fprintf(c, "250-mx.mailer.test\r\n250 STARTTLS\r\n")
					case strings.HasPrefix(line, "QUIT"):
						_, _ = fmt.Fprintf(c, "221 bye\r\n")
						return
					}
				}
			}(conn)
		}
	}()
	return lnPort(t, ln)
}

func TestSMTPIPv6(t *testing.T) {
	zone := func(t *testing.T) *fakeZone {
		t.Helper()
		return newZone(t,
			"mailer.test. 3600 IN MX 10 mx.mailer.test.",
			"mx.mailer.test. 3600 IN AAAA ::1")
	}

	t.Run("banner and EHLO over IPv6 is supported", func(t *testing.T) {
		c := &SMTPIPv6{dialer: loopbackDialer(t, zone(t)), port: startFakeSMTP(t)}
		res, err := c.Check(context.Background(), "mailer.test", KindApex)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != StatusSupported {
			t.Fatalf("status = %s (error: %v)", res.Status, res.Detail.common().Error)
		}
		d := res.Detail.(*SMTPDetail)
		if d.MXHost != "mx.mailer.test." || !strings.HasPrefix(d.Banner, "220") {
			t.Errorf("detail = %+v", d)
		}
		if d.STARTTLSOffered == nil || !*d.STARTTLSOffered {
			t.Error("STARTTLS not detected")
		}
	})

	t.Run("refused MX is unsupported", func(t *testing.T) {
		c := &SMTPIPv6{dialer: loopbackDialer(t, zone(t)), port: closedV6Port(t)}
		res, err := c.Check(context.Background(), "mailer.test", KindApex)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != StatusUnsupported {
			t.Fatalf("status = %s, want unsupported", res.Status)
		}
		if d := res.Detail.(*SMTPDetail); d.Error != errConnRefused {
			t.Errorf("error = %q", d.Error)
		}
	})
}
