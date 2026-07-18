package api

import (
	"errors"
	"math"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// CampaignListItem is the /campaigns index row (07 §4.7). Each row carries the
// same adoption block as the detail (null before the first stats tick).
type CampaignListItem struct {
	UUID        string            `json:"uuid"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	SourceFile  *string           `json:"source_file"`
	Tags        []string          `json:"tags"`
	DomainCount int64             `json:"domain_count"`
	Adoption    *CampaignAdoption `json:"adoption"`
}

// CampaignAdoption is the stats-derived adoption block, null pre-first-rollup.
type CampaignAdoption struct {
	V6ReadyPercent float64 `json:"v6_ready_percent"`
	Day            string  `json:"day"`
}

// campaignAdoption builds the §4.7 adoption block from a latest
// stats_campaign_daily row; nil before the first stats tick (no row, zero
// domains, or a NULL v6_ready). Shared by the list and detail handlers.
func campaignAdoption(day pgtype.Date, domains, v6Ready *int32) *CampaignAdoption {
	if !day.Valid || domains == nil || *domains <= 0 || v6Ready == nil {
		return nil
	}
	pct := float64(*v6Ready) * 100 / float64(*domains)
	return &CampaignAdoption{
		V6ReadyPercent: math.Round(pct*10) / 10,
		Day:            day.Time.Format("2006-01-02"),
	}
}

// CampaignDetail is the §4.7 composite: metadata + paged members + adoption.
type CampaignDetail struct {
	UUID        string            `json:"uuid"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	SourceFile  *string           `json:"source_file"`
	Tags        []string          `json:"tags"`
	Disabled    bool              `json:"disabled"`
	Adoption    *CampaignAdoption `json:"adoption"`
	Domains     struct {
		Items []DomainSummary `json:"items"`
		Page  Page            `json:"page"`
	} `json:"domains"`
	Meta Meta `json:"meta"`
}

// listCampaigns is GET /campaigns (?tag= filter, OPEN-12): a bounded curated
// set — whole list, exact count. An unknown tag is 200 with an empty
// collection, not 404.
func (s *Server) listCampaigns(w http.ResponseWriter, r *http.Request) {
	s.serveCampaignList(w, r, r.URL.Query().Get("tag"))
}

func (s *Server) serveCampaignList(w http.ResponseWriter, r *http.Request, tag string) {
	generation, asOf, err := s.generation(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheList(w, r, generation) {
		return
	}
	rows, err := s.q.CampaignPublicList(r.Context(), tag)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	items := make([]CampaignListItem, len(rows))
	for i := range rows {
		tags := rows[i].Tags
		if tags == nil {
			tags = []string{}
		}
		items[i] = CampaignListItem{
			UUID:        uuid.UUID(rows[i].Uuid.Bytes).String(),
			Name:        rows[i].Name,
			Description: rows[i].Description,
			SourceFile:  rows[i].SourceFile,
			Tags:        tags,
			DomainCount: rows[i].DomainCount,
			Adoption:    campaignAdoption(rows[i].AdoptionDay, rows[i].AdoptionDomains, rows[i].AdoptionV6Ready),
		}
	}
	count := int64(len(items))
	meta := NewMeta(asOf, generation)
	meta.Count = &count
	WriteJSON(w, http.StatusOK, ListEnvelope{Items: items, Page: Page{}, Meta: meta})
}

// campaignByPathUUID resolves the {uuid} path param (raw UUID, OPEN-1).
func (s *Server) campaignByPathUUID(w http.ResponseWriter, r *http.Request) (db.CampaignPublicDetailRow, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		NotFound(w, r, "Campaign not found", "Campaigns are keyed by their raw UUID.")
		return db.CampaignPublicDetailRow{}, false
	}
	row, err := s.q.CampaignPublicDetail(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Campaign not found", "No such campaign: "+id.String())
		return row, false
	}
	if err != nil {
		InternalError(w, r, err)
		return row, false
	}
	return row, true
}

// getCampaign is GET /campaigns/{uuid} — the composite detail: metadata,
// the first host-ordered page of members, and stats-derived adoption.
func (s *Server) getCampaign(w http.ResponseWriter, r *http.Request) {
	row, ok := s.campaignByPathUUID(w, r)
	if !ok {
		return
	}
	generation, asOf, err := s.generation(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheList(w, r, generation) {
		return
	}

	limit, err := ParseLimit(r.URL.Query())
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	members, page, err := s.campaignMembersPage(r, row.ID, generation, limit)
	if err != nil {
		if errors.Is(err, ErrCursorInvalid) {
			InvalidParameter(w, r, err.Error())
			return
		}
		InternalError(w, r, err)
		return
	}

	d := CampaignDetail{
		UUID:        uuid.UUID(row.Uuid.Bytes).String(),
		Name:        row.Name,
		Description: row.Description,
		SourceFile:  row.SourceFile,
		Tags:        row.Tags,
		Disabled:    row.Disabled,
	}
	if d.Tags == nil {
		d.Tags = []string{}
	}
	if adoption, err := s.q.CampaignAdoption(r.Context(), row.ID); err == nil {
		d.Adoption = campaignAdoption(adoption.Day, adoption.Domains, adoption.V6Ready)
	}
	d.Domains.Items = members
	d.Domains.Page = page
	d.Meta = NewMeta(asOf, generation)
	d.Meta.Count = &row.DomainCount // exact: a genuinely-bounded set
	WriteJSON(w, http.StatusOK, d)
}

// listCampaignDomains is GET /campaigns/{uuid}/domains — the standalone
// members collection (host-ordered keyset, exact count).
func (s *Server) listCampaignDomains(w http.ResponseWriter, r *http.Request) {
	row, ok := s.campaignByPathUUID(w, r)
	if !ok {
		return
	}
	generation, asOf, err := s.generation(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheList(w, r, generation) {
		return
	}
	limit, err := ParseLimit(r.URL.Query())
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	members, page, err := s.campaignMembersPage(r, row.ID, generation, limit)
	if err != nil {
		if errors.Is(err, ErrCursorInvalid) {
			InvalidParameter(w, r, err.Error())
			return
		}
		InternalError(w, r, err)
		return
	}
	meta := NewMeta(asOf, generation)
	meta.Count = &row.DomainCount
	WriteJSON(w, http.StatusOK, ListEnvelope{Items: members, Page: page, Meta: meta})
}

// campaignMembersPage runs one host-ordered keyset page over the members
// (§3.2 — host is unique, so the seek is total despite rank being NULL).
func (s *Server) campaignMembersPage(r *http.Request, campaignID, generation int32, limit int) ([]DomainSummary, Page, error) {
	filter := postgres.DomainListFilter{CampaignID: &campaignID}
	members, page, err := s.hostOrderedPage(r, &filter, generation, limit)
	for i := range members {
		ready := v6ReadyOf(&members[i].Status)
		members[i].V6Ready = &ready
	}
	return members, page, err
}
