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
		links            []LinkedResource
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
			obs := MapObservations(domain.KindApex, tc.sr, tc.preflight, scanTime, tc.links, tc.resourcesEnabled)
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

func fxRes(status checker.CheckStatus, details map[string]any, latency time.Duration) checker.Result {
	return checker.Result{Status: status, Details: details, Latency: latency}
}

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
			"dns_aaaa_base": fxRes(checker.StatusSupported, map[string]any{
				"rcode":       "NOERROR",
				"cname_chain": []string{"hero.no.", "edge.hero.no."},
				"quorum":      quorumAgree(),
				"addresses":   []string{"2a02:c0::1", "2a02:c0::2"},
				"ttl":         300,
			}, 120*time.Millisecond),
			"dns_aaaa_www": fxRes(checker.StatusSupported, map[string]any{
				"rcode":        "NOERROR",
				"cname_chain":  []string{"www.hero.no.", "hero.cdn77.net."},
				"cname_target": "hero.cdn77.net.",
				"cdn_detected": true,
				"quorum":       quorumAgree(),
				"addresses":    []string{"2a02:c0::10"},
				"ttl":          60,
			}, 110*time.Millisecond),
			"dns_ns_ipv6": fxRes(checker.StatusPartial, map[string]any{
				"zone": "hero.no",
				"nameservers": map[string]any{
					"ns1.hero.no.": map[string]any{"has_ipv6": true, "addresses": []string{"2a02:c0:2::53"}},
					"ns2.hero.no.": map[string]any{"has_ipv6": false, "addresses": []string{}},
				},
				"total":      3,
				"checked":    2,
				"ipv6_count": 1,
			}, 200*time.Millisecond),
			"dns_mx_ipv6": fxRes(checker.StatusPartial, map[string]any{
				"mx_records": map[string]any{
					"mx1.hero.no.": map[string]any{"preference": uint16(10), "has_ipv6": true, "addresses": []string{"2a02:c0:3::25"}},
					"mx2.hero.no.": map[string]any{"preference": uint16(20), "has_ipv6": false, "addresses": []string{}},
				},
				"total":      2,
				"ipv6_count": 1,
			}, 180*time.Millisecond),
			"dns_dnssec": fxRes(checker.StatusSupported, map[string]any{
				"signed": true,
				"ds_records": []map[string]any{
					{"key_tag": uint16(12345), "algorithm": "ECDSAP256SHA256", "digest_type": uint8(2)},
				},
				"chain_complete": true,
				"ad_flag":        true,
			}, 90*time.Millisecond),
			"https_ipv6": fxRes(checker.StatusSupported, map[string]any{
				"address":          "2a02:c0::1",
				"status_code":      200,
				"response_time_ms": int64(87),
				"server":           "nginx",
				"tls_version":      "TLS 1.3",
			}, 300*time.Millisecond),
			"http_ipv6": fxRes(checker.StatusSupported, map[string]any{
				"address":          "2a02:c0::1",
				"status_code":      301,
				"response_time_ms": int64(45),
				"server":           "nginx",
			}, 150*time.Millisecond),
			"tls_ipv6": fxRes(checker.StatusSupported, map[string]any{
				"address":         "2a02:c0::1",
				"tls_version":     "TLS 1.3",
				"cipher_suite":    "TLS_AES_128_GCM_SHA256",
				"valid":           true,
				"issuer":          "R11",
				"subject":         "hero.no",
				"san":             []string{"hero.no", "www.hero.no"},
				"not_before":      "2026-05-01T00:00:00Z",
				"not_after":       "2026-08-01T00:00:00Z",
				"expires_in_days": 30,
				"expires_soon":    true,
			}, 250*time.Millisecond),
			"http_response_parity": fxRes(checker.StatusSupported, map[string]any{
				"ipv4": map[string]any{
					"address": "192.0.2.10", "status_code": 200, "content_type": "text/html; charset=utf-8",
					"content_length": int64(52140), "response_time_ms": int64(95),
				},
				"ipv6": map[string]any{
					"address": "2a02:c0::1", "status_code": 200, "content_type": "text/html; charset=utf-8",
					"content_length": int64(52290), "response_time_ms": int64(88),
				},
				"status_match":            true,
				"content_type_match":      true,
				"content_length_diff_pct": 0.3,
			}, 400*time.Millisecond),
			"smtp_ipv6": fxRes(checker.StatusSupported, map[string]any{
				"mx_host":          "mx1.hero.no.",
				"mx_preference":    uint16(10),
				"address":          "2a02:c0:3::25",
				"banner":           "220 mx1.hero.no ESMTP",
				"ehlo_response":    "250-mx1.hero.no\n250 STARTTLS",
				"starttls_offered": true,
			}, 500*time.Millisecond),
			"spf_ipv6": fxRes(checker.StatusSupported, map[string]any{
				"spf_record":        "v=spf1 ip6:2a02:c0::/32 include:_spf.hero.no ~all",
				"has_ip6_mechanism": true,
				"ip6_mechanisms":    []string{"ip6:2a02:c0::/32"},
				"include_has_ip6":   false,
				"include_chain":     []string{"_spf.hero.no"},
				"lookup_count":      2,
			}, 80*time.Millisecond),
			"dns_ptr_ipv6": fxRes(checker.StatusSupported, map[string]any{
				fxKeyChecks: []map[string]any{
					{"address": "2a02:c0::1", "ptr_name": "web.hero.no.", "forward_confirmed": true},
				},
				"all_confirmed": true,
			}, 220*time.Millisecond),
			"latency_ipv4": fxRes(checker.StatusSupported, map[string]any{
				"address": "192.0.2.10", "ttfb_ms": int64(31), "measurements": []int64{30, 32, 40}, "avg_ms": int64(31),
			}, 350*time.Millisecond),
			"latency_ipv6": fxRes(checker.StatusSupported, map[string]any{
				"address": "2a02:c0::1", "ttfb_ms": int64(28), "measurements": []int64{28, 29, 35}, "avg_ms": int64(28),
			}, 340*time.Millisecond),
			"resource_discovery": fxRes(checker.StatusSupported, map[string]any{
				"hosts": []string{"cdn.example.no", "fonts.example.no"}, "total_hosts": 2,
			}, 600*time.Millisecond),
		},
	}
}

func heroLinks() []LinkedResource {
	s := domain.StatusSupported
	return []LinkedResource{{AAAAStatus: &s}, {AAAAStatus: &s}}
}

// fixtureV4Only covers the v4-only and skip shapes: conditional-A outcomes
// with the attribution a_address, quorum disagreement, unsigned DNSSEC, the
// implicit-MX fallback, runner-synthesized skip reasons, and the disabled
// resources dimension.
func fixtureV4Only(ts time.Time) checker.ScanResult {
	skip := func(reason string) checker.Result {
		return checker.Result{Status: checker.StatusNotApplicable, Details: map[string]any{"reason": reason}}
	}
	return checker.ScanResult{
		Domain:    "legacy.no",
		ScannedAt: ts,
		Duration:  18 * time.Second,
		Results: map[string]checker.Result{
			"dns_aaaa_base": fxRes(checker.StatusUnsupported, map[string]any{
				"rcode":     "NOERROR",
				"quorum":    quorumSplit(),
				"a_outcome": "a_present",
				"a_address": "192.0.2.10",
			}, 130*time.Millisecond),
			"dns_aaaa_www": fxRes(checker.StatusUnsupported, map[string]any{
				"rcode":     "NOERROR",
				"quorum":    quorumSplit(),
				"a_outcome": "a_absent",
			}, 125*time.Millisecond),
			"dns_ns_ipv6": fxRes(checker.StatusUnsupported, map[string]any{
				"nameservers": map[string]any{
					"ns1.legacy.no.": map[string]any{"has_ipv6": false, "addresses": []string{}},
					"ns2.legacy.no.": map[string]any{"has_ipv6": false, "addresses": []string{}},
				},
				"total":      2,
				"checked":    2,
				"ipv6_count": 0,
			}, 190*time.Millisecond),
			"dns_mx_ipv6": fxRes(checker.StatusSupported, map[string]any{
				"reason":    "implicit MX fallback (RFC 5321 §5.1)",
				"addresses": []string{"2a02:c0::99"},
			}, 100*time.Millisecond),
			"dns_dnssec":           fxRes(checker.StatusUnsupported, map[string]any{"signed": false}, 70*time.Millisecond),
			"http_ipv6":            skip(fxSkipNoAAAA),
			"https_ipv6":           skip(fxSkipNoAAAA),
			"tls_ipv6":             skip(fxSkipNoAAAA),
			"latency_ipv4":         skip(fxSkipNoAAAA),
			"latency_ipv6":         skip(fxSkipNoAAAA),
			"resource_discovery":   skip(fxSkipNoAAAA),
			"http_response_parity": skip(fxSkipNoAAAA),
			"dns_ptr_ipv6":         skip(fxSkipNoAAAA),
			"smtp_ipv6":            skip("no MX with AAAA record"),
			"spf_ipv6":             fxRes(checker.StatusNotApplicable, map[string]any{"reason": "no SPF record"}, 60*time.Millisecond),
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
			"dns_aaaa_base": fxRes(checker.StatusError, map[string]any{
				"rcode":        "",
				"quorum":       quorumSplit(),
				"inconsistent": true,
			}, 140*time.Millisecond),
			"dns_aaaa_www": fxRes(checker.StatusError, map[string]any{
				"rcode":      "SERVFAIL",
				"cd_outcome": "cd_fail",
				"error":      "all resolvers failed",
			}, 135*time.Millisecond),
			"dns_ns_ipv6": fxRes(checker.StatusError, map[string]any{
				"error": fxErrNoNS,
			}, 90*time.Millisecond),
			"dns_mx_ipv6": fxRes(checker.StatusNotApplicable, map[string]any{
				"reason": "null MX record",
			}, 85*time.Millisecond),
			"dns_dnssec": fxRes(checker.StatusError, map[string]any{
				"signed": true,
				"ds_records": []map[string]any{
					{"key_tag": uint16(777), "algorithm": "RSASHA256", "digest_type": uint8(2)},
				},
				"error":          "AD flag check failed: could not query domain for AD flag validation",
				"chain_complete": false,
			}, 95*time.Millisecond),
			"http_ipv6": fxRes(checker.StatusError, map[string]any{
				"error":      "context deadline exceeded",
				"error_type": "timeout",
			}, 10*time.Second),
			"https_ipv6": fxRes(checker.StatusUnsupported, map[string]any{
				"error":      "tls: failed to verify certificate: x509: certificate signed by unknown authority",
				"error_type": "certificate_error",
			}, 900*time.Millisecond),
			"tls_ipv6": fxRes(checker.StatusUnsupported, map[string]any{
				"address": "2a02:c0::b",
				"error":   "TLS handshake failed: EOF",
				"valid":   false,
			}, 800*time.Millisecond),
			"http_response_parity": fxRes(checker.StatusUnsupported, map[string]any{
				"ipv4": map[string]any{
					"address": "192.0.2.20", "status_code": 200, "content_type": "text/html",
					"content_length": int64(10000), "response_time_ms": int64(120),
				},
				"ipv6": map[string]any{
					"address": "2a02:c0::b", "status_code": 503, "content_type": "text/html",
					"content_length": int64(4730), "response_time_ms": int64(300),
				},
				"status_match":            false,
				"content_type_match":      true,
				"content_length_diff_pct": 52.7,
			}, 700*time.Millisecond),
			"smtp_ipv6": fxRes(checker.StatusUnsupported, map[string]any{
				"mx_host":       "mx.broken.no.",
				"mx_preference": uint16(5),
				"address":       "2a02:c0:4::25",
				"banner":        "554 not accepting mail",
				"error":         "unexpected banner",
			}, 450*time.Millisecond),
			"spf_ipv6": fxRes(checker.StatusError, map[string]any{
				"error": "multiple SPF records found",
			}, 55*time.Millisecond),
			"dns_ptr_ipv6": fxRes(checker.StatusPartial, map[string]any{
				fxKeyChecks: []map[string]any{
					{"address": "2a02:c0::b", "ptr_name": "", "forward_confirmed": false},
					{"address": "2a02:c0::c", "ptr_name": "host.broken.no.", "forward_confirmed": true},
				},
				"all_confirmed": false,
			}, 210*time.Millisecond),
			"latency_ipv4": fxRes(checker.StatusError, map[string]any{
				"error": "all measurements failed", "address": "192.0.2.20",
			}, 30*time.Second),
			"latency_ipv6": fxRes(checker.StatusError, map[string]any{
				"error": "address in blocked range",
			}, 5*time.Millisecond),
			"resource_discovery": fxRes(checker.StatusSupported, map[string]any{
				"hosts": []string{}, "total_hosts": 0,
			}, 300*time.Millisecond),
		},
	}
}
