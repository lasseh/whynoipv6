package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeMeta is the test adapter at the metaSource seam.
type fakeMeta struct {
	g          int32
	asOf       time.Time
	maxTS      time.Time
	genErr     error
	maxTSErr   error
	maxTSCalls int
}

func (f *fakeMeta) Generation(context.Context) (int32, time.Time, error) {
	return f.g, f.asOf, f.genErr
}

func (f *fakeMeta) ChangelogMaxTS(context.Context) (time.Time, error) {
	f.maxTSCalls++
	return f.maxTS, f.maxTSErr
}

func listServer(meta metaSource) *Server {
	return &Server{opts: Options{CSVMaxRows: 5}, meta: meta}
}

// hostRow pages on the host ordering — the simplest real seek tuple.
type hostRow struct{ Host string }

func hostRows(n int) []hostRow {
	rows := make([]hostRow, n)
	for i := range rows {
		rows[i] = hostRow{Host: fmt.Sprintf("host-%03d.example", i)}
	}
	return rows
}

// hostSpec is the median ListSpec: fixed host ordering, no CSV, no count.
func hostSpec(fetch func(ctx context.Context, sortKey string, seek *Seek, limit int, backward bool) ([]hostRow, error)) ListSpec[hostRow, string] {
	return ListSpec[hostRow, string]{
		Sort:  SortHost,
		Fetch: fetch,
		Key:   func(_ string, r *hostRow) []any { return []any{r.Host} },
		Item:  func(r *hostRow) string { return r.Host },
	}
}

func staticFetch(rows []hostRow) func(context.Context, string, *Seek, int, bool) ([]hostRow, error) {
	return func(_ context.Context, _ string, _ *Seek, limit int, _ bool) ([]hostRow, error) {
		if len(rows) > limit+1 {
			return rows[:limit+1], nil
		}
		return rows, nil
	}
}

func decodeEnvelope(t *testing.T, body string) map[string]json.RawMessage {
	t.Helper()
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("body is not a JSON object: %v\n%s", err, body)
	}
	return env
}

func problemDetail(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body is not a problem: %v\n%s", err, rec.Body.String())
	}
	return p.Detail
}

// The 304 gate runs before any window fetch — the §6.1 invariant, now
// executable: a matching If-None-Match must answer without calling Fetch.
func TestServeListETagBeforeFetch(t *testing.T) {
	s := listServer(&fakeMeta{g: 7, asOf: time.Now()})
	fetched := false
	spec := hostSpec(func(ctx context.Context, sortKey string, seek *Seek, limit int, backward bool) ([]hostRow, error) {
		fetched = true
		return hostRows(3), nil
	})

	rec := httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/hosts", nil), spec)
	if rec.Code != http.StatusOK || !fetched {
		t.Fatalf("first request: code=%d fetched=%v", rec.Code, fetched)
	}
	etag := rec.Header().Get("ETag")
	if !strings.HasPrefix(etag, `W/"g7-`) {
		t.Fatalf("generation-seeded ETag = %q", etag)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "s-maxage=3600") {
		t.Fatalf("list-class Cache-Control = %q", cc)
	}

	fetched = false
	req := httptest.NewRequest(http.MethodGet, "/hosts", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	ServeList(s, rec, req, spec)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("conditional request: code=%d", rec.Code)
	}
	if fetched {
		t.Fatal("window fetch ran despite the 304")
	}
}

// Live selects the changelog cache class: maxTS is fetched (exactly once,
// before the gate) and seeds the ETag; the plain class never consults it.
func TestServeListLiveCacheClass(t *testing.T) {
	maxTS := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	meta := &fakeMeta{g: 7, asOf: time.Now(), maxTS: maxTS}
	s := listServer(meta)

	spec := hostSpec(staticFetch(hostRows(3)))
	spec.Live = true
	rec := httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/hosts", nil), spec)
	if etag := rec.Header().Get("ETag"); !strings.HasPrefix(etag, fmt.Sprintf(`W/"cl%d-`, maxTS.UnixNano())) {
		t.Fatalf("live ETag = %q", etag)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=300" {
		t.Fatalf("live Cache-Control = %q", cc)
	}
	if meta.maxTSCalls != 1 {
		t.Fatalf("maxTS calls = %d", meta.maxTSCalls)
	}

	spec.Live = false
	ServeList(s, httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/hosts", nil), spec)
	if meta.maxTSCalls != 1 {
		t.Fatalf("plain class consulted maxTS (calls = %d)", meta.maxTSCalls)
	}
}

// Sorts[0] is the default; out-of-set values get the canonical message in
// both the two-member and three-member spellings.
func TestServeListSortResolution(t *testing.T) {
	s := listServer(&fakeMeta{g: 1})
	var gotSort string
	spec := ListSpec[hostRow, string]{
		Sorts: []string{SortCountV6, SortCountTotal},
		Fetch: func(_ context.Context, sortKey string, _ *Seek, limit int, _ bool) ([]hostRow, error) {
			gotSort = sortKey
			return nil, nil
		},
		Key:  func(_ string, r *hostRow) []any { return []any{r.Host} },
		Item: func(r *hostRow) string { return r.Host },
	}

	ServeList(s, httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil), spec)
	if gotSort != SortCountV6 {
		t.Fatalf("default sort = %q", gotSort)
	}
	ServeList(s, httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x?sort=count_total", nil), spec)
	if gotSort != SortCountTotal {
		t.Fatalf("explicit sort = %q", gotSort)
	}

	rec := httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/x?sort=bogus", nil), spec)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid sort: code=%d", rec.Code)
	}
	if d := problemDetail(t, rec); d != "sort must be count_v6 or count_total" {
		t.Fatalf("two-member detail = %q", d)
	}

	spec.Sorts = []string{"percent", "v6_sites", "sites"}
	rec = httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/x?sort=bogus", nil), spec)
	if d := problemDetail(t, rec); d != "sort must be percent, v6_sites, or sites" {
		t.Fatalf("three-member detail = %q", d)
	}
}

// ?format= is negotiated only when the spec carries a CSV writer: without
// one the param stays unread (the pinned wire behavior); with one, csv
// routes to the writer under the raised cap and bad values are 400.
func TestServeListFormatGating(t *testing.T) {
	s := listServer(&fakeMeta{g: 1}) // CSVMaxRows: 5
	var gotLimit int
	spec := hostSpec(func(_ context.Context, _ string, _ *Seek, limit int, _ bool) ([]hostRow, error) {
		gotLimit = limit
		return hostRows(2), nil
	})

	// No writer: ?format=csv is ignored, JSON served.
	rec := httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/hosts?format=csv", nil), spec)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("format ignored without writer: code=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}

	// Writer armed: csv dispatches to it with the typed items, cap raised.
	var csvItems []string
	spec.CSV = func(w http.ResponseWriter, items []string) {
		csvItems = items
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.WriteHeader(http.StatusOK)
	}
	spec.Count = func(context.Context) (int64, error) {
		t.Fatal("count ran on the CSV path")
		return 0, nil
	}
	rec = httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/hosts?format=csv&limit=100", nil), spec)
	if len(csvItems) != 2 {
		t.Fatalf("CSV writer items = %d", len(csvItems))
	}
	if gotLimit != 5 {
		t.Fatalf("CSV limit cap = %d, want CSVMaxRows", gotLimit)
	}

	// JSON path keeps the standard cap.
	spec.Count = nil
	ServeList(s, httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/hosts?limit=100", nil), spec)
	if gotLimit != 100 {
		t.Fatalf("JSON limit = %d", gotLimit)
	}

	rec = httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/hosts?format=xml", nil), spec)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("format=xml: code=%d", rec.Code)
	}
	if d := problemDetail(t, rec); d != "format must be json or csv" {
		t.Fatalf("format detail = %q", d)
	}
}

// Count runs on the JSON path only and fills meta.count; a Count failure is
// a 500; a nil Count emits no count key at all.
func TestServeListCountFlavors(t *testing.T) {
	s := listServer(&fakeMeta{g: 1, asOf: time.Now()})
	spec := hostSpec(staticFetch(hostRows(2)))

	rec := httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/hosts", nil), spec)
	env := decodeEnvelope(t, rec.Body.String())
	if strings.Contains(string(env["meta"]), `"count"`) {
		t.Fatalf("nil Count emitted a count: %s", env["meta"])
	}

	spec.Count = func(context.Context) (int64, error) { return 42, nil }
	rec = httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/hosts", nil), spec)
	env = decodeEnvelope(t, rec.Body.String())
	if !strings.Contains(string(env["meta"]), `"count":42`) {
		t.Fatalf("meta = %s", env["meta"])
	}

	spec.Count = func(context.Context) (int64, error) { return 0, errors.New("boom") }
	rec = httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/hosts", nil), spec)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("count failure: code=%d", rec.Code)
	}
}

// A cursor minted under one scope must not decode under another, and the
// full walk round-trips through the module.
func TestServeListScopeBindsCursor(t *testing.T) {
	s := listServer(&fakeMeta{g: 1, asOf: time.Now()})
	spec := hostSpec(staticFetch(hostRows(10)))
	spec.Scope = "campaign:1"

	rec := httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/hosts?limit=3", nil), spec)
	var env struct{ Page Page }
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Page.NextCursor == nil {
		t.Fatalf("no next_cursor minted: err=%v body=%s", err, rec.Body.String())
	}

	// Same cursor, same scope: decodes and pages on.
	rec = httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/hosts?limit=3&cursor="+*env.Page.NextCursor, nil), spec)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-scope replay: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// Same cursor, different scope: 400 invalid-parameter.
	spec.Scope = "campaign:2"
	rec = httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/hosts?limit=3&cursor="+*env.Page.NextCursor, nil), spec)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cross-scope replay: code=%d", rec.Code)
	}
}

// The three error modes: malformed cursor → 400 with the cursor text,
// fetch and generation failures → opaque 500.
func TestServeListErrorMapping(t *testing.T) {
	s := listServer(&fakeMeta{g: 1})

	rec := httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/hosts?cursor=%21garbage", nil), hostSpec(staticFetch(nil)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor: code=%d", rec.Code)
	}
	if d := problemDetail(t, rec); !strings.Contains(d, "invalid cursor") {
		t.Fatalf("cursor detail = %q", d)
	}

	rec = httptest.NewRecorder()
	ServeList(s, rec, httptest.NewRequest(http.MethodGet, "/hosts", nil),
		hostSpec(func(context.Context, string, *Seek, int, bool) ([]hostRow, error) {
			return nil, errors.New("secret db failure")
		}))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("fetch failure: code=%d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("error text leaked: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	ServeList(listServer(&fakeMeta{genErr: errors.New("no db")}), rec,
		httptest.NewRequest(http.MethodGet, "/hosts", nil), hostSpec(staticFetch(nil)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("generation failure: code=%d", rec.Code)
	}
}

// ServeWhole: trivial Page{}, exact count = len(items), ?limit= unread, and
// the same 304-before-fetch gate.
func TestServeWhole(t *testing.T) {
	s := listServer(&fakeMeta{g: 3, asOf: time.Now()})
	fetched := false
	spec := WholeSpec[string]{
		Sorts: []string{"percent", "v6_sites", "sites"},
		Fetch: func(_ context.Context, sortKey string) ([]string, error) {
			fetched = true
			if sortKey != "percent" {
				t.Fatalf("default sort = %q", sortKey)
			}
			return []string{"NO", "SE"}, nil
		},
	}

	rec := httptest.NewRecorder()
	ServeWhole(s, rec, httptest.NewRequest(http.MethodGet, "/countries?limit=abc", nil), spec)
	if rec.Code != http.StatusOK {
		t.Fatalf("limit must stay unread: code=%d body=%s", rec.Code, rec.Body.String())
	}
	env := decodeEnvelope(t, rec.Body.String())
	if !strings.Contains(string(env["meta"]), `"count":2`) {
		t.Fatalf("meta = %s", env["meta"])
	}
	var page Page
	if err := json.Unmarshal(env["page"], &page); err != nil || page.NextCursor != nil || page.HasMore {
		t.Fatalf("page = %s", env["page"])
	}

	fetched = false
	req := httptest.NewRequest(http.MethodGet, "/countries?limit=abc", nil)
	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	rec = httptest.NewRecorder()
	ServeWhole(s, rec, req, spec)
	if rec.Code != http.StatusNotModified || fetched {
		t.Fatalf("conditional: code=%d fetched=%v", rec.Code, fetched)
	}
}

// ListPage writes nothing on success; on failure it writes the problem and
// reports ok=false.
func TestListPage(t *testing.T) {
	ks := KeysetSpec[hostRow]{
		Sort: SortHost,
		Fetch: func(_ context.Context, _ *Seek, limit int, _ bool) ([]hostRow, error) {
			return hostRows(2), nil
		},
		Key: func(r *hostRow) []any { return []any{r.Host} },
	}
	item := func(r *hostRow) string { return strings.ToUpper(r.Host) }

	rec := httptest.NewRecorder()
	items, _, ok := ListPage(rec, httptest.NewRequest(http.MethodGet, "/x", nil), 1, 50, ks, item)
	if !ok || rec.Body.Len() != 0 {
		t.Fatalf("success wrote: ok=%v body=%q", ok, rec.Body.String())
	}
	if len(items) != 2 || items[0] != "HOST-000.EXAMPLE" {
		t.Fatalf("items = %v", items)
	}

	rec = httptest.NewRecorder()
	_, _, ok = ListPage(rec, httptest.NewRequest(http.MethodGet, "/x?cursor=%21garbage", nil), 1, 50, ks, item)
	if ok || rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor: ok=%v code=%d", ok, rec.Code)
	}

	ks.Fetch = func(context.Context, *Seek, int, bool) ([]hostRow, error) {
		return nil, errors.New("boom")
	}
	rec = httptest.NewRecorder()
	_, _, ok = ListPage(rec, httptest.NewRequest(http.MethodGet, "/x", nil), 1, 50, ks, item)
	if ok || rec.Code != http.StatusInternalServerError {
		t.Fatalf("fetch failure: ok=%v code=%d", ok, rec.Code)
	}
}
