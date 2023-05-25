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

// CountryHandler is a handler for managing countries in the country service.
type CountryHandler struct {
	Repo *core.CountryService
}

// CountryResponse is a structured response containing country data.
type CountryResponse struct {
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	// CountryTld  string  `json:"country_tld"`
	Sites   int32   `json:"sites"`
	V6sites int32   `json:"v6sites"`
	Percent float64 `json:"percent"`
}

// Routes returns a router with all country-related endpoints mounted.
func (rs CountryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	// GET /country - Retrieve a list of all countries
	r.Get("/", rs.ListCountries)
	// GET /country/{code} - Retrieve all domains without IPv6 for a specific country by country code
	r.Get("/{code}", rs.DomainsWithoutIPv6ByCountryCode)
	// GET /country/{code}/heroes - Retrieve all domains with IPv6 for a specific country by country code
	r.Get("/{code}/heroes", rs.DomainsWithIPv6ByCountryCode)

	return r
}

// ListCountries retrieves and returns a list of all countries.
func (rs CountryHandler) ListCountries(w http.ResponseWriter, r *http.Request) {
	countries, err := rs.Repo.List(r.Context())
	if err != nil {
		log.Println("Error retrieving countries:", err)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}

	var countryList []CountryResponse
	for _, country := range countries {
		// Convert pgtype.Numeric to float64
		percent, err := strconv.ParseFloat(country.Percent.Int.String(), 64)
		if err != nil {
			log.Println("Error converting percentage to float64:", err)
			continue
		}
		percent /= 10

		countryList = append(countryList, CountryResponse{
			Country:     country.Country,
			CountryCode: country.CountryCode,
			Sites:       country.Sites,
			V6sites:     country.V6sites,
			Percent:     percent,
		})
	}
	render.JSON(w, r, countryList)
}

// DomainsWithoutIPv6ByCountryCode retrieves and returns a list of domains without IPv6 support for a given country code (e.g. US).
func (rs CountryHandler) DomainsWithoutIPv6ByCountryCode(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	country, err := rs.Repo.GetCountryCode(r.Context(), strings.ToUpper(code))
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Country not found"})
		return
	}

	// Retrieve the list of domains for the country.ID
	domains, err := rs.Repo.ListDomainsByCountry(r.Context(), country.ID)
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}
	var domainList []DomainResponse
	for _, domain := range domains {
		domainList = append(domainList, DomainResponse{
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
	render.JSON(w, r, domainList)
}

// DomainsWithIPv6ByCountryCode retrieves and returns a list of domains with IPv6 support for a given country code (e.g. US).
func (rs CountryHandler) DomainsWithIPv6ByCountryCode(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	country, err := rs.Repo.GetCountryCode(r.Context(), strings.ToUpper(code))
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Country not found"})
		return
	}

	// Retrieve the list of IPv6-supported domains (heroes) for the country.ID
	heroes, err := rs.Repo.ListDomainHeroesByCountry(r.Context(), country.ID)
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}
	var heroList []DomainResponse
	for _, domain := range heroes {
		heroList = append(heroList, DomainResponse{
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
	render.JSON(w, r, heroList)
}
