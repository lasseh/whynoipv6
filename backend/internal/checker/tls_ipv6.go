package checker

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"
)

const expirySoonDays = 30

// TLSIPv6 checks TLS certificate validity over an IPv6 connection.
type TLSIPv6 struct {
	dialer *SafeDialer
}

// NewTLSIPv6 creates a new tls_ipv6 checker.
func NewTLSIPv6(dialer *SafeDialer) *TLSIPv6 {
	return &TLSIPv6{dialer: dialer}
}

func (c *TLSIPv6) Name() string { return "tls_ipv6" }
func (c *TLSIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	details := map[string]any{}

	// Resolve AAAA records.
	ips, _, _, _, err := c.dialer.Resolver().LookupAAAA(ctx, domain)
	if err != nil || len(ips) == 0 {
		return Result{
			Status:  StatusUnsupported,
			Details: map[string]any{"reason": errNoAAAARecord},
			Latency: time.Since(start),
		}, nil
	}

	ip := ips[0]
	if err := c.dialer.ValidateIP(ip); err != nil {
		details["error"] = errAddrBlocked
		return Result{
			Status:  StatusError,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}

	details["address"] = ip.String()

	// Open TCP connection to the IPv6 address on port 443.
	addr := net.JoinHostPort(ip.String(), "443")
	conn, err := c.dialer.dialer.DialContext(ctx, "tcp6", addr)
	if err != nil {
		if isConnRefused(err) {
			details["error"] = errConnRefused
			return Result{
				Status:  StatusUnsupported,
				Details: details,
				Latency: time.Since(start),
			}, nil
		}
		details["error"] = err.Error()
		return Result{
			Status:  StatusError,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}

	// Perform TLS handshake.
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: domain,
		MinVersion: tls.VersionTLS12,
	})

	if deadline, ok := ctx.Deadline(); ok {
		_ = tlsConn.SetDeadline(deadline)
	}

	err = tlsConn.HandshakeContext(ctx)
	if err != nil {
		_ = conn.Close()
		details["error"] = fmt.Sprintf("TLS handshake failed: %v", err)
		details["valid"] = false
		return Result{
			Status:  StatusUnsupported,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}
	defer func() { _ = tlsConn.Close() }()

	state := tlsConn.ConnectionState()
	details["tls_version"] = tlsVersionString(state.Version)
	details["cipher_suite"] = tls.CipherSuiteName(state.CipherSuite)

	if len(state.PeerCertificates) == 0 {
		details["error"] = "no peer certificates"
		details["valid"] = false
		return Result{
			Status:  StatusUnsupported,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}

	leaf := state.PeerCertificates[0]
	now := time.Now()

	details["valid"] = true
	details["issuer"] = leaf.Issuer.CommonName
	details["subject"] = leaf.Subject.CommonName
	if len(leaf.DNSNames) > 0 {
		details["san"] = leaf.DNSNames
	}
	details["not_before"] = leaf.NotBefore.Format(time.RFC3339)
	details["not_after"] = leaf.NotAfter.Format(time.RFC3339)

	expiresInDays := int(time.Until(leaf.NotAfter).Hours() / 24)
	details["expires_in_days"] = expiresInDays
	details["expires_soon"] = expiresInDays <= expirySoonDays

	// Verify certificate validity.
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		details["valid"] = false
		details["error"] = "certificate expired or not yet valid"
		return Result{
			Status:  StatusUnsupported,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}

	// Verify hostname matches.
	if err := leaf.VerifyHostname(domain); err != nil {
		details["valid"] = false
		details["error"] = fmt.Sprintf("hostname mismatch: %v", err)
		return Result{
			Status:  StatusUnsupported,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}

	return Result{
		Status:  StatusSupported,
		Details: details,
		Latency: time.Since(start),
	}, nil
}
