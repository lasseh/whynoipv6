package rest

import (
	"net/http"
	"regexp"
	"strings"
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
}

// CampaignListResponse represents a campaign.
type CampaignListResponse struct {
	ID          int64  `json:"id"`
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Count       int64  `json:"count"`
	V6Ready     int64  `json:"v6_ready"`
}

// CampaignDomainLogResponse is the response structure for a domain log.
type CampaignDomainLogResponse struct {
	ID         int64     `json:"id"`
	Time       time.Time `json:"time"`
	BaseDomain string    `json:"base_domain"`
	WwwDomain  string    `json:"www_domain"`
	Nameserver string    `json:"nameserver"`
	MXRecord   string    `json:"mx_record"`
}

// Routes returns a router with all campaign endpoints mounted.
func (rs CampaignHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// GET /campaign - List all campaigns
	r.Get("/", rs.CampaignList)
	// GET /campaign/{uuid} - List all domains for a given campaign UUID
	r.With(httpin.NewInput(PaginationInput{})).Get("/{uuid}", rs.CampaignDomains)
	// GET /campaign/{campaign}/{domain} - View details of a single domain in a campaign
	r.Get("/{uuid}/{domain}", rs.ViewCampaignDomain)
	// GET /campaign/{campaign}/{domain}/log - View crawler of a single domain in a campaign
	r.Get("/{uuid}/{domain}/log", rs.GetCampaignDomainLog)
	// GET /campaign/search/{domain} - search for a domain by its name
	r.With(httpin.NewInput(PaginationInput{})).Get("/search/{domain}", rs.SearchDomain)

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
			UUID:        encodeUUID(campaign.UUID),
			Name:        campaign.Name,
			Description: campaign.Description,
			Count:       campaign.Count,
			V6Ready:     campaign.V6Ready,
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
	// Decode uuid from shortuuid to google uuid
	decodeID, err := decodeUUID(campaignUUID)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Invalid UUID"})
		return
	}
	// Convert campaignUUID to uuid.UUID
	parsedUUID, err := uuid.Parse(decodeID.String())
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Invalid UUID"})
		return
	}

	// Retrieve campaign details from the repository
	campaignDetails, err := rs.Repo.GetCampaign(r.Context(), parsedUUID)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Campaign not found"})
		return
	}

	// Retrieve domains associated with the campaign
	domains, err := rs.Repo.ListCampaignDomain(r.Context(), parsedUUID, paginationInput.Offset, paginationInput.Limit)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Campaign not found"})
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

	// Connect campaign details with domain list
	campaignList := struct {
		Campaign CampaignListResponse `json:"campaign"`
		Domains  []CampaignResponse   `json:"domains"`
	}{
		Campaign: CampaignListResponse{
			ID:          campaignDetails.ID,
			UUID:        encodeUUID(campaignDetails.UUID),
			Name:        campaignDetails.Name,
			Description: campaignDetails.Description,
			Count:       campaignDetails.Count,
			V6Ready:     campaignDetails.V6Ready,
		},
		Domains: domainList,
	}

	render.JSON(w, r, campaignList)
}

// ViewCampaignDomain retrives a single domain in a campaign.
func (rs CampaignHandler) ViewCampaignDomain(w http.ResponseWriter, r *http.Request) {
	// Get campaign UUID and domain from path
	campaignUUID := chi.URLParam(r, "uuid")
	site := chi.URLParam(r, "domain")

	// Decode uuid from shortuuid to google uuid
	decodeID, err := decodeUUID(campaignUUID)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid UUID"})
		return
	}
	// Validate and parse the UUID
	uuid, err := uuid.Parse(decodeID.String())
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid UUID"})
		return
	}

	// Validate the domain
	// TODO: Move this to core package
	if !regexp.MustCompile(`^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$`).MatchString(site) {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid domain"})
		return
	}

	// Retrieve domain details from the repository
	domainDetails, err := rs.Repo.ViewCampaignDomain(r.Context(), uuid, site)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Domain not found"})
		return
	}

	// If no changelogs are found, return 404
	if len(domainDetails.Site) == 0 {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "No changelog entries found for campaign " + campaignUUID + " and domain " + site})
		return
	}

	// Send domain details as JSON response
	render.JSON(w, r, CampaignResponse{
		Domain:       domainDetails.Site,
		BaseDomain:   domainDetails.BaseDomain,
		WwwDomain:    domainDetails.WwwDomain,
		Nameserver:   domainDetails.Nameserver,
		MXRecord:     domainDetails.MXRecord,
		V6Only:       domainDetails.V6Only,
		AsName:       domainDetails.AsName,
		Country:      domainDetails.Country,
		TsBaseDomain: domainDetails.TsBaseDomain,
		TsWwwDomain:  domainDetails.TsWwwDomain,
		TsNameserver: domainDetails.TsNameserver,
		TsMXRecord:   domainDetails.TsMXRecord,
		TsV6Only:     domainDetails.TsV6Only,
		TsCheck:      domainDetails.TsCheck,
		TsUpdated:    domainDetails.TsUpdated,
	})
}

// SearchDomain returns a domain based on the provided domain name.
func (rs CampaignHandler) SearchDomain(w http.ResponseWriter, r *http.Request) {
	// Handle query params
	paginationInput := r.Context().Value(httpin.Input).(*PaginationInput)
	if paginationInput.Limit > 100 {
		paginationInput.Limit = 100
	}

	domain := chi.URLParam(r, "domain")

	// Search for campaign domains
	campaignDomains, err := rs.Repo.GetCampaignDomainsByName(r.Context(), strings.ToLower(domain), paginationInput.Offset, paginationInput.Limit)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "No domains found"})
		return
	}

	if len(campaignDomains) == 0 {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "No domains found"})
		return
	}

	var campaignDomainList []DomainResponse
	for _, domain := range campaignDomains {
		campaignDomainList = append(campaignDomainList, DomainResponse{
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
			CampaignUUID: encodeUUID(domain.CampaignID),
		})
	}

	render.JSON(w, r, render.M{
		"data": campaignDomainList,
	})
}

// GetCampaignDomainLog returns the crawler log for a domain.
func (rs CampaignHandler) GetCampaignDomainLog(w http.ResponseWriter, r *http.Request) {
	// Get campaign UUID and domain from path
	campaignUUID := chi.URLParam(r, "uuid")
	domain := chi.URLParam(r, "domain")

	// Decode uuid from shortuuid to google uuid
	decodeID, err := decodeUUID(campaignUUID)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid UUID"})
		return
	}
	// Validate and parse the UUID
	uuid, err := uuid.Parse(decodeID.String())
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid UUID"})
		return
	}

	logs, err := rs.Repo.GetCampaignDomainLog(r.Context(), uuid, domain)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "domain not found"})
		return
	}
	var domainlist []CampaignDomainLogResponse
	for _, log := range logs {
		var data map[string]any
		if err := log.Data.AssignTo(&data); err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, render.M{"error": "internal server error"})
			return
		}
		domainlist = append(domainlist, CampaignDomainLogResponse{
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
