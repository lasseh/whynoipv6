package checker

import "net"

// IsGloballyRoutableIPv6 reports whether ip is a globally routable IPv6
// address. It rejects IPv4/IPv4-mapped addresses and every range in the
// SSRF blocklist (ssrf.go — blockedIPv6): loopback, unspecified, link-local,
// multicast, ULA, and the transition/documentation ranges 64:ff9b::/96,
// 100::/64, 2001::/32, 2001:db8::/32, 2002::/16 and fec0::/10.
//
// Ported from the production resolver (01-engine.md §2); consumed by
// internal/consensus when reducing per-resolver AAAA answers to symbols.
//
// The one list matters (02 §2.5 erratum, review issue 15). This function
// used to name only loopback/link-local/ULA/unspecified, so a domain whose
// only AAAA was, say, 6to4 reduced to `exists`, confirmed base=supported,
// and then failed every pinned dial with errAddrBlocked — conn parked at
// error, non-definitive, for as long as the record stood.
func IsGloballyRoutableIPv6(ip net.IP) bool {
	if ip.To4() != nil {
		return false // IPv4 or IPv4-mapped
	}
	for _, blocked := range blockedV6Nets {
		if blocked.Contains(ip) {
			return false
		}
	}
	return true
}
