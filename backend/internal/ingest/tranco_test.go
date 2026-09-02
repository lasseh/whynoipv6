package ingest

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// trancoZip packs body as the inner top-1m.csv the parser looks for.
func trancoZip(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestParseTrancoZip pins the parse contract (06 §2.2 steps 4–5) that the
// staged rows carry into COPY: rank/host/tld per accepted line, CRLF
// tolerated, and every malformed shape counted as rejected rather than
// aborting the list.
func TestParseTrancoZip(t *testing.T) {
	body := "1,Example.COM\r\n" + // canonicalized, tld derived
		"2,sub.example.co.uk\r\n" + // multi-label ICANN suffix
		"3\r\n" + // no comma
		"x,example.net\r\n" + // non-numeric rank
		"0,example.org\r\n" + // rank out of range
		"6,not a host\r\n" + // uncanonicalizable
		"\r\n" + // blank, not counted at all
		"7,example.io\r\n" +
		"8," + strings.Repeat("a", maxTrancoLineBytes) + ".com\r\n" // over-long

	rows, lineCount, rejected, err := parseTrancoZip(trancoZip(t, "top-1m.csv", body))
	if err != nil {
		t.Fatalf("parseTrancoZip: %v", err)
	}
	want := []stagedRow{
		{rank: 1, host: "example.com", tld: "com"},
		{rank: 2, host: "sub.example.co.uk", tld: "co.uk"},
		{rank: 7, host: "example.io", tld: "io"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v, want %d", rows, len(want))
	}
	for i, w := range want {
		if rows[i] != w {
			t.Errorf("rows[%d] = %+v, want %+v", i, rows[i], w)
		}
	}
	if lineCount != 8 || rejected != 5 {
		t.Errorf("lineCount/rejected = %d/%d, want 8/5", lineCount, rejected)
	}

	if _, _, _, err := parseTrancoZip(trancoZip(t, "other.csv", "1,example.com\r\n")); err == nil {
		t.Error("a zip without top-1m.csv must be an error")
	}
}

// TestDelistBudget covers the staleness scaling and its ceiling (06 §2.2
// step 9): one day's allowance for a fresh import, one per missed day
// while ranks are frozen, never past the ceiling.
func TestDelistBudget(t *testing.T) {
	tests := []struct {
		name   string
		perDay float64
		days   float64
		want   float64
	}{
		{"same day retry floors at one day", 2.0, 0.2, 2.0},
		{"no prior import", 2.0, 1, 2.0},
		{"two days stale", 2.0, 2, 4.0},
		{"fraction scales smoothly", 2.0, 2.5, 5.0},
		{"production backlog admits 5.79%", 2.0, 7.4, 10.0},
		{"ceiling binds", 2.0, 30, 10.0},
		{"ceiling binds regardless of per-day", 25.0, 1, 10.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := delistBudget(tt.perDay, tt.days); got != tt.want {
				t.Errorf("delistBudget(%v, %v) = %v, want %v", tt.perDay, tt.days, got, tt.want)
			}
		})
	}
}
