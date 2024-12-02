package rest

import (
	"log"
	"net/http"
	"strings"
	"time"
	"whynoipv6/internal/core"

	"github.com/ggicci/httpin"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// DomainHandler is a handler for managing domain-related operations.
type DomainHandler struct {
	Repo *core.DomainService
}

// DomainResponse is the response structure for a domain.
type DomainResponse struct {
	Rank         int64     `json:"rank"`
	Domain       string    `json:"domain"`
	BaseDomain   string    `json:"base_domain"`
	WwwDomain    string    `json:"www_domain"`
	Nameserver   string    `json:"nameserver"`
	MXRecord     string    `json:"mx_record"`
	V6Only       string    `json:"v6_only"`
	AsName       string    `json:"asn"`
	Country      string    `json:"country"`
	TsBaseDomain time.Time `json:"ts_aaaa"`
	TsWwwDomain  time.Time `json:"ts_www"`
	TsNameserver time.Time `json:"ts_ns"`
	TsMXRecord   time.Time `json:"ts_mx"`
	TsV6Only     time.Time `json:"ts_curl"`
	TsCheck      time.Time `json:"ts_check"`
	TsUpdated    time.Time `json:"ts_updated"`
	CampaignUUID string    `json:"campaign_uuid,omitempty"`
}

// DomainLogResponse is the response structure for a domain log.
type DomainLogResponse struct {
	ID         int64     `json:"id"`
	Time       time.Time `json:"time"`
	BaseDomain string    `json:"base_domain"`
	WwwDomain  string    `json:"www_domain"`
	Nameserver string    `json:"nameserver"`
	MXRecord   string    `json:"mx_record"`
}

// Routes returns a router with all domain-related endpoints mounted.
func (rs DomainHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// GET /domain - list all domains
	r.With(httpin.NewInput(PaginationInput{})).Get("/", rs.DomainList)
	// GET /domain/heroes - list the domains with IPv6
	r.With(httpin.NewInput(PaginationInput{})).Get("/heroes", rs.DomainHeroes)
	// GET /domain/topsinner - list the top 10-ish domains without IPv6
	r.Get("/topsinner", rs.TopSinner)
	// GET /domain/{domain} - retrieve a domain by its name
	r.Get("/{domain}", rs.RetrieveDomain)
	// GET /domain/{domain}/log - retrieve a domain by its name
	r.Get("/{domain}/log", rs.GetDomainLog)
	// GET /domain/search/{domain} - search for a domain by its name
	r.With(httpin.NewInput(PaginationInput{})).Get("/search/{domain}", rs.SearchDomain)

	return r
}

// DomainList returns all domains.
func (rs DomainHandler) DomainList(w http.ResponseWriter, r *http.Request) {
	// Handle query params
	paginationInput := r.Context().Value(httpin.Input).(*PaginationInput)
	if paginationInput.Limit > 100 {
		paginationInput.Limit = 100
	}

	domains, err := rs.Repo.ListDomain(r.Context(), paginationInput.Offset, paginationInput.Limit)
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}
	var domainlist []DomainResponse
	for _, domain := range domains {
		domainlist = append(domainlist, DomainResponse{
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
	render.JSON(w, r, domainlist)
}

// DomainHeroes returns the domains with IPv6 support.
func (rs DomainHandler) DomainHeroes(w http.ResponseWriter, r *http.Request) {
	// Handle query params
	paginationInput := r.Context().Value(httpin.Input).(*PaginationInput)
	if paginationInput.Limit > 100 {
		paginationInput.Limit = 100
	}

	domains, err := rs.Repo.ListDomainHeroes(r.Context(), paginationInput.Offset, paginationInput.Limit)
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}
	var domainlist []DomainResponse
	for _, domain := range domains {
		domainlist = append(domainlist, DomainResponse{
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
	render.JSON(w, r, domainlist)
}

// RetrieveDomain returns a domain based on the provided domain name.
func (rs DomainHandler) RetrieveDomain(w http.ResponseWriter, r *http.Request) {
	d := chi.URLParam(r, "domain")
	domain, err := rs.Repo.ViewDomain(r.Context(), d)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "domain not found"})
		return
	}
	render.JSON(w, r, DomainResponse{
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

// SearchDomain returns a domain based on the provided domain name.
func (rs DomainHandler) SearchDomain(w http.ResponseWriter, r *http.Request) {
	// Handle query params
	paginationInput := r.Context().Value(httpin.Input).(*PaginationInput)
	if paginationInput.Limit > 100 {
		paginationInput.Limit = 100
	}

	domain := chi.URLParam(r, "domain")

	domains, err := rs.Repo.GetDomainsByName(r.Context(), strings.ToLower(domain), paginationInput.Offset, paginationInput.Limit)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Internal server error"})
		return
	}

	if len(domains) == 0 {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "no domains found"})
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

	render.JSON(w, r, render.M{
		"data": domainList,
	})
}

// TopSinner returns the top 10-ish domains without IPv6 support.
func (rs DomainHandler) TopSinner(w http.ResponseWriter, r *http.Request) {
	domains, err := rs.Repo.ListDomainShamers(r.Context())
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		log.Println("Error listing domain shamers:", err)
		return
	}
	var domainlist []DomainResponse
	for _, domain := range domains {
		domainlist = append(domainlist, DomainResponse{
			Rank:         domain.ID,
			Domain:       domain.Site,
			BaseDomain:   domain.BaseDomain,
			WwwDomain:    domain.WwwDomain,
			Nameserver:   domain.Nameserver,
			MXRecord:     domain.MXRecord,
			V6Only:       domain.V6Only,
			TsBaseDomain: domain.TsBaseDomain,
			TsWwwDomain:  domain.TsWwwDomain,
			TsNameserver: domain.TsNameserver,
			TsMXRecord:   domain.TsMXRecord,
			TsV6Only:     domain.TsV6Only,
			TsCheck:      domain.TsCheck,
			TsUpdated:    domain.TsUpdated,
		})
	}
	render.JSON(w, r, domainlist)
}

// GetDomainLog returns the crawler log for a domain.
func (rs DomainHandler) GetDomainLog(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")
	logs, err := rs.Repo.GetDomainLog(r.Context(), domain)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "domain not found"})
		return
	}
	var domainlist []DomainLogResponse
	for _, log := range logs {
		var data map[string]interface{}
		if err := log.Data.AssignTo(&data); err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, render.M{"error": "internal server error"})
			return
		}
		domainlist = append(domainlist, DomainLogResponse{
			ID:         log.ID,
			Time:       log.Time,
			BaseDomain: data["base_domain"].(string),
			WwwDomain:  data["www_domain"].(string),
			Nameserver: data["nameserver"].(string),
			MXRecord:   data["mx_record"].(string),
		})
	}
	render.JSON(w, r, domainlist)
}
