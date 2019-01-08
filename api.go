package main

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi"
	"github.com/go-chi/render"
)

type apiResource struct{}

// Routes create a REST router for the log resource
func (rs apiResource) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", rs.List)                           // GET / - list all sites
	r.Get("/country", rs.CountryList)             // GET /country - list top 50 countries without ipv6
	r.Get("/country/{id}", rs.Country)            // GET /country/:id - list all sites for :country
	r.Get("/stats/country", rs.CountryStatList)   // GET /stats/country - list stats for all
	r.Get("/stats/country/{id}", rs.CountryStats) // GET /stats/country/{id} - lists stats for country
	r.Get("/stats/asn", rs.AsnList)               // GET /stats/asn - list asn
	r.Get("/stats/asn/{id}", rs.AsnStats)         // GET /stats/asn/{id} - lists stats for asn

	return r
}

// Lists all sites
func (rs apiResource) List(w http.ResponseWriter, r *http.Request) {
	var s []Site

	// Get offset/limit value from url params
	o := r.URL.Query().Get("offset")
	l := r.URL.Query().Get("limit")

	// Convert query params to int
	// Defaults if query param fails to convert, ignore errorhandling.
	offset, _ := strconv.Atoi(o)
	limit, _ := strconv.Atoi(l)

	s = getSites(offset, limit)
	render.JSON(w, r, s)
}

// List all sites for country
func (rs apiResource) Country(w http.ResponseWriter, r *http.Request) {
	countryCode := chi.URLParam(r, "id")

	//TODO: change err to if zero records found
	s := getCountry(countryCode)
	// if err != nil {
	// 	render.Status(r, http.StatusNotFound)
	// 	render.JSON(w, r, render.M{
	// 		"error": "record not found",
	// 	})
	// } else {
	render.JSON(w, r, s)
	//}
}

// List top countries without ipv6
func (rs apiResource) CountryList(w http.ResponseWriter, r *http.Request) {
	s := getCountryList()
	render.JSON(w, r, s)
}

// Lists stats for all sites
func (rs apiResource) CountryStatList(w http.ResponseWriter, r *http.Request) {
	s := getStats()
	render.JSON(w, r, s)
}

// Lists stats for country
func (rs apiResource) CountryStats(w http.ResponseWriter, r *http.Request) {
	countryCode := chi.URLParam(r, "id")
	s := getCountryStats(countryCode)
	render.JSON(w, r, s)
}

//
func (rs apiResource) AsnList(w http.ResponseWriter, r *http.Request) {
	s := getASNList("asn")
	render.JSON(w, r, s)
}

// Lists stats for country
func (rs apiResource) AsnStats(w http.ResponseWriter, r *http.Request) {
	asn := chi.URLParam(r, "id")

	// Convert query params to int
	a, _ := strconv.Atoi(asn)

	s := getASN(a)
	render.JSON(w, r, s)
}
