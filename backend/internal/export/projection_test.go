package export

import (
	"reflect"
	"slices"
	"testing"
)

// TestRowProjection: columns and csv() both derive from the Row struct, so
// header and cells cannot misalign; the golden pins the PUBLISHED dataset
// column order (SchemaVersion 1) — reordering or renaming a field is a
// schema change and must bump SchemaVersion, not slip through.
func TestRowProjection(t *testing.T) {
	golden := []string{
		"host", "rank", "kind", "parent", "classification", "class_flags", "saint",
		"base", "www", "ns", "mx", "conn", "resources",
		"base_since", "www_since", "ns_since", "mx_since", "conn_since", "resources_since",
		"tld", "country", "asn", "dns_provider", "hosting_provider", "last_checked",
	}
	if !slices.Equal(columns, golden) {
		t.Fatalf("published column order changed:\n got %v\nwant %v", columns, golden)
	}

	// A fully-populated row renders every cell — a Row field whose kind has
	// no renderer fails here instead of in a nightly export.
	s := func(v string) *string { return &v }
	rank := int64(42)
	full := Row{
		Host: "d1.example", Rank: &rank, Kind: "apex", Parent: s("p.example"),
		Classification: "hero", ClassFlags: "www_missing", Saint: true,
		Base: s("supported"), WWW: s("supported"), NS: s("supported"),
		MX: s("supported"), Conn: s("supported"), Resources: s("supported"),
		BaseSince: s("2026-01-01T00:00:00Z"), WWWSince: s("2026-01-01T00:00:00Z"),
		NSSince: s("2026-01-01T00:00:00Z"), MXSince: s("2026-01-01T00:00:00Z"),
		ConnSince: s("2026-01-01T00:00:00Z"), ResourcesSince: s("2026-01-01T00:00:00Z"),
		TLD: s("example"), Country: "NO", ASN: 2119,
		DNSProvider: s("Cloudflare"), HostingProvider: s("cloudflare"),
		LastChecked: s("2026-08-01T00:00:00Z"),
	}
	if v := reflect.ValueOf(full); v.NumField() != len(columns) {
		t.Fatalf("Row has %d fields, columns %d", v.NumField(), len(columns))
	}
	cells := full.csv()
	if len(cells) != len(columns) {
		t.Fatalf("csv() rendered %d cells for %d columns", len(cells), len(columns))
	}
	for i, c := range cells {
		if c == "" {
			t.Errorf("column %s rendered empty on a populated row", columns[i])
		}
	}

	// A zero row: every optional renders as the empty cell.
	for i, c := range (&Row{}).csv() {
		switch columns[i] {
		case "host", "kind", "classification", "class_flags", "country":
			// required strings render empty on a zero value — fine
		case "saint":
			if c != "false" {
				t.Errorf("zero saint = %q", c)
			}
		case "asn":
			if c != "0" {
				t.Errorf("zero asn = %q", c)
			}
		default:
			if c != "" {
				t.Errorf("zero-row column %s = %q, want empty", columns[i], c)
			}
		}
	}
}
