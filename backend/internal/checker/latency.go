package checker

import (
	"context"
	"crypto/x509"
	"net"
	"slices"
	"time"
)

const (
	latencyMeasurements = 3
	latencyTimeout      = 10 * time.Second
)

// LatencyIPv4 measures HTTP response time over IPv4 (01-engine.md §11.14).
type LatencyIPv4 struct {
	dialer  *SafeDialer
	port    string
	rootCAs *x509.CertPool
}

// NewLatencyIPv4 creates a new latency_ipv4 checker.
func NewLatencyIPv4(dialer *SafeDialer) *LatencyIPv4 {
	return &LatencyIPv4{dialer: dialer, port: "443"}
}

func (c *LatencyIPv4) Name() string { return NameLatencyV4 }
func (c *LatencyIPv4) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ips, err := c.dialer.Resolver().LookupA(ctx, domain)
	if err != nil {
		// Resolver trouble is transient (error), not "no A record".
		return Result{
			Status:  StatusError,
			Detail:  &LatencyDetail{CommonDetail: CommonDetail{Error: err.Error()}},
			Latency: time.Since(start),
		}, nil
	}
	if len(ips) == 0 {
		return Result{
			Status:  StatusNotApplicable,
			Detail:  &LatencyDetail{CommonDetail: CommonDetail{Reason: "no A record"}},
			Latency: time.Since(start),
		}, nil
	}

	ip := ips[0]
	if err := c.dialer.ValidateIP(ip); err != nil {
		return Result{
			Status:  StatusError,
			Detail:  &LatencyDetail{CommonDetail: CommonDetail{Error: errAddrBlocked}},
			Latency: time.Since(start),
		}, nil
	}

	return measureLatency(ctx, probe{dialer: c.dialer, port: c.port, rootCAs: c.rootCAs}, domain, ip, "tcp4", start)
}

// LatencyIPv6 measures HTTP response time over IPv6 (01-engine.md §11.14).
type LatencyIPv6 struct {
	dialer  *SafeDialer
	port    string
	rootCAs *x509.CertPool
}

// NewLatencyIPv6 creates a new latency_ipv6 checker.
func NewLatencyIPv6(dialer *SafeDialer) *LatencyIPv6 {
	return &LatencyIPv6{dialer: dialer, port: "443"}
}

func (c *LatencyIPv6) Name() string { return NameLatencyV6 }
func (c *LatencyIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ips, _, _, _, err := c.dialer.Resolver().LookupAAAA(ctx, domain)
	if err != nil {
		return Result{
			Status:  StatusError,
			Detail:  &LatencyDetail{CommonDetail: CommonDetail{Error: err.Error()}},
			Latency: time.Since(start),
		}, nil
	}
	if len(ips) == 0 {
		return Result{
			Status:  StatusNotApplicable,
			Detail:  &LatencyDetail{CommonDetail: CommonDetail{Reason: errNoAAAARecord}},
			Latency: time.Since(start),
		}, nil
	}

	ip := ips[0]
	if err := c.dialer.ValidateIP(ip); err != nil {
		return Result{
			Status:  StatusError,
			Detail:  &LatencyDetail{CommonDetail: CommonDetail{Error: errAddrBlocked}},
			Latency: time.Since(start),
		}, nil
	}

	return measureLatency(ctx, probe{dialer: c.dialer, port: c.port, rootCAs: c.rootCAs}, domain, ip, "tcp6", start)
}

// measureLatency performs latency measurements (3 requests, discard highest, average remaining 2).
func measureLatency(ctx context.Context, p probe, domain string, ip net.IP, network string, start time.Time) (Result, error) { //nolint:unparam // error is always nil but kept for interface consistency

	var measurements []int64

	for range latencyMeasurements {
		if ctx.Err() != nil {
			break
		}

		ttfb, err := measureTTFB(ctx, p, domain, ip, network)
		if err != nil {
			if len(measurements) == 0 {
				return Result{
					Status:  StatusError,
					Detail:  &LatencyDetail{CommonDetail: CommonDetail{Error: err.Error()}, Address: ip.String()},
					Latency: time.Since(start),
				}, nil
			}
			continue
		}
		measurements = append(measurements, ttfb)
	}

	if len(measurements) == 0 {
		return Result{
			Status:  StatusError,
			Detail:  &LatencyDetail{CommonDetail: CommonDetail{Error: "all measurements failed"}, Address: ip.String()},
			Latency: time.Since(start),
		}, nil
	}

	// Discard highest, average remaining.
	slices.Sort(measurements)
	var avgMS int64
	if len(measurements) >= 2 {
		// Discard the highest value (last after sort).
		sum := int64(0)
		count := len(measurements) - 1
		for i := range count {
			sum += measurements[i]
		}
		avgMS = sum / int64(count)
	} else {
		avgMS = measurements[0]
	}

	return Result{
		Status: StatusSupported,
		Detail: &LatencyDetail{
			Address:      ip.String(),
			TTFBMS:       &avgMS,
			Measurements: measurements,
			AvgMS:        &avgMS,
		},
		Latency: time.Since(start),
	}, nil
}

// measureTTFB measures time to first byte for a single HTTPS request.
func measureTTFB(ctx context.Context, p probe, domain string, ip net.IP, network string) (int64, error) {
	reqCtx, cancel := context.WithTimeout(ctx, latencyTimeout)
	defer cancel()

	reqStart := time.Now()
	resp, err := p.get(reqCtx, ip, domain, "https", probeOptions{TLS: true, Network: network})
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	ttfb := time.Since(reqStart).Milliseconds()
	return ttfb, nil
}
