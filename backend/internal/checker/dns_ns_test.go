package checker

import (
	"context"
	"testing"
)

// TestDNSNSIPv6 (01 §11.3) covers NSDetail: the all-hosts / partial rule, the
// per-nameserver AAAA sample, and the zone walk-up for subdomains (Zone is set
// only when the answering zone differs from the queried name).
func TestDNSNSIPv6(t *testing.T) {
	tests := []struct {
		name       string
		domain     string
		records    []string
		wantStatus CheckStatus
		check      func(t *testing.T, d NSDetail)
	}{
		{
			name:   "all nameservers ipv6 supported",
			domain: "example.org",
			records: []string{
				"example.org. 3600 IN NS ns1.example.org.",
				"example.org. 3600 IN NS ns2.example.org.",
				"ns1.example.org. 3600 IN AAAA 2001:db8::53",
				"ns2.example.org. 3600 IN AAAA 2001:db8::54",
			},
			wantStatus: StatusSupported,
			check: func(t *testing.T, d NSDetail) {
				if d.Zone != "" {
					t.Errorf("zone = %q, want empty (apex answered directly)", d.Zone)
				}
				if d.Total != 2 || d.Checked != 2 {
					t.Errorf("total/checked = %d/%d, want 2/2", d.Total, d.Checked)
				}
				if d.IPv6Count == nil || *d.IPv6Count != 2 {
					t.Errorf("ipv6_count = %v, want 2", d.IPv6Count)
				}
			},
		},
		{
			name:   "one nameserver ipv6 partial",
			domain: "example.org",
			records: []string{
				"example.org. 3600 IN NS ns1.example.org.",
				"example.org. 3600 IN NS ns2.example.org.",
				"ns1.example.org. 3600 IN AAAA 2001:db8::53",
			},
			wantStatus: StatusPartial,
			check: func(t *testing.T, d NSDetail) {
				if d.IPv6Count == nil || *d.IPv6Count != 1 {
					t.Errorf("ipv6_count = %v, want 1", d.IPv6Count)
				}
			},
		},
		{
			name:   "no nameserver ipv6 unsupported",
			domain: "example.org",
			records: []string{
				"example.org. 3600 IN NS ns1.example.org.",
			},
			wantStatus: StatusUnsupported,
			check: func(t *testing.T, d NSDetail) {
				if d.IPv6Count == nil || *d.IPv6Count != 0 {
					t.Errorf("ipv6_count = %v, want 0", d.IPv6Count)
				}
			},
		},
		{
			name:   "subdomain walks up to zone apex",
			domain: "blog.example.org",
			records: []string{
				// No NS at blog.example.org — the check walks up to example.org.
				"example.org. 3600 IN NS ns1.example.org.",
				"ns1.example.org. 3600 IN AAAA 2001:db8::53",
			},
			wantStatus: StatusSupported,
			check: func(t *testing.T, d NSDetail) {
				if d.Zone != "example.org" {
					t.Errorf("zone = %q, want example.org (walked up)", d.Zone)
				}
			},
		},
		{
			name:   "subdomain under multi-label suffix walks up to registrable domain",
			domain: "blog.example.co.uk",
			records: []string{
				// No NS at blog.example.co.uk — the check walks up to example.co.uk.
				"example.co.uk. 3600 IN NS ns1.example.co.uk.",
				"ns1.example.co.uk. 3600 IN AAAA 2001:db8::53",
			},
			wantStatus: StatusSupported,
			check: func(t *testing.T, d NSDetail) {
				if d.Zone != "example.co.uk" {
					t.Errorf("zone = %q, want example.co.uk (walked up)", d.Zone)
				}
			},
		},
		{
			name:   "dead domain under multi-label suffix stops at public suffix",
			domain: "deadsite.co.uk",
			records: []string{
				// Registry NS at co.uk must never be reached — the walk-up
				// stops at the registrable-domain boundary so dead detection
				// (no delegated zone) can fire.
				"co.uk. 3600 IN NS ns1.nic.uk.",
				"ns1.nic.uk. 3600 IN AAAA 2001:db8::53",
			},
			wantStatus: StatusError,
			check: func(t *testing.T, d NSDetail) {
				if d.Zone != "" {
					t.Errorf("zone = %q, want empty (public suffix must not answer)", d.Zone)
				}
				if d.Error != "no NS records found" {
					t.Errorf("error = %q, want %q", d.Error, "no NS records found")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewDNSNSIPv6(zoneDialer(t, newZone(t, tc.records...)), 4)
			res, err := c.Check(context.Background(), tc.domain, KindApex)
			if err != nil {
				t.Fatalf("Check returned err: %v", err)
			}
			if res.Status != tc.wantStatus {
				t.Errorf("status = %s, want %s", res.Status, tc.wantStatus)
			}
			d, ok := res.Detail.(*NSDetail)
			if !ok {
				t.Fatalf("detail type = %T, want *NSDetail", res.Detail)
			}
			tc.check(t, *d)
		})
	}
}
