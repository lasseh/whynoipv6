package main

import (
	"testing"
	"time"
)

func TestParseCutoff(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "the past, UTC", in: "2026-10-29T03:30:00Z"},
		{name: "the past, with an offset", in: "2026-10-29T03:30:00+02:00"},
		// A future cutoff matches every discovered link in the table.
		{name: "the future", in: "2026-10-31T00:00:00Z", wantErr: true},
		{name: "exactly now", in: "2026-10-30T12:00:00Z", wantErr: true},
		// A bare time would silently mean UTC on a server and something else
		// in the operator's head.
		{name: "no time zone", in: "2026-10-29T03:30:00", wantErr: true},
		{name: "a date alone", in: "2026-10-29", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCutoff(tt.in, now)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCutoff(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
		})
	}
}
