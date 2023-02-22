package rest

import (
	"net/http"
	"time"
	"whynoipv6/internal/core"

	"github.com/ggicci/httpin"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
)

// CampaignHandler is a handler for domain endpoints.
type CampaignHandler struct {
	Repo *core.CampaignService
}

// CampaignResponse is the response for a domain.
type CampaignResponse struct {
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

// CampaignListResponse represents a campaign.
type CampaignListResponse struct {
	ID int64 `json:"id"`
	// CreatedAt   time.Time `json:"created_at"`
	UUID        uuid.UUID `json:"uuid"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Count       int64     `json:"count"`
}

// Routes returns a router with all domain endpoints mounted.
func (rs CampaignHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.CampaignList)                                                   // GET /campaign - List all campaigns
	r.Get("/domain/{domain}", rs.ViewCampaignDomain)                              // GET /campaign/domain/domain.com - View single domain
	r.With(httpin.NewInput(PaginationInput{})).Get("/{uuid}", rs.CampaignDomains) // GET /campaign/{uuid} - list all domains for a campaign uuid
	return r
}

// CampaignList lists all domains.
func (rs CampaignHandler) CampaignList(w http.ResponseWriter, r *http.Request) {
	campaigns, err := rs.Repo.ListCampaign(r.Context())
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}
	var campaignlist []CampaignListResponse
	for _, campaign := range campaigns {
		campaignlist = append(campaignlist, CampaignListResponse{
			ID:          campaign.ID,
			UUID:        campaign.UUID,
			Name:        campaign.Name,
			Description: campaign.Description,
			Count:       campaign.Count,
		})
	}
	render.JSON(w, r, campaignlist)
}

// CampaignDomains lists all domains in a campaign.
func (rs CampaignHandler) CampaignDomains(w http.ResponseWriter, r *http.Request) {
	// Handle query params
	input := r.Context().Value(httpin.Input).(*PaginationInput)
	if input.Limit > 100 {
		input.Limit = 100
	}

	// Get domain from path
	campaign := chi.URLParam(r, "uuid")

	// Convert to uuid.UUID
	uuid, err := uuid.Parse(campaign)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid uuid"})
		return
	}

	domains, err := rs.Repo.ListCampaignDomain(r.Context(), uuid, int32(input.Offset), int32(input.Limit))
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}
	if len(domains) == 0 {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "campaign not found"})
		return
	}

	var domainlist []CampaignResponse
	for _, domain := range domains {
		domainlist = append(domainlist, CampaignResponse{
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

// ViewCampaignDomain lists a sigle domain in a campaign.
func (rs CampaignHandler) ViewCampaignDomain(w http.ResponseWriter, r *http.Request) {
	// Get domain from path
	paramdomain := chi.URLParam(r, "domain")
	domain, err := rs.Repo.ViewCampaignDomain(r.Context(), paramdomain)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "domain not found"})
		return
	}
	render.JSON(w, r, CampaignResponse{
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
