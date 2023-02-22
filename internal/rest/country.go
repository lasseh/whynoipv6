package rest

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"whynoipv6/internal/core"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// CountryHandler is a handler for the country service.
type CountryHandler struct {
	Repo *core.CountryService
}

// CountryResponse is a response for a country.
type CountryResponse struct {
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	// CountryTld  string  `json:"country_tld"`
	Sites   int32   `json:"sites"`
	V6sites int32   `json:"v6sites"`
	Percent float64 `json:"percent"`
}

// Routes returns a router with all country endpoints mounted.
func (rs CountryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.CountryList)               // GET /country - list all countries
	r.Get("/{code}", rs.CountryByCode)       // GET /country/{code} - list all domains without IPv6 for a country
	r.Get("/{code}/heroes", rs.HeroesByCode) // GET /country/{code}/heroes - list all domains with IPv6 for a country
	return r
}

// CountryList lists all countries.
func (rs CountryHandler) CountryList(w http.ResponseWriter, r *http.Request) {
	countries, err := rs.Repo.List(r.Context())
	if err != nil {
		//TODO: Log this to a logging service
		log.Println("Error listing countries:", err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}

	var countrylist []CountryResponse
	for _, country := range countries {
		// Convert pgtype.Numeric to float64
		var percent float64
		percent, err = strconv.ParseFloat(country.Percent.Int.String(), 64)
		percent /= 10
		if err != nil {
			log.Println("Error converting percent to float64")
		}

		countrylist = append(countrylist, CountryResponse{
			Country:     country.Country,
			CountryCode: country.CountryCode,
			Sites:       country.Sites,
			V6sites:     country.V6sites,
			Percent:     percent,
		})
	}
	render.JSON(w, r, countrylist)
}

// CountryByCode gets a country entry by code (e.g. US).
func (rs CountryHandler) CountryByCode(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	country, err := rs.Repo.GetCountryCode(r.Context(), strings.ToUpper(code))
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Country not found"})
		return
	}

	// Get list of domains for country.id
	domains, err := rs.Repo.ListDomainsByCountry(r.Context(), country.ID)
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}
	var domainlist []DomainResponse
	for _, domain := range domains {
		domainlist = append(domainlist, DomainResponse{
			Rank:      domain.Rank,
			Domain:    domain.Site,
			CheckAAAA: domain.CheckAAAA,
			CheckWWW:  domain.CheckWWW,
			CheckNS:   domain.CheckNS,
			CheckCurl: domain.CheckCurl,
			AsName:    domain.AsName,
			Country:   domain.Country,
			TsAAAA:    domain.TsAAAA,
			TsWWW:     domain.TsWWW,
			TsNS:      domain.TsNS,
			TsCurl:    domain.TsCurl,
			TsCheck:   domain.TsCheck,
			TsUpdated: domain.TsUpdated,
		})
	}
	render.JSON(w, r, domainlist)
}

// HeroesByCode gets a country entry by code (e.g. US).
func (rs CountryHandler) HeroesByCode(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	country, err := rs.Repo.GetCountryCode(r.Context(), strings.ToUpper(code))
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Country not found"})
		return
	}

	// Get list of heroes from country.id
	heroes, err := rs.Repo.ListDomainHeroesByCountry(r.Context(), country.ID)
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}
	var herolist []DomainResponse
	for _, domain := range heroes {
		herolist = append(herolist, DomainResponse{
			Rank:      domain.Rank,
			Domain:    domain.Site,
			CheckAAAA: domain.CheckAAAA,
			CheckWWW:  domain.CheckWWW,
			CheckNS:   domain.CheckNS,
			CheckCurl: domain.CheckCurl,
			AsName:    domain.AsName,
			Country:   domain.Country,
			TsAAAA:    domain.TsAAAA,
			TsWWW:     domain.TsWWW,
			TsNS:      domain.TsNS,
			TsCurl:    domain.TsCurl,
			TsCheck:   domain.TsCheck,
			TsUpdated: domain.TsUpdated,
		})
	}
	render.JSON(w, r, herolist)
}
