package checker

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestDetailJSONRoundTrip pins the name↔type↔JSON-tag contract for EVERY check
// in the newDetail dispatch. A fully-populated Result per name is marshaled as
// a ScanResult, then reloaded through ScanResult.UnmarshalJSON (the re-typing
// dispatch) and read back via the typed accessor. reflect.DeepEqual on the
// detail catches any drift between an emitted key and its struct tag; ok=true
// catches drift between newDetail and the accessor's type argument.
func TestDetailJSONRoundTrip(t *testing.T) {
	quorum := &QuorumInfo{
		PerResolver: map[string]string{"google": "exists"},
		Rcodes:      map[string]string{"google": "NOERROR"},
		Agreement:   "3of3",
		Disagreed:   true,
	}

	// Each entry: the name's fully-populated detail, the status to carry, and
	// an accessor wrapper returning the reloaded detail as `any`.
	type entry struct {
		status   CheckStatus
		detail   Detail
		accessor func(ScanResult) (CheckStatus, any, bool)
	}
	table := map[string]entry{
		NameDNSAAAABase: {
			status: StatusSupported,
			detail: &AAAADetail{
				CommonDetail: CommonDetail{Error: "e", Reason: "r"},
				Rcode:        "NOERROR",
				CNAMEChain:   []string{"a.", "b."},
				AOutcome:     "a_present",
				AAddress:     "192.0.2.1",
				CDOutcome:    "cd_present",
				Inconsistent: true,
				Quorum:       quorum,
				Addresses:    []string{"2001:db8::1"},
				TTL:          new(300),
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.AAAABase(); return s, d, ok },
		},
		NameDNSAAAAWWW: {
			status: StatusSupported,
			detail: &AAAADetail{
				Rcode:       "NOERROR",
				CNAMEChain:  []string{"www.example.org.cdn.cloudflare.net."},
				CNAMETarget: "www.example.org.cdn.cloudflare.net.",
				CDNDetected: true,
				Addresses:   []string{"2001:db8::2"},
				TTL:         new(120),
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.AAAAWWW(); return s, d, ok },
		},
		NameDNSNS: {
			status: StatusPartial,
			detail: &NSDetail{
				CommonDetail: CommonDetail{Reason: "r"},
				Zone:         "example.org",
				Nameservers:  map[string]NSHost{"ns1.example.org.": {HasIPv6: true, Addresses: []string{"2001:db8::53"}}},
				Total:        2,
				Checked:      2,
				IPv6Count:    new(1),
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.NS(); return s, d, ok },
		},
		NameDNSMX: {
			status: StatusSupported,
			detail: &MXDetail{
				Addresses: []string{"2001:db8::25"},
				MXRecords: map[string]MXHost{"mail.example.org.": {Preference: 10, HasIPv6: true, Addresses: []string{"2001:db8::25"}}},
				Total:     1,
				IPv6Count: new(1),
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.MX(); return s, d, ok },
		},
		NameDNSSEC: {
			status: StatusSupported,
			detail: &DNSSECDetail{
				Signed:        true,
				DSRecords:     []DSRecord{{KeyTag: 12345, Algorithm: "ECDSAP256SHA256", DigestType: 2}},
				ChainComplete: new(true),
				ADFlag:        new(true),
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.DNSSEC(); return s, d, ok },
		},
		NameHTTP: {
			status: StatusSupported,
			detail: &HTTPDetail{
				ErrorType:      "timeout",
				Address:        "2001:db8::1",
				StatusCode:     200,
				ResponseTimeMS: new(int64(123)),
				Server:         "nginx",
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.HTTP(); return s, d, ok },
		},
		NameHTTPS: {
			status: StatusSupported,
			detail: &HTTPDetail{
				Address:        "2001:db8::1",
				StatusCode:     200,
				ResponseTimeMS: new(int64(88)),
				Server:         "caddy",
				TLSVersion:     "TLS 1.3",
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.HTTPS(); return s, d, ok },
		},
		NameTLS: {
			status: StatusSupported,
			detail: &TLSDetail{
				Address:       "2001:db8::1",
				TLSVersion:    "TLS 1.3",
				CipherSuite:   "TLS_AES_128_GCM_SHA256",
				Valid:         new(true),
				Issuer:        "Let's Encrypt",
				Subject:       "example.org",
				SAN:           []string{"example.org", "www.example.org"},
				NotBefore:     "2026-01-01T00:00:00Z",
				NotAfter:      "2026-04-01T00:00:00Z",
				ExpiresInDays: new(90),
				ExpiresSoon:   new(false),
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.TLS(); return s, d, ok },
		},
		NameParity: {
			status: StatusSupported,
			detail: &ParityDetail{
				IPv4:                 &ParityFetch{Address: "192.0.2.1", StatusCode: 200, ContentType: "text/html", ContentLength: 100, ResponseTimeMS: 10},
				IPv6:                 &ParityFetch{Address: "2001:db8::1", StatusCode: 200, ContentType: "text/html", ContentLength: 102, ResponseTimeMS: 12},
				StatusMatch:          new(true),
				ContentTypeMatch:     new(true),
				ContentLengthDiffPct: new(2.0),
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.Parity(); return s, d, ok },
		},
		NameSMTP: {
			status: StatusSupported,
			detail: &SMTPDetail{
				MXHost:          "mail.example.org.",
				MXPreference:    new(uint16(10)),
				Address:         "2001:db8::25",
				Banner:          "220 mail.example.org ESMTP",
				EHLOResponse:    "250 mail.example.org",
				STARTTLSOffered: new(true),
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.SMTP(); return s, d, ok },
		},
		NameSPF: {
			status: StatusSupported,
			detail: &SPFDetail{
				SPFRecord:       "v=spf1 ip6:2001:db8::/32 -all",
				HasIP6Mechanism: new(true),
				IP6Mechanisms:   []string{"ip6:2001:db8::/32"},
				IncludeHasIP6:   new(false),
				IncludeChain:    []string{"_spf.example.net"},
				LookupCount:     new(2),
				Implicit:        true,
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.SPF(); return s, d, ok },
		},
		NamePTR: {
			status: StatusSupported,
			detail: &PTRDetail{
				Checks:       []PTRCheck{{Address: "2001:db8::1", PTRName: "host.example.org.", ForwardConfirmed: true}},
				AllConfirmed: new(true),
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.PTR(); return s, d, ok },
		},
		NameLatencyV4: {
			status: StatusSupported,
			detail: &LatencyDetail{
				Address:      "192.0.2.1",
				TTFBMS:       new(int64(50)),
				Measurements: []int64{50, 55, 60},
				AvgMS:        new(int64(55)),
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.LatencyV4(); return s, d, ok },
		},
		NameLatencyV6: {
			status: StatusSupported,
			detail: &LatencyDetail{
				Address:      "2001:db8::1",
				TTFBMS:       new(int64(45)),
				Measurements: []int64{45, 48, 52},
				AvgMS:        new(int64(48)),
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.LatencyV6(); return s, d, ok },
		},
		NameResourceDiscovery: {
			status: StatusSupported,
			detail: &ResourceDiscoveryDetail{
				Hosts:      []string{"cdn.example.com", "fonts.example.net"},
				TotalHosts: new(2),
			},
			accessor: func(sr ScanResult) (CheckStatus, any, bool) { s, d, ok := sr.ResourceDiscovery(); return s, d, ok },
		},
	}

	// Guard against a new check name appearing in newDetail without a
	// round-trip case here.
	for _, name := range dispatchNames() {
		if _, ok := table[name]; !ok {
			t.Errorf("newDetail dispatches %q but the round-trip table has no case for it", name)
		}
	}

	in := ScanResult{Domain: "example.org", Results: map[string]Result{}}
	for name, e := range table {
		in.Results[name] = Result{Status: e.status, Detail: e.detail}
	}

	blob, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out ScanResult
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for name, e := range table {
		t.Run(name, func(t *testing.T) {
			gotStatus, gotDetail, ok := e.accessor(out)
			if !ok {
				t.Fatalf("accessor ok=false — newDetail dispatch and accessor type argument have drifted for %q", name)
			}
			if gotStatus != e.status {
				t.Errorf("status = %s, want %s", gotStatus, e.status)
			}
			// The accessor returns a value; the input detail is a pointer.
			want := reflect.ValueOf(e.detail).Elem().Interface()
			if !reflect.DeepEqual(gotDetail, want) {
				t.Errorf("detail round-trip drift:\n got  %+v\n want %+v", gotDetail, want)
			}
		})
	}
}

// dispatchNames returns every check name newDetail maps to a concrete type
// (i.e. all names except the CommonDetail fallback). Kept beside the test so a
// new case added to newDetail forces a matching round-trip entry above.
func dispatchNames() []string {
	return []string{
		NameDNSAAAABase, NameDNSAAAAWWW, NameDNSNS, NameDNSMX, NameDNSSEC,
		NameHTTP, NameHTTPS, NameTLS, NameParity, NameSMTP, NameSPF, NamePTR,
		NameLatencyV4, NameLatencyV6, NameResourceDiscovery,
	}
}
