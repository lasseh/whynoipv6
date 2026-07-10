package geoip

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// CountryMap is the in-memory country.tld → id map plus the sentinel ids,
// loaded once per run by lookup — never literal ids (06-ingest.md §6.5, §6.7).
type CountryMap struct {
	byTLD           map[string]int32 // key: dot-prefixed uppercase, ".NO"
	byCode          map[string]int32 // key: ISO code, "NO"
	SentinelCountry int32
	SentinelASN     int32
}

// LoadCountryMap resolves the sentinels and the tld/code maps from the DB.
func LoadCountryMap(ctx context.Context, q *db.Queries) (*CountryMap, error) {
	sentASN, err := q.ASNSentinelID(ctx)
	if err != nil {
		return nil, fmt.Errorf("geoip: sentinel asn: %w", err)
	}
	sentCountry, err := q.CountrySentinelID(ctx)
	if err != nil {
		return nil, fmt.Errorf("geoip: sentinel country: %w", err)
	}
	rows, err := q.CountryTLDMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("geoip: country map: %w", err)
	}
	m := &CountryMap{
		byTLD:           make(map[string]int32, len(rows)),
		byCode:          make(map[string]int32, len(rows)),
		SentinelCountry: sentCountry,
		SentinelASN:     sentASN,
	}
	for _, r := range rows {
		if r.Tld != nil {
			m.byTLD[*r.Tld] = r.ID
		}
	}
	codes, err := q.CountryCodeMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("geoip: country codes: %w", err)
	}
	for _, r := range codes {
		m.byCode[strings.TrimSpace(r.Code)] = r.ID
	}
	return m, nil
}

// ccTLDID applies §6.4 step 1: the host's final DNS label, upper-cased and
// dot-prefixed, probed against country.tld. 0 = no match.
func (m *CountryMap) ccTLDID(host string) int32 {
	label := host[strings.LastIndexByte(host, '.')+1:]
	return m.byTLD["."+strings.ToUpper(label)]
}

// InsertCountryID is the mmdb-free insert-time rule (§6.5):
// ccTLD-or-sentinel. Insert-time ASN is always the sentinel.
func (m *CountryMap) InsertCountryID(host string) int32 {
	if id := m.ccTLDID(host); id != 0 {
		return id
	}
	return m.SentinelCountry
}

// Attributor computes scan-commit attribution (§6.2–§6.4). Meta is the mmdb
// seam (nil is invalid here — commit attribution always has readers).
type Attributor struct {
	Meta      IPMeta
	Countries *CountryMap
}

// ASNResult carries the §6.3 outcome: Number 0 ⇒ use the sentinel row;
// otherwise ensure-by-number with Org as the initial name.
type ASNResult struct {
	Number uint
	Org    string
}

// ASN looks up the input IP; a zero addr (no input IP, §6.2 step 3) or a
// lookup miss yields Number 0 (sentinel).
func (a *Attributor) ASN(ip netip.Addr) ASNResult {
	if !ip.IsValid() {
		return ASNResult{}
	}
	num, org := a.Meta.ASN(ip)
	return ASNResult{Number: num, Org: org}
}

// CountryID applies §6.4: ccTLD wins unconditionally (no GeoIP lookup);
// else GeoIP of the input IP; else sentinel.
func (a *Attributor) CountryID(host string, ip netip.Addr) int32 {
	if id := a.Countries.ccTLDID(host); id != 0 {
		return id
	}
	if ip.IsValid() {
		if code := a.Meta.CountryCode(ip); code != "" {
			if id, ok := a.Countries.byCode[code]; ok {
				return id
			}
		}
	}
	return a.Countries.SentinelCountry
}
