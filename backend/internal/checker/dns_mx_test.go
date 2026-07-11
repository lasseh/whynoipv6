package checker

import (
	"context"
	"testing"
)

// TestDNSMXIPv6 (01 §11.4) covers MXDetail population: preference sort + cap,
// per-host AAAA evidence, null-MX and subdomain no-explicit-MX skips, and the
// RFC 5321 §5.1 implicit-MX fallback.
func TestDNSMXIPv6(t *testing.T) {
	tests := []struct {
		name       string
		kind       Kind
		records    []string
		wantStatus CheckStatus
		check      func(t *testing.T, d MXDetail)
	}{
		{
			name: "all mx have ipv6 supported",
			kind: KindApex,
			records: []string{
				"example.org. 3600 IN MX 10 mail1.example.org.",
				"example.org. 3600 IN MX 20 mail2.example.org.",
				"mail1.example.org. 3600 IN AAAA 2001:db8::11",
				"mail2.example.org. 3600 IN AAAA 2001:db8::12",
			},
			wantStatus: StatusSupported,
			check: func(t *testing.T, d MXDetail) {
				if d.Total != 2 {
					t.Errorf("total = %d, want 2", d.Total)
				}
				if d.IPv6Count == nil || *d.IPv6Count != 2 {
					t.Errorf("ipv6_count = %v, want 2", d.IPv6Count)
				}
				if h := d.MXRecords["mail1.example.org."]; !h.HasIPv6 || h.Preference != 10 {
					t.Errorf("mail1 = %+v", h)
				}
			},
		},
		{
			name: "partial when one mx lacks ipv6",
			kind: KindApex,
			records: []string{
				"example.org. 3600 IN MX 10 mail1.example.org.",
				"example.org. 3600 IN MX 20 mail2.example.org.",
				"mail1.example.org. 3600 IN AAAA 2001:db8::11",
			},
			wantStatus: StatusPartial,
			check: func(t *testing.T, d MXDetail) {
				if d.IPv6Count == nil || *d.IPv6Count != 1 {
					t.Errorf("ipv6_count = %v, want 1", d.IPv6Count)
				}
				if d.MXRecords["mail2.example.org."].HasIPv6 {
					t.Error("mail2 should lack ipv6")
				}
			},
		},
		{
			name: "no mx ipv6 unsupported",
			kind: KindApex,
			records: []string{
				"example.org. 3600 IN MX 10 mail1.example.org.",
			},
			wantStatus: StatusUnsupported,
			check: func(t *testing.T, d MXDetail) {
				if d.IPv6Count == nil || *d.IPv6Count != 0 {
					t.Errorf("ipv6_count = %v, want 0", d.IPv6Count)
				}
			},
		},
		{
			name: "preference walk caps at max lookups",
			kind: KindApex,
			records: []string{
				"example.org. 3600 IN MX 30 mail3.example.org.",
				"example.org. 3600 IN MX 10 mail1.example.org.",
				"example.org. 3600 IN MX 20 mail2.example.org.",
				"mail1.example.org. 3600 IN AAAA 2001:db8::11",
				"mail2.example.org. 3600 IN AAAA 2001:db8::12",
				"mail3.example.org. 3600 IN AAAA 2001:db8::13",
			},
			wantStatus: StatusSupported,
			check: func(t *testing.T, d MXDetail) {
				// maxLookups=2: only the two lowest-preference MX are walked.
				if d.Total != 2 {
					t.Errorf("total = %d, want 2 (capped)", d.Total)
				}
				if len(d.MXRecords) != 2 {
					t.Errorf("mx_records = %d entries, want 2", len(d.MXRecords))
				}
				if _, ok := d.MXRecords["mail3.example.org."]; ok {
					t.Error("highest-preference mail3 must be dropped by the cap")
				}
			},
		},
		{
			name:       "null mx not applicable",
			kind:       KindApex,
			records:    []string{"example.org. 3600 IN MX 0 ."},
			wantStatus: StatusNotApplicable,
			check: func(t *testing.T, d MXDetail) {
				if d.Reason != "null MX record" {
					t.Errorf("reason = %q", d.Reason)
				}
			},
		},
		{
			name:       "subdomain no explicit mx not applicable",
			kind:       KindSubdomain,
			records:    []string{"blog.example.org. 3600 IN AAAA 2001:db8::99"}, // AAAA present, but no MX
			wantStatus: StatusNotApplicable,
			check: func(t *testing.T, d MXDetail) {
				if d.Reason != "no explicit MX records (subdomain entity)" {
					t.Errorf("reason = %q (implicit-MX fallback must be skipped)", d.Reason)
				}
			},
		},
		{
			name:       "apex implicit mx fallback supported",
			kind:       KindApex,
			records:    []string{"example.org. 3600 IN AAAA 2001:db8::1"}, // no MX, AAAA on apex
			wantStatus: StatusSupported,
			check: func(t *testing.T, d MXDetail) {
				if d.Reason != "implicit MX fallback (RFC 5321 §5.1)" {
					t.Errorf("reason = %q", d.Reason)
				}
				if len(d.Addresses) != 1 || d.Addresses[0] != "2001:db8::1" {
					t.Errorf("addresses = %v", d.Addresses)
				}
			},
		},
		{
			name:       "apex no mx no aaaa not applicable",
			kind:       KindApex,
			records:    nil, // no MX, no AAAA
			wantStatus: StatusNotApplicable,
			check: func(t *testing.T, d MXDetail) {
				if d.Reason != "no MX records and no implicit AAAA fallback" {
					t.Errorf("reason = %q", d.Reason)
				}
			},
		},
	}

	host := map[Kind]string{KindApex: "example.org", KindSubdomain: "blog.example.org"}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewDNSMXIPv6(zoneDialer(t, newZone(t, tc.records...)), 2)
			res, err := c.Check(context.Background(), host[tc.kind], tc.kind)
			if err != nil {
				t.Fatalf("Check returned err: %v", err)
			}
			if res.Status != tc.wantStatus {
				t.Errorf("status = %s, want %s", res.Status, tc.wantStatus)
			}
			d, ok := res.Detail.(*MXDetail)
			if !ok {
				t.Fatalf("detail type = %T, want *MXDetail", res.Detail)
			}
			tc.check(t, *d)
		})
	}
}
