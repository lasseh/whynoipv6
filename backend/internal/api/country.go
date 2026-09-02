package api

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
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

// percentOf converts the stored share to a float for the wire.
//
// `country.percent` is NUMERIC(5,2) NOT NULL DEFAULT 0, so anything this
// rejects means the column holds what the schema says it cannot — a NaN
// written by a reworked rollup is the plausible route. Both call sites used
// to spell this `pct, _ := row.Percent.Float64Value()` (review issue 61).
//
// Checking that error would not have been enough: pgtype returns a NaN
// Numeric as `{Valid: true, Float64: NaN}` with a nil error, so the value
// flows straight through to the encoder — and encoding/json refuses NaN, so
// ONE bad row fails the marshal for the whole 251-row leaderboard. The
// explicit IsNaN/IsInf test, not the error, is what keeps that from being an
// outage.
//
// It serves 0 rather than failing the request. 0.0 on this surface reads as
// "no IPv6", which is a claim rather than an absence, so it says so in the
// log. The response shape is unchanged: making the field nullable would mean
// an OpenAPI change for a value the schema forbids to be null.
func percentOf(code string, n pgtype.Numeric) float64 {
	v, err := n.Float64Value()
	switch {
	case err != nil || !v.Valid:
		slog.Warn("country percent is not a usable number; serving 0",
			"country", strings.TrimSpace(code), "err", err)
		return 0
	case math.IsNaN(v.Float64) || math.IsInf(v.Float64, 0):
		slog.Warn("country percent is not a usable number; serving 0",
			"country", strings.TrimSpace(code), "value", v.Float64)
		return 0
	}
	return v.Float64
}

// listCountries is GET /countries — the country leaderboard (07 §4.5): a
// bounded set (~251 incl. the UN sentinel), served whole with an exact
// count; ?sort=percent (default) | v6_sites | sites, descending.
func (s *Server) listCountries(w http.ResponseWriter, r *http.Request) {
	ServeWhole(s, w, r, WholeSpec[CountryBody]{
		Sorts: []string{"percent", "v6_sites", "sites"},
		Fetch: func(ctx context.Context, sortKey string) ([]CountryBody, error) {
			rows, err := s.q.CountryLeaderboard(ctx)
			if err != nil {
				return nil, err
			}
			items := make([]CountryBody, len(rows))
			for i := range rows {
				items[i] = countryBody(strings.TrimSpace(rows[i].Code), rows[i].Name, rows[i].Tld,
					rows[i].Sites, rows[i].V6sites, percentOf(rows[i].Code, rows[i].Percent))
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
			return items, nil
		},
		CSV: writeCountriesCSV,
	})
}

// countryByPathCode resolves the {code} path param to its country row
// (path-parameter failure policy, 07 §2.8).
func (s *Server) countryByPathCode(w http.ResponseWriter, r *http.Request) (db.CountryByCodeRow, bool) {
	code := chi.URLParam(r, "code")
	if len(code) != 2 {
		NotFound(w, r, "Country not found", "Country codes are two-letter ISO 3166-1 alpha-2.")
		return db.CountryByCodeRow{}, false
	}
	row, err := s.q.CountryByCode(r.Context(), code)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Country not found", "No such country: "+strings.ToUpper(code))
		return row, false
	}
	if err != nil {
		InternalError(w, r, err)
		return row, false
	}
	return row, true
}

// getCountry is GET /countries/{code} (07 §4.5).
func (s *Server) getCountry(w http.ResponseWriter, r *http.Request) {
	row, ok := s.countryByPathCode(w, r)
	if !ok {
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
	body := countryBody(strings.TrimSpace(row.Code), row.Name, row.Tld, row.Sites, row.V6sites,
		percentOf(row.Code, row.Percent))
	body.Meta = &DetailMeta{AsOf: asOf.UTC(), Generation: generation}
	WriteJSON(w, http.StatusOK, body)
}

// listCountryDomains is GET /countries/{code}/domains — the country-scoped
// leaderboard, ≡ /domains?country={code} (07 §4.5).
func (s *Server) listCountryDomains(w http.ResponseWriter, r *http.Request) {
	row, ok := s.countryByPathCode(w, r)
	if !ok {
		return
	}
	s.serveDomainList(w, r, url.Values{paramCountry: {strings.TrimSpace(row.Code)}})
}
