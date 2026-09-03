package checker

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"
)

const userAgent = "WhyNoIPv6Bot/1.0 (+https://whynoipv6.com/bot)"

// HTTPIPv6 checks whether the domain responds to HTTP over IPv6
// (01-engine.md §11.6). port is an internal seam: "80" in production, a
// test server's port in the dial tests.
type HTTPIPv6 struct {
	dialer *SafeDialer
	port   string
}

// NewHTTPIPv6 creates a new http_ipv6 checker.
func NewHTTPIPv6(dialer *SafeDialer) *HTTPIPv6 {
	return &HTTPIPv6{dialer: dialer, port: "80"}
}

// probe is the pinned fetch; port is this check's test seam.
func (c *HTTPIPv6) probe() probe { return probe{dialer: c.dialer, port: c.port} }

func (c *HTTPIPv6) Name() string { return NameHTTP }
func (c *HTTPIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return dialOverAAAA(ctx, c.dialer, domain, start, false,
		func(ctx context.Context, ip net.IP) (Result, error) {
			return c.tryHTTP(ctx, domain, ip)
		})
}

func (c *HTTPIPv6) tryHTTP(ctx context.Context, domain string, ip net.IP) (Result, error) {
	reqStart := time.Now()

	resp, err := c.probe().get(ctx, ip, domain, "http", probeOptions{})
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	rt := time.Since(reqStart).Milliseconds()

	return Result{
		Status: StatusSupported,
		Detail: &HTTPDetail{
			Address:        ip.String(),
			StatusCode:     resp.StatusCode,
			ResponseTimeMS: &rt,
			Server:         sanitizeText(resp.Header.Get("Server")),
		},
	}, nil
}

// isConnRefused checks if an error is a connection refused error. The chain
// for a refused dial is *net.OpError → *os.SyscallError → syscall.Errno (a
// value, not a pointer), so match the whole chain with errors.Is.
func isConnRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}

// isUnreachable reports the kernel refusing to route the dial at all:
// ENETUNREACH (no route for the family or prefix) or EHOSTUNREACH (an ICMP
// destination-unreachable came back). Like a timeout and unlike a refusal,
// it is a statement about our vantage, not the far end's — the far end was
// never reached to make one. smtp_ipv6 and the parity check's v6 branch
// defer on it; the parity v4 branch does not (01-engine.md §11.10 erratum).
func isUnreachable(err error) bool {
	return errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH)
}
