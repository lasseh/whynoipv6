package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/lasseh/whynoipv6/internal/postgres"
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

	generation, asOf, err := s.generation(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheList(w, r, generation) {
		return
	}

	items, page, err := KeysetPage(r, generation, limit, KeysetSpec[ASNBody]{
		Sort: sortKey,
		Fetch: func(ctx context.Context, seek *Seek, lim int, backward bool) ([]ASNBody, error) {
			var as *postgres.ASNSeek
			if seek != nil && seek.Rank != nil {
				as = &postgres.ASNSeek{Count: *seek.Rank, Number: seek.ID}
			}
			rows, err := postgres.ListASNLeaderboard(ctx, s.pool, q.Get("q"),
				sortKey == SortCountTotal, as, lim, backward)
			if err != nil {
				return nil, err
			}
			bodies := make([]ASNBody, len(rows))
			for i := range rows {
				bodies[i] = asnBody(rows[i].Number, rows[i].Name, rows[i].CountTotal, rows[i].CountV6)
			}
			return bodies, nil
		},
		Key: func(a *ASNBody) []any {
			count := a.CountV6
			if sortKey == SortCountTotal {
				count = a.CountTotal
			}
			return []any{count, a.Number}
		},
	})
	if errors.Is(err, ErrCursorInvalid) {
		InvalidParameter(w, r, err.Error())
		return
	}
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if wantCSV {
		writeASNsCSV(w, items)
		return
	}

	WriteJSON(w, http.StatusOK, ListEnvelope{
		Items: items,
		Page:  page,
		Meta:  NewMeta(asOf, generation),
	})
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
	row, err := s.q.ASNByNumber(r.Context(), n)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Network not found", "No such ASN: "+strconv.FormatInt(n, 10))
		return
	}
	if err != nil {
		InternalError(w, r, err)
		return
	}
	generation, asOf, err := s.generation(r.Context())
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
	if _, err := s.q.ASNByNumber(r.Context(), n); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			NotFound(w, r, "Network not found", "No such ASN: "+strconv.FormatInt(n, 10))
			return
		}
		InternalError(w, r, err)
		return
	}
	s.serveDomainList(w, r, url.Values{paramASN: {strconv.FormatInt(n, 10)}})
}
