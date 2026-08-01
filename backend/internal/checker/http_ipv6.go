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
	addr := net.JoinHostPort(ip.String(), c.port)

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

	rt := time.Since(reqStart).Milliseconds()

	return Result{
		Status: StatusSupported,
		Detail: &HTTPDetail{
			Address:        ip.String(),
			StatusCode:     resp.StatusCode,
			ResponseTimeMS: &rt,
			Server:         resp.Header.Get("Server"),
		},
	}, nil
}

// isConnRefused checks if an error is a connection refused error. The chain
// for a refused dial is *net.OpError → *os.SyscallError → syscall.Errno (a
// value, not a pointer), so match the whole chain with errors.Is.
func isConnRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}
