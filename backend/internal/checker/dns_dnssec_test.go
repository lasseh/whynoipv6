package checker

import (
	"context"
	"testing"
)

// TestDNSSEC covers DNSSECDetail: unsigned (no DS), signed+validated (DS present,
// AD flag set), and signed-but-failing (DS present, AD=0). The fake resolver's
// AD bit stands in for a validating recursive resolver.
func TestDNSSEC(t *testing.T) {
	const dsRecord = "example.org. 3600 IN DS 12345 13 2 " +
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	tests := []struct {
		name       string
		records    []string
		ad         bool
		wantStatus CheckStatus
		check      func(t *testing.T, d DNSSECDetail)
	}{
		{
			name:       "unsigned no ds",
			records:    nil,
			wantStatus: StatusUnsupported,
			check: func(t *testing.T, d DNSSECDetail) {
				if d.Signed {
					t.Error("signed = true, want false")
				}
			},
		},
		{
			name:       "signed and validated",
			records:    []string{dsRecord},
			ad:         true,
			wantStatus: StatusSupported,
			check: func(t *testing.T, d DNSSECDetail) {
				if !d.Signed {
					t.Error("signed = false, want true")
				}
				if len(d.DSRecords) != 1 || d.DSRecords[0].KeyTag != 12345 {
					t.Errorf("ds_records = %+v", d.DSRecords)
				}
				if d.DSRecords[0].Algorithm != "ECDSAP256SHA256" {
					t.Errorf("algorithm = %q", d.DSRecords[0].Algorithm)
				}
				if d.ADFlag == nil || !*d.ADFlag {
					t.Errorf("ad_flag = %v, want true", d.ADFlag)
				}
			},
		},
		{
			name:       "signed but validation fails",
			records:    []string{dsRecord},
			ad:         false,
			wantStatus: StatusError,
			check: func(t *testing.T, d DNSSECDetail) {
				if !d.Signed {
					t.Error("signed = false, want true")
				}
				if d.ADFlag == nil || *d.ADFlag {
					t.Errorf("ad_flag = %v, want false", d.ADFlag)
				}
				if d.Error != "DNSSEC signed but validation failed (AD=0)" {
					t.Errorf("error = %q", d.Error)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			z := newZone(t, tc.records...)
			z.ad = tc.ad
			c := NewDNSSEC(zoneDialer(t, z))
			res, err := c.Check(context.Background(), "example.org", KindApex)
			if err != nil {
				t.Fatalf("Check returned err: %v", err)
			}
			if res.Status != tc.wantStatus {
				t.Errorf("status = %s, want %s", res.Status, tc.wantStatus)
			}
			d, ok := res.Detail.(*DNSSECDetail)
			if !ok {
				t.Fatalf("detail type = %T, want *DNSSECDetail", res.Detail)
			}
			tc.check(t, *d)
		})
	}
}
