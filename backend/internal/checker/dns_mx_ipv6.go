package checker

import (
	"context"
	"net"
	"sort"
	"time"
)

// DNSMXIPv6 checks whether the domain's MX records have AAAA records
// (01-engine.md §11.4).
type DNSMXIPv6 struct {
	dialer     *SafeDialer
	maxLookups int // config checks.max_mx_lookups (01-engine.md §11.4)
}

// NewDNSMXIPv6 creates a new dns_mx_ipv6 checker.
func NewDNSMXIPv6(dialer *SafeDialer, maxLookups int) *DNSMXIPv6 {
	return &DNSMXIPv6{dialer: dialer, maxLookups: maxLookups}
}

func (c *DNSMXIPv6) Name() string { return NameDNSMX }
func (c *DNSMXIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	d := &MXDetail{}

	mxRecords, rcode, err := c.dialer.Resolver().LookupMX(ctx, domain)
	if err != nil {
		d.Error = err.Error()
		return Result{
			Status:  StatusError,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}
	// A SERVFAIL/REFUSED answer is resolver trouble, not "no MX": read as
	// empty it would run the implicit-MX fallback and land on a definitive
	// not_applicable (02 §4). NXDOMAIN is a real empty answer.
	if rcode != RcodeNoError && rcode != RcodeNXDomain {
		d.Error = "mx lookup for " + domain + " returned " + rcode
		return Result{
			Status:  StatusError,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	if len(mxRecords) == 0 {
		// Kind-aware skip (01-engine.md §11.4): "the AAAA accepts mail" is
		// not evidence for a subdomain entity — no implicit-MX fallback.
		if kind == KindSubdomain {
			d.Reason = "no explicit MX records (subdomain entity)"
			return Result{
				Status:  StatusNotApplicable,
				Detail:  d,
				Latency: time.Since(start),
			}, nil
		}
		// RFC 5321 §5.1: when no MX records exist, fall back to the domain
		// itself as an implicit MX. Check if the domain has AAAA records.
		ips, _, _, _, lookupErr := c.dialer.Resolver().LookupAAAA(ctx, domain)
		if lookupErr != nil {
			d.Error = lookupErr.Error()
			return Result{
				Status:  StatusError,
				Detail:  d,
				Latency: time.Since(start),
			}, nil
		}
		if len(ips) == 0 {
			d.Reason = "no MX records and no implicit AAAA fallback"
			return Result{
				Status:  StatusNotApplicable,
				Detail:  d,
				Latency: time.Since(start),
			}, nil
		}
		addrs := make([]string, len(ips))
		for i, ip := range ips {
			addrs[i] = ip.String()
		}
		d.Reason = "implicit MX fallback (RFC 5321 §5.1)"
		d.Addresses = addrs
		return Result{
			Status:  StatusSupported,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	// Check for null MX (RFC 7505).
	if len(mxRecords) == 1 && mxRecords[0].Mx == "." && mxRecords[0].Preference == 0 {
		d.Reason = "null MX record"
		return Result{
			Status:  StatusNotApplicable,
			Detail:  d,
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

	mxResults := map[string]MXHost{}
	ipv6Count := 0
	total := len(mxRecords)
	looked, answered := 0, 0 // hosts looked up; hosts whose lookup answered
	var lastLookupErr error

	for _, mx := range mxRecords {
		hostname := mx.Mx
		mxInfo := MXHost{Preference: mx.Preference, Addresses: []string{}}

		// Skip if MX points to an IP address (non-standard).
		if isIPAddress(hostname) {
			mxResults[hostname] = mxInfo
			continue
		}

		looked++
		ips, _, _, _, lookupErr := c.dialer.Resolver().LookupAAAA(ctx, hostname)
		if lookupErr == nil {
			answered++
		} else {
			lastLookupErr = lookupErr
		}
		if lookupErr == nil && len(ips) > 0 {
			mxInfo.HasIPv6 = true
			addrs := make([]string, len(ips))
			for i, ip := range ips {
				addrs[i] = ip.String()
			}
			mxInfo.Addresses = addrs
			ipv6Count++
		}
		mxResults[hostname] = mxInfo
	}

	d.MXRecords = mxResults
	d.Total = total
	d.IPv6Count = &ipv6Count

	// Same rule as dns_ns_ipv6: no answering host means no verdict, and a
	// zero lookup cap cannot read as "every MX has AAAA".
	if total == 0 || (looked > 0 && answered == 0) {
		d.Error = "no MX host AAAA lookup answered"
		if lastLookupErr != nil {
			d.Error = lastLookupErr.Error()
		}
		return Result{
			Status:  StatusError,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

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
		Detail:  d,
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
