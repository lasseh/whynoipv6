package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// ProviderBody is the §4.6 DNS-provider league-table row — exact stored
// counters (recomputed daily like asn), count_v4 synthesized.
type ProviderBody struct {
	ID         int64       `json:"id"`
	Name       string      `json:"name"`
	CountTotal int32       `json:"count_total"`
	CountV6    int32       `json:"count_v6"`
	CountV4    int32       `json:"count_v4"`
	Meta       *DetailMeta `json:"meta,omitempty"` // detail only
}

// listProviders is GET /providers — the bounded curated registry, served
// whole with an exact count (07 §4.6).
func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	wantCSV, err := parseFormat(r.URL.Query())
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	generation, asOf, err := s.svc.Generation(r.Context())
	if err != nil {
		InternalError(w, r)
		return
	}
	if CacheList(w, r, generation) {
		return
	}
	rows, err := s.svc.Q.ProviderLeaderboard(r.Context())
	if err != nil {
		InternalError(w, r)
		return
	}
	items := make([]ProviderBody, len(rows))
	for i := range rows {
		items[i] = ProviderBody{ID: rows[i].ID, Name: rows[i].Name,
			CountTotal: rows[i].CountTotal, CountV6: rows[i].CountV6,
			CountV4: rows[i].CountTotal - rows[i].CountV6}
	}
	if wantCSV {
		writeProvidersCSV(w, items)
		return
	}
	count := int64(len(items))
	meta := NewMeta(asOf, generation)
	meta.Count = &count
	WriteJSON(w, http.StatusOK, ListEnvelope{Items: items, Page: Page{}, Meta: meta})
}

// parseProviderID resolves the {id} path param; malformed → 404.
func parseProviderID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		NotFound(w, r, "Provider not found", "DNS providers are keyed by their numeric id.")
		return 0, false
	}
	return id, true
}

// getProvider is GET /providers/{id} (07 §4.6).
func (s *Server) getProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProviderID(w, r)
	if !ok {
		return
	}
	row, err := s.svc.Q.ProviderDetail(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Provider not found", "No such DNS provider: "+strconv.FormatInt(id, 10))
		return
	}
	if err != nil {
		InternalError(w, r)
		return
	}
	generation, asOf, err := s.svc.Generation(r.Context())
	if err != nil {
		InternalError(w, r)
		return
	}
	if CacheList(w, r, generation) {
		return
	}
	WriteJSON(w, http.StatusOK, ProviderBody{ID: row.ID, Name: row.Name,
		CountTotal: row.CountTotal, CountV6: row.CountV6, CountV4: row.CountTotal - row.CountV6,
		Meta: &DetailMeta{AsOf: asOf.UTC(), Generation: generation}})
}

// listProviderDomains is GET /providers/{id}/domains ≡ /domains?provider={id}.
func (s *Server) listProviderDomains(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProviderID(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.Q.ProviderDetail(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			NotFound(w, r, "Provider not found", "No such DNS provider: "+strconv.FormatInt(id, 10))
			return
		}
		InternalError(w, r)
		return
	}
	s.serveDomainList(w, r, url.Values{paramProvider: {strconv.FormatInt(id, 10)}})
}
