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
	ID      int64     `json:"id"`
	Ts      time.Time `json:"ts"`
	Domain  string    `json:"domain"`
	Message string    `json:"message"`
}

// Routes returns a router with all changelog endpoints mounted.
func (rs ChangelogHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.With(httpin.NewInput(PaginationInput{})).Get("/", rs.ChangelogList)                                     // GET /changelog - list all changelog entries
	r.With(httpin.NewInput(PaginationInput{})).Get("/{domain}", rs.ChangelogByDomain)                         // GET /changelog/{domain} - read all changelog entries for a domain
	r.With(httpin.NewInput(PaginationInput{})).Get("/campaign/{uuid}", rs.ChangelogByCampaign)                // GET /changelog/campaign/{uuid} - read all changelog entries for a campaign uuid
	r.With(httpin.NewInput(PaginationInput{})).Get("/campaign/{uuid}/{domain}", rs.ChangelogByCampaignDomain) // GET /changelog/campaign/{uuid} - read all changelog entries for a domain in a campaign uuid
	return r
}

// ChangelogList lists all changelog entries.
func (rs ChangelogHandler) ChangelogList(w http.ResponseWriter, r *http.Request) {
	// Handle query params
	input := r.Context().Value(httpin.Input).(*PaginationInput)
	if input.Limit > 100 {
		input.Limit = 100
	}

	changelogs, err := rs.Repo.List(r.Context(), int32(input.Offset), int32(input.Limit))
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, render.M{"error": "internal server error"})
		return
	}
	var changeloglist []ChangelogResponse
	for _, changelog := range changelogs {
		changeloglist = append(changeloglist, ChangelogResponse{
			ID:      changelog.ID,
			Ts:      changelog.Ts,
			Domain:  changelog.Site,
			Message: changelog.Message,
		})
	}
	render.JSON(w, r, changeloglist)
}

// ChangelogByDomain gets a changelog entry by id.
func (rs ChangelogHandler) ChangelogByDomain(w http.ResponseWriter, r *http.Request) {
	// Handle query params
	input := r.Context().Value(httpin.Input).(*PaginationInput)
	if input.Limit > 100 {
		input.Limit = 100
	}

	// Get domain from path
	site := chi.URLParam(r, "domain")

	// Check if site is a valid domain
	if !regexp.MustCompile(`^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$`).MatchString(site) {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid domain"})
		return
	}

	changelogs, err := rs.Repo.GetChangelogByDomain(r.Context(), site, int32(input.Offset), int32(input.Limit))
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Unable to find changelog entry for " + site})
		return
	}
	// If empty, return 404
	if len(changelogs) == 0 {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Unable to find changelog entry for " + site})
		return
	}

	var changeloglist []ChangelogResponse
	for _, changelog := range changelogs {
		changeloglist = append(changeloglist, ChangelogResponse{
			ID:      changelog.ID,
			Ts:      changelog.Ts,
			Domain:  changelog.Site,
			Message: changelog.Message,
		})
	}
	render.JSON(w, r, changeloglist)
}

// ChangelogByCampaign gets a changelog entry by uuid.
func (rs ChangelogHandler) ChangelogByCampaign(w http.ResponseWriter, r *http.Request) {
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

	changelogs, err := rs.Repo.GetChangelogByCampaign(r.Context(), uuid, int32(input.Offset), int32(input.Limit))
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Unable to find changelog entry for " + campaign})
		return
	}
	// If empty, return 404
	if len(changelogs) == 0 {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Unable to find changelog entry for " + campaign})
		return
	}

	var changeloglist []ChangelogResponse
	for _, changelog := range changelogs {
		changeloglist = append(changeloglist, ChangelogResponse{
			ID:      changelog.ID,
			Ts:      changelog.Ts,
			Domain:  changelog.Site,
			Message: changelog.Message,
		})
	}
	render.JSON(w, r, changeloglist)
}

// ChangelogByCampaignDomain gets a changelog entry by uuid and domain.
func (rs ChangelogHandler) ChangelogByCampaignDomain(w http.ResponseWriter, r *http.Request) {
	// Handle query params
	input := r.Context().Value(httpin.Input).(*PaginationInput)
	if input.Limit > 100 {
		input.Limit = 100
	}

	// Get domain from path
	campaign := chi.URLParam(r, "uuid")
	site := chi.URLParam(r, "domain")

	// Check if site is a valid domain
	if !regexp.MustCompile(`^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$`).MatchString(site) {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid domain"})
		return
	}

	// Convert to uuid.UUID
	uuid, err := uuid.Parse(campaign)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, render.M{"error": "Invalid uuid"})
		return
	}

	changelogs, err := rs.Repo.GetChangelogByCampaignDomain(r.Context(), uuid, site, int32(input.Offset), int32(input.Limit))
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Unable to find changelog entry for " + campaign})
		return
	}
	// If empty, return 404
	if len(changelogs) == 0 {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, render.M{"error": "Unable to find changelog entry for " + campaign})
		return
	}

	var changeloglist []ChangelogResponse
	for _, changelog := range changelogs {
		changeloglist = append(changeloglist, ChangelogResponse{
			ID:      changelog.ID,
			Ts:      changelog.Ts,
			Domain:  changelog.Site,
			Message: changelog.Message,
		})
	}
	render.JSON(w, r, changeloglist)
}
