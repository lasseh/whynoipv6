package ingest

import "testing"

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
