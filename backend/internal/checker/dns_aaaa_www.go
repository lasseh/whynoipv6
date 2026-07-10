package checker

import (
	"context"
	"errors"
	"strings"
	"time"
)

// knownCDNPatterns are CNAME targets that indicate CDN usage.
var knownCDNPatterns = []string{
	"cloudfront.net",
	"cloudflare.net",
	"akamaiedge.net",
	"akamai.net",
	"fastly.net",
	"edgekey.net",
	"azureedge.net",
	"cdn.cloudflarenet.com",
	"edgecastcdn.net",
	"stackpathdns.com",
	"googleapis.com",
}

// DNSAAAAWww checks whether www.<host> has at least one AAAA record,
// resolved through the consensus AAAAResolver seam (01-engine.md §11.2).
// For kind=subdomain the runner skips this check entirely.
type DNSAAAAWww struct {
	res AAAAResolver
}

// NewDNSAAAAWWW creates a new dns_aaaa_www checker.
func NewDNSAAAAWWW(res AAAAResolver) *DNSAAAAWww {
	return &DNSAAAAWww{res: res}
}

func (c *DNSAAAAWww) Name() string { return "dns_aaaa_www" }

func (c *DNSAAAAWww) Check(ctx context.Context, host string, _ Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	wwwDomain := "www." + host
	details := map[string]any{}

	ans, err := c.res.LookupAAAA(ctx, wwwDomain)
	details["rcode"] = ans.Rcode
	if len(ans.CNAMEChain) > 0 {
		details["cname_chain"] = ans.CNAMEChain
		details["cname_target"] = ans.CNAMEChain[len(ans.CNAMEChain)-1]

		// Detect CDN usage.
		for _, cname := range ans.CNAMEChain {
			for _, pattern := range knownCDNPatterns {
				if strings.HasSuffix(strings.TrimSuffix(cname, "."), pattern) {
					details["cdn_detected"] = true
					break
				}
			}
		}
	}
	if ans.Quorum != nil {
		details["quorum"] = ans.Quorum
	}
	if ans.AOutcome != "" {
		details["a_outcome"] = ans.AOutcome
	}
	if ans.CDOutcome != "" {
		details["cd_outcome"] = ans.CDOutcome
	}

	if errors.Is(err, ErrQuorumInconsistent) {
		details["inconsistent"] = true
		return Result{Status: StatusError, Details: details, Latency: time.Since(start)}, nil
	}
	if err != nil {
		details["error"] = err.Error()
		return Result{Status: StatusError, Details: details, Latency: time.Since(start)}, nil
	}

	// NXDOMAIN means the www subdomain doesn't exist — the domain simply
	// doesn't use www, so this check is not applicable (not unsupported).
	if ans.Rcode == "NXDOMAIN" {
		details["reason"] = "www subdomain does not exist"
		return Result{Status: StatusNotApplicable, Details: details, Latency: time.Since(start)}, nil
	}

	if len(ans.IPs) == 0 {
		return Result{Status: StatusUnsupported, Details: details, Latency: time.Since(start)}, nil
	}

	addrs := make([]string, len(ans.IPs))
	for i, ip := range ans.IPs {
		addrs[i] = ip.String()
	}
	details["addresses"] = addrs
	details["ttl"] = ans.TTL

	return Result{Status: StatusSupported, Details: details, Latency: time.Since(start)}, nil
}
