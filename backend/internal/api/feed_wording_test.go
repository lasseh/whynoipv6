package api

import "testing"

// TestFeedItemTitleGoldens pins the §7.4 wording table. The frontend's
// utils/changelog.ts renders the identical sentences (its goldens live in
// frontend/src/utils/__tests__/changelog.test.ts) — change them together
// or the two trust surfaces drift.
func TestFeedItemTitleGoldens(t *testing.T) {
	goldens := []struct {
		name                    string
		field, oldV, newV, want string
	}{
		{"base gained", "base", "unsupported", "supported", "example.com now supports IPv6 on the base domain"},
		{"www added with IPv6", "www", "not_applicable", "supported", "example.com now supports IPv6 on www"},
		{"www added without IPv6", "www", "not_applicable", "unsupported", "example.com started using www — without IPv6"},
		{"ns lost", "ns", "supported", "unsupported", "example.com lost IPv6 on nameservers"},
		{"mx added without records", "mx", "not_applicable", "no_record", "example.com started publishing mail — without IPv6 records"},
		{"mx records withdrawn", "mx", "supported", "no_record", "example.com no longer publishes records for mail"},
		{"www removed", "www", "supported", "not_applicable", "example.com no longer uses www"},
		{"conn reachable", "conn", "unsupported", "supported", "example.com is now reachable over IPv6"},
		{"conn unreachable", "conn", "supported", "unsupported", "example.com is no longer reachable over IPv6"},
		{"conn addresses published but failing", "conn", "not_applicable", "unsupported", "example.com published IPv6 addresses — but connections fail"},
		{"conn addresses withdrawn", "conn", "unsupported", "not_applicable", "example.com has no IPv6 addresses left to test"},
		{"resources all over IPv6", "resources", "unsupported", "supported", "example.com now passes the page-resource IPv6 grade"},
		{"resources partially v4-only", "resources", "supported", "unsupported", "example.com uses some page-resource hosts without IPv6"},
		// Review issue 02: this row stopped being defensive-only, and the
		// two arms say different things. The roll-up reaches
		// not_applicable either because conn left supported (suppressed as
		// a shadow, never rendered) or because no third-party host is left
		// to grade — so the surviving rows are all "nothing left to
		// check", never "checking stopped". From unsupported that also
		// clears resources_v4only; from supported nothing else moves.
		{"resources dependency dropped", "resources", "unsupported", "not_applicable", "example.com no longer depends on IPv4-only page resources"},
		{"resources hosts all gone", "resources", "supported", "not_applicable", "example.com no longer loads third-party page resources"},
		// Transitions out of not_applicable are real rows (03 §11 suppresses
		// only the way in); the `supported` arm is origin-agnostic for every
		// field, and these pin that for the five fields the table lacked.
		{"base from not_applicable", "base", "not_applicable", "supported", "example.com now supports IPv6 on the base domain"},
		{"ns from not_applicable", "ns", "not_applicable", "supported", "example.com now supports IPv6 on nameservers"},
		{"mx from not_applicable", "mx", "not_applicable", "supported", "example.com now supports IPv6 on mail"},
		{"conn from not_applicable", "conn", "not_applicable", "supported", "example.com is now reachable over IPv6"},
		{"resources from not_applicable", "resources", "not_applicable", "supported", "example.com now passes the page-resource IPv6 grade"},
	}
	for _, g := range goldens {
		t.Run(g.name, func(t *testing.T) {
			it := &ChangelogItem{Host: "example.com", Field: g.field, OldValue: g.oldV, NewValue: g.newV}
			if got := feedItemTitle(it); got != g.want {
				t.Errorf("feedItemTitle(%s %s→%s) = %q, want %q", g.field, g.oldV, g.newV, got, g.want)
			}
		})
	}
}
