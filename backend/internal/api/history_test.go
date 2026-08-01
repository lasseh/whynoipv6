package api

import (
	"net/url"
	"testing"
	"time"
)

// TestParseHistoryWindowBounds: the window end is bounded — a future `to`
// clamps to today UTC, so ?to=9999-12-31 cannot drive history's per-day
// synthesis loop.
func TestParseHistoryWindowBounds(t *testing.T) {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	day := func(t time.Time) string { return t.Format("2006-01-02") }

	tests := []struct {
		name     string
		query    string
		wantFrom time.Time
		wantTo   time.Time
		wantErr  bool
	}{
		{"defaults", "", today.AddDate(0, 0, -90), today, false},
		{"future to clamps to today", "to=9999-12-31", today.AddDate(0, 0, -90), today, false},
		{"explicit window inside bounds", "from=" + day(today.AddDate(0, 0, -10)) + "&to=" + day(today.AddDate(0, 0, -5)), today.AddDate(0, 0, -10), today.AddDate(0, 0, -5), false},
		{"entirely future window is rejected", "from=" + day(today.AddDate(0, 0, 1)) + "&to=9999-12-31", time.Time{}, time.Time{}, true},
		{"from after to", "from=2026-02-01&to=2026-01-01", time.Time{}, time.Time{}, true},
		{"malformed to", "to=9999-99-99", time.Time{}, time.Time{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			from, to, _, err := parseHistoryWindow(q)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got window %s..%s", day(from), day(to))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !from.Equal(tc.wantFrom) || !to.Equal(tc.wantTo) {
				t.Errorf("window = %s..%s, want %s..%s", day(from), day(to), day(tc.wantFrom), day(tc.wantTo))
			}
		})
	}
}

// TestCapHistoryWindow: history's synthesized window never spans more than
// the changelog retention, whatever `from` the client asks for.
func TestCapHistoryWindow(t *testing.T) {
	to := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		from     time.Time
		wantFrom time.Time
	}{
		{"ancient from is capped", time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), to.AddDate(0, 0, -changelogRetentionDays)},
		{"from at the cap survives", to.AddDate(0, 0, -changelogRetentionDays), to.AddDate(0, 0, -changelogRetentionDays)},
		{"from inside the cap survives", to.AddDate(0, 0, -90), to.AddDate(0, 0, -90)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := capHistoryWindow(tc.from, to); !got.Equal(tc.wantFrom) {
				t.Errorf("capHistoryWindow = %s, want %s", got.Format("2006-01-02"), tc.wantFrom.Format("2006-01-02"))
			}
		})
	}
}
