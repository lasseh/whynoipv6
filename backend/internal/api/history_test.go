package api

import (
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
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

// TestDimTrackValueAt (07 §4.9): the changelog replay wins wherever a row
// exists; a confirmed flip that wrote no row — a shadow transition (03 §11)
// or the bootstrap after a Step R reset (03 §6) — is taken from the
// confirmed (value, since) seed from its day on, so the trajectory and the
// status block agree.
func TestDimTrackValueAt(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 8, d, 0, 0, 0, 0, time.UTC) }
	at := func(d int) time.Time { return day(d).Add(3 * time.Hour) }
	row := func(d int, from, to db.Ipv6Status) db.ChangelogReplayRow {
		return db.ChangelogReplayRow{Ts: pgtype.Timestamptz{Time: at(d), Valid: true}, Field: "conn", OldValue: from, NewValue: to}
	}
	sup, unsup, na := db.Ipv6StatusSupported, db.Ipv6StatusUnsupported, db.Ipv6StatusNotApplicable
	str := func(s db.Ipv6Status) *string { v := string(s); return &v }

	cases := []struct {
		name  string
		track dimTrack
		want  map[int]*string // day → confirmed value at end of that day
	}{
		{
			"seed only holds from its day",
			dimTrack{current: &sup, since: at(5), hasSince: true},
			map[int]*string{4: nil, 5: str(sup), 9: str(sup)},
		},
		{
			"row replays: old value before, new value from its day",
			dimTrack{events: []db.ChangelogReplayRow{row(5, sup, unsup)}, current: &unsup, since: at(5), hasSince: true},
			map[int]*string{4: str(sup), 5: str(unsup), 9: str(unsup)},
		},
		{
			"row-less shadow flip after the last row wins from its day",
			dimTrack{events: []db.ChangelogReplayRow{row(5, sup, unsup)}, current: &na, since: at(8), hasSince: true},
			map[int]*string{4: str(sup), 6: str(unsup), 8: str(na), 12: str(na)},
		},
		{
			"step R bootstrap newer than the pre-death rows",
			dimTrack{events: []db.ChangelogReplayRow{row(2, sup, unsup)}, current: &sup, since: at(10), hasSince: true},
			map[int]*string{3: str(unsup), 10: str(sup), 20: str(sup)},
		},
		{
			"nil current after a reset keeps the replay",
			dimTrack{events: []db.ChangelogReplayRow{row(2, sup, unsup)}},
			map[int]*string{1: str(sup), 3: str(unsup)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for d, want := range tc.want {
				got := tc.track.valueAt(day(d))
				switch {
				case want == nil && got != nil:
					t.Errorf("day %d = %q, want null", d, *got)
				case want != nil && got == nil:
					t.Errorf("day %d = null, want %q", d, *want)
				case want != nil && *got != *want:
					t.Errorf("day %d = %q, want %q", d, *got, *want)
				}
			}
		})
	}
}
