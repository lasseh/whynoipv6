package api

import (
	"bytes"
	"log/slog"
	"math/big"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestPercentOf is review issue 61: both country handlers discarded the
// Numeric conversion error, so a column holding something the schema forbids
// served 0.0 — which on a leaderboard reads as "no IPv6" — with nothing in
// the log to say the number was never computed.
func TestPercentOf(t *testing.T) {
	capture := func(fn func()) string {
		t.Helper()
		var buf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
		defer slog.SetDefault(prev)
		fn()
		return buf.String()
	}

	ordinary := pgtype.Numeric{Int: big.NewInt(4173), Exp: -2, Valid: true}
	if got := percentOf("NO", ordinary); got != 41.73 {
		t.Errorf("percentOf(41.73) = %v", got)
	}
	if logged := capture(func() { percentOf("NO", ordinary) }); logged != "" {
		t.Errorf("an ordinary value logged: %s", logged)
	}

	// The two shapes the column is not supposed to be able to hold.
	for name, n := range map[string]pgtype.Numeric{
		"nan":  {NaN: true, Valid: true},
		"null": {Valid: false},
	} {
		t.Run(name, func(t *testing.T) {
			var got float64
			logged := capture(func() { got = percentOf("SE ", n) })
			if got != 0 {
				t.Errorf("percentOf(%s) = %v, want the 0 fallback", name, got)
			}
			if !strings.Contains(logged, "country percent is not a usable number") ||
				!strings.Contains(logged, "country=SE") {
				t.Errorf("percentOf(%s) logged %q, want a warn naming the country", name, logged)
			}
		})
	}
}
