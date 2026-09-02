package api

import (
	"context"
	"net/http"
)

// KeysetSpec carries the four things that vary per keyset-paginated
// endpoint; everything else — fingerprint, cursor decode, direction, the
// N+1 trim, cursor minting — is owned by KeysetPage (07 §3.2).
type KeysetSpec[Row any] struct {
	// Sort is the cursor sort domain (SortRank, SortHost, SortChangelog, …).
	Sort string

	// Preceded, when non-nil, answers whether any row precedes the window
	// on a page positioned outside the cursor (after_rank on /domains).
	// A cursor is its own proof that a previous row exists; a positioning
	// param is not — after_rank=0, or below a scope's first rank, has
	// nothing before it, and minting a prev_cursor there strands the
	// client on an empty page (07 §2.4: prev_cursor is null when there is
	// no previous page).
	Preceded func(ctx context.Context, first *Row) (bool, error)

	// Fingerprint, when non-empty, overrides the fingerprint KeysetPage
	// derives from the raw query — set where the filter set includes
	// path-derived scope (presets, campaign/parent/resource ids) the
	// query string alone cannot see.
	Fingerprint string

	// Fetch runs the window query: up to limit+1 rows in DISPLAY order.
	// A backward fetch flips its comparisons and ORDER internally,
	// re-reverses into display order, and carries its N+1 overflow row at
	// the FRONT (the trimWindow convention).
	Fetch func(ctx context.Context, seek *Seek, limit int, backward bool) ([]Row, error)

	// Key extracts a row's seek tuple for cursor minting; nil = this row
	// cannot anchor a cursor (e.g. a rank-NULL row on a rank ordering).
	Key func(row *Row) []any
}

// KeysetPage is the shared pagination pipeline: decode → seek → fetch →
// trim → mint. It returns the display window and the Page block; cursor
// errors satisfy errors.Is(err, ErrCursorInvalid) (→ 400), anything else
// is the fetch's own failure (→ 500). Envelopes, CSV, caching, and meta
// stay with the caller.
func KeysetPage[Row any](r *http.Request, generation int32, limit int, spec KeysetSpec[Row]) ([]Row, Page, error) {
	q := r.URL.Query()
	fingerprint := spec.Fingerprint
	if fingerprint == "" {
		fingerprint = FilterFingerprint(q)
	}

	var seek *Seek
	backward := false
	if token := q.Get(paramCursor); token != "" {
		c, err := DecodeCursor(token, spec.Sort, fingerprint, generation)
		if err != nil {
			return nil, Page{}, err
		}
		st, err := c.SeekTuple()
		if err != nil {
			return nil, Page{}, err
		}
		seek = &st
		backward = c.Backward()
	}

	rows, err := spec.Fetch(r.Context(), seek, limit, backward)
	if err != nil {
		return nil, Page{}, err
	}

	rows, forwardMore, backwardMore := trimWindow(rows, limit, backward, seek != nil)
	// A window positioned by a param rather than a cursor has to ask the
	// scope whether anything precedes it. Only on a forward first page, and
	// only when the first row can anchor a cursor at all — a rank-NULL row
	// on a rank ordering mints nothing either way.
	if !backward && seek == nil && spec.Preceded != nil && len(rows) > 0 && spec.Key(&rows[0]) != nil {
		backwardMore, err = spec.Preceded(r.Context(), &rows[0])
		if err != nil {
			return nil, Page{}, err
		}
	}
	return rows, MintPage(generation, spec.Sort, fingerprint, forwardMore, backwardMore, rows, spec.Key), nil
}

// MintPage encodes a window's first/last seek tuples into the Page block —
// shared by KeysetPage and the /domains around_rank branch, whose window
// arrives from a two-sided fetch instead of the cursor walk.
func MintPage[Row any](g int32, sortKey, fingerprint string, forwardMore, backwardMore bool, rows []Row, key func(*Row) []any) Page {
	var firstK, lastK []any
	if len(rows) > 0 {
		firstK, lastK = key(&rows[0]), key(&rows[len(rows)-1])
	}
	return BuildPage(g, sortKey, fingerprint, forwardMore, backwardMore, firstK, lastK)
}
