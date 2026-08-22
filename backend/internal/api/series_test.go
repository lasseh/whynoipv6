package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// dayRow is the median series row: a day column and one counter.
type dayRow struct {
	Day   time.Time
	Count int32
}

// dayPoint is its wire point.
type dayPoint struct {
	Day   string `json:"day"`
	Count int32  `json:"count"`
}

func daySpec(fetch func(ctx context.Context, from, to time.Time) ([]dayRow, error)) SeriesSpec[dayRow, dayPoint] {
	return SeriesSpec[dayRow, dayPoint]{
		Fetch: fetch,
		Day:   func(r *dayRow) time.Time { return r.Day },
		Point: func(r *dayRow) dayPoint {
			return dayPoint{Day: r.Day.Format("2006-01-02"), Count: r.Count}
		},
	}
}

func staticRows(rows []dayRow) func(context.Context, time.Time, time.Time) ([]dayRow, error) {
	return func(context.Context, time.Time, time.Time) ([]dayRow, error) { return rows, nil }
}

// rowsFrom builds n consecutive daily rows starting at start, counting up.
func rowsFrom(start time.Time, n int) []dayRow {
	rows := make([]dayRow, n)
	for i := range rows {
		rows[i] = dayRow{Day: start.AddDate(0, 0, i), Count: int32(i)}
	}
	return rows
}

func decodePoints(t *testing.T, rec *httptest.ResponseRecorder) []dayPoint {
	t.Helper()
	var env struct {
		Points []dayPoint `json:"points"`
		Meta   Meta       `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body is not a points envelope: %v\n%s", err, rec.Body.String())
	}
	return env.Points
}

// The 304 gate runs before the window fetch — the §6.1 invariant the list
// rim already pins, now executable for series too.
func TestServeSeriesETagBeforeFetch(t *testing.T) {
	s := listServer(&fakeMeta{g: 7, asOf: time.Now()})
	fetched := false
	spec := daySpec(func(context.Context, time.Time, time.Time) ([]dayRow, error) {
		fetched = true
		return rowsFrom(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), 3), nil
	})

	rec := httptest.NewRecorder()
	ServeSeries(s, rec, httptest.NewRequest(http.MethodGet, "/stats/x", nil), spec)
	if rec.Code != http.StatusOK || !fetched {
		t.Fatalf("first request: code=%d fetched=%v", rec.Code, fetched)
	}
	etag := rec.Header().Get("ETag")
	if !strings.HasPrefix(etag, `W/"g7-`) {
		t.Fatalf("generation-seeded ETag = %q", etag)
	}

	fetched = false
	req := httptest.NewRequest(http.MethodGet, "/stats/x", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	ServeSeries(s, rec, req, spec)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("conditional request: code=%d", rec.Code)
	}
	if fetched {
		t.Fatal("window fetch ran despite the 304")
	}
}

// Live selects the changelog cache class — /stats/changes alone, because a
// generation-seeded ETag would 304-freeze it until the next daily tick.
func TestServeSeriesLiveCacheClass(t *testing.T) {
	maxTS := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	meta := &fakeMeta{g: 7, asOf: time.Now(), maxTS: maxTS}
	s := listServer(meta)

	spec := daySpec(staticRows(rowsFrom(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), 2)))
	spec.Live = true

	rec := httptest.NewRecorder()
	ServeSeries(s, rec, httptest.NewRequest(http.MethodGet, "/stats/changes", nil), spec)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if meta.maxTSCalls != 1 {
		t.Fatalf("ChangelogMaxTS called %d times, want exactly 1", meta.maxTSCalls)
	}
	if etag := rec.Header().Get("ETag"); strings.HasPrefix(etag, `W/"g7-`) {
		t.Fatalf("live class must not seed the ETag from the generation: %q", etag)
	}

	// The plain class never consults the changelog high-water mark.
	meta.maxTSCalls = 0
	plain := daySpec(staticRows(nil))
	rec = httptest.NewRecorder()
	ServeSeries(s, rec, httptest.NewRequest(http.MethodGet, "/stats/x", nil), plain)
	if meta.maxTSCalls != 0 {
		t.Fatalf("plain class consulted ChangelogMaxTS %d times", meta.maxTSCalls)
	}
}

// Every series is labelled confirmed_state — the §4.10 rule that keeps
// telemetry from being served under the same banner.
func TestServeSeriesMetaSource(t *testing.T) {
	asOf := time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)
	s := listServer(&fakeMeta{g: 11, asOf: asOf})

	rec := httptest.NewRecorder()
	ServeSeries(s, rec, httptest.NewRequest(http.MethodGet, "/stats/x", nil),
		daySpec(staticRows(rowsFrom(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), 1))))

	var env struct {
		Meta Meta `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Meta.Source != sourceConfirmedState {
		t.Fatalf("meta.source = %q, want %q", env.Meta.Source, sourceConfirmedState)
	}
	if env.Meta.Generation != 11 {
		t.Fatalf("meta.generation = %d, want 11", env.Meta.Generation)
	}
}

// The window contract is the shared §4.10 parser: bad input is a 400 and
// never reaches the query.
func TestServeSeriesWindowRejects(t *testing.T) {
	cases := []struct {
		name, query, detail string
	}{
		{"interval", "?interval=hourly", "interval must be daily or weekly"},
		{"from", "?from=nonsense", "from must be YYYY-MM-DD"},
		{"to", "?to=13-13-13", "to must be YYYY-MM-DD"},
		{"order", "?from=2026-08-10&to=2026-08-01", "from must not be after to"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := listServer(&fakeMeta{g: 1, asOf: time.Now()})
			fetched := false
			spec := daySpec(func(context.Context, time.Time, time.Time) ([]dayRow, error) {
				fetched = true
				return nil, nil
			})
			rec := httptest.NewRecorder()
			ServeSeries(s, rec, httptest.NewRequest(http.MethodGet, "/stats/x"+tc.query, nil), spec)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400", rec.Code)
			}
			if got := problemDetail(t, rec); got != tc.detail {
				t.Fatalf("detail = %q, want %q", got, tc.detail)
			}
			if fetched {
				t.Fatal("query ran on an invalid window")
			}
		})
	}
}

// Weekly sampling keeps the latest row per ISO week (a sample, never an
// average) and the surviving point is the one belonging to that same row —
// the lockstep ServeSeries now derives structurally.
func TestServeSeriesWeeklySampleStaysAligned(t *testing.T) {
	// Mon 2026-08-03 .. Sun 2026-08-16: two full ISO weeks, 14 rows.
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	s := listServer(&fakeMeta{g: 1, asOf: time.Now()})
	spec := daySpec(staticRows(rowsFrom(start, 14)))

	rec := httptest.NewRecorder()
	ServeSeries(s, rec, httptest.NewRequest(http.MethodGet, "/stats/x?interval=weekly", nil), spec)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	points := decodePoints(t, rec)
	if len(points) != 2 {
		t.Fatalf("weekly sample kept %d points, want 2", len(points))
	}
	// Sunday of each week is the latest day in it, carrying counts 6 and 13.
	want := []dayPoint{{Day: "2026-08-09", Count: 6}, {Day: "2026-08-16", Count: 13}}
	for i, w := range want {
		if points[i] != w {
			t.Fatalf("point %d = %+v, want %+v — day and counter came from different rows", i, points[i], w)
		}
	}

	// Daily keeps every row untouched.
	rec = httptest.NewRecorder()
	ServeSeries(s, rec, httptest.NewRequest(http.MethodGet, "/stats/x", nil), spec)
	if got := len(decodePoints(t, rec)); got != 14 {
		t.Fatalf("daily interval kept %d points, want 14", got)
	}
}

// An empty window serves an empty array, never a null — the envelope shape
// clients index into unconditionally.
func TestServeSeriesEmptyWindow(t *testing.T) {
	s := listServer(&fakeMeta{g: 1, asOf: time.Now()})
	rec := httptest.NewRecorder()
	ServeSeries(s, rec, httptest.NewRequest(http.MethodGet, "/stats/x", nil), daySpec(staticRows(nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"points":[]`) {
		t.Fatalf("empty window body = %s", body)
	}
}

// Window narrows the parsed range before the fetch — the /stats/changes
// floor, now assertable at the rim rather than inside the handler.
func TestServeSeriesWindowHook(t *testing.T) {
	s := listServer(&fakeMeta{g: 1, asOf: time.Now()})
	floor := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	var gotFrom time.Time
	spec := daySpec(func(_ context.Context, from, _ time.Time) ([]dayRow, error) {
		gotFrom = from
		return nil, nil
	})
	spec.Window = func(_, to time.Time) (time.Time, time.Time) { return floor, to }

	rec := httptest.NewRecorder()
	ServeSeries(s, rec, httptest.NewRequest(http.MethodGet, "/stats/changes?from=2020-01-01", nil), spec)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if !gotFrom.Equal(floor) {
		t.Fatalf("fetch saw from = %s, want the floored %s", gotFrom, floor)
	}
}

// A failing query is a 500 and the driver's text never reaches the client.
func TestServeSeriesFetchError(t *testing.T) {
	s := listServer(&fakeMeta{g: 1, asOf: time.Now()})
	spec := daySpec(func(context.Context, time.Time, time.Time) ([]dayRow, error) {
		return nil, errors.New("relation stats_global_daily does not exist")
	})
	rec := httptest.NewRecorder()
	ServeSeries(s, rec, httptest.NewRequest(http.MethodGet, "/stats/x", nil), spec)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "stats_global_daily") {
		t.Fatalf("driver text leaked: %s", rec.Body.String())
	}
}

// A generation lookup failure is a 500 before any query runs.
func TestServeSeriesGenerationError(t *testing.T) {
	s := listServer(&fakeMeta{genErr: errors.New("pool exhausted")})
	fetched := false
	spec := daySpec(func(context.Context, time.Time, time.Time) ([]dayRow, error) {
		fetched = true
		return nil, nil
	})
	rec := httptest.NewRecorder()
	ServeSeries(s, rec, httptest.NewRequest(http.MethodGet, "/stats/x", nil), spec)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
	if fetched {
		t.Fatal("query ran despite the generation lookup failing")
	}
}
