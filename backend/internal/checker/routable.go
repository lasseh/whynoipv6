package checker

import "net"

// IsGloballyRoutableIPv6 reports whether ip is a globally routable IPv6
// address. It rejects IPv4/IPv4-mapped addresses, loopback, link-local
// unicast/multicast, ULA (fc00::/7), and the unspecified address.
// Ported from the production resolver (01-engine.md §2); consumed by
// internal/consensus when reducing per-resolver AAAA answers to symbols.
func IsGloballyRoutableIPv6(ip net.IP) bool {
	if ip.To4() != nil {
		return false // IPv4 or IPv4-mapped
	}
	if ip.IsLoopback() {
		return false
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	// Unique local addresses (fc00::/7): first byte 0xfc or 0xfd.
	if len(ip) >= 1 && (ip[0] == 0xfc || ip[0] == 0xfd) {
		return false
	}
	if ip.IsUnspecified() {
		return false
	}
	return true
}
