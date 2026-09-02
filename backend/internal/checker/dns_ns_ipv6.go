package checker

import (
	"context"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

// DNSNSIPv6 checks whether the domain's authoritative nameservers have AAAA
// records (01-engine.md §11.3).
type DNSNSIPv6 struct {
	dialer     *SafeDialer
	maxLookups int // config checks.max_ns_lookups (01-engine.md §11.3)
}

// NewDNSNSIPv6 creates a new dns_ns_ipv6 checker.
func NewDNSNSIPv6(dialer *SafeDialer, maxLookups int) *DNSNSIPv6 {
	return &DNSNSIPv6{dialer: dialer, maxLookups: maxLookups}
}

func (c *DNSNSIPv6) Name() string { return NameDNSNS }
func (c *DNSNSIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	d := &NSDetail{}

	// Walk up domain labels to find the zone with NS records.
	// Subdomains (e.g. blog.example.com) typically don't have their own NS
	// records — those live at the zone apex (example.com).
	var nameservers []string
	var nsErr error
	qname := domain
	for {
		nameservers, nsErr = c.dialer.Resolver().LookupNS(ctx, qname)
		if nsErr == nil && len(nameservers) > 0 {
			break
		}
		// Move up one label: blog.example.co.uk → example.co.uk. Stop at the
		// registrable-domain boundary (01-engine.md §11.3): never query the
		// public suffix itself (com, co.uk, ...) — answering registry
		// nameservers would fabricate a "zone" for nonexistent domains and
		// break dead-detection branch (a) (03-state-machine.md §4). Only
		// the ICANN section is a registry boundary: a private-section
		// suffix (github.io, blogspot.com) is a real zone whose NS answer
		// is the delegated zone for the hosts under it.
		idx := strings.Index(qname, ".")
		if idx < 0 || idx == len(qname)-1 {
			break // bare name or trailing dot
		}
		qname = qname[idx+1:]
		if suffix, icann := publicsuffix.PublicSuffix(qname); icann && suffix == qname {
			nameservers, nsErr = nil, nil
			break // reached the public suffix
		}
	}

	if nsErr != nil && len(nameservers) == 0 {
		d.Error = nsErr.Error()
		return Result{
			Status:  StatusError,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	if len(nameservers) == 0 {
		d.Error = NoNSRecordsMessage
		return Result{
			Status:  StatusError,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	if qname != domain {
		d.Zone = qname
	}

	total := len(nameservers)

	// Sort by name and cap AAAA lookups to avoid excessive DNS queries.
	// The ratio (ipv6_count / checked) is extrapolated to the full set.
	sort.Strings(nameservers)
	checked := nameservers
	if len(checked) > c.maxLookups {
		checked = checked[:c.maxLookups]
	}

	nsResults := map[string]NSHost{}
	ipv6Count := 0
	answered := 0 // hosts whose AAAA lookup returned an answer, empty or not
	var lastLookupErr error

	for _, ns := range checked {
		ips, _, _, _, lookupErr := c.dialer.Resolver().LookupAAAA(ctx, ns)
		nsInfo := NSHost{Addresses: []string{}}
		if lookupErr == nil {
			answered++
		} else {
			lastLookupErr = lookupErr
		}
		if lookupErr == nil && len(ips) > 0 {
			nsInfo.HasIPv6 = true
			addrs := make([]string, len(ips))
			for i, ip := range ips {
				addrs[i] = ip.String()
			}
			nsInfo.Addresses = addrs
			ipv6Count++
		}
		nsResults[ns] = nsInfo
	}

	d.Nameservers = nsResults
	d.Total = total
	d.Checked = len(checked)
	d.IPv6Count = &ipv6Count

	// A definitive verdict needs at least one nameserver that actually
	// answered: an all-errors sample (resolver trouble, a SERVFAILing
	// nameserver zone) is `error`, never `unsupported` (02 §4), and a zero
	// lookup cap cannot read as "every checked host has AAAA".
	if len(checked) == 0 || (ipv6Count == 0 && answered == 0) {
		d.Error = "no nameserver AAAA lookup answered"
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
	case ipv6Count == len(checked):
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
