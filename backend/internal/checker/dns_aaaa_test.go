package checker

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
)

// TestDNSAAAABase (02 §2.7) drives dns_aaaa_base's mapping of a canned
// consensus answer onto CheckStatus + AAAADetail: the fields crawler.Unresolvable
// and observe.mapAAAA read (Rcode, Quorum, AOutcome, AAddress, CDOutcome).
func TestDNSAAAABase(t *testing.T) {
	v4 := net.ParseIP("192.0.2.1")
	quorum3 := &QuorumInfo{
		PerResolver: map[string]string{"google": "exists", "cloudflare": "exists", "quad9": "exists"},
		Rcodes:      map[string]string{"google": "NOERROR", "cloudflare": "NOERROR", "quad9": "NOERROR"},
		Agreement:   "3of3",
	}
	quorumEmpty := &QuorumInfo{
		PerResolver: map[string]string{"google": "empty", "cloudflare": "empty", "quad9": "empty"},
		Rcodes:      map[string]string{"google": "NOERROR", "cloudflare": "NOERROR", "quad9": "NOERROR"},
		Agreement:   "3of3",
	}
	quorumErr := &QuorumInfo{
		Rcodes: map[string]string{"google": "SERVFAIL", "cloudflare": "SERVFAIL", "quad9": "SERVFAIL"},
	}

	tests := []struct {
		name       string
		ans        AAAAAnswer
		err        error
		wantStatus CheckStatus
		check      func(t *testing.T, d AAAADetail)
	}{
		{
			name:       "quorum agree supported",
			ans:        AAAAAnswer{IPs: []net.IP{net.ParseIP("2001:db8::1")}, TTL: 300, Rcode: "NOERROR", Quorum: quorum3},
			wantStatus: StatusSupported,
			check: func(t *testing.T, d AAAADetail) {
				if !reflect.DeepEqual(d.Addresses, []string{"2001:db8::1"}) {
					t.Errorf("addresses = %v", d.Addresses)
				}
				if d.TTL == nil || *d.TTL != 300 {
					t.Errorf("ttl = %v, want 300", d.TTL)
				}
				if d.Quorum != quorum3 {
					t.Errorf("quorum not preserved")
				}
			},
		},
		{
			name:       "quorum empty a_present",
			ans:        AAAAAnswer{Rcode: "NOERROR", Quorum: quorumEmpty, AOutcome: "a_present", AIP: v4},
			wantStatus: StatusUnsupported,
			check: func(t *testing.T, d AAAADetail) {
				if d.AOutcome != "a_present" {
					t.Errorf("a_outcome = %q, want a_present", d.AOutcome)
				}
				if d.AAddress != "192.0.2.1" {
					t.Errorf("a_address = %q, want 192.0.2.1", d.AAddress)
				}
			},
		},
		{
			name:       "quorum empty a_absent",
			ans:        AAAAAnswer{Rcode: "NOERROR", Quorum: quorumEmpty, AOutcome: "a_absent"},
			wantStatus: StatusUnsupported,
			check: func(t *testing.T, d AAAADetail) {
				if d.AOutcome != "a_absent" {
					t.Errorf("a_outcome = %q, want a_absent", d.AOutcome)
				}
				if d.AAddress != "" {
					t.Errorf("a_address = %q, want empty", d.AAddress)
				}
			},
		},
		{
			name:       "quorum empty a_error",
			ans:        AAAAAnswer{Rcode: "NOERROR", Quorum: quorumEmpty, AOutcome: "a_error"},
			wantStatus: StatusUnsupported,
			check: func(t *testing.T, d AAAADetail) {
				if d.AOutcome != "a_error" {
					t.Errorf("a_outcome = %q, want a_error", d.AOutcome)
				}
			},
		},
		{
			name:       "nxdomain",
			ans:        AAAAAnswer{Rcode: "NXDOMAIN"},
			wantStatus: StatusNotApplicable,
			check: func(t *testing.T, d AAAADetail) {
				if d.Rcode != "NXDOMAIN" {
					t.Errorf("rcode = %q, want NXDOMAIN", d.Rcode)
				}
				if d.Reason != "domain does not exist" {
					t.Errorf("reason = %q", d.Reason)
				}
			},
		},
		{
			name:       "two valid disagree inconsistent",
			ans:        AAAAAnswer{Quorum: quorum3},
			err:        ErrQuorumInconsistent,
			wantStatus: StatusError,
			check: func(t *testing.T, d AAAADetail) {
				if !d.Inconsistent {
					t.Error("inconsistent = false, want true")
				}
				if d.Quorum != quorum3 {
					t.Error("quorum not preserved on inconsistent")
				}
			},
		},
		{
			name:       "at most one valid error",
			ans:        AAAAAnswer{Rcode: "SERVFAIL", Quorum: quorumErr},
			err:        errors.New("SERVFAIL for example.org"),
			wantStatus: StatusError,
			check: func(t *testing.T, d AAAADetail) {
				if d.Error != "SERVFAIL for example.org" {
					t.Errorf("error = %q", d.Error)
				}
			},
		},
		{
			name:       "cd rescue present",
			ans:        AAAAAnswer{IPs: []net.IP{net.ParseIP("2001:db8::2")}, TTL: 60, Rcode: "NOERROR", CDOutcome: "cd_present"},
			wantStatus: StatusSupported,
			check: func(t *testing.T, d AAAADetail) {
				if d.CDOutcome != "cd_present" {
					t.Errorf("cd_outcome = %q, want cd_present", d.CDOutcome)
				}
			},
		},
		{
			name:       "cd rescue fail",
			ans:        AAAAAnswer{Rcode: "SERVFAIL", CDOutcome: "cd_fail", Quorum: quorumErr},
			err:        errors.New("all resolvers SERVFAIL"),
			wantStatus: StatusError,
			check: func(t *testing.T, d AAAADetail) {
				if d.CDOutcome != "cd_fail" {
					t.Errorf("cd_outcome = %q, want cd_fail", d.CDOutcome)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			seam := newScriptAAAA()
			seam.ans["example.org"] = tc.ans
			seam.err["example.org"] = tc.err
			c := NewDNSAAAABase(seam)

			res, err := c.Check(context.Background(), "example.org", KindApex)
			if err != nil {
				t.Fatalf("Check returned err: %v", err)
			}
			if res.Status != tc.wantStatus {
				t.Errorf("status = %s, want %s", res.Status, tc.wantStatus)
			}
			d, ok := res.Detail.(*AAAADetail)
			if !ok {
				t.Fatalf("detail type = %T, want *AAAADetail", res.Detail)
			}
			tc.check(t, *d)
		})
	}
}

// TestDNSAAAAWWW covers dns_aaaa_www's www.<host> prefix, CNAME-target capture,
// CDN detection, and the not_applicable-on-NXDOMAIN branch (distinct wording
// from the base check).
func TestDNSAAAAWWW(t *testing.T) {
	tests := []struct {
		name       string
		ans        AAAAAnswer
		err        error
		wantStatus CheckStatus
		check      func(t *testing.T, d AAAADetail)
	}{
		{
			name:       "supported with cdn cname",
			ans:        AAAAAnswer{IPs: []net.IP{net.ParseIP("2001:db8::1")}, TTL: 120, Rcode: "NOERROR", CNAMEChain: []string{"www.example.org.cdn.cloudflare.net."}},
			wantStatus: StatusSupported,
			check: func(t *testing.T, d AAAADetail) {
				if d.CNAMETarget != "www.example.org.cdn.cloudflare.net." {
					t.Errorf("cname_target = %q", d.CNAMETarget)
				}
				if !d.CDNDetected {
					t.Error("cdn_detected = false, want true")
				}
			},
		},
		{
			name:       "unsupported no cdn",
			ans:        AAAAAnswer{Rcode: "NOERROR", CNAMEChain: []string{"origin.example.org."}},
			wantStatus: StatusUnsupported,
			check: func(t *testing.T, d AAAADetail) {
				if d.CDNDetected {
					t.Error("cdn_detected = true, want false")
				}
				if d.CNAMETarget != "origin.example.org." {
					t.Errorf("cname_target = %q", d.CNAMETarget)
				}
			},
		},
		{
			name:       "nxdomain not applicable",
			ans:        AAAAAnswer{Rcode: "NXDOMAIN"},
			wantStatus: StatusNotApplicable,
			check: func(t *testing.T, d AAAADetail) {
				if d.Reason != "www subdomain does not exist" {
					t.Errorf("reason = %q", d.Reason)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			seam := newScriptAAAA()
			seam.ans["www.example.org"] = tc.ans
			seam.err["www.example.org"] = tc.err
			c := NewDNSAAAAWWW(seam)

			res, err := c.Check(context.Background(), "example.org", KindApex)
			if err != nil {
				t.Fatalf("Check returned err: %v", err)
			}
			if res.Status != tc.wantStatus {
				t.Errorf("status = %s, want %s", res.Status, tc.wantStatus)
			}
			d, ok := res.Detail.(*AAAADetail)
			if !ok {
				t.Fatalf("detail type = %T, want *AAAADetail", res.Detail)
			}
			tc.check(t, *d)
		})
	}
}
