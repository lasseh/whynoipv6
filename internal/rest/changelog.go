package rest

import (
	"net/http"
	"regexp"
	"time"
	"whynoipv6/internal/core"

	"github.com/ggicci/httpin"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
)

// ChangelogHandler is a handler for changelog endpoints.
type ChangelogHandler struct {
	Repo *core.ChangelogService
}

// ChangelogResponse is the response for a changelog.
type ChangelogResponse struct {
	ID         int64     `json:"id"`
	Ts         time.Time `json:"ts"`
	Domain     string    `json:"domain"`
	DomainURL  string    `json:"domain_url"`
	Message    string    `json:"message"`
	IPv6Status string    `json:"ipv6_status"`
}

// Routes returns a router with all changelog endpoints mounted.
func (rs ChangelogHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// GET /changelog - List all changelog entries
	r.With(httpin.NewInput(PaginationInput{})).Get("/", rs.ChangelogList)
	// GET /changelog/campaign - List all campaign changelog entries
	r.With(httpin.NewInput(PaginationInput{})).Get("/campaign", rs.CampaignChangelogList)
	// GET /changelog/{domain} - List all changelog entries for a specific domain
	r.With(httpin.NewInput(PaginationInput{})).Get("/{domain}", rs.ChangelogByDomain)
	// GET /changelog/campaign/{uuid} - List all changelog entries for a specific campaign UUID
	r.With(httpin.NewInput(PaginationInput{})).Get("/campaign/{uuid}", rs.ChangelogByCampaign)
	// GET /changelog/campaign/{uuid}/{domain} - List all changelog entries for a specific domain within a campaign UUID
	r.With(httpin.NewInput(PaginationInput{})).Get("/campaign/{uuid}/{domain}", rs.ChangelogByCampaignDomain)

	return r
}

// ChangelogList lists all changelog entries with pagination.
func (rs ChangelogHandler) ChangelogList(w http.ResponseWriter, r *http.Request) {
	// Retrieve pagination input from context
	paginationInput := r.Context().Value(httpin.Input).(*PaginationInput)

	// Limit the maximum number of entries per page to 100
	if paginationInput.Limit > 100 {
		paginationInput.Limit = 100
	}

	// Fetch changelogs from the repository
	changelogs, err := rs.Repo.List(r.Context(), paginationInput.Offset, paginationInput.Limit)
	if err != nil {
		// Handle any errors while fetching changelogs
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}

	// Convert changelogs to ChangelogResponse objects
	var changelogList []ChangelogResponse
	for _, changelog := range changelogs {
		changelogList = append(changelogList, ChangelogResponse{
			ID:         changelog.ID,
			Ts:         changelog.Ts,
			Domain:     changelog.Site,
			DomainURL:  "/domain/" + changelog.Site,
			Message:    changelog.Message,
			IPv6Status: changelog.IPv6Status,
		})
	}

	// Send the changelog list as JSON
	render.JSON(w, r, changelogList)
}

// CampaignChangelogList lists all changelog entries with pagination.
func (rs ChangelogHandler) CampaignChangelogList(w http.ResponseWriter, r *http.Request) {
	// Retrieve pagination input from context
	paginationInput := r.Context().Value(httpin.Input).(*PaginationInput)

	// Limit the maximum number of entries per page to 100
	if paginationInput.Limit > 100 {
		paginationInput.Limit = 100
	}

	// Fetch changelogs from the repository
	changelogs, err := rs.Repo.CampaignList(r.Context(), paginationInput.Offset, paginationInput.Limit)
	if err != nil {
		// Handle any errors while fetching changelogs
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}

	// Convert changelogs to ChangelogResponse objects
	var changelogList []ChangelogResponse
	for _, changelog := range changelogs {
		changelogList = append(changelogList, ChangelogResponse{
			ID:         changelog.ID,
			Ts:         changelog.Ts,
			Domain:     changelog.Site,
			DomainURL:  "/campaign/" + string(changelog.CampaignID.String()) + "/" + changelog.Site,
			Message:    changelog.Message,
			IPv6Status: changelog.IPv6Status,
		})
	}

	// Send the changelog list as JSON
	render.JSON(w, r, changelogList)
}

// ChangelogByDomain lists all changelog entries for a specific domain with pagination.
func (rs ChangelogHandler) ChangelogByDomain(w http.ResponseWriter, r *http.Request) {
	// Retrieve pagination input from context
	paginationInput := r.Context().Value(httpin.Input).(*PaginationInput)

	// Limit the maximum number of entries per page to 100
	if paginationInput.Limit > 100 {
		paginationInput.Limit = 100
	}

	// Get domain from path
	site := chi.URLParam(r, "domain")

	// Validate domain
	// TODO: Move this to core package
	if !regexp.MustCompile(`^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$`).MatchString(site) {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid domain"})
		return
	}

	// Fetch changelogs for the domain from the repository
	changelogs, err := rs.Repo.GetChangelogByDomain(r.Context(), site, paginationInput.Offset, paginationInput.Limit)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Unable to find changelog entries for " + site})
		return
	}

	// If no changelogs are found, return 404
	if len(changelogs) == 0 {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "No changelog entries found for " + site})
		return
	}

	// Convert changelogs to ChangelogResponse objects
	var changelogList []ChangelogResponse
	for _, changelog := range changelogs {
		changelogList = append(changelogList, ChangelogResponse{
			ID:         changelog.ID,
			Ts:         changelog.Ts,
			Domain:     changelog.Site,
			Message:    changelog.Message,
			IPv6Status: changelog.IPv6Status,
		})
	}

	// Send the changelog list as JSON
	render.JSON(w, r, changelogList)
}

// ChangelogByCampaign lists all changelog entries for a specific campaign UUID with pagination.
func (rs ChangelogHandler) ChangelogByCampaign(w http.ResponseWriter, r *http.Request) {
	// Retrieve pagination input from context
	paginationInput := r.Context().Value(httpin.Input).(*PaginationInput)

	// Limit the maximum number of entries per page to 100
	if paginationInput.Limit > 100 {
		paginationInput.Limit = 100
	}

	// Get campaign UUID from path
	campaign := chi.URLParam(r, "uuid")

	// Validate and parse the UUID
	uuid, err := uuid.Parse(campaign)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid UUID"})
		return
	}

	// Fetch changelogs for the campaign from the repository
	changelogs, err := rs.Repo.GetChangelogByCampaign(r.Context(), uuid, paginationInput.Offset, paginationInput.Limit)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Unable to find changelog entries for campaign " + campaign})
		return
	}

	// If no changelogs are found, return 404
	if len(changelogs) == 0 {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "No changelog entries found for campaign " + campaign})
		return
	}

	// Convert changelogs to ChangelogResponse objects
	var changelogList []ChangelogResponse
	for _, changelog := range changelogs {
		changelogList = append(changelogList, ChangelogResponse{
			ID:         changelog.ID,
			Ts:         changelog.Ts,
			Domain:     changelog.Site,
			Message:    changelog.Message,
			IPv6Status: changelog.IPv6Status,
		})
	}

	// Send the changelog list as JSON
	render.JSON(w, r, changelogList)
}

// ChangelogByCampaignDomain lists all changelog entries for a specific campaign UUID and domain with pagination.
func (rs ChangelogHandler) ChangelogByCampaignDomain(w http.ResponseWriter, r *http.Request) {
	// Retrieve pagination input from context
	paginationInput := r.Context().Value(httpin.Input).(*PaginationInput)

	// Limit the maximum number of entries per page to 100
	if paginationInput.Limit > 100 {
		paginationInput.Limit = 100
	}

	// Get campaign UUID and domain from path
	campaign := chi.URLParam(r, "uuid")
	site := chi.URLParam(r, "domain")

	// Validate the domain
	// TODO: Move this to core package
	if !regexp.MustCompile(`^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$`).MatchString(site) {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid domain"})
		return
	}

	// Validate and parse the UUID
	uuid, err := uuid.Parse(campaign)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid UUID"})
		return
	}

	// Fetch changelogs for the campaign and domain from the repository
	changelogs, err := rs.Repo.GetChangelogByCampaignDomain(r.Context(), uuid, site, paginationInput.Offset, paginationInput.Limit)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Unable to find changelog entries for campaign " + campaign + " and domain " + site})
		return
	}

	// If no changelogs are found, return 404
	if len(changelogs) == 0 {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "No changelog entries found for campaign " + campaign + " and domain " + site})
		return
	}

	// Convert changelogs to ChangelogResponse objects
	var changelogList []ChangelogResponse
	for _, changelog := range changelogs {
		changelogList = append(changelogList, ChangelogResponse{
			ID:         changelog.ID,
			Ts:         changelog.Ts,
			Domain:     changelog.Site,
			Message:    changelog.Message,
			IPv6Status: changelog.IPv6Status,
		})
	}

	// Send the changelog list as JSON
	render.JSON(w, r, changelogList)
}
