package checker

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

const maxPTRAddresses = 3

// DNSPTRIPv6 checks reverse DNS (PTR) records for the domain's IPv6 addresses
// (01-engine.md §11.12).
type DNSPTRIPv6 struct {
	dialer *SafeDialer
}

// NewDNSPTRIPv6 creates a new dns_ptr_ipv6 checker.
func NewDNSPTRIPv6(dialer *SafeDialer) *DNSPTRIPv6 {
	return &DNSPTRIPv6{dialer: dialer}
}

func (c *DNSPTRIPv6) Name() string { return NamePTR }
func (c *DNSPTRIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	d := &PTRDetail{}

	// Resolve AAAA records.
	ips, _, _, _, err := c.dialer.Resolver().LookupAAAA(ctx, domain)
	if err != nil || len(ips) == 0 {
		d.Reason = errNoAAAARecord
		return Result{
			Status:  StatusNotApplicable,
			Detail:  d,
			Latency: time.Since(start),
		}, nil
	}

	if len(ips) > maxPTRAddresses {
		ips = ips[:maxPTRAddresses]
	}

	var checks []PTRCheck
	allConfirmed := true
	anyPTR := false

	for _, ip := range ips {
		reverseName := reverseIPv6(ip)
		ptrNames, lookupErr := c.dialer.Resolver().LookupPTR(ctx, reverseName)
		if lookupErr != nil || len(ptrNames) == 0 {
			checks = append(checks, PTRCheck{
				Address:          ip.String(),
				ForwardConfirmed: false,
			})
			allConfirmed = false
			continue
		}

		anyPTR = true
		ptrName := ptrNames[0]

		// Forward-confirmed reverse DNS: check if PTR resolves back.
		confirmed := false
		fwdIPs, _, _, _, fwdErr := c.dialer.Resolver().LookupAAAA(ctx, ptrName)
		if fwdErr == nil {
			for _, fwdIP := range fwdIPs {
				if fwdIP.Equal(ip) {
					confirmed = true
					break
				}
			}
		}

		if !confirmed {
			allConfirmed = false
		}

		checks = append(checks, PTRCheck{
			Address:          ip.String(),
			PTRName:          ptrName,
			ForwardConfirmed: confirmed,
		})
	}

	d.Checks = checks
	d.AllConfirmed = &allConfirmed

	var status CheckStatus
	switch {
	case allConfirmed && anyPTR:
		status = StatusSupported
	case anyPTR:
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

// reverseIPv6 constructs the reverse DNS name for an IPv6 address.
func reverseIPv6(ip net.IP) string {
	ip = ip.To16()
	if ip == nil {
		return ""
	}
	var buf strings.Builder
	for i := len(ip) - 1; i >= 0; i-- {
		_, _ = fmt.Fprintf(&buf, "%x.%x.", ip[i]&0x0f, ip[i]>>4)
	}
	buf.WriteString("ip6.arpa.")
	return buf.String()
}
