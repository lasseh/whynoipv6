package checker

import (
	"context"
	"testing"
)

// TestSPFIPv6 covers SPFDetail mechanism evaluation: direct ip6:, include:
// chains, implicit a/mx AAAA resolution, explicit -ip6: rejection, ip4-only,
// and the no-SPF skip.
func TestSPFIPv6(t *testing.T) {
	tests := []struct {
		name       string
		records    []string
		wantStatus CheckStatus
		check      func(t *testing.T, d SPFDetail)
	}{
		{
			name:       "direct ip6 supported",
			records:    []string{`example.org. 3600 IN TXT "v=spf1 ip6:2001:db8::/32 -all"`},
			wantStatus: StatusSupported,
			check: func(t *testing.T, d SPFDetail) {
				if d.HasIP6Mechanism == nil || !*d.HasIP6Mechanism {
					t.Errorf("has_ip6_mechanism = %v, want true", d.HasIP6Mechanism)
				}
				if len(d.IP6Mechanisms) != 1 || d.IP6Mechanisms[0] != "ip6:2001:db8::/32" {
					t.Errorf("ip6_mechanisms = %v", d.IP6Mechanisms)
				}
				if d.Implicit {
					t.Error("implicit = true, want false for direct ip6")
				}
			},
		},
		{
			name:       "ip4 only unsupported",
			records:    []string{`example.org. 3600 IN TXT "v=spf1 ip4:192.0.2.0/24 -all"`},
			wantStatus: StatusUnsupported,
			check: func(t *testing.T, d SPFDetail) {
				if d.HasIP6Mechanism == nil || *d.HasIP6Mechanism {
					t.Errorf("has_ip6_mechanism = %v, want false", d.HasIP6Mechanism)
				}
				if len(d.IP6Mechanisms) != 0 {
					t.Errorf("ip6_mechanisms = %v, want empty", d.IP6Mechanisms)
				}
			},
		},
		{
			name: "include chain has ip6 supported",
			records: []string{
				`example.org. 3600 IN TXT "v=spf1 include:_spf.example.net -all"`,
				`_spf.example.net. 3600 IN TXT "v=spf1 ip6:2001:db8::/32 -all"`,
			},
			wantStatus: StatusSupported,
			check: func(t *testing.T, d SPFDetail) {
				if d.IncludeHasIP6 == nil || !*d.IncludeHasIP6 {
					t.Errorf("include_has_ip6 = %v, want true", d.IncludeHasIP6)
				}
				if len(d.IncludeChain) != 1 || d.IncludeChain[0] != "_spf.example.net" {
					t.Errorf("include_chain = %v", d.IncludeChain)
				}
			},
		},
		{
			name: "a mechanism implicit ipv6 supported",
			records: []string{
				`example.org. 3600 IN TXT "v=spf1 a -all"`,
				"example.org. 3600 IN AAAA 2001:db8::1",
			},
			wantStatus: StatusSupported,
			check: func(t *testing.T, d SPFDetail) {
				if !d.Implicit {
					t.Error("implicit = false, want true for resolving a mechanism")
				}
			},
		},
		{
			name: "mx mechanism implicit ipv6 supported",
			records: []string{
				`example.org. 3600 IN TXT "v=spf1 mx -all"`,
				"example.org. 3600 IN MX 10 mail.example.org.",
				"mail.example.org. 3600 IN AAAA 2001:db8::25",
			},
			wantStatus: StatusSupported,
			check: func(t *testing.T, d SPFDetail) {
				if !d.Implicit {
					t.Error("implicit = false, want true for resolving mx mechanism")
				}
			},
		},
		{
			name:       "explicit reject ip6 unsupported",
			records:    []string{`example.org. 3600 IN TXT "v=spf1 -ip6:2001:db8::/32 -all"`},
			wantStatus: StatusUnsupported,
			check: func(t *testing.T, d SPFDetail) {
				if d.Reason != "SPF explicitly rejects IPv6" {
					t.Errorf("reason = %q", d.Reason)
				}
			},
		},
		{
			name:       "no spf record not applicable",
			records:    []string{`example.org. 3600 IN TXT "some-other-verification=abc"`},
			wantStatus: StatusNotApplicable,
			check: func(t *testing.T, d SPFDetail) {
				if d.Reason != "no SPF record" {
					t.Errorf("reason = %q", d.Reason)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewSPFIPv6(zoneDialer(t, newZone(t, tc.records...)))
			res, err := c.Check(context.Background(), "example.org", KindApex)
			if err != nil {
				t.Fatalf("Check returned err: %v", err)
			}
			if res.Status != tc.wantStatus {
				t.Errorf("status = %s, want %s", res.Status, tc.wantStatus)
			}
			d, ok := res.Detail.(*SPFDetail)
			if !ok {
				t.Fatalf("detail type = %T, want *SPFDetail", res.Detail)
			}
			tc.check(t, *d)
		})
	}
}
