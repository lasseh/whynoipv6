package checker

import (
	"context"
	"fmt"
	"time"

	"github.com/miekg/dns"
)

// DNSSEC checks whether the domain has a valid DNSSEC chain of trust
// (01-engine.md §11.5; transport note §11.13).
// Uses the AD (Authenticated Data) flag from a validating resolver to determine
// whether the full chain of trust from root to zone is intact, rather than
// manually checking individual DS/DNSKEY/RRSIG records.
type DNSSEC struct {
	dialer *SafeDialer
}

// NewDNSSEC creates a new dns_dnssec checker.
func NewDNSSEC(dialer *SafeDialer) *DNSSEC {
	return &DNSSEC{dialer: dialer}
}

func (c *DNSSEC) Name() string { return NameDNSSEC }
func (c *DNSSEC) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	d := &DNSSECDetail{}

	fqdn := dns.Fqdn(domain)
	resolver := c.dialer.Resolver()

	// Step 1: ask the validating resolver for the DS RRset at the child name —
	// its presence means the zone is signed.
	dsRecords, err := c.queryDS(ctx, resolver, fqdn)
	if err != nil {
		d.Error = fmt.Sprintf("DS lookup failed: %v", err)
		return Result{
			Status:  StatusError,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	if len(dsRecords) == 0 {
		// No DS record in parent zone — domain is not signed.
		return Result{
			Status:  StatusUnsupported,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	d.Signed = true
	dsInfo := make([]DSRecord, len(dsRecords))
	for i, ds := range dsRecords {
		dsInfo[i] = DSRecord{
			KeyTag:     ds.KeyTag,
			Algorithm:  dns.AlgorithmToString[ds.Algorithm],
			DigestType: ds.DigestType,
		}
	}
	d.DSRecords = dsInfo

	// Step 2: Query with RD=1, CD=0 and check AD flag.
	// The validating resolver performs full chain-of-trust verification
	// (DS→DNSKEY→RRSIG signatures, expiration, NSEC/NSEC3) and sets AD=1
	// only if everything validates correctly.
	adValid, err := c.checkADFlag(ctx, resolver, fqdn)
	if err != nil {
		d.Error = fmt.Sprintf("AD flag check failed: %v", err)
		chain := false
		d.ChainComplete = &chain
		return Result{
			Status:  StatusError,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	d.ChainComplete = &adValid
	d.ADFlag = &adValid

	if !adValid {
		d.Error = "DNSSEC signed but validation failed (AD=0)"
		return Result{
			Status:  StatusError,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	return Result{
		Status:  StatusSupported,
		Detail:  d,
		Latency: time.Since(start),
	}, nil
}

// queryDS asks the validating resolver for the DS RRset at fqdn.
func (c *DNSSEC) queryDS(ctx context.Context, resolver *Resolver, fqdn string) ([]*dns.DS, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(fqdn, dns.TypeDS)
	msg.RecursionDesired = true
	msg.SetEdns0(4096, true)

	resp, err := resolver.QueryWithRetry(ctx, msg)
	if err != nil {
		return nil, err
	}

	var records []*dns.DS
	for _, rr := range resp.Answer {
		if ds, ok := rr.(*dns.DS); ok {
			records = append(records, ds)
		}
	}
	return records, nil
}

// checkADFlag queries a record with RD=1 and CD=0 (default) and checks the AD
// flag in the response. A validating resolver sets AD=1 only when the full DNSSEC
// chain of trust validates successfully (cryptographic signatures, expiration, etc.).
func (c *DNSSEC) checkADFlag(ctx context.Context, resolver *Resolver, fqdn string) (bool, error) {
	// Try SOA first (always exists for a valid zone), then A as fallback.
	for _, qtype := range []uint16{dns.TypeSOA, dns.TypeA} {
		msg := new(dns.Msg)
		msg.SetQuestion(fqdn, qtype)
		msg.RecursionDesired = true
		// CD (Checking Disabled) must be false (default) so the resolver validates.
		// Do NOT set DO bit — we want the resolver to validate and report via AD flag.
		msg.SetEdns0(4096, false)

		resp, err := resolver.QueryWithRetry(ctx, msg)
		if err != nil {
			continue
		}

		// AD flag is set by the validating resolver when the answer is authenticated.
		if resp.AuthenticatedData {
			return true, nil
		}

		// If we got a valid response (not SERVFAIL), the AD flag absence is meaningful.
		if resp.Rcode == dns.RcodeSuccess || resp.Rcode == dns.RcodeNameError {
			return false, nil
		}
	}

	return false, fmt.Errorf("could not query domain for AD flag validation")
}
