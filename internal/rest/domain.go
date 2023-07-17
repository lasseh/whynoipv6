package rest

import (
	"net/http"
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
	CheckAAAA    bool      `json:"v6_aaaa"`
	CheckWWW     bool      `json:"v6_www"`
	CheckNS      bool      `json:"v6_ns"`
	CheckCurl    bool      `json:"v6_curl"`
	AsName       string    `json:"asn"`
	Country      string    `json:"country"`
	TsAAAA       time.Time `json:"ts_aaaa"`
	TsWWW        time.Time `json:"ts_www"`
	TsNS         time.Time `json:"ts_ns"`
	TsCurl       time.Time `json:"ts_curl"`
	TsCheck      time.Time `json:"ts_check"`
	TsUpdated    time.Time `json:"ts_updated"`
	CampaignUUID string    `json:"campaign_uuid,omitempty"`
}

// Routes returns a router with all domain-related endpoints mounted.
func (rs DomainHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// GET /domain - list top 100 domains without IPv6
	r.Get("/", rs.DomainList)
	// GET /domain/heroes - list top 100 domains with IPv6
	r.Get("/heroes", rs.DomainHeroes)
	// GET /domain/shamers - list the top 10-ish domains without IPv6
	r.Get("/shamers", rs.DomainShamers)
	// GET /domain/{domain} - retrieve a domain by its name
	r.Get("/{domain}", rs.RetrieveDomain)
	// GET /domain/search/{domain} - search for a domain by its name
	r.With(httpin.NewInput(PaginationInput{})).Get("/search/{domain}", rs.SearchDomain)

	return r
}

// DomainList returns the top 100 domains without IPv6 support.
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

// DomainHeroes returns the top 100 domains with IPv6 support.
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

// SearchDomain returns a domain based on the provided domain name.
func (rs DomainHandler) SearchDomain(w http.ResponseWriter, r *http.Request) {
	// Handle query params
	paginationInput := r.Context().Value(httpin.Input).(*PaginationInput)
	if paginationInput.Limit > 100 {
		paginationInput.Limit = 100
	}

	domain := chi.URLParam(r, "domain")

	domains, err := rs.Repo.GetDomainsByName(r.Context(), domain, int32(paginationInput.Offset), int32(paginationInput.Limit))
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Internal server error"})
		return
	}

	// Search for campaign domains
	campaignDomains, err := rs.Repo.GetCampaignDomainsByName(r.Context(), domain, int32(paginationInput.Offset), int32(paginationInput.Limit))
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Internal server error"})
		return
	}
	// Append campaign domains to result
	// result = append(result, campaignDomains...)

	if len(domains) == 0 && len(campaignDomains) == 0 {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "no domains found"})
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

	var campaignDomainList []DomainResponse
	for _, domain := range campaignDomains {
		campaignDomainList = append(campaignDomainList, DomainResponse{
			Domain:       domain.Site,
			CheckAAAA:    domain.CheckAAAA,
			CheckWWW:     domain.CheckWWW,
			CheckNS:      domain.CheckNS,
			CheckCurl:    domain.CheckCurl,
			AsName:       domain.AsName,
			Country:      domain.Country,
			TsAAAA:       domain.TsAAAA,
			TsWWW:        domain.TsWWW,
			TsNS:         domain.TsNS,
			TsCurl:       domain.TsCurl,
			TsCheck:      domain.TsCheck,
			TsUpdated:    domain.TsUpdated,
			CampaignUUID: domain.CampaignID.String(),
		})
	}

	render.JSON(w, r, render.M{
		"domains":         domainList,
		"campaignDomains": campaignDomainList,
	})
}

// DomainShamers returns the top 10-ish domains without IPv6 support.
func (rs DomainHandler) DomainShamers(w http.ResponseWriter, r *http.Request) {
	domains, err := rs.Repo.ListDomainShamers(r.Context())
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}
	var domainlist []DomainResponse
	for _, domain := range domains {
		domainlist = append(domainlist, DomainResponse{
			Rank:      domain.ID,
			Domain:    domain.Site,
			CheckAAAA: domain.CheckAAAA,
			CheckWWW:  domain.CheckWWW,
			CheckNS:   domain.CheckNS,
			CheckCurl: domain.CheckCurl,
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
