package checker

import (
	"context"
	"strings"
	"time"
)

const maxSPFLookups = 10

// SPFIPv6 checks whether the domain's SPF record authorizes IPv6 senders.
type SPFIPv6 struct {
	dialer *SafeDialer
}

// NewSPFIPv6 creates a new spf_ipv6 checker.
func NewSPFIPv6(dialer *SafeDialer) *SPFIPv6 {
	return &SPFIPv6{dialer: dialer}
}

func (c *SPFIPv6) Name() string { return "spf_ipv6" }

// spfQualifier represents an SPF qualifier (+, -, ~, ?).
type spfQualifier byte

const (
	qualPass     spfQualifier = '+'
	qualFail     spfQualifier = '-'
	qualSoftFail spfQualifier = '~'
	qualNeutral  spfQualifier = '?'
)

// parseQualifier extracts the qualifier and bare mechanism from an SPF term.
// Default qualifier is '+' (pass) per RFC 7208.
func parseQualifier(term string) (qual spfQualifier, mechanism string) {
	if term == "" {
		return qualPass, term
	}
	switch term[0] {
	case '+', '-', '~', '?':
		return spfQualifier(term[0]), term[1:]
	default:
		return qualPass, term
	}
}

// isPassQualifier returns true if the qualifier authorizes the sender.
func isPassQualifier(q spfQualifier) bool {
	return q == qualPass
}

func (c *SPFIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	d := &SPFDetail{}

	txtRecords, err := c.dialer.Resolver().LookupTXT(ctx, domain)
	if err != nil {
		d.Error = err.Error()
		return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
	}

	// Find SPF records. Must match "v=spf1" followed by space or end-of-string
	// to avoid matching "v=spf10" or similar (RFC 7208 §4.5).
	var spfRecords []string
	for _, txt := range txtRecords {
		lower := strings.ToLower(strings.TrimSpace(txt))
		if lower == "v=spf1" || strings.HasPrefix(lower, "v=spf1 ") {
			spfRecords = append(spfRecords, txt)
		}
	}

	if len(spfRecords) == 0 {
		d.Reason = "no SPF record"
		return Result{
			Status:  StatusNotApplicable,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	// Multiple SPF records is an error per RFC 7208.
	if len(spfRecords) > 1 {
		d.Error = "multiple SPF records found"
		return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
	}

	spfRecord := spfRecords[0]
	d.SPFRecord = spfRecord

	lookupCount := 0
	ip6Mechanisms := []string{}
	includeChain := []string{}
	includeHasIP6 := false

	// Parse mechanisms.
	parts := strings.Fields(spfRecord)
	hasDirectIP6 := false
	hasImplicitIP6 := false
	hasExplicitRejectIP6 := false
	mechanismMatched := false // tracks if any mechanism matched (for redirect= semantics)

	// Collect redirect= modifier — only evaluated if no mechanisms match.
	var redirectDomain string

	for _, part := range parts {
		lower := strings.ToLower(part)
		qual, mechanism := parseQualifier(lower)

		if strings.HasPrefix(mechanism, "ip6:") {
			mechanismMatched = true
			if isPassQualifier(qual) {
				hasDirectIP6 = true
				ip6Mechanisms = append(ip6Mechanisms, part)
			} else if qual == qualFail || qual == qualSoftFail {
				// Domain explicitly rejects IPv6 mail via -ip6: or ~ip6:.
				hasExplicitRejectIP6 = true
			}
			continue
		}

		if strings.HasPrefix(mechanism, "include:") {
			mechanismMatched = true
			includeDomain := strings.TrimPrefix(mechanism, "include:")
			includeChain = append(includeChain, includeDomain)
			lookupCount++
			if lookupCount > maxSPFLookups {
				d.Error = "too many DNS lookups"
				d.LookupCount = &lookupCount
				return Result{Status: StatusError, Detail: d, Latency: time.Since(start)}, nil
			}
			if c.includeHasIPv6(ctx, includeDomain, &lookupCount) {
				includeHasIP6 = true
			}
			continue
		}

		// redirect= is a modifier, not a mechanism. Only applies if no
		// mechanisms in this record matched. Collect it but don't evaluate yet.
		if after, ok := strings.CutPrefix(mechanism, "redirect="); ok {
			redirectDomain = after
			continue
		}

		// Check 'a' mechanism for implicit IPv6.
		if mechanism == "a" || strings.HasPrefix(mechanism, "a:") || strings.HasPrefix(mechanism, "a/") {
			mechanismMatched = true
			lookupCount++
			target := domain
			if after, ok := strings.CutPrefix(mechanism, "a:"); ok {
				target = after
				// Strip CIDR suffix if present.
				if idx := strings.Index(target, "/"); idx >= 0 {
					target = target[:idx]
				}
			}
			ips, _, _, _, lookupErr := c.dialer.Resolver().LookupAAAA(ctx, target)
			if lookupErr == nil && len(ips) > 0 && isPassQualifier(qual) {
				hasImplicitIP6 = true
			}
			continue
		}

		// Check 'mx' mechanism for implicit IPv6 by resolving MX hosts' AAAA records.
		if mechanism == "mx" || strings.HasPrefix(mechanism, "mx:") || strings.HasPrefix(mechanism, "mx/") {
			mechanismMatched = true
			lookupCount++
			target := domain
			if after, ok := strings.CutPrefix(mechanism, "mx:"); ok {
				target = after
				if idx := strings.Index(target, "/"); idx >= 0 {
					target = target[:idx]
				}
			}
			if isPassQualifier(qual) {
				mxRecords, _, mxErr := c.dialer.Resolver().LookupMX(ctx, target)
				if mxErr == nil {
					for _, mx := range mxRecords {
						lookupCount++
						if lookupCount > maxSPFLookups {
							break
						}
						mxIPs, _, _, _, mxAAAAErr := c.dialer.Resolver().LookupAAAA(ctx, mx.Mx)
						if mxAAAAErr == nil && len(mxIPs) > 0 {
							hasImplicitIP6 = true
							break
						}
					}
				}
			}
			continue
		}

		// "all" is a catch-all mechanism — it always matches.
		if mechanism == "all" {
			mechanismMatched = true
			continue
		}
	}

	// Evaluate redirect= only if no mechanisms in this record matched.
	// Per RFC 7208 §6.1: redirect is only evaluated after all mechanisms are checked.
	if redirectDomain != "" && !mechanismMatched {
		lookupCount++
		if lookupCount <= maxSPFLookups {
			if c.includeHasIPv6(ctx, redirectDomain, &lookupCount) {
				includeHasIP6 = true
			}
		}
	}

	hasIP6 := hasDirectIP6 || includeHasIP6
	d.HasIP6Mechanism = &hasIP6
	d.IP6Mechanisms = ip6Mechanisms
	d.IncludeHasIP6 = &includeHasIP6
	d.IncludeChain = includeChain
	d.LookupCount = &lookupCount

	var status CheckStatus
	switch {
	case hasExplicitRejectIP6 && !hasDirectIP6 && !includeHasIP6:
		// Domain explicitly rejects IPv6 senders.
		status = StatusUnsupported
		d.Reason = "SPF explicitly rejects IPv6"
	case hasDirectIP6 || includeHasIP6:
		status = StatusSupported
	case hasImplicitIP6:
		// The 'a' or 'mx' mechanism resolves to AAAA, which is valid IPv6 support.
		status = StatusSupported
		d.Implicit = true
	default:
		status = StatusUnsupported
	}

	return Result{
		Status:  status,
		Detail:  d,
		Latency: time.Since(start),
	}, nil
}

// includeHasIPv6 recursively checks if an included SPF domain has ip6: mechanisms
// with a pass qualifier.
func (c *SPFIPv6) includeHasIPv6(ctx context.Context, domain string, lookupCount *int) bool {
	if *lookupCount > maxSPFLookups {
		return false
	}

	txtRecords, err := c.dialer.Resolver().LookupTXT(ctx, domain)
	if err != nil {
		return false
	}

	for _, txt := range txtRecords {
		lower := strings.ToLower(strings.TrimSpace(txt))
		if lower != "v=spf1" && !strings.HasPrefix(lower, "v=spf1 ") {
			continue
		}

		parts := strings.FieldsSeq(txt)
		for part := range parts {
			lower := strings.ToLower(part)
			qual, mechanism := parseQualifier(lower)

			if strings.HasPrefix(mechanism, "ip6:") {
				// Only count pass qualifiers (+ip6: or ip6: with implicit +).
				if isPassQualifier(qual) {
					return true
				}
				continue
			}

			if strings.HasPrefix(mechanism, "include:") {
				*lookupCount++
				includeDomain := strings.TrimPrefix(mechanism, "include:")
				if c.includeHasIPv6(ctx, includeDomain, lookupCount) {
					return true
				}
			}

			// redirect= in included records: only follow if no mechanisms matched.
			// For simplicity in recursive includes, we follow redirects since we're
			// only looking for ip6: mechanisms, not evaluating full SPF results.
			if strings.HasPrefix(mechanism, "redirect=") {
				*lookupCount++
				redirectDomain := strings.TrimPrefix(mechanism, "redirect=")
				if c.includeHasIPv6(ctx, redirectDomain, lookupCount) {
					return true
				}
			}
		}
	}
	return false
}
