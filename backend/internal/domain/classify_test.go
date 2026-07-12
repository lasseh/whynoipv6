package domain

import (
	"slices"
	"testing"
)

func conf(base, www, ns, conn, mx, res *IPv6Status) map[Dimension]*IPv6Status {
	return map[Dimension]*IPv6Status{
		DimBase: base, DimWWW: www, DimNS: ns, DimConn: conn, DimMX: mx, DimResources: res,
	}
}

// TestClassify reproduces the 03 §10 truth table (10-testing.md §6.1–§6.3)
// plus a totality sweep over the full cross-product.
func TestClassify(t *testing.T) {
	sup, uns, nr, na := new(StatusSupported), new(StatusUnsupported), new(StatusNoRecord), new(StatusNotApplicable)

	classRows := []struct {
		name                    string
		base, www, ns, conn, mx *IPv6Status
		want                    Classification
	}{
		{"unknown_base_null", nil, sup, sup, sup, sup, ClassUnknown},
		{"inactive_norecord", nr, sup, sup, sup, sup, ClassInactive},
		{"inactive_beats_everything", nr, sup, sup, sup, sup, ClassInactive},
		{"sinner_unsupported", uns, sup, sup, sup, sup, ClassSinner},
		{"hero_all_supported", sup, sup, sup, sup, sup, ClassHero},
		{"hero_www_na", sup, na, sup, sup, sup, ClassHero},
		{"hero_www_norecord", sup, nr, sup, sup, sup, ClassHero},
		{"hero_mx_na", sup, sup, sup, sup, na, ClassHero},
		{"partial_conn_null", sup, sup, sup, nil, sup, ClassPartial},
		{"partial_conn_na", sup, sup, sup, na, sup, ClassPartial},
		{"partial_ns_null", sup, sup, nil, sup, sup, ClassPartial},
		{"partial_www_unsupported", sup, uns, sup, sup, sup, ClassPartial},
		{"partial_conn_unsupported", sup, sup, sup, uns, sup, ClassPartial},
		{"partial_mx_unsupported", sup, sup, sup, sup, uns, ClassPartial},
		{"partial_www_null", sup, nil, sup, sup, sup, ClassPartial},
		{"partial_mx_null", sup, sup, sup, sup, nil, ClassPartial},
		{"partial_ns_na_blocks_hero", sup, sup, na, sup, sup, ClassPartial},
	}
	for _, tc := range classRows {
		t.Run("class/"+tc.name, func(t *testing.T) {
			got, _, _ := Classify(conf(tc.base, tc.www, tc.ns, tc.conn, tc.mx, nil))
			if got != tc.want {
				t.Errorf("Classify = %s, want %s", got, tc.want)
			}
		})
	}

	// conn=NULL partial carries NO flag (03 §10 note a).
	if _, flags, _ := Classify(conf(sup, sup, sup, nil, sup, nil)); len(flags) != 0 {
		t.Errorf("conn=NULL partial flags = %v, want none", flags)
	}

	flagRows := []struct {
		name                   string
		conn, www, ns, mx, res *IPv6Status
		want                   []string
	}{
		{"flag_none_all_supported", sup, sup, sup, sup, sup, nil},
		{"flag_broken_v6", uns, sup, sup, sup, sup, []string{"broken_v6"}},
		{"flag_www_missing", sup, uns, sup, sup, sup, []string{"www_missing"}},
		{"flag_ns_missing", sup, sup, uns, sup, sup, []string{"ns_missing"}},
		{"flag_mail_missing", sup, sup, sup, uns, sup, []string{"mail_missing"}},
		{"flag_resources_v4only", sup, sup, sup, sup, uns, []string{"resources_v4only"}},
		{"flag_null_no_flag", nil, nil, nil, nil, nil, nil},
		{"flag_na_no_flag", na, na, na, na, na, nil},
		{"flag_norecord_no_flag", sup, nr, sup, nr, nr, nil},
		{"flag_all_five", uns, uns, uns, uns, uns,
			[]string{"broken_v6", "www_missing", "ns_missing", "mail_missing", "resources_v4only"}},
	}
	for _, tc := range flagRows {
		t.Run("flags/"+tc.name, func(t *testing.T) {
			_, flags, _ := Classify(conf(sup, tc.www, tc.ns, tc.conn, tc.mx, tc.res))
			if !slices.Equal(flags, tc.want) {
				t.Errorf("flags = %v, want %v (fixed order)", flags, tc.want)
			}
		})
	}

	// Flags are computed regardless of classification: a sinner with
	// conn=unsupported carries broken_v6.
	if got, flags, _ := Classify(conf(uns, sup, sup, uns, sup, nil)); got != ClassSinner || !slices.Contains(flags, "broken_v6") {
		t.Errorf("sinner flags = %s/%v, want sinner with broken_v6", got, flags)
	}

	saintRows := []struct {
		name string
		res  *IPv6Status
		hero bool
		want bool
	}{
		{"saint_hero_res_supported", sup, true, true},
		{"saint_hero_res_na", na, true, true},
		{"saint_hero_res_unsupported", uns, true, false},
		{"saint_hero_res_null", nil, true, false},
		{"saint_partial_res_supported", sup, false, false},
	}
	for _, tc := range saintRows {
		t.Run("saint/"+tc.name, func(t *testing.T) {
			base := sup
			conn := sup
			if !tc.hero {
				conn = nil // hero bar not met → partial
			}
			class, _, saint := Classify(conf(base, sup, sup, conn, sup, tc.res))
			if saint != tc.want {
				t.Errorf("saint = %t (class %s), want %t", saint, class, tc.want)
			}
		})
	}
}

// TestClassifyTotality sweeps the full cross-product of the five gating
// dimensions × {4 values, NULL}: Classify must be total (always one of the
// five classes) and non-contradicting (unknown iff base NULL, etc.).
func TestClassifyTotality(t *testing.T) {
	vals := []*IPv6Status{nil,
		new(StatusSupported), new(StatusUnsupported), new(StatusNoRecord), new(StatusNotApplicable)}
	valid := map[Classification]bool{
		ClassUnknown: true, ClassInactive: true, ClassSinner: true,
		ClassPartial: true, ClassHero: true,
	}
	n := 0
	for _, base := range vals {
		for _, www := range vals {
			for _, ns := range vals {
				for _, conn := range vals {
					for _, mx := range vals {
						n++
						class, _, _ := Classify(conf(base, www, ns, conn, mx, nil))
						if !valid[class] {
							t.Fatalf("non-total: %v", class)
						}
						if (base == nil) != (class == ClassUnknown) {
							t.Fatalf("unknown iff base NULL violated: base=%v class=%s", base, class)
						}
						if base != nil && *base == StatusNoRecord && class != ClassInactive {
							t.Fatalf("base=no_record must be inactive, got %s", class)
						}
						if class == ClassHero && (conn == nil || *conn != StatusSupported) {
							t.Fatalf("hero without conn=supported")
						}
					}
				}
			}
		}
	}
	if n != 3125 {
		t.Fatalf("cross-product size = %d, want 5^5", n)
	}
}

// TestIPv6Only pins the full 5×5 truth table of the conn+resources fold
// (03 §10): strict on NULL resources, first-match on a broken conn, and
// nothing claimed on the impossible no_record inputs.
func TestIPv6Only(t *testing.T) {
	sup, uns, nr, na := new(StatusSupported), new(StatusUnsupported), new(StatusNoRecord), new(StatusNotApplicable)

	// want[conn][resources]; key "" = nil input, value nil = nil result.
	key := func(s *IPv6Status) IPv6Status {
		if s == nil {
			return ""
		}
		return *s
	}
	want := map[IPv6Status]map[IPv6Status]*IPv6Status{
		"": {"": nil, StatusSupported: nil, StatusUnsupported: nil, StatusNoRecord: nil, StatusNotApplicable: nil},
		StatusSupported: {
			"":                  nil, // reachable, resources unconfirmed → claim nothing (strict)
			StatusSupported:     sup,
			StatusUnsupported:   uns,
			StatusNoRecord:      nil, // impossible input
			StatusNotApplicable: sup, // vacuous pass: no required resources
		},
		StatusUnsupported: { // broken_v6 wins regardless of resources
			"": uns, StatusSupported: uns, StatusUnsupported: uns, StatusNoRecord: uns, StatusNotApplicable: uns,
		},
		StatusNoRecord: { // impossible input → claim nothing
			"": nil, StatusSupported: nil, StatusUnsupported: nil, StatusNoRecord: nil, StatusNotApplicable: nil,
		},
		StatusNotApplicable: { // no AAAA anywhere — nothing to assess
			"": na, StatusSupported: na, StatusUnsupported: na, StatusNoRecord: na, StatusNotApplicable: na,
		},
	}

	vals := []*IPv6Status{nil, sup, uns, nr, na}
	n := 0
	for _, conn := range vals {
		for _, res := range vals {
			n++
			got := IPv6Only(conn, res)
			expected := want[key(conn)][key(res)]
			if key(got) != key(expected) {
				t.Errorf("IPv6Only(%q, %q) = %q, want %q", key(conn), key(res), key(got), key(expected))
			}
			// Trust invariants: supported requires reachable + clean/vacuous
			// resources; unsupported requires a definitive negative.
			if got != nil && *got == StatusSupported &&
				(key(conn) != StatusSupported ||
					(key(res) != StatusSupported && key(res) != StatusNotApplicable)) {
				t.Errorf("supported claimed without evidence: conn=%q res=%q", key(conn), key(res))
			}
			if got != nil && *got == StatusUnsupported &&
				key(conn) != StatusUnsupported && key(res) != StatusUnsupported {
				t.Errorf("unsupported claimed without a definitive negative: conn=%q res=%q", key(conn), key(res))
			}
		}
	}
	if n != 25 {
		t.Fatalf("cross-product size = %d, want 5^2", n)
	}
}
