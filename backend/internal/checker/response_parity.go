package checker

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"time"
)

const (
	maxRedirects    = 3
	maxBodySize     = 1 << 20 // 1MB
	parityTolerance = 0.10    // 10%
)

// ResponseParity compares HTTP responses over IPv4 and IPv6
// (01-engine.md §11.8).
type ResponseParity struct {
	dialer  *SafeDialer
	port    string
	rootCAs *x509.CertPool
}

// NewResponseParity creates a new http_response_parity checker.
func NewResponseParity(dialer *SafeDialer) *ResponseParity {
	return &ResponseParity{dialer: dialer, port: "443"}
}

// probe is the pinned fetch; port and rootCAs are this check's test seams.
func (c *ResponseParity) probe() probe {
	return probe{dialer: c.dialer, port: c.port, rootCAs: c.rootCAs}
}

func (c *ResponseParity) Name() string { return NameParity }
func (c *ResponseParity) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	d := &ParityDetail{}

	// Resolve both A and AAAA records. Resolver trouble is transient
	// (error); only an empty answer is "no record" (not_applicable).
	v4IPs, err := c.dialer.Resolver().LookupA(ctx, domain)
	if err != nil {
		d.Error = err.Error()
		return Result{
			Status:  StatusError,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}
	if len(v4IPs) == 0 {
		d.Reason = "no A record"
		return Result{
			Status:  StatusNotApplicable,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	v6IPs, _, _, _, err := c.dialer.Resolver().LookupAAAA(ctx, domain)
	if err != nil {
		d.Error = err.Error()
		return Result{
			Status:  StatusError,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}
	if len(v6IPs) == 0 {
		d.Reason = errNoAAAARecord
		return Result{
			Status:  StatusNotApplicable,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	v4IP := v4IPs[0]
	v6IP := v6IPs[0]

	// Validate both IPs.
	if err := c.dialer.ValidateIP(v4IP); err != nil {
		d.Error = "IPv4 address in blocked range"
		return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
	}
	if err := c.dialer.ValidateIP(v6IP); err != nil {
		d.Error = "IPv6 address in blocked range"
		return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
	}

	// Fetch over IPv4 (baseline).
	v4Result, err := c.fetch(ctx, domain, v4IP, "tcp4")
	if err != nil {
		// Can't establish a baseline — nothing to compare against.
		d.Error = fmt.Sprintf("IPv4 request failed: %v", err)
		return Result{Status: StatusNotApplicable, Detail: d, Latency: time.Since(start)}, nil
	}

	// Fetch over IPv6.
	v6Result, err := c.fetch(ctx, domain, v6IP, "tcp6")
	if err != nil {
		// IPv6 HTTPS doesn't work — parity is unsupported, not an internal error.
		d.Error = fmt.Sprintf("IPv6 request failed: %v", err)
		return Result{Status: StatusUnsupported, Detail: d, Latency: time.Since(start)}, nil
	}

	d.IPv4 = &v4Result
	d.IPv6 = &v6Result

	statusMatch := v4Result.StatusCode == v6Result.StatusCode
	d.StatusMatch = &statusMatch

	// Compare Content-Type (base type only, ignoring params like charset).
	contentTypeMatch := baseContentType(v4Result.ContentType) == baseContentType(v6Result.ContentType)
	d.ContentTypeMatch = &contentTypeMatch

	// Calculate content length diff.
	diffPct := 0.0
	if v4Result.ContentLength > 0 {
		diffPct = math.Abs(float64(v6Result.ContentLength-v4Result.ContentLength)) / float64(v4Result.ContentLength)
	}
	rounded := math.Round(diffPct*1000) / 10 // one decimal
	d.ContentLengthDiffPct = &rounded

	// Matching redirect status codes (3xx) indicate full parity regardless
	// of body size differences — redirect bodies are edge-specific boilerplate.
	isRedirect := func(code int) bool { return code >= 300 && code < 400 }
	bothRedirects := isRedirect(v4Result.StatusCode) && isRedirect(v6Result.StatusCode)

	var status CheckStatus
	switch {
	case !statusMatch:
		status = StatusUnsupported
	case !contentTypeMatch:
		// Different Content-Type (e.g., HTML vs error page) means degraded parity.
		status = StatusPartial
	case bothRedirects:
		status = StatusSupported
	case diffPct > parityTolerance:
		status = StatusPartial
	default:
		status = StatusSupported
	}

	return Result{
		Status:  status,
		Detail:  d,
		Latency: time.Since(start),
	}, nil
}

func (c *ResponseParity) fetch(ctx context.Context, domain string, ip net.IP, network string) (ParityFetch, error) {
	reqStart := time.Now()

	resp, err := c.probe().get(ctx, ip, domain, "https", probeOptions{
		TLS:      true,
		Network:  network,
		Redirect: sameHostRedirect(domain, maxRedirects),
	})
	if err != nil {
		return ParityFetch{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Read body to measure content length (up to 1MB).
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return ParityFetch{}, fmt.Errorf("reading body: %w", err)
	}

	contentLength := resp.ContentLength
	if contentLength < 0 {
		contentLength = int64(len(body))
	}

	return ParityFetch{
		Address:        ip.String(),
		StatusCode:     resp.StatusCode,
		ContentType:    sanitizeText(resp.Header.Get("Content-Type")),
		ContentLength:  contentLength,
		ResponseTimeMS: time.Since(reqStart).Milliseconds(),
	}, nil
}

// baseContentType extracts the media type from a Content-Type header,
// stripping parameters like charset.
func baseContentType(ct string) string {
	if ct == "" {
		return ""
	}
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = ct[:idx]
	}
	return strings.TrimSpace(strings.ToLower(ct))
}
