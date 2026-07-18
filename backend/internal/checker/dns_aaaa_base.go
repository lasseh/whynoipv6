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

func (c *DNSAAAABase) Name() string { return NameDNSAAAABase }

func (c *DNSAAAABase) Check(ctx context.Context, host string, _ Kind) (Result, error) {
	start := time.Now()
	// 15s: quorum fan-out worst case + the conditional bulk A lookup
	// (01-engine.md §11.1 Decision).
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	ans, err := c.res.LookupAAAA(ctx, host)
	d := &AAAADetail{
		Rcode:      ans.Rcode,
		CNAMEChain: ans.CNAMEChain,
		Quorum:     ans.Quorum,
		AOutcome:   ans.AOutcome,
		CDOutcome:  ans.CDOutcome,
	}
	if ans.AIP != nil {
		d.AAddress = ans.AIP.String() // v4-only attribution input (06 §6.2)
	}

	if errors.Is(err, ErrQuorumInconsistent) {
		d.Inconsistent = true
		return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
	}
	if err != nil {
		d.Error = err.Error()
		return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
	}

	// NXDOMAIN: raw engine status stays not_applicable with the raw rcode
	// preserved — the observation layer maps base NXDOMAIN to no_record;
	// dead-detection requires NXDOMAIN specifically (02/03).
	if ans.Rcode == RcodeNXDomain {
		d.Reason = "domain does not exist"
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
