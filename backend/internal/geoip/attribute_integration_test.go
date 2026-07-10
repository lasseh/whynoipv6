//go:build integration

package geoip

import (
	"context"
	"os"
	"testing"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

// TestLoadCountryMap proves sentinel ids are resolved by lookup (never
// literals) and the seed's dot-prefixed uppercase tld form maps (§6.5, §6.7).
func TestLoadCountryMap(t *testing.T) {
	pool := pgtest.NewDB(t)
	m, err := LoadCountryMap(context.Background(), db.New(pool))
	if err != nil {
		t.Fatalf("LoadCountryMap: %v", err)
	}
	if m.SentinelASN == 0 || m.SentinelCountry == 0 {
		t.Errorf("sentinels not resolved: %+v", m)
	}
	no := m.InsertCountryID("dnb.no")
	if no == 0 || no == m.SentinelCountry {
		t.Errorf("InsertCountryID(dnb.no) = %d, want the NO row", no)
	}
	if got := m.InsertCountryID("example.com"); got != m.SentinelCountry {
		t.Errorf("InsertCountryID(example.com) = %d, want sentinel %d", got, m.SentinelCountry)
	}
}
