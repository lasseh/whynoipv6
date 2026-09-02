package checker

import (
	"context"
	"fmt"
	"testing"
)

// spfIncludeChain builds n linked include: records — c1 → c2 → … → cn —
// with only the last one carrying the ip6: the walk is looking for. Each
// link costs one term lookup, so n > maxSPFLookups blows the budget before
// the ip6: is ever seen.
func spfIncludeChain(n int) []string {
	recs := []string{`example.org. 3600 IN TXT "v=spf1 include:c1.example.org -all"`}
	for i := 1; i < n; i++ {
		recs = append(recs, fmt.Sprintf(
			`c%d.example.org. 3600 IN TXT "v=spf1 include:c%d.example.org -all"`, i, i+1))
	}
	return append(recs, fmt.Sprintf(
		`c%d.example.org. 3600 IN TXT "v=spf1 ip6:2001:db8::/32 -all"`, n))
}

// spfManyMX is an RFC-compliant record: one mx term over six hosts plus four
// includes. RFC 7208 §4.6.4 charges the record five term lookups; charging
// the six MX address lookups too would put it at eleven.
func spfManyMX() []string {
	recs := []string{`example.org. 3600 IN TXT "v=spf1 mx include:i1.example.org ` +
		`include:i2.example.org include:i3.example.org include:i4.example.org -all"`}
	for i := 1; i <= 6; i++ {
		recs = append(recs, fmt.Sprintf("example.org. 3600 IN MX %d mx%d.example.org.", i*10, i))
	}
	for i := 1; i <= 4; i++ {
		recs = append(recs, fmt.Sprintf(
			`i%d.example.org. 3600 IN TXT "v=spf1 ip4:192.0.2.0/24 -all"`, i))
	}
	return recs
}

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
			// 01 §11.11: the budget belongs to the whole evaluation. This
			// used to come back `unsupported` — "the domain rejects IPv6
			// senders" — for a record RFC 7208 calls permerror.
			name:       "budget blown inside a nested include errors",
			records:    spfIncludeChain(11),
			wantStatus: StatusError,
			check: func(t *testing.T, d SPFDetail) {
				if d.Error != "too many DNS lookups" {
					t.Errorf("error = %q, want the budget error", d.Error)
				}
				if d.LookupCount == nil || *d.LookupCount <= maxSPFLookups {
					t.Errorf("lookup_count = %v, want > %d", d.LookupCount, maxSPFLookups)
				}
			},
		},
		{
			name:       "include chain inside the budget still resolves",
			records:    spfIncludeChain(5),
			wantStatus: StatusSupported,
			check: func(t *testing.T, d SPFDetail) {
				if d.IncludeHasIP6 == nil || !*d.IncludeHasIP6 {
					t.Errorf("include_has_ip6 = %v, want true", d.IncludeHasIP6)
				}
				if d.LookupCount == nil || *d.LookupCount != 5 {
					t.Errorf("lookup_count = %v, want 5", d.LookupCount)
				}
			},
		},
		{
			// RFC 7208 §4.6.4 keeps MX address lookups under their own
			// per-term ceiling; charging them to the record's budget made
			// this compliant record error at the fourth include.
			name:       "mx hosts do not spend the term budget",
			records:    spfManyMX(),
			wantStatus: StatusUnsupported,
			check: func(t *testing.T, d SPFDetail) {
				if d.Error != "" {
					t.Errorf("error = %q, want none", d.Error)
				}
				if d.LookupCount == nil || *d.LookupCount != 5 {
					t.Errorf("lookup_count = %v, want 5 (mx + 4 includes)", d.LookupCount)
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
