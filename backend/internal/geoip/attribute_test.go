package geoip

import (
	"net/netip"
	"testing"
)

// fakeMeta scripts mmdb answers without mmdb fixtures (the Reader is a thin
// wrapper over the maxminddb reader; the algorithm is what §9.10 pins).
type fakeMeta struct {
	asn  uint
	org  string
	code string
}

func (f *fakeMeta) ASN(netip.Addr) (number uint, org string) { return f.asn, f.org }
func (f *fakeMeta) CountryCode(netip.Addr) string            { return f.code }

func testCountries() *CountryMap {
	return &CountryMap{
		byTLD:           map[string]int32{".NO": 42, ".UN": 99},
		byCode:          map[string]int32{"NO": 42, "DE": 55, "UN": 99},
		SentinelCountry: 99,
		SentinelASN:     7,
	}
}

func TestAttribution(t *testing.T) {
	ip := netip.MustParseAddr("2001:db8::1")
	var zero netip.Addr

	t.Run("cctld_beats_geoip", func(t *testing.T) {
		// GeoIP says DE, but the .no ccTLD wins unconditionally (§6.4).
		a := &Attributor{Meta: &fakeMeta{code: "DE"}, Countries: testCountries()}
		if got := a.CountryID("dnb.no", ip); got != 42 {
			t.Errorf("CountryID(dnb.no) = %d, want 42 (ccTLD wins)", got)
		}
	})

	t.Run("geoip_fallback", func(t *testing.T) {
		a := &Attributor{Meta: &fakeMeta{code: "DE"}, Countries: testCountries()}
		if got := a.CountryID("example.com", ip); got != 55 {
			t.Errorf("CountryID(example.com) = %d, want 55 (GeoIP DE)", got)
		}
	})

	t.Run("sentinel_when_no_input_ip", func(t *testing.T) {
		a := &Attributor{Meta: &fakeMeta{code: "DE"}, Countries: testCountries()}
		if got := a.CountryID("example.com", zero); got != 99 {
			t.Errorf("CountryID with no input IP = %d, want sentinel 99", got)
		}
	})

	t.Run("sentinel_on_unmapped_code", func(t *testing.T) {
		a := &Attributor{Meta: &fakeMeta{code: "XX"}, Countries: testCountries()}
		if got := a.CountryID("example.com", ip); got != 99 {
			t.Errorf("CountryID with unmapped code = %d, want sentinel 99", got)
		}
	})

	t.Run("asn_lookup", func(t *testing.T) {
		a := &Attributor{Meta: &fakeMeta{asn: 2119, org: "Telenor"}, Countries: testCountries()}
		if got := a.ASN(ip); got.Number != 2119 || got.Org != "Telenor" {
			t.Errorf("ASN = %+v, want 2119/Telenor", got)
		}
		if got := a.ASN(zero); got.Number != 0 {
			t.Errorf("ASN with no input IP = %+v, want sentinel signal 0", got)
		}
	})

	t.Run("insert_time_rule", func(t *testing.T) {
		// §6.5: no mmdb, no input IP — ccTLD-or-sentinel; ASN = sentinel.
		m := testCountries()
		if got := m.InsertCountryID("dnb.no"); got != 42 {
			t.Errorf("InsertCountryID(dnb.no) = %d, want 42", got)
		}
		if got := m.InsertCountryID("example.com"); got != 99 {
			t.Errorf("InsertCountryID(example.com) = %d, want sentinel 99", got)
		}
	})
}
