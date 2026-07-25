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

		{"global_unicast_routable", "2606:4700::1111", true},
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
