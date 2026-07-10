package checker

import (
	"context"
	"log/slog"
	"net"
	"sync/atomic"
	"time"
)

// PreflightFreshness is the window within which a passed preflight makes
// conn=unsupported observations definitive (02-observation-model.md, conn
// rows 5a/5b).
const PreflightFreshness = 5 * time.Minute

// Preflight verifies and tracks this process's IPv6 connectivity
// (01-engine.md §12 — a v6-dark crawler mass-producing false `unsupported`
// is the #1 false-negative source).
type Preflight struct {
	res       *Resolver // the bulk resolver
	probeHost string    // config preflight.probe_host, "host:port"
	logger    *slog.Logger
	lastPass  atomic.Int64 // unix nanos of the last successful probe; 0 = never
}

// NewPreflight builds the preflight prober. probeHost must carry a port.
func NewPreflight(res *Resolver, probeHost string, logger *slog.Logger) *Preflight {
	return &Preflight{res: res, probeHost: probeHost, logger: logger}
}

// Run performs one probe: AAAA-resolve the host part of probeHost via the
// bulk resolver, then tcp6-dial the first address on the port part with a 5s
// dialer timeout (net.Dialer directly — the probe target is public and
// fixed; SSRF validation is unnecessary and the SafeDialer would resolve a
// second time). Success records the pass time; failure logs at Error and
// leaves lastPass untouched.
func (p *Preflight) Run(ctx context.Context) bool {
	host, port, err := net.SplitHostPort(p.probeHost)
	if err != nil {
		p.logger.Error("ipv6 preflight: probe host must be host:port", "host", p.probeHost, "error", err)
		return false
	}

	ips, _, _, _, err := p.res.LookupAAAA(ctx, host)
	if err != nil {
		p.logger.Error("ipv6 preflight: AAAA lookup failed", "host", host, "error", err)
		return false
	}
	if len(ips) == 0 {
		p.logger.Error("ipv6 preflight: no AAAA records", "host", host)
		return false
	}

	addr := net.JoinHostPort(ips[0].String(), port)
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp6", addr)
	if err != nil {
		p.logger.Error("ipv6 preflight: tcp6 dial failed", "host", host, "addr", addr, "error", err)
		return false
	}
	_ = conn.Close()

	p.lastPass.Store(time.Now().UnixNano())
	p.logger.Debug("ipv6 preflight: connectivity ok", "host", host, "addr", addr)
	return true
}

// PassedWithin reports whether the last successful probe is younger than d.
func (p *Preflight) PassedWithin(d time.Duration) bool {
	last := p.lastPass.Load()
	return last != 0 && time.Since(time.Unix(0, last)) < d
}

// LastPass returns the time of the last successful probe (the zero Time if
// none yet). It is the worker's source for the mapper's preflightPassedAt
// input (02-observation-model.md — MapObservations).
func (p *Preflight) LastPass() time.Time {
	last := p.lastPass.Load()
	if last == 0 {
		return time.Time{}
	}
	return time.Unix(0, last)
}
