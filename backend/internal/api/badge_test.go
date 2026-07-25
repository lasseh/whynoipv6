package api

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGoldens = flag.Bool("update", false, "rewrite golden files")

// TestBadgeGoldens (10-testing §7.6): six byte-exact SVG variants; the
// renderer is a pure (variant) → []byte with fixed geometry.
func TestBadgeGoldens(t *testing.T) {
	for name := range badgeVariants {
		t.Run(name, func(t *testing.T) {
			got := RenderBadgeSVG(name)
			path := filepath.Join("testdata", "badge", name+".svg")
			if *updateGoldens {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to write goldens)", err)
			}
			if !bytes.Equal(got, want) {
				t.Error("rendered SVG diverges from the golden")
			}
		})
	}
}

// TestBadgeVariantTable pins the normative copy/color mapping (07 §5.2).
func TestBadgeVariantTable(t *testing.T) {
	cases := []struct {
		name                string
		found               bool
		class               string
		saint, disabled     bool
		variant, msg, color string
		isError             bool
	}{
		{"hero", true, "hero", false, false, "supported", "supported", "brightgreen", false},
		{"hero saint", true, "hero", true, false, "full", "full", "brightgreen", false},
		{"partial", true, "partial", false, false, "partial", "partial", "yellow", false},
		{"partial saint", true, "partial", true, false, "partial", "partial", "yellow", false}, // saint ⊂ hero only
		{"sinner", true, "sinner", false, false, "no-ipv6", "no IPv6", "red", false},
		{"inactive", true, "inactive", false, false, "inactive", "inactive", "lightgrey", false},
		{"unknown", true, "unknown", false, false, "unknown", "unknown", "lightgrey", true},
		{"not found", false, "", false, false, "unknown", "unknown", "lightgrey", true},
		{"disabled", true, "hero", true, true, "unknown", "unknown", "lightgrey", true}, // disabled wins first
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pickBadgeVariant(tc.found, tc.class, tc.saint, tc.disabled)
			if got != tc.variant {
				t.Fatalf("pick(%v,%q,%v,%v) = %s, want %s", tc.found, tc.class, tc.saint, tc.disabled, got, tc.variant)
			}
			v := badgeVariants[got]
			if v.Message != tc.msg || v.Color != tc.color || v.IsError != tc.isError {
				t.Errorf("%s: %+v, want msg=%q color=%q isError=%v", got, v, tc.msg, tc.color, tc.isError)
			}
		})
	}
	// The three unknown inputs render byte-identically.
	a := RenderBadgeSVG(pickBadgeVariant(false, "", false, false))
	b := RenderBadgeSVG(pickBadgeVariant(true, "unknown", false, false))
	c := RenderBadgeSVG(pickBadgeVariant(true, "sinner", false, true))
	if !bytes.Equal(a, b) || !bytes.Equal(b, c) {
		t.Error("unknown variants must render byte-for-byte identically")
	}
	// Ladder branding never leaks into badge copy.
	for name, v := range badgeVariants {
		if strings.Contains(v.Message, "hero") || strings.Contains(v.Message, "sinner") {
			t.Errorf("%s message %q leaks ladder branding", name, v.Message)
		}
	}
}
