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

// Routes returns a router with all campaign endpoints mounted.
func (rs CampaignHandler) Routes() chi.Router {
	// Create a new Chi router instance
	r := chi.NewRouter()

	// Mount the campaign endpoints
	r.Get("/", rs.CampaignList)                                                   // GET /campaign - List all campaigns
	r.Get("/domain/{domain}", rs.ViewCampaignDomain)                              // GET /campaign/domain/{domain} - View details of a single domain
	r.With(httpin.NewInput(PaginationInput{})).Get("/{uuid}", rs.CampaignDomains) // GET /campaign/{uuid} - List all domains for a given campaign UUID

	// Return the router with the mounted endpoints
	return r
}

// CampaignList retrieves and lists all campaigns.
func (rs CampaignHandler) CampaignList(w http.ResponseWriter, r *http.Request) {
	// Retrieve all campaigns from the repository
	allCampaigns, err := rs.Repo.ListCampaign(r.Context())
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "Internal server error"})
		return
	}

	// Prepare the response with campaign details
	var campaignList []CampaignListResponse
	for _, campaign := range allCampaigns {
		campaignList = append(campaignList, CampaignListResponse{
			ID:          campaign.ID,
			UUID:        campaign.UUID,
			Name:        campaign.Name,
			Description: campaign.Description,
			Count:       campaign.Count,
		})
	}

	// Send campaign list as JSON response
	render.JSON(w, r, campaignList)
}

// CampaignDomains lists all domains in a campaign.
func (rs CampaignHandler) CampaignDomains(w http.ResponseWriter, r *http.Request) {
	// Handle query params
	paginationInput := r.Context().Value(httpin.Input).(*PaginationInput)
	if paginationInput.Limit > 100 {
		paginationInput.Limit = 100
	}

	// Get campaign UUID from path
	campaignUUID := chi.URLParam(r, "uuid")

	// Convert campaignUUID to uuid.UUID
	parsedUUID, err := uuid.Parse(campaignUUID)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid UUID"})
		return
	}

	// Retrieve domains associated with the campaign
	domains, err := rs.Repo.ListCampaignDomain(r.Context(), parsedUUID, int32(paginationInput.Offset), int32(paginationInput.Limit))
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "Internal server error"})
		return
	}
	if len(domains) == 0 {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Campaign not found"})
		return
	}

	var domainList []CampaignResponse
	for _, domain := range domains {
		domainList = append(domainList, CampaignResponse{
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

// ViewCampaignDomain retrieves a single domain in a campaign.
func (rs CampaignHandler) ViewCampaignDomain(w http.ResponseWriter, r *http.Request) {
	// Get domain from path
	paramDomain := chi.URLParam(r, "domain")

	// Retrieve domain details from the repository
	domainDetails, err := rs.Repo.ViewCampaignDomain(r.Context(), paramDomain)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Domain not found"})
		return
	}

	// Send domain details as JSON response
	render.JSON(w, r, CampaignResponse{
		Domain:    domainDetails.Site,
		CheckAAAA: domainDetails.CheckAAAA,
		CheckWWW:  domainDetails.CheckWWW,
		CheckNS:   domainDetails.CheckNS,
		CheckCurl: domainDetails.CheckCurl,
		AsName:    domainDetails.AsName,
		Country:   domainDetails.Country,
		TsAAAA:    domainDetails.TsAAAA,
		TsWWW:     domainDetails.TsWWW,
		TsNS:      domainDetails.TsNS,
		TsCurl:    domainDetails.TsCurl,
		TsCheck:   domainDetails.TsCheck,
		TsUpdated: domainDetails.TsUpdated,
	})
}
