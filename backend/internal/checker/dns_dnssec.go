package checker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// DNSSEC checks whether the domain has a valid DNSSEC chain of trust.
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

func (c *DNSSEC) Name() string { return "dns_dnssec" }
func (c *DNSSEC) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	details := map[string]any{
		"signed": false,
	}

	fqdn := dns.Fqdn(domain)
	resolver := c.dialer.Resolver()

	// Step 1: Check for DS record in parent zone to determine if domain is signed.
	parentZone := parentZoneOf(fqdn)
	dsRecords, err := c.queryDS(ctx, resolver, fqdn, parentZone)
	if err != nil {
		details["error"] = fmt.Sprintf("DS lookup failed: %v", err)
		return Result{
			Status:  StatusError,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}

	if len(dsRecords) == 0 {
		// No DS record in parent zone — domain is not signed.
		return Result{
			Status:  StatusUnsupported,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}

	details["signed"] = true
	dsInfo := make([]map[string]any, len(dsRecords))
	for i, ds := range dsRecords {
		dsInfo[i] = map[string]any{
			"key_tag":     ds.KeyTag,
			"algorithm":   dns.AlgorithmToString[ds.Algorithm],
			"digest_type": ds.DigestType,
		}
	}
	details["ds_records"] = dsInfo

	// Step 2: Query with RD=1, CD=0 and check AD flag.
	// The validating resolver performs full chain-of-trust verification
	// (DS→DNSKEY→RRSIG signatures, expiration, NSEC/NSEC3) and sets AD=1
	// only if everything validates correctly.
	adValid, err := c.checkADFlag(ctx, resolver, fqdn)
	if err != nil {
		details["error"] = fmt.Sprintf("AD flag check failed: %v", err)
		details["chain_complete"] = false
		return Result{
			Status:  StatusError,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}

	details["chain_complete"] = adValid
	details["ad_flag"] = adValid

	if !adValid {
		details["error"] = "DNSSEC signed but validation failed (AD=0)"
		return Result{
			Status:  StatusError,
			Details: details,
			Latency: time.Since(start),
		}, nil
	}

	return Result{
		Status:  StatusSupported,
		Details: details,
		Latency: time.Since(start),
	}, nil
}

// parentZoneOf returns the parent zone of a FQDN.
// For "example.com.", it returns "com.".
func parentZoneOf(fqdn string) string {
	labels := dns.SplitDomainName(fqdn)
	if len(labels) <= 1 {
		return "."
	}
	return dns.Fqdn(strings.Join(labels[1:], "."))
}

// queryDS queries the parent zone for DS records of the domain.
func (c *DNSSEC) queryDS(ctx context.Context, resolver *Resolver, fqdn, _ string) ([]*dns.DS, error) {
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
