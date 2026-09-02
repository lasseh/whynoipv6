package checker

import (
	"context"
	"strings"
	"testing"
)

// TestDNSChecksKeepResolverTroubleNonDefinitive (02 §4): a per-host AAAA
// lookup that errors is not evidence of "no AAAA". When no checked host
// answers at all, ns/mx report `error`, never `unsupported`; a zero lookup
// cap cannot read as "every checked host has AAAA".
func TestDNSChecksKeepResolverTroubleNonDefinitive(t *testing.T) {
	nsZone := []string{
		"example.org. 3600 IN NS ns1.example.org.",
		"example.org. 3600 IN NS ns2.example.org.",
		"ns1.example.org. 3600 IN AAAA 2001:db8::53",
	}
	mxZone := []string{
		"example.org. 3600 IN MX 10 mail1.example.org.",
		"example.org. 3600 IN MX 20 mail2.example.org.",
		"mail1.example.org. 3600 IN AAAA 2001:db8::11",
	}

	t.Run("ns all hosts servfail is error", func(t *testing.T) {
		z := newZone(t, nsZone...)
		z.servfail["ns1.example.org."] = true
		z.servfail["ns2.example.org."] = true
		res, _ := NewDNSNSIPv6(zoneDialer(t, z), 4).Check(context.Background(), "example.org", KindApex)
		if res.Status != StatusError {
			t.Fatalf("status = %s, want error", res.Status)
		}
		if d := res.Detail.(*NSDetail); !strings.Contains(d.Error, "SERVFAIL") {
			t.Errorf("error = %q, want the resolver failure text", d.Error)
		}
	})

	t.Run("ns one host servfail keeps the answered host's verdict", func(t *testing.T) {
		z := newZone(t, nsZone...)
		z.servfail["ns2.example.org."] = true // ns1 answers with AAAA
		res, _ := NewDNSNSIPv6(zoneDialer(t, z), 4).Check(context.Background(), "example.org", KindApex)
		if res.Status != StatusPartial {
			t.Errorf("status = %s, want partial (one AAAA of two checked)", res.Status)
		}
		z = newZone(t, nsZone...)
		z.servfail["ns1.example.org."] = true // ns2 answers empty
		res, _ = NewDNSNSIPv6(zoneDialer(t, z), 4).Check(context.Background(), "example.org", KindApex)
		if res.Status != StatusUnsupported {
			t.Errorf("status = %s, want unsupported (the answering host has no AAAA)", res.Status)
		}
	})

	t.Run("ns zero lookup cap is error not supported", func(t *testing.T) {
		res, _ := NewDNSNSIPv6(zoneDialer(t, newZone(t, nsZone...)), 0).Check(context.Background(), "example.org", KindApex)
		if res.Status != StatusError {
			t.Errorf("status = %s, want error", res.Status)
		}
	})

	t.Run("mx servfail answer is error not not_applicable", func(t *testing.T) {
		z := newZone(t, mxZone...)
		z.servfail["example.org."] = true
		res, _ := NewDNSMXIPv6(zoneDialer(t, z), 2).Check(context.Background(), "example.org", KindApex)
		if res.Status != StatusError {
			t.Fatalf("status = %s, want error", res.Status)
		}
		if d := res.Detail.(*MXDetail); !strings.Contains(d.Error, "SERVFAIL") {
			t.Errorf("error = %q, want the rcode", d.Error)
		}
	})

	t.Run("mx all hosts servfail is error", func(t *testing.T) {
		z := newZone(t, mxZone...)
		z.servfail["mail1.example.org."] = true
		z.servfail["mail2.example.org."] = true
		res, _ := NewDNSMXIPv6(zoneDialer(t, z), 2).Check(context.Background(), "example.org", KindApex)
		if res.Status != StatusError {
			t.Errorf("status = %s, want error", res.Status)
		}
	})

	t.Run("mx zero lookup cap is error not supported", func(t *testing.T) {
		res, _ := NewDNSMXIPv6(zoneDialer(t, newZone(t, mxZone...)), 0).Check(context.Background(), "example.org", KindApex)
		if res.Status != StatusError {
			t.Errorf("status = %s, want error", res.Status)
		}
	})

	t.Run("dnssec servfail on the DS query is error not unsigned", func(t *testing.T) {
		z := newZone(t, "example.org. 3600 IN AAAA 2001:db8::1")
		z.servfail["example.org."] = true
		res, _ := NewDNSSEC(zoneDialer(t, z)).Check(context.Background(), "example.org", KindApex)
		if res.Status != StatusError {
			t.Errorf("status = %s, want error (01 §11.5: DS lookup error → error)", res.Status)
		}
	})
}

// TestDNSNSWalkUpStopsAtICANNSuffixOnly (01 §11.3, 06 §3.4): a private-section
// PSL suffix (github.io) is a real zone whose NS answer is the delegated zone
// for the hosts under it; the registry boundary is the ICANN section.
func TestDNSNSWalkUpStopsAtICANNSuffixOnly(t *testing.T) {
	z := newZone(t,
		"github.io. 3600 IN NS ns1.github.io.",
		"ns1.github.io. 3600 IN AAAA 2001:db8::53",
	)
	res, _ := NewDNSNSIPv6(zoneDialer(t, z), 4).Check(context.Background(), "docs.example.github.io", KindSubdomain)
	if res.Status != StatusSupported {
		t.Fatalf("status = %s, want supported (walked up into github.io)", res.Status)
	}
	if d := res.Detail.(*NSDetail); d.Zone != "github.io" {
		t.Errorf("zone = %q, want github.io", d.Zone)
	}
}
