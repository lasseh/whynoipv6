package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// This file is the list rim — the shared pipeline around the keyset spec
// (07 §2.4/§3.2/§5.5/§6.1). KeysetSpec/KeysetPage own the cursor walk;
// ServeList/ServeWhole/ListPage own everything the handlers used to
// hand-write around it: sort resolution, ?format= negotiation, the CSV
// limit-cap raise, generation/maxTS acquisition, the ETag/304 gate (always
// before any window fetch), cursor error mapping, the row→item copy, and
// the {items,page,meta} envelope.

// metaSource is the consumer-side seam for the two DB lookups the rim
// buries: the crawl generation pair (ETag seed + meta, 07 §2.4) and the
// changelog high-water mark (the live cache class, 07 §6.1). pgMeta adapts
// db.Queries in production; list tests substitute a fake, so the whole rim
// unit-tests without a database.
type metaSource interface {
	Generation(ctx context.Context) (generation int32, asOf time.Time, err error)
	ChangelogMaxTS(ctx context.Context) (time.Time, error)
}

// pgMeta is the production adapter at the metaSource seam.
type pgMeta struct{ q *db.Queries }

func (m pgMeta) Generation(ctx context.Context) (int32, time.Time, error) {
	return postgres.Generation(ctx, m.q)
}

func (m pgMeta) ChangelogMaxTS(ctx context.Context) (time.Time, error) {
	ts, err := m.q.ChangelogMaxTS(ctx)
	return ts.Time, err
}

// ListSpec declares one keyset list endpoint to ServeList. The zero value
// of every optional field means "this endpoint doesn't have that": no
// ?sort= param, no CSV, no count, unscoped fingerprint, generation-class
// caching.
type ListSpec[Row, Item any] struct {
	// Sorts is the closed ?sort= set; Sorts[0] is the default when the
	// param is empty. Values outside the set → 400 invalid-parameter with
	// the canonical "sort must be …" detail. Nil: ?sort= is never read
	// and the resolved ordering is Sort. Exactly one of Sorts/Sort is set.
	Sorts []string

	// Sort is the fixed cursor ordering for endpoints without a ?sort=
	// param (SortChangelog, SortHost).
	Sort string

	// Scope, when non-empty, binds cursors to a path-derived scope via
	// ScopedFingerprint ("domain:%d", "subdomains:%d"); empty keeps
	// KeysetPage's own FilterFingerprint derivation.
	Scope string

	// Live selects the live-surface cache class (07 §6.1): the ETag seeds
	// from max(changelog.ts) — fetched before the 304 check — via
	// CacheChangelog instead of CacheList. Generation is fetched either
	// way; cursors and meta always ride it.
	Live bool

	// Fetch runs the window query under the KeysetSpec.Fetch contract
	// (up to limit+1 rows in display order, backward overflow at the
	// front). sortKey is the resolved ordering; fixed-Sort endpoints may
	// ignore it.
	Fetch func(ctx context.Context, sortKey string, seek *Seek, limit int, backward bool) ([]Row, error)

	// Key extracts a row's seek tuple for cursor minting (KeysetSpec.Key
	// contract; nil = the row cannot anchor a cursor). It sees the DB
	// row, never the wire item — seek tuples may need fields the wire
	// deliberately lacks.
	Key func(sortKey string, row *Row) []any

	// Item maps one row to its wire item, after trim/mint and before CSV
	// or the envelope: cursors are minted from rows, responses from items.
	Item func(row *Row) Item

	// CSV, when set, enables ?format=csv (07 §5.5): the limit cap rises
	// from MaxLimit to export.csv_max_rows and the writer replaces the
	// JSON envelope. Nil leaves ?format= unread, exactly as the endpoints
	// without a writer behave today.
	CSV func(w http.ResponseWriter, items []Item)

	// Count fills meta.count (exact — bounded curated sets only) on the
	// JSON path, after a successful page. Nil: no count (asns, changelog).
	Count func(ctx context.Context) (int64, error)
}

// ServeList runs one keyset list endpoint through the shared rim: sort →
// format → limit → generation (+ maxTS when Live) → cache/304 → cursor
// walk → item map → CSV | {items,page,meta}. It always completes the
// response; handlers do path-scope resolution and endpoint-specific
// validation BEFORE the call and write nothing after. Error modes: parse
// failures and invalid cursors → 400 invalid-parameter, everything else →
// 500 internal-error (logged, text never leaks). A free function — Go
// methods cannot carry type parameters (precedent: KeysetPage).
func ServeList[Row, Item any](s *Server, w http.ResponseWriter, r *http.Request, spec ListSpec[Row, Item]) {
	q := r.URL.Query()
	sortKey, ok := resolveSort(w, r, q, spec.Sorts, spec.Sort)
	if !ok {
		return
	}
	wantCSV := false
	if spec.CSV != nil {
		var err error
		if wantCSV, err = parseFormat(q); err != nil {
			invalidParam(w, r, err)
			return
		}
	}
	limitCap := MaxLimit
	if wantCSV {
		limitCap = s.opts.CSVMaxRows // §5.5: CSV raises the cap, same view
	}
	limit, err := ParseLimitCap(q, limitCap)
	if err != nil {
		invalidParam(w, r, err)
		return
	}

	generation, asOf, ok := s.enterCache(w, r, spec.Live)
	if !ok {
		return
	}

	ks := KeysetSpec[Row]{
		Sort: sortKey,
		Fetch: func(ctx context.Context, seek *Seek, limit int, backward bool) ([]Row, error) {
			return spec.Fetch(ctx, sortKey, seek, limit, backward)
		},
		Key: func(row *Row) []any { return spec.Key(sortKey, row) },
	}
	if spec.Scope != "" {
		ks.Fingerprint = ScopedFingerprint(spec.Scope, q)
	}
	items, page, ok := ListPage(w, r, generation, limit, ks, spec.Item)
	if !ok {
		return
	}
	if wantCSV {
		spec.CSV(w, items)
		return
	}

	meta := NewMeta(asOf, generation)
	if spec.Count != nil {
		n, err := spec.Count(r.Context())
		if err != nil {
			InternalError(w, r, err)
			return
		}
		meta.Count = &n
	}
	WriteJSON(w, http.StatusOK, ListEnvelope{Items: items, Page: page, Meta: meta})
}

// WholeSpec declares one bounded whole-collection endpoint to ServeWhole
// (07 §4.5/§4.6: "served whole with an exact count"). Conventions: no
// cursor, ?limit= never read, trivial Page{}, meta.count = len(items),
// generation-class caching. Fetch returns wire items in final display
// order — in-memory sorting by the resolved sortKey stays inside the
// endpoint's closure.
type WholeSpec[Item any] struct {
	Sorts []string // optional closed ?sort= set, Sorts[0] default; nil ⇒ ?sort= unread
	Fetch func(ctx context.Context, sortKey string) ([]Item, error)
	CSV   func(w http.ResponseWriter, items []Item) // as ListSpec.CSV
}

// ServeWhole runs the whole-collection rim: sort → format → generation →
// cache/304 → Fetch → CSV | {items, Page{}, meta+count}. Response and
// error discipline as ServeList.
func ServeWhole[Item any](s *Server, w http.ResponseWriter, r *http.Request, spec WholeSpec[Item]) {
	q := r.URL.Query()
	sortKey, ok := resolveSort(w, r, q, spec.Sorts, "")
	if !ok {
		return
	}
	wantCSV := false
	if spec.CSV != nil {
		var err error
		if wantCSV, err = parseFormat(q); err != nil {
			invalidParam(w, r, err)
			return
		}
	}
	generation, asOf, ok := s.enterCache(w, r, false)
	if !ok {
		return
	}
	items, err := spec.Fetch(r.Context(), sortKey)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if wantCSV {
		spec.CSV(w, items)
		return
	}
	count := int64(len(items))
	meta := NewMeta(asOf, generation)
	meta.Count = &count
	WriteJSON(w, http.StatusOK, ListEnvelope{Items: items, Page: Page{}, Meta: meta})
}

// ListPage is the page-production half without the response half: one
// cursor walk (KeysetPage) plus the row→item map — for callers that own
// their envelope (the dependents composite, the campaign detail's embedded
// members page, the /domains cursor branch). On failure it writes the
// problem itself — ErrCursorInvalid → 400, anything else → 500 — and
// reports ok=false (the (value, ok) house idiom); the success path writes
// nothing.
func ListPage[Row, Item any](w http.ResponseWriter, r *http.Request, generation int32, limit int,
	ks KeysetSpec[Row], item func(*Row) Item,
) ([]Item, Page, bool) {
	rows, page, err := KeysetPage(r, generation, limit, ks)
	if errors.Is(err, ErrCursorInvalid) {
		InvalidParameter(w, r, err.Error())
		return nil, Page{}, false
	}
	if err != nil {
		InternalError(w, r, err)
		return nil, Page{}, false
	}
	items := make([]Item, len(rows))
	for i := range rows {
		items[i] = item(&rows[i])
	}
	return items, page, true
}

// enterCache resolves the freshness pair through the metaSource seam (and
// the changelog high-water mark under the live class), seeds the class
// ETag, and answers a matching conditional GET — always before any window
// fetch (07 §6.1). ok=false: the response is already written (304 or 500).
func (s *Server) enterCache(w http.ResponseWriter, r *http.Request, live bool) (generation int32, asOf time.Time, ok bool) {
	generation, asOf, err := s.meta.Generation(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return 0, time.Time{}, false
	}
	if live {
		maxTS, err := s.meta.ChangelogMaxTS(r.Context())
		if err != nil {
			InternalError(w, r, err)
			return 0, time.Time{}, false
		}
		if CacheChangelog(w, r, maxTS) {
			return 0, time.Time{}, false
		}
	} else if CacheList(w, r, generation) {
		return 0, time.Time{}, false
	}
	return generation, asOf, true
}

// resolveSort validates ?sort= against the closed set (sorts[0] default) or
// returns the fixed ordering; out-of-set values answer the canonical 400.
func resolveSort(w http.ResponseWriter, r *http.Request, q url.Values, sorts []string, fixed string) (string, bool) {
	if len(sorts) == 0 {
		return fixed, true
	}
	sortKey := q.Get("sort")
	if sortKey == "" {
		return sorts[0], true
	}
	if slices.Contains(sorts, sortKey) {
		return sortKey, true
	}
	InvalidParameter(w, r, "sort must be "+oxfordOr(sorts))
	return "", false
}

// oxfordOr joins a closed set the way the hand-written messages did:
// "a or b" for two members, "a, b, or c" beyond.
func oxfordOr(vals []string) string {
	switch len(vals) {
	case 1:
		return vals[0]
	case 2:
		return vals[0] + " or " + vals[1]
	default:
		return strings.Join(vals[:len(vals)-1], ", ") + ", or " + vals[len(vals)-1]
	}
}
