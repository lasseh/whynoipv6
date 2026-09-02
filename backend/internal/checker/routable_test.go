package checker

import (
	"net"
	"testing"
)

// TestIsGloballyRoutableIPv6 pins one vector per rejection branch of
// routable.go. The predicate gates whether a resource host counts as
// v6-capable (consensus.go, crawler/resourcesweep.go), so every branch that
// can demote an address needs a named row.
func TestIsGloballyRoutableIPv6(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		wantTrue bool
	}{
		{"ipv4_rejected", "1.2.3.4", false},
		{"ipv4_mapped_rejected", "::ffff:1.2.3.4", false},
		{"loopback_rejected", "::1", false},
		{"link_local_unicast_rejected", "fe80::1", false},
		{"link_local_multicast_rejected", "ff02::1", false},
		{"ula_fc00_rejected", "fc00::1", false},
		{"ula_fd00_rejected", "fd00::1", false},
		{"unspecified_rejected", "::", false},

		// Review issue 15: the ranges the SSRF dial guard blocks. Each of
		// these used to reduce to `exists`, confirm base=supported, and then
		// fail every dial with errAddrBlocked.
		{"nat64_rejected", "64:ff9b::102:304", false},
		{"discard_only_rejected", "100::1", false},
		{"teredo_rejected", "2001::5ef5:79fd", false},
		{"documentation_rejected", "2001:db8::1", false},
		{"6to4_rejected", "2002:c000:0204::1", false},
		{"site_local_rejected", "fec0::1", false},
		{"aws_metadata_rejected", "fd00:ec2::254", false},

		{"global_unicast_routable", "2606:4700::1111", true},
		// 2001:4860::/32 is Google's production range: neighbouring
		// 2001::/32 must not swallow it.
		{"google_public_routable", "2001:4860:4860::8888", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("unparseable test vector %q", tc.ip)
			}
			if got := IsGloballyRoutableIPv6(ip); got != tc.wantTrue {
				t.Errorf("IsGloballyRoutableIPv6(%s) = %v, want %v", tc.ip, got, tc.wantTrue)
			}
		})
	}
}

// TestRoutableMatchesTheSSRFBlocklist is the anti-drift assertion behind
// review issue 15: whatever the dialer refuses to connect to, the consensus
// reducer must refuse to count as `exists`. Adding a range to blockedIPv6
// without this holding would recreate the base=supported / conn=error trap.
func TestRoutableMatchesTheSSRFBlocklist(t *testing.T) {
	for _, network := range blockedV6Nets {
		if IsGloballyRoutableIPv6(network.IP) {
			t.Errorf("%s is blocked for dialing but counts as routable", network)
		}
	}
}
