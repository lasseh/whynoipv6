package checker

import (
	"context"
	"net"
	"sort"
	"time"
)

// DNSMXIPv6 checks whether the domain's MX records have AAAA records.
type DNSMXIPv6 struct {
	dialer     *SafeDialer
	maxLookups int // config checks.max_mx_lookups (01-engine.md §11.4)
}

// NewDNSMXIPv6 creates a new dns_mx_ipv6 checker.
func NewDNSMXIPv6(dialer *SafeDialer, maxLookups int) *DNSMXIPv6 {
	return &DNSMXIPv6{dialer: dialer, maxLookups: maxLookups}
}

func (c *DNSMXIPv6) Name() string { return "dns_mx_ipv6" }
func (c *DNSMXIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	details := map[string]any{}

	mxRecords, _, err := c.dialer.Resolver().LookupMX(ctx, domain)
	if err != nil {
		details["error"] = err.Error()
		return Result{
			Status:  StatusError,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}

	if len(mxRecords) == 0 {
		// Kind-aware skip (01-engine.md §11.4): "the AAAA accepts mail" is
		// not evidence for a subdomain entity — no implicit-MX fallback.
		if kind == KindSubdomain {
			return Result{
				Status:  StatusNotApplicable,
				Details: map[string]any{"reason": "no explicit MX records (subdomain entity)"},
				Latency: time.Since(start),
			}, nil
		}
		// RFC 5321 §5.1: when no MX records exist, fall back to the domain
		// itself as an implicit MX. Check if the domain has AAAA records.
		ips, _, _, _, lookupErr := c.dialer.Resolver().LookupAAAA(ctx, domain)
		if lookupErr != nil || len(ips) == 0 {
			return Result{
				Status:  StatusNotApplicable,
				Details: map[string]any{"reason": "no MX records and no implicit AAAA fallback"},
				Latency: time.Since(start),
			}, nil
		}
		addrs := make([]string, len(ips))
		for i, ip := range ips {
			addrs[i] = ip.String()
		}
		return Result{
			Status: StatusSupported,
			Details: map[string]any{
				"reason":    "implicit MX fallback (RFC 5321 §5.1)",
				"addresses": addrs,
			},
			Latency: time.Since(start),
		}, nil
	}

	// Check for null MX (RFC 7505).
	if len(mxRecords) == 1 && mxRecords[0].Mx == "." && mxRecords[0].Preference == 0 {
		return Result{
			Status:  StatusNotApplicable,
			Details: map[string]any{"reason": "null MX record"},
			Latency: time.Since(start),
		}, nil
	}

	// Sort by preference (lowest first) and cap.
	sort.Slice(mxRecords, func(i, j int) bool {
		return mxRecords[i].Preference < mxRecords[j].Preference
	})
	if len(mxRecords) > c.maxLookups {
		mxRecords = mxRecords[:c.maxLookups]
	}

	mxResults := map[string]any{}
	ipv6Count := 0
	total := len(mxRecords)

	for _, mx := range mxRecords {
		hostname := mx.Mx
		mxInfo := map[string]any{
			"preference": mx.Preference,
			"has_ipv6":   false,
			"addresses":  []string{},
		}

		// Skip if MX points to an IP address (non-standard).
		if isIPAddress(hostname) {
			mxResults[hostname] = mxInfo
			continue
		}

		ips, _, _, _, lookupErr := c.dialer.Resolver().LookupAAAA(ctx, hostname)
		if lookupErr == nil && len(ips) > 0 {
			mxInfo["has_ipv6"] = true
			addrs := make([]string, len(ips))
			for i, ip := range ips {
				addrs[i] = ip.String()
			}
			mxInfo["addresses"] = addrs
			ipv6Count++
		}
		mxResults[hostname] = mxInfo
	}

	details["mx_records"] = mxResults
	details["total"] = total
	details["ipv6_count"] = ipv6Count

	var status CheckStatus
	switch {
	case ipv6Count == total:
		status = StatusSupported
	case ipv6Count > 0:
		status = StatusPartial
	default:
		status = StatusUnsupported
	}

	return Result{
		Status:  status,
		Details: details,
		Latency: time.Since(start),
	}, nil
}

// isIPAddress checks if a string (potentially FQDN) is actually an IP.
func isIPAddress(s string) bool {
	// Strip trailing dot from FQDN.
	if s != "" && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return net.ParseIP(s) != nil
}
