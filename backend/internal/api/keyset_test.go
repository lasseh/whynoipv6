package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/url"
	"testing"
)

// ksRow is the fake row for the pipeline tests: keyed (rank, id) — the
// SortRank tuple shape.
type ksRow struct {
	Rank int32
	ID   int64
}

func ksKey(r *ksRow) []any { return []any{r.Rank, r.ID} }

func ksRows(ranks ...int32) []ksRow {
	rows := make([]ksRow, len(ranks))
	for i, n := range ranks {
		rows[i] = ksRow{Rank: n, ID: int64(n)}
	}
	return rows
}

// ksFetch records the pipeline's fetch call and plays back a scripted window.
type ksFetch struct {
	rows     []ksRow
	err      error
	seek     *Seek
	limit    int
	backward bool
	calls    int
}

func (f *ksFetch) fn(_ context.Context, seek *Seek, limit int, backward bool) ([]ksRow, error) {
	f.seek, f.limit, f.backward = seek, limit, backward
	f.calls++
	return f.rows, f.err
}

const ksGen int32 = 20260701

// ksPage runs KeysetPage over a bare request with the given raw query.
// preceded is the after_rank backward probe; nil = no positioning param.
func ksPage(t *testing.T, rawQuery string, fetch *ksFetch,
	preceded func(context.Context, *ksRow) (bool, error),
) ([]ksRow, Page, error) {
	t.Helper()
	r := httptest.NewRequest("GET", "/domains?"+rawQuery, nil)
	return KeysetPage(r, ksGen, 5, KeysetSpec[ksRow]{
		Sort:     SortRank,
		Preceded: preceded,
		Fetch:    fetch.fn,
		Key:      ksKey,
	})
}

// ksPreceded scripts the backward probe: does anything precede the window?
func ksPreceded(before bool) func(context.Context, *ksRow) (bool, error) {
	return func(context.Context, *ksRow) (bool, error) { return before, nil }
}

// ksCursor mints a token the way BuildPage would, against the fingerprint of
// the given non-paging query params.
func ksCursor(g int32, sortKey string, filters url.Values, k []any, backward bool) string {
	dir := ""
	if backward {
		dir = "p"
	}
	return encodeCursorDir(g, sortKey, FilterFingerprint(filters), k, dir)
}

func ksDecode(t *testing.T, token string, filters url.Values) Seek {
	t.Helper()
	c, err := DecodeCursor(token, SortRank, FilterFingerprint(filters), ksGen)
	if err != nil {
		t.Fatalf("minted cursor does not decode: %v", err)
	}
	st, err := c.SeekTuple()
	if err != nil {
		t.Fatalf("minted cursor seek: %v", err)
	}
	return st
}

func TestKeysetPageForward(t *testing.T) {
	t.Run("first_page_no_overflow", func(t *testing.T) {
		f := &ksFetch{rows: ksRows(1, 2, 3)}
		rows, page, err := ksPage(t, "", f, nil)
		if err != nil {
			t.Fatal(err)
		}
		if f.seek != nil || f.backward || f.limit != 5 {
			t.Errorf("fetch args = (%v,%d,%t), want (nil,5,false)", f.seek, f.limit, f.backward)
		}
		if len(rows) != 3 || page.HasMore || page.NextCursor != nil || page.PrevCursor != nil {
			t.Errorf("page = %+v over %d rows, want plain end", page, len(rows))
		}
	})

	t.Run("first_page_overflow_mints_next", func(t *testing.T) {
		f := &ksFetch{rows: ksRows(1, 2, 3, 4, 5, 6)}
		rows, page, err := ksPage(t, "", f, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 5 || !page.HasMore || page.NextCursor == nil {
			t.Fatalf("want trimmed window with next_cursor, got %d rows %+v", len(rows), page)
		}
		if page.PrevCursor != nil {
			t.Error("unpositioned first page must not mint prev_cursor")
		}
		st := ksDecode(t, *page.NextCursor, nil)
		if st.Rank == nil || *st.Rank != 5 || st.ID != 5 {
			t.Errorf("next_cursor K = (%v,%d), want (5,5) — the last DISPLAYED row", st.Rank, st.ID)
		}
	})

	t.Run("cursor_positioned_mints_prev", func(t *testing.T) {
		f := &ksFetch{rows: ksRows(6, 7, 8)}
		tok := ksCursor(ksGen, SortRank, nil, []any{int32(5), int64(5)}, false)
		rows, page, err := ksPage(t, "cursor="+url.QueryEscape(tok), f, nil)
		if err != nil {
			t.Fatal(err)
		}
		if f.seek == nil || f.seek.Rank == nil || *f.seek.Rank != 5 || f.seek.ID != 5 || f.backward {
			t.Fatalf("fetch seek = %+v backward=%t, want (5,5) forward", f.seek, f.backward)
		}
		if len(rows) != 3 || page.HasMore || page.NextCursor != nil {
			t.Errorf("short positioned page must not claim more: %+v", page)
		}
		if page.PrevCursor == nil {
			t.Fatal("positioned page must mint prev_cursor from its first row")
		}
		st := ksDecode(t, *page.PrevCursor, nil)
		if st.Rank == nil || *st.Rank != 6 {
			t.Errorf("prev_cursor K rank = %v, want 6", st.Rank)
		}
	})

	// after_rank positions the window with no cursor to prove a previous
	// row exists, so the probe decides (07 §2.4). Minting unconditionally
	// stranded /domains?after_rank=0 on an empty prev page.
	t.Run("after_rank_probe", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			before   bool
			wantPrev bool
		}{
			{"rows before the window mint prev", true, true},
			{"nothing before the window mints none", false, false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				f := &ksFetch{rows: ksRows(11, 12)}
				_, page, err := ksPage(t, "", f, ksPreceded(tt.before))
				if err != nil {
					t.Fatal(err)
				}
				if (page.PrevCursor != nil) != tt.wantPrev {
					t.Errorf("prev_cursor = %v, want minted %t", page.PrevCursor, tt.wantPrev)
				}
			})
		}
	})

	t.Run("probe_error_surfaces", func(t *testing.T) {
		f := &ksFetch{rows: ksRows(11, 12)}
		want := errors.New("probe failed")
		_, _, err := ksPage(t, "", f, func(context.Context, *ksRow) (bool, error) {
			return false, want
		})
		if !errors.Is(err, want) {
			t.Errorf("err = %v, want the probe's own error", err)
		}
	})

	t.Run("probe_skipped_on_a_cursor_page", func(t *testing.T) {
		f := &ksFetch{rows: ksRows(6, 7, 8)}
		tok := ksCursor(ksGen, SortRank, nil, []any{int32(5), int64(5)}, false)
		probed := false
		_, page, err := ksPage(t, "cursor="+url.QueryEscape(tok), f,
			func(context.Context, *ksRow) (bool, error) { probed = true; return false, nil })
		if err != nil {
			t.Fatal(err)
		}
		if probed {
			t.Error("a cursor is its own proof of a previous row; no probe should run")
		}
		if page.PrevCursor == nil {
			t.Error("cursor page must still mint prev_cursor")
		}
	})
}

func TestKeysetPageBackward(t *testing.T) {
	t.Run("overflow_at_front", func(t *testing.T) {
		// Display order with the N+1 overflow row FIRST (the backward
		// convention): row 1 proves more rows exist above.
		f := &ksFetch{rows: ksRows(1, 2, 3, 4, 5, 6)}
		tok := ksCursor(ksGen, SortRank, nil, []any{int32(7), int64(7)}, true)
		rows, page, err := ksPage(t, "cursor="+url.QueryEscape(tok), f, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !f.backward {
			t.Fatal("fetch must run backward")
		}
		if len(rows) != 5 || rows[0].Rank != 2 {
			t.Fatalf("front overflow must be trimmed: got %d rows first=%d", len(rows), rows[0].Rank)
		}
		if page.NextCursor == nil || !page.HasMore {
			t.Error("a backward page always has a forward continuation")
		}
		if page.PrevCursor == nil {
			t.Error("front overflow proves rows above: prev_cursor must mint")
		}
	})

	t.Run("no_overflow_reaches_start", func(t *testing.T) {
		f := &ksFetch{rows: ksRows(1, 2, 3)}
		tok := ksCursor(ksGen, SortRank, nil, []any{int32(4), int64(4)}, true)
		rows, page, err := ksPage(t, "cursor="+url.QueryEscape(tok), f, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 3 || rows[0].Rank != 1 {
			t.Fatalf("short backward window must keep all rows: %d", len(rows))
		}
		if page.PrevCursor != nil {
			t.Error("reaching the start must not mint prev_cursor")
		}
		if page.NextCursor == nil {
			t.Error("forward continuation must still mint")
		}
	})

	t.Run("empty_backward_page", func(t *testing.T) {
		f := &ksFetch{}
		tok := ksCursor(ksGen, SortRank, nil, []any{int32(1), int64(1)}, true)
		rows, page, err := ksPage(t, "cursor="+url.QueryEscape(tok), f, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 0 {
			t.Fatal("scripted empty window")
		}
		if page.HasMore || page.NextCursor != nil || page.PrevCursor != nil {
			t.Errorf("an empty page can mint nothing: %+v", page)
		}
	})
}

func TestKeysetPageCursorValidation(t *testing.T) {
	t.Run("wrong_sort_rejected", func(t *testing.T) {
		f := &ksFetch{}
		tok := ksCursor(ksGen, SortHost, nil, []any{"x.no"}, false)
		_, _, err := ksPage(t, "cursor="+url.QueryEscape(tok), f, nil)
		if !errors.Is(err, ErrCursorInvalid) {
			t.Fatalf("err = %v, want ErrCursorInvalid", err)
		}
		if f.calls != 0 {
			t.Error("fetch must not run on a rejected cursor")
		}
	})

	t.Run("fingerprint_mismatch_rejected", func(t *testing.T) {
		f := &ksFetch{}
		tok := ksCursor(ksGen, SortRank, nil, []any{int32(5), int64(5)}, false)
		_, _, err := ksPage(t, "class=hero&cursor="+url.QueryEscape(tok), f, nil)
		if !errors.Is(err, ErrCursorInvalid) {
			t.Fatalf("err = %v, want ErrCursorInvalid", err)
		}
	})

	t.Run("stale_generation_reanchors", func(t *testing.T) {
		f := &ksFetch{rows: ksRows(6, 7)}
		tok := ksCursor(ksGen-1, SortRank, nil, []any{int32(5), int64(5)}, false)
		_, page, err := ksPage(t, "cursor="+url.QueryEscape(tok), f, nil)
		if err != nil {
			t.Fatal(err)
		}
		if f.seek == nil || f.seek.ID != 0 {
			t.Fatalf("re-anchored seek must zero the id tiebreaker: %+v", f.seek)
		}
		if page.PrevCursor != nil {
			st := ksDecode(t, *page.PrevCursor, nil) // minted at CURRENT generation
			if st.ID == 0 {
				t.Error("freshly minted cursors carry real tiebreakers")
			}
		}
	})

	t.Run("fetch_error_propagates", func(t *testing.T) {
		f := &ksFetch{err: errors.New("boom")}
		_, _, err := ksPage(t, "", f, nil)
		if err == nil || errors.Is(err, ErrCursorInvalid) {
			t.Fatalf("err = %v, want the fetch error verbatim", err)
		}
	})
}

func TestMintPage(t *testing.T) {
	rows := ksRows(3, 4, 5)
	page := MintPage(ksGen, SortRank, "", true, true, rows, ksKey)
	if page.NextCursor == nil || page.PrevCursor == nil || !page.HasMore {
		t.Fatalf("both continuations must mint: %+v", page)
	}

	// A key extractor may return nil (rank-NULL rows): no continuation.
	nilKey := func(*ksRow) []any { return nil }
	page = MintPage(ksGen, SortRank, "", true, true, rows, nilKey)
	if page.NextCursor != nil || page.HasMore || page.PrevCursor != nil {
		t.Errorf("nil keys must not mint cursors: %+v", page)
	}

	page = MintPage(ksGen, SortRank, "", true, true, nil, ksKey)
	if page.NextCursor != nil || page.HasMore || page.PrevCursor != nil {
		t.Errorf("an empty window must not mint cursors: %+v", page)
	}
}

// TestCursorInt64Precision pins the json.Number decode path: the changelog
// seek carries ts.UnixNano() (~1.8e18), which float64 decoding would round
// to a 256 ns grid — repeating or skipping same-ts siblings at the page
// boundary.
func TestCursorInt64Precision(t *testing.T) {
	ts := int64(1780000000123456789) // not representable in float64
	tok := EncodeCursor(ksGen, SortChangelog, FilterFingerprint(nil), []any{ts, int64(7), "base"})
	c, err := DecodeCursor(tok, SortChangelog, FilterFingerprint(nil), ksGen)
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.SeekTuple()
	if err != nil {
		t.Fatal(err)
	}
	if st.TS != ts {
		t.Errorf("seek ts = %d, want %d (lost %d ns to float64)", st.TS, ts, st.TS-ts)
	}
}
