package checker

import (
	"context"
	"errors"
	"strings"
	"time"
)

// CDNSuffixTags are the CNAME target suffixes that indicate CDN usage
// (01-engine.md §11.2), each mapped to the normalized hosting tag the commit
// path writes (06-ingest.md §6.10, erratum: single-sourced here beside
// §11.2's list rather than in ingest).
//
//nolint:goconst // a literal data table; repeated tags are values
var CDNSuffixTags = map[string]string{
	"cloudfront.net":        "cloudfront",
	"cloudflare.net":        "cloudflare",
	"akamaiedge.net":        "akamai",
	"akamai.net":            "akamai",
	"fastly.net":            "fastly",
	"edgekey.net":           "akamai",
	"azureedge.net":         "azure",
	"cdn.cloudflarenet.com": "cloudflare",
	"edgecastcdn.net":       "edgecast",
	"stackpathdns.com":      "stackpath",
	"googleapis.com":        "google",
}

// DNSAAAAWWW checks whether www.<host> has at least one AAAA record,
// resolved through the consensus AAAAResolver seam (01-engine.md §11.2).
// For kind=subdomain the runner skips this check entirely.
type DNSAAAAWWW struct {
	res AAAAResolver
}

// NewDNSAAAAWWW creates a new dns_aaaa_www checker.
func NewDNSAAAAWWW(res AAAAResolver) *DNSAAAAWWW {
	return &DNSAAAAWWW{res: res}
}

func (c *DNSAAAAWWW) Name() string { return NameDNSAAAAWWW }

func (c *DNSAAAAWWW) Check(ctx context.Context, host string, _ Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	wwwDomain := "www." + host

	ans, err := c.res.LookupAAAA(ctx, wwwDomain)
	d := &AAAADetail{
		Rcode:      ans.Rcode,
		CNAMEChain: ans.CNAMEChain,
		Quorum:     ans.Quorum,
		AOutcome:   ans.AOutcome,
		CDOutcome:  ans.CDOutcome,
	}
	if len(ans.CNAMEChain) > 0 {
		d.CNAMETarget = ans.CNAMEChain[len(ans.CNAMEChain)-1]

		// Detect CDN usage.
		for _, cname := range ans.CNAMEChain {
			for pattern := range CDNSuffixTags {
				if strings.HasSuffix(strings.TrimSuffix(cname, "."), pattern) {
					d.CDNDetected = true
					break
				}
			}
		}
	}

	if errors.Is(err, ErrQuorumInconsistent) {
		d.Inconsistent = true
		return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
	}
	if err != nil {
		d.Error = err.Error()
		return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
	}

	// NXDOMAIN means the www subdomain doesn't exist — the domain simply
	// doesn't use www, so this check is not applicable (not unsupported).
	if ans.Rcode == RcodeNXDomain {
		d.Reason = "www subdomain does not exist"
		return Result{Status: StatusNotApplicable, Detail: d, Latency: time.Since(start)}, nil
	}

	if len(ans.IPs) == 0 {
		return Result{Status: StatusUnsupported, Detail: d, Latency: time.Since(start)}, nil
	}

	addrs := make([]string, len(ans.IPs))
	for i, ip := range ans.IPs {
		addrs[i] = ip.String()
	}
	d.Addresses = addrs
	ttl := ans.TTL
	d.TTL = &ttl

	return Result{Status: StatusSupported, Detail: d, Latency: time.Since(start)}, nil
}
