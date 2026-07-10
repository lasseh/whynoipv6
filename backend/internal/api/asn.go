package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// ASNBody is the §4.6 network representation; count_v4 is synthesized
// (count_total − count_v6), never stored.
type ASNBody struct {
	Number     int64       `json:"number"`
	Name       string      `json:"name"`
	CountTotal int32       `json:"count_total"`
	CountV6    int32       `json:"count_v6"`
	CountV4    int32       `json:"count_v4"`
	Meta       *DetailMeta `json:"meta,omitempty"` // detail only
}

func asnBody(number int64, name string, total, v6 int32) ASNBody {
	return ASNBody{Number: number, Name: name, CountTotal: total, CountV6: v6, CountV4: total - v6}
}

// listASNs is GET /asns — the hosting-ASN league table (07 §4.6):
// ?sort=count_v6 (default) | count_total, ?q= substring on the name,
// keyset-paginated on (count, number) descending.
func (s *Server) listASNs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sortKey := q.Get("sort")
	switch sortKey {
	case "":
		sortKey = SortCountV6
	case SortCountV6, SortCountTotal:
	default:
		InvalidParameter(w, r, "sort must be count_v6 or count_total")
		return
	}
	wantCSV, err := parseFormat(q)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	limitCap := MaxLimit
	if wantCSV {
		limitCap = s.opts.CSVMaxRows
	}
	limit, err := ParseLimitCap(q, limitCap)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}

	generation, asOf, err := s.svc.Generation(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheList(w, r, generation) {
		return
	}

	fingerprint := FilterFingerprint(q)
	params := db.ASNLeaderboardByV6Params{Q: q.Get("q"), Lim: int32(limit + 1)}
	backward := false
	if token := q.Get(paramCursor); token != "" {
		c, err := DecodeCursor(token, sortKey, fingerprint, generation)
		if err != nil {
			InvalidParameter(w, r, err.Error())
			return
		}
		st, err := c.SeekTuple()
		if err != nil {
			InvalidParameter(w, r, err.Error())
			return
		}
		if st.Rank != nil {
			params.WithSeek, params.SeekCount, params.SeekNumber = true, *st.Rank, st.ID
			backward = c.Backward()
		}
	}

	items, err := s.asnLeaderboardRows(r, sortKey, params, backward)
	if err != nil {
		InternalError(w, r, err)
		return
	}

	var forwardMore, backwardMore bool
	overflow := len(items) > limit
	if backward {
		backwardMore, forwardMore = overflow, true
		if overflow {
			items = items[1:]
		}
	} else {
		forwardMore, backwardMore = overflow, params.WithSeek
		if overflow {
			items = items[:limit]
		}
	}
	if wantCSV {
		writeASNsCSV(w, items)
		return
	}
	asnKey := func(a ASNBody) []any {
		count := a.CountV6
		if sortKey == SortCountTotal {
			count = a.CountTotal
		}
		return []any{count, a.Number}
	}
	var firstK, lastK []any
	if len(items) > 0 {
		firstK, lastK = asnKey(items[0]), asnKey(items[len(items)-1])
	}

	WriteJSON(w, http.StatusOK, ListEnvelope{
		Items: items,
		Page:  BuildPage(generation, sortKey, fingerprint, forwardMore, backwardMore, firstK, lastK),
		Meta:  NewMeta(asOf, generation),
	})
}

// asnLeaderboardRows dispatches the sort × direction query matrix; backward
// rows come back re-reversed into display order.
func (s *Server) asnLeaderboardRows(r *http.Request, sortKey string, params db.ASNLeaderboardByV6Params, backward bool) ([]ASNBody, error) {
	var items []ASNBody
	add := func(number int64, name string, total, v6 int32) {
		items = append(items, asnBody(number, name, total, v6))
	}
	switch {
	case sortKey == SortCountTotal && backward:
		rows, err := s.svc.Q.ASNLeaderboardByTotalPrev(r.Context(), db.ASNLeaderboardByTotalPrevParams{
			Q: params.Q, SeekCount: params.SeekCount, SeekNumber: params.SeekNumber, Lim: params.Lim,
		})
		if err != nil {
			return nil, err
		}
		for i := range rows {
			add(rows[i].Number, rows[i].Name, rows[i].CountTotal, rows[i].CountV6)
		}
	case sortKey == SortCountTotal:
		rows, err := s.svc.Q.ASNLeaderboardByTotal(r.Context(), db.ASNLeaderboardByTotalParams(params))
		if err != nil {
			return nil, err
		}
		for i := range rows {
			add(rows[i].Number, rows[i].Name, rows[i].CountTotal, rows[i].CountV6)
		}
	case backward:
		rows, err := s.svc.Q.ASNLeaderboardByV6Prev(r.Context(), db.ASNLeaderboardByV6PrevParams{
			Q: params.Q, SeekCount: params.SeekCount, SeekNumber: params.SeekNumber, Lim: params.Lim,
		})
		if err != nil {
			return nil, err
		}
		for i := range rows {
			add(rows[i].Number, rows[i].Name, rows[i].CountTotal, rows[i].CountV6)
		}
	default:
		rows, err := s.svc.Q.ASNLeaderboardByV6(r.Context(), params)
		if err != nil {
			return nil, err
		}
		for i := range rows {
			add(rows[i].Number, rows[i].Name, rows[i].CountTotal, rows[i].CountV6)
		}
	}
	if backward { // ASC fetch → re-reverse into leaderboard order
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}
	return items, nil
}

// parseASNNumber resolves the {number} path param; malformed → 404
// (path-parameter failure policy, 07 §2.2).
func parseASNNumber(w http.ResponseWriter, r *http.Request) (int64, bool) {
	n, err := strconv.ParseInt(chi.URLParam(r, "number"), 10, 64)
	if err != nil || n < 0 {
		NotFound(w, r, "Network not found", "ASNs are keyed by their bare AS number.")
		return 0, false
	}
	return n, true
}

// getASN is GET /asns/{number} (07 §4.6).
func (s *Server) getASN(w http.ResponseWriter, r *http.Request) {
	n, ok := parseASNNumber(w, r)
	if !ok {
		return
	}
	row, err := s.svc.Q.ASNByNumber(r.Context(), n)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Network not found", "No such ASN: "+strconv.FormatInt(n, 10))
		return
	}
	if err != nil {
		InternalError(w, r, err)
		return
	}
	generation, asOf, err := s.svc.Generation(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheList(w, r, generation) {
		return
	}
	body := asnBody(row.Number, row.Name, row.CountTotal, row.CountV6)
	body.Meta = &DetailMeta{AsOf: asOf.UTC(), Generation: generation}
	WriteJSON(w, http.StatusOK, body)
}

// listASNDomains is GET /asns/{number}/domains ≡ /domains?asn={number}.
func (s *Server) listASNDomains(w http.ResponseWriter, r *http.Request) {
	n, ok := parseASNNumber(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.Q.ASNByNumber(r.Context(), n); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			NotFound(w, r, "Network not found", "No such ASN: "+strconv.FormatInt(n, 10))
			return
		}
		InternalError(w, r, err)
		return
	}
	s.serveDomainList(w, r, url.Values{paramASN: {strconv.FormatInt(n, 10)}})
}
