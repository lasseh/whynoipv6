package checker

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// HTTPSIPv6 checks whether the domain responds to HTTPS over IPv6.
type HTTPSIPv6 struct {
	dialer *SafeDialer
}

// NewHTTPSIPv6 creates a new https_ipv6 checker.
func NewHTTPSIPv6(dialer *SafeDialer) *HTTPSIPv6 {
	return &HTTPSIPv6{dialer: dialer}
}

func (c *HTTPSIPv6) Name() string { return "https_ipv6" }
func (c *HTTPSIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
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

	maxAttempts := min(len(ips), 3)

	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		ip := ips[i]
		if err := c.dialer.ValidateIP(ip); err != nil {
			details["error"] = errAddrBlocked
			return Result{
				Status:  StatusError,
				Details: details,
				Latency: time.Since(start),
			}, nil
		}

		result, tryErr := c.tryHTTPS(ctx, domain, ip)
		if tryErr == nil {
			result.Latency = time.Since(start)
			return result, nil
		}
		lastErr = tryErr

		if ctx.Err() != nil {
			break
		}
	}

	if isConnRefused(lastErr) {
		details["error"] = errConnRefused
		details["error_type"] = "connection_refused"
		return Result{
			Status:  StatusUnsupported,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}

	if isTimeout(lastErr) {
		details["error"] = lastErr.Error()
		details["error_type"] = "timeout"
		return Result{
			Status:  StatusError,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}

	if isTLSError(lastErr) {
		details["error"] = lastErr.Error()
		details["error_type"] = "certificate_error"
		return Result{
			Status:  StatusUnsupported,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}

	details["error"] = lastErr.Error()
	details["error_type"] = "unknown"
	return Result{
		Status:  StatusError,
		Details: details,
		Latency: time.Since(start),
	}, nil
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
	if _, ok := errors.AsType[*tls.RecordHeaderError](err); ok {
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
	addr := net.JoinHostPort(ip.String(), "443")

	transport := &http.Transport{
		DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
			return c.dialer.dialer.DialContext(dialCtx, "tcp6", addr)
		},
		TLSClientConfig: &tls.Config{
			ServerName: domain,
			MinVersion: tls.VersionTLS12,
		},
		DisableKeepAlives: true,
	}

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	url := fmt.Sprintf("https://%s/", domain)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	responseTime := time.Since(reqStart)

	details := map[string]any{
		"address":          ip.String(),
		"status_code":      resp.StatusCode,
		"response_time_ms": responseTime.Milliseconds(),
	}
	if server := resp.Header.Get("Server"); server != "" {
		details["server"] = server
	}
	if resp.TLS != nil {
		details["tls_version"] = tlsVersionString(resp.TLS.Version)
	}

	return Result{
		Status:  StatusSupported,
		Details: details,
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
