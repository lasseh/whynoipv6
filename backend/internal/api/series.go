package api

import (
	"context"
	"net/http"
	"time"
)

// This file is the series rim — the shared pipeline behind every §4.10
// confirmed-state series, and the {points} counterpart to list.go's
// {items} rim. ServeSeries owns what the handlers used to hand-write
// around their one range query: window parsing, the optional window
// floor, generation/maxTS acquisition through the metaSource seam, the
// ETag/304 gate (always before the fetch), the row→point map, weekly
// sampling, and the {points,meta} envelope.
//
// The lockstep invariant sampleWeekly documents — points and days must be
// the same length, or the weekly sample indexes out of range — is not
// delegated to adopters here. ServeSeries derives both slices from the
// same row index, so no adopter can desynchronize them.

// SeriesSpec declares one confirmed-state series endpoint to ServeSeries.
// The zero value of every optional field means "this endpoint doesn't have
// that": no window floor, generation-class caching.
type SeriesSpec[Row, Point any] struct {
	// Live selects the live-surface cache class (07 §6.1): the ETag seeds
	// from max(changelog.ts) via CacheChangelog instead of CacheList.
	// /stats/changes alone needs it — the crawler commits transitions
	// continuously, and a generation-seeded ETag would 304-freeze the
	// endpoint until the next daily stats tick.
	Live bool

	// Window, when set, narrows the parsed [from,to] before the fetch —
	// the server-side floor /stats/changes shares with the per-domain
	// history. Nil leaves the parsed window untouched.
	Window func(from, to time.Time) (from2, to2 time.Time)

	// Fetch runs the one range query over the resolved window.
	Fetch func(ctx context.Context, from, to time.Time) ([]Row, error)

	// Day extracts the row's day for weekly sampling. It reads the raw
	// column, never the formatted wire string: pgtype.Date rows carry no
	// zone, Timestamptz rows do, and Point is where that difference is
	// spelled out.
	Day func(row *Row) time.Time

	// Point maps one row to its wire point.
	Point func(row *Row) Point
}

// ServeSeries runs one series endpoint through the shared rim: window →
// generation (+ maxTS when Live) → cache/304 → fetch → point map → weekly
// sample → {points,meta}. It always completes the response; handlers do
// path-scope resolution and endpoint-specific validation BEFORE the call
// and write nothing after. Error modes: window parse failures → 400
// invalid-parameter, everything else → 500 internal-error (logged, text
// never leaks). A free function — Go methods cannot carry type parameters
// (precedent: ServeList, KeysetPage).
func ServeSeries[Row, Point any](s *Server, w http.ResponseWriter, r *http.Request, spec SeriesSpec[Row, Point]) {
	from, to, weekly, err := statsWindow(r)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	if spec.Window != nil {
		from, to = spec.Window(from, to)
	}

	generation, asOf, ok := s.enterCache(w, r, spec.Live)
	if !ok {
		return
	}

	rows, err := spec.Fetch(r.Context(), from, to)
	if err != nil {
		InternalError(w, r, err)
		return
	}

	// Both slices are derived from the same index in one pass — the
	// lockstep sampleWeekly requires is structural here, not remembered.
	points := make([]Point, len(rows))
	days := make([]time.Time, len(rows))
	for i := range rows {
		days[i] = spec.Day(&rows[i])
		points[i] = spec.Point(&rows[i])
	}

	meta := NewMeta(asOf, generation)
	meta.Source = sourceConfirmedState
	WriteJSON(w, http.StatusOK, PointsEnvelope{Points: sampleWeekly(points, days, weekly), Meta: meta})
}
