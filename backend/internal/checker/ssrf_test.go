package checker

import (
	"net"
	"testing"
)

// TestValidateIPBlocklist pins the SSRF blocklist (ssrf.go) one vector per
// family of blocked range, plus the public controls. The v4-mapped rows are
// the load-bearing ones: isBlocked matches v4 and v6 addresses against
// separate lists precisely because ::ffff:0:0/96 parses as 0.0.0.0/0 —
// collapsing the two loops would block every IPv4 address in the engine.
func TestValidateIPBlocklist(t *testing.T) {
	tests := []struct {
		name        string
		ip          string
		wantBlocked bool
	}{
		{"v4_private_10", "10.0.0.1", true},
		{"v4_loopback", "127.0.0.1", true},
		{"v4_link_local_metadata", "169.254.169.254", true},
		{"v4_cgnat", "100.64.0.1", true},
		{"v4_private_192_168", "192.168.1.1", true},

		{"v6_loopback", "::1", true},
		{"v6_ula", "fd00::1", true},
		{"v6_link_local", "fe80::1", true},
		{"v6_6to4", "2002::1", true},
		{"v6_documentation", "2001:db8::1", true},

		{"v4mapped_private_blocked_by_v4_list", "::ffff:10.0.0.1", true},

		{"public_v4_not_blocked_by_v4mapped_cidr", "8.8.8.8", false},
		{"public_v4_not_blocked_by_v4mapped_cidr_2", "1.1.1.1", false},
		{"public_v6", "2606:4700::1111", false},
	}

	d := NewSafeDialer(NewResolver(nil))

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("unparseable test vector %q", tc.ip)
			}
			err := d.ValidateIP(ip)
			if gotBlocked := err != nil; gotBlocked != tc.wantBlocked {
				t.Errorf("ValidateIP(%s) blocked = %v (err %v), want blocked = %v",
					tc.ip, gotBlocked, err, tc.wantBlocked)
			}
		})
	}
}
