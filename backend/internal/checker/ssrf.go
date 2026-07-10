package checker

import (
	"context"
	"fmt"
	"net"
	"time"
)

// blockedIPv4 contains all IPv4 ranges that must be blocked for SSRF protection.
var blockedIPv4 = []string{
	"0.0.0.0/8",          // RFC 1122 - "This network"
	"10.0.0.0/8",         // RFC 1918 - Private
	"100.64.0.0/10",      // RFC 6598 - CGNAT
	"127.0.0.0/8",        // RFC 1122 - Loopback
	"169.254.0.0/16",     // RFC 3927 - Link-local (includes cloud metadata)
	"172.16.0.0/12",      // RFC 1918 - Private
	"192.0.0.0/24",       // RFC 6890 - IETF protocol assignments
	"192.0.2.0/24",       // RFC 5737 - Documentation (TEST-NET-1)
	"192.88.99.0/24",     // RFC 7526 - 6to4 relay anycast (deprecated)
	"192.168.0.0/16",     // RFC 1918 - Private
	"198.18.0.0/15",      // RFC 2544 - Benchmarking
	"198.51.100.0/24",    // RFC 5737 - Documentation (TEST-NET-2)
	"203.0.113.0/24",     // RFC 5737 - Documentation (TEST-NET-3)
	"224.0.0.0/4",        // RFC 5771 - Multicast
	"240.0.0.0/4",        // RFC 1112 - Reserved
	"255.255.255.255/32", // Broadcast
}

// blockedIPv6 contains all IPv6 ranges that must be blocked for SSRF protection.
var blockedIPv6 = []string{
	"::1/128",           // RFC 4291 - Loopback
	"::/128",            // RFC 4291 - Unspecified
	"::ffff:0:0/96",     // RFC 4291 - IPv4-mapped
	"64:ff9b::/96",      // RFC 6052 - NAT64
	"100::/64",          // RFC 6666 - Discard-only
	"2001::/32",         // RFC 4380 - Teredo (embeds obfuscated IPv4)
	"2001:db8::/32",     // RFC 3849 - Documentation
	"2002::/16",         // RFC 3056 - 6to4 (encapsulates arbitrary IPv4)
	"fc00::/7",          // RFC 4193 - Unique local (ULA)
	"fe80::/10",         // RFC 4291 - Link-local
	"fec0::/10",         // RFC 3879 - Site-local (deprecated but still routable)
	"ff00::/8",          // RFC 4291 - Multicast
	"fd00:ec2::254/128", // AWS IPv6 metadata
}

// SafeDialer wraps net.Dialer with SSRF protection.
type SafeDialer struct {
	resolver  *Resolver
	dialer    *net.Dialer
	blockedV4 []*net.IPNet
	blockedV6 []*net.IPNet
}

// NewSafeDialer creates a SafeDialer with the full SSRF blocklist.
func NewSafeDialer(resolver *Resolver) *SafeDialer {
	v4Nets := make([]*net.IPNet, 0, len(blockedIPv4))
	for _, cidr := range blockedIPv4 {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid blocked CIDR %q: %v", cidr, err))
		}
		v4Nets = append(v4Nets, network)
	}
	v6Nets := make([]*net.IPNet, 0, len(blockedIPv6))
	for _, cidr := range blockedIPv6 {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid blocked CIDR %q: %v", cidr, err))
		}
		v6Nets = append(v6Nets, network)
	}

	return &SafeDialer{
		resolver: resolver,
		dialer: &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		},
		blockedV4: v4Nets,
		blockedV6: v6Nets,
	}
}

// Resolver returns the underlying DNS resolver.
func (d *SafeDialer) Resolver() *Resolver {
	return d.resolver
}

// isBlocked checks whether an IP falls within any blocked range.
// IPv4 addresses (including IPv4-mapped IPv6) are checked against IPv4 ranges only.
// IPv6 addresses are checked against IPv6 ranges only.
// This prevents Go's net.ParseCIDR from cross-matching (e.g. ::ffff:0:0/96 parsing
// as 0.0.0.0/0 and blocking all IPv4).
func (d *SafeDialer) isBlocked(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		for _, network := range d.blockedV4 {
			if network.Contains(v4) {
				return true
			}
		}
		return false
	}

	for _, network := range d.blockedV6 {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateIP checks whether an IP is safe to connect to.
func (d *SafeDialer) ValidateIP(ip net.IP) error {
	if d.isBlocked(ip) {
		return fmt.Errorf("connection to %s blocked: reserved IP range", ip)
	}
	return nil
}

// DialContext resolves the host, validates the IP, and dials the validated IP.
func (d *SafeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q: %w", addr, err)
	}

	// If host is already an IP, validate it directly.
	if ip := net.ParseIP(host); ip != nil {
		if d.isBlocked(ip) {
			return nil, fmt.Errorf("connection to %s blocked: reserved IP range", ip)
		}
		return d.dialer.DialContext(ctx, network, addr)
	}

	// Resolve domain to IPs using our resolver (DNS pinning).
	ips, err := d.resolve(ctx, host, network)
	if err != nil {
		return nil, fmt.Errorf("DNS resolution failed for %s: %w", host, err)
	}

	// Try each resolved IP.
	var lastErr error
	for _, ip := range ips {
		if d.isBlocked(ip) {
			lastErr = fmt.Errorf("connection to %s (%s) blocked: reserved IP range", host, ip)
			continue
		}
		target := net.JoinHostPort(ip.String(), port)
		conn, dialErr := d.dialer.DialContext(ctx, network, target)
		if dialErr != nil {
			lastErr = dialErr
			continue
		}
		return conn, nil
	}
	return nil, fmt.Errorf("all addresses for %s failed: %w", host, lastErr)
}

// resolve performs DNS resolution based on the requested network type.
func (d *SafeDialer) resolve(ctx context.Context, host, network string) ([]net.IP, error) {
	switch network {
	case "tcp4", "udp4":
		return d.resolver.LookupA(ctx, host)
	case "tcp6", "udp6":
		ips, _, _, _, err := d.resolver.LookupAAAA(ctx, host)
		return ips, err
	default:
		// For "tcp" or "udp", try both.
		ips4, _ := d.resolver.LookupA(ctx, host)
		ips6, _, _, _, _ := d.resolver.LookupAAAA(ctx, host)
		return append(ips4, ips6...), nil
	}
}
