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
	cases := []struct{ host, want string }{
		{"dnb.no", "no"},
		{"example.com", "com"},
		{"bbc.co.uk", "co.uk"},   // multi-label registry suffix
		{"www.gov.uk", "gov.uk"}, // multi-label registry suffix
		{"usa.gov", "gov"},
		{"foo.blogspot.com", "com"}, // private-registry suffix walks up to ICANN
		{"xn--mre-qla.no", "no"},
	}
	for _, tc := range cases {
		if got := TLD(tc.host); got != tc.want {
			t.Errorf("TLD(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
}

func TestETLDPlusOne(t *testing.T) {
	cases := []struct{ host, want string }{
		{"www.bbc.co.uk", "bbc.co.uk"},
		{"api.dnb.no", "dnb.no"},
		{"dnb.no", "dnb.no"},
	}
	for _, tc := range cases {
		got, err := ETLDPlusOne(tc.host)
		if err != nil {
			t.Fatalf("ETLDPlusOne(%q) error: %v", tc.host, err)
		}
		if got != tc.want {
			t.Errorf("ETLDPlusOne(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
	if _, err := ETLDPlusOne("co.uk"); err == nil {
		t.Error("ETLDPlusOne(co.uk) should fail (bare public suffix)")
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
