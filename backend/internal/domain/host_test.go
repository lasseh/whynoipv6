package domain

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Boundary hosts generated in code so the 253/254-octet edge is exact
// (10-testing.md §2).
func hostOfLen(n int) string {
	label63 := strings.Repeat("a", 63)
	// 63 + 1 + 63 + 1 + 63 + 1 + k + 1 + 2 == n  =>  k = n - 195
	return label63 + "." + label63 + "." + label63 + "." + strings.Repeat("a", n-195) + ".no"
}

//nolint:goconst // vector tables repeat fixture hostnames by design
func TestCanonicalize(t *testing.T) {
	label63 := strings.Repeat("a", 63)

	accept := []struct {
		name, raw, want string
	}{
		{"trailing_dot", "dnb.no.", "dnb.no"},
		{"upper_and_dot", "DNB.no.", "dnb.no"},
		// Spec correction (2026-07-10): 06/10 listed xn--mre-qla.no, but that
		// A-label decodes to "märe"; the correct encoding of "møre" is below.
		{"idn_unicode", "møre.no", "xn--mre-0na.no"},
		{"idn_already_punycode", "XN--MRE-QLA.no", "xn--mre-qla.no"},
		{"leading_trailing_space", "  dnb.no  ", "dnb.no"},
		{"mixed_case", "Example.COM", "example.com"},
		{"three_labels", "api.dnb.no", "api.dnb.no"},
		{"max_label_63", label63 + ".no", label63 + ".no"},
		{"total_len_253", hostOfLen(253), hostOfLen(253)},
		{"uts46_fold", "ß.example", "xn--zca.example"},
	}
	for _, tc := range accept {
		t.Run("accept/"+tc.name, func(t *testing.T) {
			got, err := Canonicalize(tc.raw)
			if err != nil {
				t.Fatalf("Canonicalize(%q) error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("Canonicalize(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}

	reject := []struct {
		name, raw string
	}{
		{"empty", ""},
		{"whitespace_only", "   "},
		{"underscore_wildcard", "_wildcard_.ph"},
		{"empty_label", "a..b"},
		{"double_trailing_dot", "dnb.no.."},
		{"ipv4_literal", "1.2.3.4"},
		{"ipv6_bracketed", "[::1]"},
		{"single_label", "localhost"},
		{"tld_only", "no"},
		{"over_253", hostOfLen(254)},
		{"label_over_63", strings.Repeat("a", 64) + ".no"},
		{"has_scheme", "http://x.no"},
		{"has_path", "x.no/foo"},
		{"has_port", "x.no:443"},
		{"has_at", "a@b.no"},
		{"has_query", "x.no?a=1"},
		{"internal_space", "foo bar.no"},
	}
	for _, tc := range reject {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			got, err := Canonicalize(tc.raw)
			if err == nil {
				t.Fatalf("Canonicalize(%q) = %q, want error", tc.raw, got)
			}
			if !errors.Is(err, ErrInvalidHost) {
				t.Errorf("Canonicalize(%q) error %v does not wrap ErrInvalidHost", tc.raw, err)
			}
		})
	}
}

func TestTLD(t *testing.T) {
	cases := []struct{ name, host, want string }{
		{"cctld", "dnb.no", "no"},
		{"gtld", "example.com", "com"},
		{"multi_label_registry", "bbc.co.uk", "co.uk"},
		{"multi_label_registry_subdomain", "www.gov.uk", "gov.uk"},
		{"sponsored_tld", "usa.gov", "gov"},
		{"private_registry_walks_up", "foo.blogspot.com", "com"},
		{"punycode", "xn--mre-qla.no", "no"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TLD(tc.host); got != tc.want {
				t.Errorf("TLD(%q) = %q, want %q", tc.host, got, tc.want)
			}
		})
	}
}

// TestTLDAgreesWithPSLParse is review issue 34's invariant: `tld` comes from
// ONE derivation whatever the ingress. It used to come from
// x/net/publicsuffix on the Tranco path and weppos everywhere else — two
// vendored snapshots that could disagree on a second-level rule and split a
// suffix across two ?tld= facets.
func TestTLDAgreesWithPSLParse(t *testing.T) {
	for _, host := range []string{
		"dnb.no", "example.com", "bbc.co.uk", "www.gov.uk", "usa.gov",
		"foo.blogspot.com", "xn--mre-qla.no", "a.b.c.example.co.uk",
	} {
		t.Run(host, func(t *testing.T) {
			_, tld, err := PSLParse(host)
			if err != nil {
				t.Fatalf("PSLParse(%q): %v", host, err)
			}
			if got := TLD(host); got != tld {
				t.Errorf("TLD = %q but PSLParse = %q: one derivation, two answers", got, tld)
			}
		})
	}
}

// TestTLDFallsBackToTheFinalLabel: `domain.tld` is NOT NULL and the Tranco
// import admits its own rows, so a host PSLParse refuses — an unknown or
// not-yet-listed suffix — still lands in a facet named after its label
// rather than failing the import (review issue 34).
func TestTLDFallsBackToTheFinalLabel(t *testing.T) {
	for host, want := range map[string]string{
		"example.unknowntld999": "unknowntld999",
		"deep.example.invalid":  "invalid",
		"localhost":             "localhost",
	} {
		t.Run(host, func(t *testing.T) {
			if _, _, err := PSLParse(host); err == nil {
				t.Skipf("%q now parses; the fallback is untested by this vector", host)
			}
			if got := TLD(host); got != want {
				t.Errorf("TLD(%q) = %q, want the final label %q", host, got, want)
			}
		})
	}
}

// TestPSLParseRejectsUnknownTLD keeps the property campaign validation
// depends on: no wildcard default rule, so an unknown TLD is an invalid
// entry rather than a registrable name (06 §4.2).
func TestPSLParseRejectsUnknownTLD(t *testing.T) {
	for _, host := range []string{"example.unknowntld999", "no", "co.uk"} {
		t.Run(host, func(t *testing.T) {
			if _, _, err := PSLParse(host); err == nil {
				t.Errorf("PSLParse(%q) succeeded, want an error", host)
			}
		})
	}
}

// TestNoStrayHostLowercasing is the 06-ingest §9.1 grep gate: strings.ToLower
// may touch a hostname only inside host.go. Non-hostname uses must be listed
// here with a justification.
func TestNoStrayHostLowercasing(t *testing.T) {
	allowed := map[string]string{
		"internal/domain/host.go":     "the sanctioned Canonicalize step 3",
		"internal/config/config.go":   "LOG_LEVEL parsing, not a hostname",
		"internal/campaign/parse.go":  "tag/uuid normalization, not a hostname",
		"internal/ingest/provider.go": "operator-entered NS suffixes normalized at the single write path",
		"internal/ingest/hosting.go":  "CNAME targets folded for the CDN-suffix match; never a hostname write",
		// Lifted engine files (01-engine.md): behavior-identical lift; their
		// lowercasing predates Canonicalize and never reaches a DB write.
		"internal/checker/resource_discovery.go": "lifted tokenizer hostname folding (01 §11.9)",
		"internal/checker/response_parity.go":    "lifted content-type case folding (01 §11.8)",
		"internal/checker/spf_ipv6.go":           "lifted SPF mechanism folding (01 §11.11)",
	}
	root := moduleRoot(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == ".git" || name == "db" && filepath.Dir(path) == filepath.Join(root, "internal", "postgres") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if _, ok := allowed[filepath.ToSlash(rel)]; ok {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "strings.ToLower") {
			t.Errorf("%s uses strings.ToLower — hostname lowercasing is allowed only in internal/domain/host.go (06-ingest §9.1)", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test dir")
		}
		dir = parent
	}
}
