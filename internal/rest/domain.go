package rest

import (
	"net/http"
	"time"
	"whynoipv6/internal/core"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// DomainHandler is a handler for domain endpoints.
type DomainHandler struct {
	Repo *core.DomainService
}

// DomainResponse is the response for a domain.
type DomainResponse struct {
	Rank      int64     `json:"rank"`
	Domain    string    `json:"domain"`
	CheckAAAA bool      `json:"v6_aaaa"`
	CheckWWW  bool      `json:"v6_www"`
	CheckNS   bool      `json:"v6_ns"`
	CheckCurl bool      `json:"v6_curl"`
	AsName    string    `json:"asn"`
	Country   string    `json:"country"`
	TsAAAA    time.Time `json:"ts_aaaa"`
	TsWWW     time.Time `json:"ts_www"`
	TsNS      time.Time `json:"ts_ns"`
	TsCurl    time.Time `json:"ts_curl"`
	TsCheck   time.Time `json:"ts_check"`
	TsUpdated time.Time `json:"ts_updated"`
}

// Routes returns a router with all domain endpoints mounted.
func (rs DomainHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.DomainList)             // GET /domain - list top 100 domains without IPv6
	r.Get("/heroes", rs.DomainHeroes)     // GET /domain/heroes - list top 100 domains with IPv6
	r.Get("/{domain}", rs.DomainByDomain) // GET /domain/{domain} - read a domain by domain
	return r
}

// DomainList lists all domains.
func (rs DomainHandler) DomainList(w http.ResponseWriter, r *http.Request) {
	domains, err := rs.Repo.ListDomain(r.Context())
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

// DomainHeroes lists all domains with IPv6.
func (rs DomainHandler) DomainHeroes(w http.ResponseWriter, r *http.Request) {
	domains, err := rs.Repo.ListDomainHeroes(r.Context())
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

// DomainByDomain returns a domain by domain.
func (rs DomainHandler) DomainByDomain(w http.ResponseWriter, r *http.Request) {
	d := chi.URLParam(r, "domain")
	domain, err := rs.Repo.ViewDomain(r.Context(), d)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "domain not found"})
		return
	}
	render.JSON(w, r, DomainResponse{
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
