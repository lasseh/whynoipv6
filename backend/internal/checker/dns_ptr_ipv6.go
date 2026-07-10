package checker

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

const maxPTRAddresses = 3

// DNSPTRIPv6 checks reverse DNS (PTR) records for the domain's IPv6 addresses.
type DNSPTRIPv6 struct {
	dialer *SafeDialer
}

// NewDNSPTRIPv6 creates a new dns_ptr_ipv6 checker.
func NewDNSPTRIPv6(dialer *SafeDialer) *DNSPTRIPv6 {
	return &DNSPTRIPv6{dialer: dialer}
}

func (c *DNSPTRIPv6) Name() string { return "dns_ptr_ipv6" }
func (c *DNSPTRIPv6) Check(ctx context.Context, domain string, kind Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	details := map[string]any{}

	// Resolve AAAA records.
	ips, _, _, _, err := c.dialer.Resolver().LookupAAAA(ctx, domain)
	if err != nil || len(ips) == 0 {
		return Result{
			Status:  StatusNotApplicable,
			Details: map[string]any{"reason": "no AAAA record"},
			Latency: time.Since(start),
		}, nil
	}

	if len(ips) > maxPTRAddresses {
		ips = ips[:maxPTRAddresses]
	}

	type ptrCheck struct {
		Address          string `json:"address"`
		PTRName          string `json:"ptr_name"`
		ForwardConfirmed bool   `json:"forward_confirmed"`
	}

	var checks []ptrCheck
	allConfirmed := true
	anyPTR := false

	for _, ip := range ips {
		reverseName := reverseIPv6(ip)
		ptrNames, lookupErr := c.dialer.Resolver().LookupPTR(ctx, reverseName)
		if lookupErr != nil || len(ptrNames) == 0 {
			checks = append(checks, ptrCheck{
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

		checks = append(checks, ptrCheck{
			Address:          ip.String(),
			PTRName:          ptrName,
			ForwardConfirmed: confirmed,
		})
	}

	details["checks"] = checks
	details["all_confirmed"] = allConfirmed

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
		Details: details,
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
