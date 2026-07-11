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

func (c *TLSIPv6) Name() string { return NameTLS }
func (c *TLSIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	d := &TLSDetail{}
	setValid := func(v bool) { d.Valid = &v }

	// Resolve AAAA records.
	ips, _, _, _, err := c.dialer.Resolver().LookupAAAA(ctx, domain)
	if err != nil || len(ips) == 0 {
		d.Reason = errNoAAAARecord
		return Result{
			Status:  StatusUnsupported,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	ip := ips[0]
	if err := c.dialer.ValidateIP(ip); err != nil {
		d.Error = errAddrBlocked
		return Result{
			Status:  StatusError,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	d.Address = ip.String()

	// Open TCP connection to the IPv6 address on port 443.
	addr := net.JoinHostPort(ip.String(), "443")
	conn, err := c.dialer.dialer.DialContext(ctx, "tcp6", addr)
	if err != nil {
		if isConnRefused(err) {
			d.Error = errConnRefused
			return Result{
				Status:  StatusUnsupported,
				Detail:  d,
				Latency: time.Since(start),
			}, nil
		}
		d.Error = err.Error()
		return Result{
			Status:  StatusError,
			Detail:  d,
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
		d.Error = fmt.Sprintf("TLS handshake failed: %v", err)
		setValid(false)
		return Result{
			Status:  StatusUnsupported,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}
	defer func() { _ = tlsConn.Close() }()

	state := tlsConn.ConnectionState()
	d.TLSVersion = tlsVersionString(state.Version)
	d.CipherSuite = tls.CipherSuiteName(state.CipherSuite)

	if len(state.PeerCertificates) == 0 {
		d.Error = "no peer certificates"
		setValid(false)
		return Result{
			Status:  StatusUnsupported,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	leaf := state.PeerCertificates[0]
	now := time.Now()

	setValid(true)
	d.Issuer = leaf.Issuer.CommonName
	d.Subject = leaf.Subject.CommonName
	d.SAN = leaf.DNSNames
	d.NotBefore = leaf.NotBefore.Format(time.RFC3339)
	d.NotAfter = leaf.NotAfter.Format(time.RFC3339)

	expiresInDays := int(time.Until(leaf.NotAfter).Hours() / 24)
	d.ExpiresInDays = &expiresInDays
	expiresSoon := expiresInDays <= expirySoonDays
	d.ExpiresSoon = &expiresSoon

	// Verify certificate validity.
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		setValid(false)
		d.Error = "certificate expired or not yet valid"
		return Result{
			Status:  StatusUnsupported,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	// Verify hostname matches.
	if err := leaf.VerifyHostname(domain); err != nil {
		setValid(false)
		d.Error = fmt.Sprintf("hostname mismatch: %v", err)
		return Result{
			Status:  StatusUnsupported,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	return Result{
		Status:  StatusSupported,
		Detail:  d,
		Latency: time.Since(start),
	}, nil
}
