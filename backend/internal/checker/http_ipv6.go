package checker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

const userAgent = "WhyNoIPv6Bot/1.0 (+https://whynoipv6.com/bot)"

// HTTPIPv6 checks whether the domain responds to HTTP over IPv6.
type HTTPIPv6 struct {
	dialer *SafeDialer
}

// NewHTTPIPv6 creates a new http_ipv6 checker.
func NewHTTPIPv6(dialer *SafeDialer) *HTTPIPv6 {
	return &HTTPIPv6{dialer: dialer}
}

func (c *HTTPIPv6) Name() string { return "http_ipv6" }
func (c *HTTPIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
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

	// Try each IP (up to 3).
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

		result, tryErr := c.tryHTTP(ctx, domain, ip)
		if tryErr == nil {
			result.Latency = time.Since(start)
			return result, nil
		}
		lastErr = tryErr

		// Don't retry on context cancellation.
		if ctx.Err() != nil {
			break
		}
	}

	// All attempts failed — classify the terminal error exactly as
	// https_ipv6 does (01-engine.md §11.6, enumerated deviation 3), so the
	// conn composition applies identically on the http-only fallback path.
	// No certificate_error branch exists here (no TLS on port 80).
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

	details["error"] = lastErr.Error()
	details["error_type"] = "unknown"
	return Result{
		Status:  StatusError,
		Details: details,
		Latency: time.Since(start),
	}, nil
}

func (c *HTTPIPv6) tryHTTP(ctx context.Context, domain string, ip net.IP) (Result, error) {
	reqStart := time.Now()
	addr := net.JoinHostPort(ip.String(), "80")

	transport := &http.Transport{
		DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
			return c.dialer.dialer.DialContext(dialCtx, "tcp6", addr)
		},
		DisableKeepAlives: true,
	}

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	url := fmt.Sprintf("http://%s/", domain)
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

	return Result{
		Status:  StatusSupported,
		Details: details,
	}, nil
}

// isConnRefused checks if an error is a connection refused error.
func isConnRefused(err error) bool {
	if err == nil {
		return false
	}
	if opErr, ok := errors.AsType[*net.OpError](err); ok {
		if sysErr, ok := errors.AsType[*syscall.Errno](opErr.Err); ok {
			return *sysErr == syscall.ECONNREFUSED
		}
	}
	return false
}
