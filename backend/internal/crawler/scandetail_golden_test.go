package crawler

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/observe"
)

var updateGolden = flag.Bool("update", false, "rewrite scan_detail golden files")

// Fixture literals that would otherwise trip goconst against non-test code.
const (
	fxErrNoNS    = "no NS records found"
	fxKeyChecks  = "checks"
	fxSkipNoAAAA = "no AAAA record on base domain"
)

// TestScanDetailGolden pins the serialized scan_detail wire shape (03 §14.2)
// against golden files: three representative scans whose fixtures cover every
// detail key each check can emit. Comparison is canonical (decoded JSON, not
// bytes) — key order is not part of the contract. Regenerate deliberately
// with: go test ./internal/crawler -run TestScanDetailGolden -update
func TestScanDetailGolden(t *testing.T) {
	scanTime := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name             string
		sr               checker.ScanResult
		preflight        time.Time
		links            []observe.LinkedResource
		resourcesEnabled bool
	}{
		{
			name:             "hero",
			sr:               fixtureHero(scanTime),
			preflight:        scanTime,
			links:            heroLinks(),
			resourcesEnabled: true,
		},
		{
			name:             "v4only_skips",
			sr:               fixtureV4Only(scanTime),
			preflight:        scanTime,
			resourcesEnabled: false,
		},
		{
			name:             "error_paths",
			sr:               fixtureErrorPaths(scanTime),
			preflight:        time.Time{}, // stale preflight: conn guard path
			resourcesEnabled: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs := observe.MapObservations(domain.KindApex, tc.sr, tc.preflight, scanTime, tc.links, tc.resourcesEnabled)
			raw := buildDetails(tc.sr, &obs)

			golden := filepath.Join("testdata", "scan_detail_"+tc.name+".golden.json")
			if *updateGolden {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatalf("mkdir testdata: %v", err)
				}
				var pretty any
				if err := json.Unmarshal(raw, &pretty); err != nil {
					t.Fatalf("buildDetails produced invalid JSON: %v", err)
				}
				out, err := json.MarshalIndent(pretty, "", "  ")
				if err != nil {
					t.Fatalf("re-marshal golden: %v", err)
				}
				if err := os.WriteFile(golden, append(out, '\n'), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden (run with -update to create): %v", err)
			}
			if !reflect.DeepEqual(decodeJSON(t, raw), decodeJSON(t, want)) {
				t.Errorf("scan_detail shape drifted from %s\ngot:\n%s", golden, indentJSON(t, raw))
			}
		})
	}
}

// TestScanDetailRoundTrip proves the fresh-vs-loaded equivalence the typed
// payload exists for: a scan_detail envelope (hoist keys included)
// unmarshaled into checker.ScanResult carries the same typed details as the
// fresh engine result. Runner-synthesized results (bare CommonDetail) load
// as the check's own struct with the common fields folded in.
func TestScanDetailRoundTrip(t *testing.T) {
	scanTime := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	for _, fresh := range []checker.ScanResult{
		fixtureHero(scanTime), fixtureV4Only(scanTime), fixtureErrorPaths(scanTime),
	} {
		t.Run(fresh.Domain, func(t *testing.T) {
			obs := observe.MapObservations(domain.KindApex, fresh, scanTime, scanTime, heroLinks(), true)
			raw := buildDetails(fresh, &obs)

			var loaded checker.ScanResult
			if err := json.Unmarshal(raw, &loaded); err != nil {
				t.Fatalf("unmarshal scan_detail: %v", err)
			}
			if loaded.Domain != fresh.Domain || !loaded.ScannedAt.Equal(fresh.ScannedAt) || loaded.Duration != fresh.Duration {
				t.Errorf("envelope fields drifted: %s %s %s", loaded.Domain, loaded.ScannedAt, loaded.Duration)
			}
			if len(loaded.Results) != len(fresh.Results) {
				t.Fatalf("results = %d checks, want %d", len(loaded.Results), len(fresh.Results))
			}
			for name, want := range fresh.Results {
				got, ok := loaded.Results[name]
				if !ok {
					t.Errorf("%s missing after round trip", name)
					continue
				}
				if got.Status != want.Status || got.Latency != want.Latency {
					t.Errorf("%s status/latency drifted: %s/%v", name, got.Status, got.Latency)
				}
				if cd, isCommon := want.Detail.(*checker.CommonDetail); isCommon {
					// Synthesized skip: loads as the check's own struct with
					// the common fields folded in.
					var gotCD checker.CommonDetail
					b, err := json.Marshal(got.Detail)
					if err != nil || json.Unmarshal(b, &gotCD) != nil || gotCD != *cd {
						t.Errorf("%s synthesized detail drifted: %#v", name, got.Detail)
					}
					continue
				}
				if !reflect.DeepEqual(got.Detail, want.Detail) {
					t.Errorf("%s detail drifted after round trip:\n got %#v\nwant %#v", name, got.Detail, want.Detail)
				}
			}
		})
	}
}

func decodeJSON(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, b)
	}
	return v
}

func indentJSON(t *testing.T, b []byte) string {
	t.Helper()
	out, err := json.MarshalIndent(decodeJSON(t, b), "", "  ")
	if err != nil {
		t.Fatalf("indent: %v", err)
	}
	return string(out)
}

func fxRes(status checker.CheckStatus, d checker.Detail, latency time.Duration) checker.Result {
	return checker.Result{Status: status, Detail: d, Latency: latency}
}

func fxPtr[T any](v T) *T { return &v }

func quorumAgree() *checker.QuorumInfo {
	return &checker.QuorumInfo{
		PerResolver: map[string]string{"cloudflare": "exists", "google": "exists", "quad9": "exists"},
		Rcodes:      map[string]string{"cloudflare": "NOERROR", "google": "NOERROR", "quad9": "NOERROR"},
		Agreement:   "3of3",
	}
}

func quorumSplit() *checker.QuorumInfo {
	return &checker.QuorumInfo{
		PerResolver: map[string]string{"cloudflare": "empty", "google": "empty", "quad9": "timeout"},
		Rcodes:      map[string]string{"cloudflare": "NOERROR", "google": "NOERROR", "quad9": ""},
		Agreement:   "2of3",
		Disagreed:   true,
	}
}

// fixtureHero covers every success-path detail key: full AAAA answers with
// quorum + CNAME + CDN, partial NS/MX host maps, signed DNSSEC, web checks
// with certificate fields, parity comparison, SMTP dialogue, evaluated SPF,
// confirmed PTR, latency measurements, and non-empty resource discovery.
func fixtureHero(ts time.Time) checker.ScanResult {
	return checker.ScanResult{
		Domain:    "hero.no",
		ScannedAt: ts,
		Duration:  42 * time.Second,
		Results: map[string]checker.Result{
			"dns_aaaa_base": fxRes(checker.StatusSupported, &checker.AAAADetail{
				Rcode:      "NOERROR",
				CNAMEChain: []string{"hero.no.", "edge.hero.no."},
				Quorum:     quorumAgree(),
				Addresses:  []string{"2a02:c0::1", "2a02:c0::2"},
				TTL:        fxPtr(300),
			}, 120*time.Millisecond),
			"dns_aaaa_www": fxRes(checker.StatusSupported, &checker.AAAADetail{
				Rcode:       "NOERROR",
				CNAMEChain:  []string{"www.hero.no.", "hero.cdn77.net."},
				CNAMETarget: "hero.cdn77.net.",
				CDNDetected: true,
				Quorum:      quorumAgree(),
				Addresses:   []string{"2a02:c0::10"},
				TTL:         fxPtr(60),
			}, 110*time.Millisecond),
			"dns_ns_ipv6": fxRes(checker.StatusPartial, &checker.NSDetail{
				Zone: "hero.no",
				Nameservers: map[string]checker.NSHost{
					"ns1.hero.no.": {HasIPv6: true, Addresses: []string{"2a02:c0:2::53"}},
					"ns2.hero.no.": {HasIPv6: false, Addresses: []string{}},
				},
				Total:     3,
				Checked:   2,
				IPv6Count: fxPtr(1),
			}, 200*time.Millisecond),
			"dns_mx_ipv6": fxRes(checker.StatusPartial, &checker.MXDetail{
				MXRecords: map[string]checker.MXHost{
					"mx1.hero.no.": {Preference: 10, HasIPv6: true, Addresses: []string{"2a02:c0:3::25"}},
					"mx2.hero.no.": {Preference: 20, HasIPv6: false, Addresses: []string{}},
				},
				Total:     2,
				IPv6Count: fxPtr(1),
			}, 180*time.Millisecond),
			"dns_dnssec": fxRes(checker.StatusSupported, &checker.DNSSECDetail{
				Signed: true,
				DSRecords: []checker.DSRecord{
					{KeyTag: 12345, Algorithm: "ECDSAP256SHA256", DigestType: 2},
				},
				ChainComplete: fxPtr(true),
				ADFlag:        fxPtr(true),
			}, 90*time.Millisecond),
			"https_ipv6": fxRes(checker.StatusSupported, &checker.HTTPDetail{
				Address:        "2a02:c0::1",
				StatusCode:     200,
				ResponseTimeMS: fxPtr(int64(87)),
				Server:         "nginx",
				TLSVersion:     "TLS 1.3",
			}, 300*time.Millisecond),
			"http_ipv6": fxRes(checker.StatusSupported, &checker.HTTPDetail{
				Address:        "2a02:c0::1",
				StatusCode:     301,
				ResponseTimeMS: fxPtr(int64(45)),
				Server:         "nginx",
			}, 150*time.Millisecond),
			"tls_ipv6": fxRes(checker.StatusSupported, &checker.TLSDetail{
				Address:       "2a02:c0::1",
				TLSVersion:    "TLS 1.3",
				CipherSuite:   "TLS_AES_128_GCM_SHA256",
				Valid:         fxPtr(true),
				Issuer:        "R11",
				Subject:       "hero.no",
				SAN:           []string{"hero.no", "www.hero.no"},
				NotBefore:     "2026-05-01T00:00:00Z",
				NotAfter:      "2026-08-01T00:00:00Z",
				ExpiresInDays: fxPtr(30),
				ExpiresSoon:   fxPtr(true),
			}, 250*time.Millisecond),
			"http_response_parity": fxRes(checker.StatusSupported, &checker.ParityDetail{
				IPv4: &checker.ParityFetch{
					Address: "192.0.2.10", StatusCode: 200, ContentType: "text/html; charset=utf-8",
					ContentLength: 52140, ResponseTimeMS: 95,
				},
				IPv6: &checker.ParityFetch{
					Address: "2a02:c0::1", StatusCode: 200, ContentType: "text/html; charset=utf-8",
					ContentLength: 52290, ResponseTimeMS: 88,
				},
				StatusMatch:          fxPtr(true),
				ContentTypeMatch:     fxPtr(true),
				ContentLengthDiffPct: fxPtr(0.3),
			}, 400*time.Millisecond),
			"smtp_ipv6": fxRes(checker.StatusSupported, &checker.SMTPDetail{
				MXHost:          "mx1.hero.no.",
				MXPreference:    fxPtr(uint16(10)),
				Address:         "2a02:c0:3::25",
				Banner:          "220 mx1.hero.no ESMTP",
				EHLOResponse:    "250-mx1.hero.no\n250 STARTTLS",
				STARTTLSOffered: fxPtr(true),
			}, 500*time.Millisecond),
			"spf_ipv6": fxRes(checker.StatusSupported, &checker.SPFDetail{
				SPFRecord:       "v=spf1 ip6:2a02:c0::/32 include:_spf.hero.no ~all",
				HasIP6Mechanism: fxPtr(true),
				IP6Mechanisms:   []string{"ip6:2a02:c0::/32"},
				IncludeHasIP6:   fxPtr(false),
				IncludeChain:    []string{"_spf.hero.no"},
				LookupCount:     fxPtr(2),
			}, 80*time.Millisecond),
			"dns_ptr_ipv6": fxRes(checker.StatusSupported, &checker.PTRDetail{
				Checks: []checker.PTRCheck{
					{Address: "2a02:c0::1", PTRName: "web.hero.no.", ForwardConfirmed: true},
				},
				AllConfirmed: fxPtr(true),
			}, 220*time.Millisecond),
			"latency_ipv4": fxRes(checker.StatusSupported, &checker.LatencyDetail{
				Address: "192.0.2.10", TTFBMS: fxPtr(int64(31)), Measurements: []int64{30, 32, 40}, AvgMS: fxPtr(int64(31)),
			}, 350*time.Millisecond),
			"latency_ipv6": fxRes(checker.StatusSupported, &checker.LatencyDetail{
				Address: "2a02:c0::1", TTFBMS: fxPtr(int64(28)), Measurements: []int64{28, 29, 35}, AvgMS: fxPtr(int64(28)),
			}, 340*time.Millisecond),
			"resource_discovery": fxRes(checker.StatusSupported, &checker.ResourceDiscoveryDetail{
				Hosts: []string{"cdn.example.no", "fonts.example.no"}, TotalHosts: fxPtr(2),
			}, 600*time.Millisecond),
		},
	}
}

func heroLinks() []observe.LinkedResource {
	s := domain.StatusSupported
	return []observe.LinkedResource{{AAAAStatus: &s}, {AAAAStatus: &s}}
}

// fixtureV4Only covers the v4-only and skip shapes: conditional-A outcomes
// with the attribution a_address, quorum disagreement, unsigned DNSSEC, the
// implicit-MX fallback, runner-synthesized skip reasons, and the disabled
// resources dimension.
func fixtureV4Only(ts time.Time) checker.ScanResult {
	skip := func(reason string) checker.Result {
		return checker.Result{Status: checker.StatusNotApplicable, Detail: &checker.CommonDetail{Reason: reason}}
	}
	return checker.ScanResult{
		Domain:    "legacy.no",
		ScannedAt: ts,
		Duration:  18 * time.Second,
		Results: map[string]checker.Result{
			"dns_aaaa_base": fxRes(checker.StatusUnsupported, &checker.AAAADetail{
				Rcode:    "NOERROR",
				Quorum:   quorumSplit(),
				AOutcome: "a_present",
				AAddress: "192.0.2.10",
			}, 130*time.Millisecond),
			"dns_aaaa_www": fxRes(checker.StatusUnsupported, &checker.AAAADetail{
				Rcode:    "NOERROR",
				Quorum:   quorumSplit(),
				AOutcome: "a_absent",
			}, 125*time.Millisecond),
			"dns_ns_ipv6": fxRes(checker.StatusUnsupported, &checker.NSDetail{
				Nameservers: map[string]checker.NSHost{
					"ns1.legacy.no.": {HasIPv6: false, Addresses: []string{}},
					"ns2.legacy.no.": {HasIPv6: false, Addresses: []string{}},
				},
				Total:     2,
				Checked:   2,
				IPv6Count: fxPtr(0),
			}, 190*time.Millisecond),
			"dns_mx_ipv6": fxRes(checker.StatusSupported, &checker.MXDetail{
				CommonDetail: checker.CommonDetail{Reason: "implicit MX fallback (RFC 5321 §5.1)"},
				Addresses:    []string{"2a02:c0::99"},
			}, 100*time.Millisecond),
			"dns_dnssec":           fxRes(checker.StatusUnsupported, &checker.DNSSECDetail{Signed: false}, 70*time.Millisecond),
			"http_ipv6":            skip(fxSkipNoAAAA),
			"https_ipv6":           skip(fxSkipNoAAAA),
			"tls_ipv6":             skip(fxSkipNoAAAA),
			"latency_ipv4":         skip(fxSkipNoAAAA),
			"latency_ipv6":         skip(fxSkipNoAAAA),
			"resource_discovery":   skip(fxSkipNoAAAA),
			"http_response_parity": skip(fxSkipNoAAAA),
			"dns_ptr_ipv6":         skip(fxSkipNoAAAA),
			"smtp_ipv6":            skip("no MX with AAAA record"),
			"spf_ipv6":             fxRes(checker.StatusNotApplicable, &checker.SPFDetail{CommonDetail: checker.CommonDetail{Reason: "no SPF record"}}, 60*time.Millisecond),
		},
	}
}

// fixtureErrorPaths covers the failure shapes: an inconsistent quorum, the
// CD=1 rescue outcome, error_type tokens, a failed TLS handshake, a parity
// mismatch, an SMTP banner rejection, RFC-violating SPF, unconfirmed PTR,
// blocked/failed latency, and empty-but-present resource discovery.
func fixtureErrorPaths(ts time.Time) checker.ScanResult {
	return checker.ScanResult{
		Domain:    "broken.no",
		ScannedAt: ts,
		Duration:  61 * time.Second,
		Results: map[string]checker.Result{
			"dns_aaaa_base": fxRes(checker.StatusError, &checker.AAAADetail{
				Rcode:        "",
				Quorum:       quorumSplit(),
				Inconsistent: true,
			}, 140*time.Millisecond),
			"dns_aaaa_www": fxRes(checker.StatusError, &checker.AAAADetail{
				CommonDetail: checker.CommonDetail{Error: "all resolvers failed"},
				Rcode:        "SERVFAIL",
				CDOutcome:    "cd_fail",
			}, 135*time.Millisecond),
			"dns_ns_ipv6": fxRes(checker.StatusError, &checker.NSDetail{
				CommonDetail: checker.CommonDetail{Error: fxErrNoNS},
			}, 90*time.Millisecond),
			"dns_mx_ipv6": fxRes(checker.StatusNotApplicable, &checker.MXDetail{
				CommonDetail: checker.CommonDetail{Reason: "null MX record"},
			}, 85*time.Millisecond),
			"dns_dnssec": fxRes(checker.StatusError, &checker.DNSSECDetail{
				CommonDetail: checker.CommonDetail{Error: "AD flag check failed: could not query domain for AD flag validation"},
				Signed:       true,
				DSRecords: []checker.DSRecord{
					{KeyTag: 777, Algorithm: "RSASHA256", DigestType: 2},
				},
				ChainComplete: fxPtr(false),
			}, 95*time.Millisecond),
			"http_ipv6": fxRes(checker.StatusError, &checker.HTTPDetail{
				CommonDetail: checker.CommonDetail{Error: "context deadline exceeded"},
				ErrorType:    "timeout",
			}, 10*time.Second),
			"https_ipv6": fxRes(checker.StatusUnsupported, &checker.HTTPDetail{
				CommonDetail: checker.CommonDetail{Error: "tls: failed to verify certificate: x509: certificate signed by unknown authority"},
				ErrorType:    "certificate_error",
			}, 900*time.Millisecond),
			"tls_ipv6": fxRes(checker.StatusUnsupported, &checker.TLSDetail{
				CommonDetail: checker.CommonDetail{Error: "TLS handshake failed: EOF"},
				Address:      "2a02:c0::b",
				Valid:        fxPtr(false),
			}, 800*time.Millisecond),
			"http_response_parity": fxRes(checker.StatusUnsupported, &checker.ParityDetail{
				IPv4: &checker.ParityFetch{
					Address: "192.0.2.20", StatusCode: 200, ContentType: "text/html",
					ContentLength: 10000, ResponseTimeMS: 120,
				},
				IPv6: &checker.ParityFetch{
					Address: "2a02:c0::b", StatusCode: 503, ContentType: "text/html",
					ContentLength: 4730, ResponseTimeMS: 300,
				},
				StatusMatch:          fxPtr(false),
				ContentTypeMatch:     fxPtr(true),
				ContentLengthDiffPct: fxPtr(52.7),
			}, 700*time.Millisecond),
			"smtp_ipv6": fxRes(checker.StatusUnsupported, &checker.SMTPDetail{
				CommonDetail: checker.CommonDetail{Error: "unexpected banner"},
				MXHost:       "mx.broken.no.",
				MXPreference: fxPtr(uint16(5)),
				Address:      "2a02:c0:4::25",
				Banner:       "554 not accepting mail",
			}, 450*time.Millisecond),
			"spf_ipv6": fxRes(checker.StatusError, &checker.SPFDetail{
				CommonDetail: checker.CommonDetail{Error: "multiple SPF records found"},
			}, 55*time.Millisecond),
			"dns_ptr_ipv6": fxRes(checker.StatusPartial, &checker.PTRDetail{
				Checks: []checker.PTRCheck{
					{Address: "2a02:c0::b", PTRName: "", ForwardConfirmed: false},
					{Address: "2a02:c0::c", PTRName: "host.broken.no.", ForwardConfirmed: true},
				},
				AllConfirmed: fxPtr(false),
			}, 210*time.Millisecond),
			"latency_ipv4": fxRes(checker.StatusError, &checker.LatencyDetail{
				CommonDetail: checker.CommonDetail{Error: "all measurements failed"}, Address: "192.0.2.20",
			}, 30*time.Second),
			"latency_ipv6": fxRes(checker.StatusError, &checker.LatencyDetail{
				CommonDetail: checker.CommonDetail{Error: "address in blocked range"},
			}, 5*time.Millisecond),
			"resource_discovery": fxRes(checker.StatusSupported, &checker.ResourceDiscoveryDetail{
				Hosts: []string{}, TotalHosts: fxPtr(0),
			}, 300*time.Millisecond),
		},
	}
}
