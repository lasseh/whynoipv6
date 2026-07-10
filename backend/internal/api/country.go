package api

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// CountryBody is the §4.5 representation. v6_sites is the snake_case wire
// name for the schema column v6sites; percent serves the NUMERIC(5,2)
// column directly.
type CountryBody struct {
	Code    string      `json:"code"`
	Name    string      `json:"name"`
	TLD     *string     `json:"tld"`
	Sites   int32       `json:"sites"`
	V6Sites int32       `json:"v6_sites"`
	Percent float64     `json:"percent"`
	Meta    *DetailMeta `json:"meta,omitempty"` // detail only
}

func countryBody(code, name string, tld *string, sites, v6sites int32, percent float64) CountryBody {
	return CountryBody{Code: code, Name: name, TLD: tld, Sites: sites, V6Sites: v6sites, Percent: percent}
}

// listCountries is GET /countries — the country leaderboard (07 §4.5): a
// bounded set (~251 incl. the UN sentinel), served whole with an exact
// count; ?sort=percent (default) | v6_sites | sites, descending.
func (s *Server) listCountries(w http.ResponseWriter, r *http.Request) {
	sortKey := r.URL.Query().Get("sort")
	switch sortKey {
	case "", "percent", "v6_sites", "sites":
	default:
		InvalidParameter(w, r, "sort must be percent, v6_sites, or sites")
		return
	}
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

	rows, err := s.svc.Q.CountryLeaderboard(r.Context())
	if err != nil {
		InternalError(w, r)
		return
	}
	items := make([]CountryBody, len(rows))
	for i := range rows {
		pct, _ := rows[i].Percent.Float64Value()
		items[i] = countryBody(strings.TrimSpace(rows[i].Code), rows[i].Name, rows[i].Tld,
			rows[i].Sites, rows[i].V6sites, pct.Float64)
	}
	sort.SliceStable(items, func(i, j int) bool {
		switch sortKey {
		case "v6_sites":
			if items[i].V6Sites != items[j].V6Sites {
				return items[i].V6Sites > items[j].V6Sites
			}
		case "sites":
			if items[i].Sites != items[j].Sites {
				return items[i].Sites > items[j].Sites
			}
		default:
			if items[i].Percent != items[j].Percent {
				return items[i].Percent > items[j].Percent
			}
		}
		return items[i].Code < items[j].Code
	})

	if wantCSV {
		writeCountriesCSV(w, items)
		return
	}
	count := int64(len(items))
	meta := NewMeta(asOf, generation)
	meta.Count = &count
	WriteJSON(w, http.StatusOK, ListEnvelope{Items: items, Page: Page{}, Meta: meta})
}

// getCountry is GET /countries/{code} (07 §4.5).
func (s *Server) getCountry(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if len(code) != 2 {
		NotFound(w, r, "Country not found", "Country codes are two-letter ISO 3166-1 alpha-2.")
		return
	}
	row, err := s.svc.Q.CountryByCode(r.Context(), code)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Country not found", "No such country: "+strings.ToUpper(code))
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
	pct, _ := row.Percent.Float64Value()
	body := countryBody(strings.TrimSpace(row.Code), row.Name, row.Tld, row.Sites, row.V6sites, pct.Float64)
	body.Meta = &DetailMeta{AsOf: asOf.UTC(), Generation: generation}
	WriteJSON(w, http.StatusOK, body)
}

// listCountryDomains is GET /countries/{code}/domains — the country-scoped
// leaderboard, ≡ /domains?country={code} (07 §4.5).
func (s *Server) listCountryDomains(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if len(code) != 2 {
		NotFound(w, r, "Country not found", "Country codes are two-letter ISO 3166-1 alpha-2.")
		return
	}
	if _, err := s.svc.Q.CountryByCode(r.Context(), code); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			NotFound(w, r, "Country not found", "No such country: "+strings.ToUpper(code))
			return
		}
		InternalError(w, r)
		return
	}
	s.serveDomainList(w, r, url.Values{paramCountry: {strings.ToUpper(code)}})
}
