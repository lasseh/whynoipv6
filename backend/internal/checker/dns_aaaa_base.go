package checker

import (
	"context"
	"errors"
	"time"
)

// DNSAAAABase checks whether the entity host has at least one AAAA record,
// resolved through the consensus AAAAResolver seam (01-engine.md §11.1).
type DNSAAAABase struct {
	res AAAAResolver
}

// NewDNSAAAABase creates a new dns_aaaa_base checker.
func NewDNSAAAABase(res AAAAResolver) *DNSAAAABase {
	return &DNSAAAABase{res: res}
}

func (c *DNSAAAABase) Name() string { return "dns_aaaa_base" }

func (c *DNSAAAABase) Check(ctx context.Context, host string, _ Kind) (Result, error) {
	start := time.Now()
	// 15s: quorum fan-out worst case + the conditional bulk A lookup
	// (01-engine.md §11.1 Decision).
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	details := map[string]any{}

	ans, err := c.res.LookupAAAA(ctx, host)
	details["rcode"] = ans.Rcode
	if len(ans.CNAMEChain) > 0 {
		details["cname_chain"] = ans.CNAMEChain
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

	// NXDOMAIN: raw engine status stays not_applicable with the raw rcode
	// preserved — the observation layer maps base NXDOMAIN to no_record;
	// dead-detection requires NXDOMAIN specifically (02/03).
	if ans.Rcode == "NXDOMAIN" {
		details["reason"] = "domain does not exist"
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
