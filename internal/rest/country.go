package rest

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"whynoipv6/internal/core"

	"github.com/ggicci/httpin"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// CountryHandler is a handler for managing countries in the country service.
type CountryHandler struct {
	Repo *core.CountryService
}

// CountryResponse is a structured response containing country data.
type CountryResponse struct {
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Sites       int32   `json:"sites"`
	V6sites     int32   `json:"v6sites"`
	Percent     float64 `json:"percent"`
}

// Routes returns a router with all country-related endpoints mounted.
func (rs CountryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	// GET /country - Retrieve a list of all countries
	r.Get("/", rs.CountryList)

	// GET /country/{code} - Retrieve information about a specific country
	r.Get("/{code}", rs.CountryInfo)

	// GET /country/{code}/sinners - Retrieve all domains without IPv6 for a specific country by country code
	r.With(httpin.NewInput(PaginationInput{})).Get("/{code}/sinners", rs.CountrySinners)

	// GET /country/{code}/heroes - Retrieve all domains with IPv6 for a specific country by country code
	r.With(httpin.NewInput(PaginationInput{})).Get("/{code}/heroes", rs.CountryHeroes)

	return r
}

// CountryList retrieves and returns a list of all countries.
func (rs CountryHandler) CountryList(w http.ResponseWriter, r *http.Request) {
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

// CountryInfo retrieves and returns information about a specific country and all the domains it has.
func (rs CountryHandler) CountryInfo(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	// Get the country information
	countryInfo, err := rs.Repo.GetCountryCode(r.Context(), strings.ToUpper(code))
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Country not found"})
		return
	}
	// Convert pgtype.Numeric to float64
	percent, err := strconv.ParseFloat(countryInfo.Percent.Int.String(), 64)
	if err != nil {
		log.Println("Error converting percentage to float64:", err)
	}
	percent /= 10

	// Build the response
	countryRespose := CountryResponse{
		Country:     countryInfo.Country,
		CountryCode: countryInfo.CountryCode,
		Sites:       countryInfo.Sites,
		V6sites:     countryInfo.V6sites,
		Percent:     percent,
	}

	render.JSON(w, r, countryRespose)
}

// CountrySinners retrieves and returns a list of domains without IPv6 support for a given country code (e.g. US).
func (rs CountryHandler) CountrySinners(w http.ResponseWriter, r *http.Request) {
	// Handle query params
	paginationInput := r.Context().Value(httpin.Input).(*PaginationInput)
	if paginationInput.Limit > 100 {
		paginationInput.Limit = 100
	}
	code := chi.URLParam(r, "code")

	country, err := rs.Repo.GetCountryCode(r.Context(), strings.ToUpper(code))
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Country not found"})
		return
	}

	// Retrieve the list of domains for the country.ID
	domains, err := rs.Repo.ListDomainsByCountry(
		r.Context(),
		country.ID,
		paginationInput.Offset,
		paginationInput.Limit,
	)
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}
	var domainList []DomainResponse
	for _, domain := range domains {
		domainList = append(domainList, DomainResponse{
			Rank:         domain.Rank,
			Domain:       domain.Site,
			BaseDomain:   domain.BaseDomain,
			WwwDomain:    domain.WwwDomain,
			Nameserver:   domain.Nameserver,
			MXRecord:     domain.MXRecord,
			V6Only:       domain.V6Only,
			AsName:       domain.AsName,
			Country:      domain.Country,
			TsBaseDomain: domain.TsBaseDomain,
			TsWwwDomain:  domain.TsWwwDomain,
			TsNameserver: domain.TsNameserver,
			TsMXRecord:   domain.TsMXRecord,
			TsV6Only:     domain.TsV6Only,
			TsCheck:      domain.TsCheck,
			TsUpdated:    domain.TsUpdated,
		})
	}
	render.JSON(w, r, domainList)
}

// CountryHeroes retrieves and returns a list of domains with IPv6 support for a given country code (e.g. US).
func (rs CountryHandler) CountryHeroes(w http.ResponseWriter, r *http.Request) {
	// Handle query params
	paginationInput := r.Context().Value(httpin.Input).(*PaginationInput)
	if paginationInput.Limit > 100 {
		paginationInput.Limit = 100
	}
	code := chi.URLParam(r, "code")

	country, err := rs.Repo.GetCountryCode(r.Context(), strings.ToUpper(code))
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Country not found"})
		return
	}

	// Retrieve the list of IPv6-supported domains (heroes) for the country.ID
	heroes, err := rs.Repo.ListDomainHeroesByCountry(
		r.Context(),
		country.ID,
		paginationInput.Offset,
		paginationInput.Limit,
	)
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}
	var heroList []DomainResponse
	for _, domain := range heroes {
		heroList = append(heroList, DomainResponse{
			Rank:         domain.Rank,
			Domain:       domain.Site,
			BaseDomain:   domain.BaseDomain,
			WwwDomain:    domain.WwwDomain,
			Nameserver:   domain.Nameserver,
			MXRecord:     domain.MXRecord,
			V6Only:       domain.V6Only,
			AsName:       domain.AsName,
			Country:      domain.Country,
			TsBaseDomain: domain.TsBaseDomain,
			TsWwwDomain:  domain.TsWwwDomain,
			TsNameserver: domain.TsNameserver,
			TsMXRecord:   domain.TsMXRecord,
			TsV6Only:     domain.TsV6Only,
			TsCheck:      domain.TsCheck,
			TsUpdated:    domain.TsUpdated,
		})
	}
	render.JSON(w, r, heroList)
}
