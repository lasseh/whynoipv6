package checker

import (
	"context"
	"crypto/x509"
	"errors"
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

	// maxAddressAttempts bounds the per-family address walk (review issue
	// 63). The check used to try v6IPs[0] and stop, so a site announcing
	// four AAAAs with a decommissioned first edge earned a definitive
	// `unsupported` on parity — while every browser reached it on the
	// second address. Three is enough for the realistic "one address in the
	// rotation is dead" case without letting a large rotation eat the 20s
	// budget.
	//
	// parity is informational (02 §7.4): it does NOT feed conn, and so not
	// broken_v6 or ipv6_only either — those come from https_ipv6 through
	// composeConn. An earlier draft of this comment said otherwise.
	maxAddressAttempts = 3
)

// errNoUsableAddress means every candidate was refused by the SSRF
// blocklist, so nothing was probed. That is our refusal, not the site's
// failure, and it maps to error rather than to a verdict.
var errNoUsableAddress = errors.New("no usable address")

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

	// Fetch over IPv4 (baseline).
	v4Result, err := c.fetchAny(ctx, domain, v4IPs, "tcp4")
	if err != nil {
		if errors.Is(err, errNoUsableAddress) {
			d.Error = "IPv4 address in blocked range"
			return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
		}
		if isTimeout(err) {
			// Our clock ran out, which is not the same as "this site has no
			// IPv4 baseline" — and not_applicable is a value, not an
			// absence. Defer, the same way the v6 branch below does.
			d.Error = fmt.Sprintf("IPv4 request timed out: %v", err)
			return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
		}
		// Can't establish a baseline — nothing to compare against.
		d.Error = fmt.Sprintf("IPv4 request failed: %v", err)
		return Result{Status: StatusNotApplicable, Detail: d, Latency: time.Since(start)}, nil
	}

	// Fetch over IPv6.
	v6Result, err := c.fetchAny(ctx, domain, v6IPs, "tcp6")
	if err != nil {
		if errors.Is(err, errNoUsableAddress) {
			d.Error = "IPv6 address in blocked range"
			return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
		}
		if isTimeout(err) {
			// A timeout is not evidence, and the address walk makes running
			// out of budget easy: up to maxAddressAttempts dials per family
			// at the dialer's 10s against this check's 20s, so a slow IPv4
			// baseline can leave the first IPv6 dial no time at all. One
			// dead v4 address plus one slow v6 address is exactly 20s, and
			// the site would be recorded as serving a broken IPv6 response
			// over a clock we set. Every other check in this package
			// already defers here — dial.go's isTimeout branch, tls_ipv6,
			// smtp_ipv6.
			d.Error = fmt.Sprintf("IPv6 request timed out: %v", err)
			return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
		}
		// Every address we were allowed to try failed for a reason the site
		// gave us — refused, reset, bad certificate: IPv6 HTTPS doesn't work
		// here — unsupported, not an internal error.
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

// fetchAny walks one family's addresses in order and returns the first that
// answers, up to maxAddressAttempts (review issue 63).
//
// A blocked address is skipped rather than fatal and does not consume an
// attempt: a rotation with one address in a blocked range still has the
// others. Only when nothing was probed at all does this report
// errNoUsableAddress, which is the case that stays an error.
func (c *ResponseParity) fetchAny(ctx context.Context, domain string, ips []net.IP, network string) (ParityFetch, error) {
	var lastErr error
	attempts := 0
	for _, ip := range ips {
		if attempts >= maxAddressAttempts {
			break
		}
		if err := c.dialer.ValidateIP(ip); err != nil {
			lastErr = err
			continue
		}
		attempts++
		f, err := c.fetch(ctx, domain, ip, network)
		if err == nil {
			return f, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break // the whole-check budget is gone; further tries cannot help
		}
	}
	if attempts == 0 {
		return ParityFetch{}, fmt.Errorf("%w: %w", errNoUsableAddress, lastErr)
	}
	return ParityFetch{}, lastErr
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

	return ParityFetch{
		Address:     ip.String(),
		StatusCode:  resp.StatusCode,
		ContentType: sanitizeText(resp.Header.Get("Content-Type")),
		// The bytes actually read, never resp.ContentLength: the header is
		// uncapped and absent on a chunked or gunzipped response, so mixing
		// the two measures different quantities per family (01 §11.8 —
		// "body read up to maxBodySize for length measurement").
		ContentLength:  int64(len(body)),
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
