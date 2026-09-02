package checker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// HTTPSIPv6 checks whether the domain responds to HTTPS over IPv6
// (01-engine.md §11.7). port and rootCAs are internal seams: "80"-style
// production defaults ("443", system roots via nil) with test overrides.
type HTTPSIPv6 struct {
	dialer  *SafeDialer
	port    string
	rootCAs *x509.CertPool
}

// NewHTTPSIPv6 creates a new https_ipv6 checker.
func NewHTTPSIPv6(dialer *SafeDialer) *HTTPSIPv6 {
	return &HTTPSIPv6{dialer: dialer, port: "443"}
}

// probe is the pinned fetch; port and rootCAs are this check's test seams.
func (c *HTTPSIPv6) probe() probe {
	return probe{dialer: c.dialer, port: c.port, rootCAs: c.rootCAs}
}

func (c *HTTPSIPv6) Name() string { return NameHTTPS }
func (c *HTTPSIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return dialOverAAAA(ctx, c.dialer, domain, start, true,
		func(ctx context.Context, ip net.IP) (Result, error) {
			return c.tryHTTPS(ctx, domain, ip)
		})
}

// isTimeout checks if an error is a timeout error.
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if netErr, ok := errors.AsType[net.Error](err); ok {
		return netErr.Timeout()
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// isTLSError checks if an error is a TLS/certificate error.
func isTLSError(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return true
	}
	// crypto/tls surfaces RecordHeaderError by value, and net/http replaces
	// a record that looks like HTTP with the bare ErrSchemeMismatch: both
	// mean a plaintext listener on the TLS port (01 §11.7: TLS error).
	if _, ok := errors.AsType[tls.RecordHeaderError](err); ok {
		return true
	}
	if errors.Is(err, http.ErrSchemeMismatch) {
		return true
	}
	// Check for generic TLS alert errors via the error string as a fallback.
	if opErr, ok := errors.AsType[*net.OpError](err); ok {
		if opErr.Err != nil {
			msg := opErr.Err.Error()
			return len(msg) > 4 && msg[:4] == "tls:"
		}
	}
	return false
}

func (c *HTTPSIPv6) tryHTTPS(ctx context.Context, domain string, ip net.IP) (Result, error) {
	reqStart := time.Now()

	resp, err := c.probe().get(ctx, ip, domain, "https", probeOptions{TLS: true})
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	rt := time.Since(reqStart).Milliseconds()

	d := &HTTPDetail{
		Address:        ip.String(),
		StatusCode:     resp.StatusCode,
		ResponseTimeMS: &rt,
		Server:         sanitizeText(resp.Header.Get("Server")),
	}
	if resp.TLS != nil {
		d.TLSVersion = tlsVersionString(resp.TLS.Version)
	}

	return Result{
		Status: StatusSupported,
		Detail: d,
	}, nil
}

// tlsVersionString returns a human-readable TLS version string.
func tlsVersionString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("unknown (0x%04x)", version)
	}
}
