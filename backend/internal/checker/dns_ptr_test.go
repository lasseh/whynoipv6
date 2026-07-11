package checker

import (
	"context"
	"net"
	"testing"
)

// TestDNSPTRIPv6 covers PTRDetail: forward-confirmed reverse DNS (supported),
// a PTR that fails to resolve back (partial), and no PTR at all (unsupported).
// The reverse name is built with the production reverseIPv6 helper so the fake
// zone answers the exact query the check emits.
func TestDNSPTRIPv6(t *testing.T) {
	rev := reverseIPv6(net.ParseIP("2001:db8::1"))

	tests := []struct {
		name       string
		records    []string
		wantStatus CheckStatus
		check      func(t *testing.T, d PTRDetail)
	}{
		{
			name: "forward confirmed supported",
			records: []string{
				"example.org. 3600 IN AAAA 2001:db8::1",
				rev + " 3600 IN PTR host.example.org.",
				"host.example.org. 3600 IN AAAA 2001:db8::1", // forward-confirms
			},
			wantStatus: StatusSupported,
			check: func(t *testing.T, d PTRDetail) {
				if d.AllConfirmed == nil || !*d.AllConfirmed {
					t.Errorf("all_confirmed = %v, want true", d.AllConfirmed)
				}
				if len(d.Checks) != 1 || !d.Checks[0].ForwardConfirmed {
					t.Errorf("checks = %+v", d.Checks)
				}
				if d.Checks[0].PTRName != "host.example.org." {
					t.Errorf("ptr_name = %q", d.Checks[0].PTRName)
				}
			},
		},
		{
			name: "ptr not forward confirmed partial",
			records: []string{
				"example.org. 3600 IN AAAA 2001:db8::1",
				rev + " 3600 IN PTR host.example.org.",
				"host.example.org. 3600 IN AAAA 2001:db8::99", // resolves elsewhere
			},
			wantStatus: StatusPartial,
			check: func(t *testing.T, d PTRDetail) {
				if d.AllConfirmed == nil || *d.AllConfirmed {
					t.Errorf("all_confirmed = %v, want false", d.AllConfirmed)
				}
				if d.Checks[0].ForwardConfirmed {
					t.Error("forward_confirmed = true, want false")
				}
			},
		},
		{
			name: "no ptr unsupported",
			records: []string{
				"example.org. 3600 IN AAAA 2001:db8::1",
			},
			wantStatus: StatusUnsupported,
			check: func(t *testing.T, d PTRDetail) {
				if len(d.Checks) != 1 || d.Checks[0].PTRName != "" {
					t.Errorf("checks = %+v", d.Checks)
				}
			},
		},
		{
			name:       "no aaaa not applicable",
			records:    nil,
			wantStatus: StatusNotApplicable,
			check: func(t *testing.T, d PTRDetail) {
				if d.Reason != "no AAAA record" {
					t.Errorf("reason = %q", d.Reason)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewDNSPTRIPv6(zoneDialer(t, newZone(t, tc.records...)))
			res, err := c.Check(context.Background(), "example.org", KindApex)
			if err != nil {
				t.Fatalf("Check returned err: %v", err)
			}
			if res.Status != tc.wantStatus {
				t.Errorf("status = %s, want %s", res.Status, tc.wantStatus)
			}
			d, ok := res.Detail.(*PTRDetail)
			if !ok {
				t.Fatalf("detail type = %T, want *PTRDetail", res.Detail)
			}
			tc.check(t, *d)
		})
	}
}
