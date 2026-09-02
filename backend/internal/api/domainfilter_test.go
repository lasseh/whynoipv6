package api

import (
	"errors"
	"net/url"
	"testing"

	"github.com/lasseh/whynoipv6/internal/domain"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// The §3.3 grammar is pure, so every closed-set rejection is a table row
// with no *Server, no *http.Request and no database. Before the split, all
// fourteen validations were unreachable without a live pool.
func TestParseDomainFilterRejections(t *testing.T) {
	cases := []struct{ name, query, field, msg string }{
		{"class", "class=nonsense", "class", "must be one of hero, partial, sinner, inactive, unknown"},
		{"saint", "saint=false", "saint", "the only accepted value is true"},
		{"almost_hero", "almost_hero=1", "almost_hero", "the only accepted value is true"},
		{"asn_negative", "asn=-1", "asn", "must be a non-negative AS number"},
		{"asn_text", "asn=AS13335", "asn", "must be a non-negative AS number"},
		{"provider_zero", "provider=0", "provider", "must be a dns_provider id"},
		{"provider_text", "provider=cloudflare", "provider", "must be a dns_provider id"},
		{"flag", "flag=made_up", "flag", "unknown flag"},
		{"rank_min", "rank_min=-5", "rank_min", "must be a non-negative integer"},
		{"rank_max", "rank_max=abc", "rank_max", "must be a non-negative integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			_, err = parseDomainFilter(q)
			var ve validationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want a validationError", err)
			}
			if ve.field != tc.field || ve.msg != tc.msg {
				t.Errorf("got {%q, %q}, want {%q, %q}", ve.field, ve.msg, tc.field, tc.msg)
			}
		})
	}
}

// Every status dimension is validated against the ipv6_status set, and the
// loop's last-dimension-wins behaviour is deliberate.
func TestParseDomainFilterStatusDimensions(t *testing.T) {
	for _, dim := range statusDims {
		q := url.Values{dim: {"nonsense"}}
		_, err := parseDomainFilter(q)
		var ve validationError
		if !errors.As(err, &ve) || ve.field != dim {
			t.Errorf("%s: err = %v, want a validationError on %s", dim, err, dim)
		}
	}

	// Two dimensions at once: the last one in statusDims order wins.
	q := url.Values{statusDims[0]: {"supported"}, statusDims[1]: {"unsupported"}}
	spec, err := parseDomainFilter(q)
	if err != nil {
		t.Fatalf("two dimensions rejected: %v", err)
	}
	if spec.StatusDim != statusDims[1] || spec.StatusVal != "unsupported" {
		t.Errorf("dim/val = %q/%q, want %q/unsupported", spec.StatusDim, spec.StatusVal, statusDims[1])
	}
}

// The pivots are parsed, not resolved: the grammar reports what the query
// said and leaves the lookups to resolveDomainFilter.
func TestParseDomainFilterLeavesPivotsUnresolved(t *testing.T) {
	spec, err := parseDomainFilter(url.Values{"country": {"no"}, "asn": {"13335"}})
	if err != nil {
		t.Fatal(err)
	}
	if spec.CountryCode != "NO" {
		t.Errorf("country code = %q, want the upper-cased NO", spec.CountryCode)
	}
	if spec.ASNNumber == nil || *spec.ASNNumber != 13335 {
		t.Errorf("asn number = %v, want 13335", spec.ASNNumber)
	}
	// The resolved ids stay nil until resolveDomainFilter runs.
	if spec.CountryID != nil || spec.ASNID != nil {
		t.Error("the grammar resolved a pivot id")
	}
}

// Accepted values survive into the filter, and an empty query filters
// nothing — the shortcut isUnfiltered depends on.
func TestParseDomainFilterAccepts(t *testing.T) {
	spec, err := parseDomainFilter(url.Values{
		"class": {"hero"}, "saint": {"true"}, "almost_hero": {"true"},
		"tld": {"no"}, "hosting": {"hetzner"}, "q": {"example"},
		"rank_min": {"1"}, "rank_max": {"1000"}, "provider": {"7"},
	})
	if err != nil {
		t.Fatalf("valid filter rejected: %v", err)
	}
	if spec.Class != "hero" || !spec.Saint || !spec.AlmostHero {
		t.Errorf("spec = %+v", spec.DomainListFilter)
	}
	if spec.TLD != "no" || spec.Hosting != "hetzner" || spec.Query != "example" {
		t.Errorf("free-text fields = %+v", spec.DomainListFilter)
	}
	if spec.RankMin == nil || *spec.RankMin != 1 || spec.RankMax == nil || *spec.RankMax != 1000 {
		t.Errorf("rank window = %v..%v", spec.RankMin, spec.RankMax)
	}
	if spec.Provider == nil || *spec.Provider != 7 {
		t.Errorf("provider = %v", spec.Provider)
	}

	empty, err := parseDomainFilter(url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if !isUnfiltered(&empty.DomainListFilter) {
		t.Error("an empty query produced a filtered spec")
	}
}

// TestMaskObservationTotality is review issue 54's second item. Every arm of
// maskObservation's switch falls through to "render it", so the exhaustive
// linter cannot gate it and a new Observation value would silently become
// public. Same shape as observe's TestBridgeTotality: drive every declared
// value plus one that is not declared at all.
//
// The §4.3 rule: error and inconsistent are never public; partial is public
// only where the caller allows it (ptr, parity); everything else renders
// verbatim.
func TestMaskObservationTotality(t *testing.T) {
	never := map[domain.Observation]bool{
		domain.ObsError:        true,
		domain.ObsInconsistent: true,
	}
	inputs := append([]domain.Observation{"bogus"}, domain.ObservationValues...)

	for _, o := range inputs {
		obs := db.Observation(o)
		for _, allowPartial := range []bool{false, true} {
			got := maskObservation(&obs, allowPartial)

			if never[o] || (o == domain.ObsPartial && !allowPartial) {
				if got != nil {
					t.Errorf("maskObservation(%s, allowPartial=%t) = %q, want masked", o, allowPartial, *got)
				}
				continue
			}
			switch {
			case got == nil:
				t.Errorf("maskObservation(%s, allowPartial=%t) = nil, want it rendered", o, allowPartial)
			case *got != string(o):
				t.Errorf("maskObservation(%s, allowPartial=%t) = %q, want the value verbatim", o, allowPartial, *got)
			}
		}
	}

	if maskObservation(nil, true) != nil {
		t.Error("a NULL observation must render as null")
	}
}
