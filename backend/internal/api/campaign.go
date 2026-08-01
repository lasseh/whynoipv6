package api

import (
	"context"
	"errors"
	"fmt"
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
	ServeWhole(s, w, r, WholeSpec[CampaignListItem]{
		Fetch: func(ctx context.Context, _ string) ([]CampaignListItem, error) {
			rows, err := s.q.CampaignPublicList(ctx, tag)
			if err != nil {
				return nil, err
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
			return items, nil
		},
	})
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
	limit, err := ParseLimit(r.URL.Query())
	if err != nil {
		invalidParam(w, r, err)
		return
	}
	generation, asOf, ok := s.enterCache(w, r, false)
	if !ok {
		return
	}

	members, page, ok := s.campaignMembersPage(w, r, row.ID, generation, limit)
	if !ok {
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
	adoption, err := s.q.CampaignAdoption(r.Context(), row.ID)
	switch {
	case err == nil:
		d.Adoption = campaignAdoption(adoption.Day, adoption.Domains, adoption.V6Ready)
	case !errors.Is(err, pgx.ErrNoRows): // no rows = pre-first-rollup: adoption stays null
		InternalError(w, r, err)
		return
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
	limit, err := ParseLimit(r.URL.Query())
	if err != nil {
		invalidParam(w, r, err)
		return
	}
	generation, asOf, ok := s.enterCache(w, r, false)
	if !ok {
		return
	}
	members, page, ok := s.campaignMembersPage(w, r, row.ID, generation, limit)
	if !ok {
		return
	}
	meta := NewMeta(asOf, generation)
	meta.Count = &row.DomainCount
	WriteJSON(w, http.StatusOK, ListEnvelope{Items: members, Page: page, Meta: meta})
}

// campaignMembersPage runs one host-ordered members page through ListPage
// (§3.2 — host is unique, so the seek is total despite rank being NULL) —
// one spec shared by the standalone collection and the campaign-detail
// embed, so their cursors stay interchangeable. Failures are already
// written when ok is false.
func (s *Server) campaignMembersPage(w http.ResponseWriter, r *http.Request, campaignID, generation int32, limit int) ([]DomainSummary, Page, bool) {
	filter := postgres.DomainListFilter{CampaignID: &campaignID}
	return ListPage(w, r, generation, limit, KeysetSpec[postgres.DomainRow]{
		Sort:        SortHost,
		Fingerprint: ScopedFingerprint(fmt.Sprintf("campaign:%d", campaignID), r.URL.Query()),
		Fetch: func(ctx context.Context, seek *Seek, lim int, backward bool) ([]postgres.DomainRow, error) {
			var ds *postgres.DomainSeek
			if seek != nil {
				ds = &postgres.DomainSeek{Host: seek.Host}
			}
			return postgres.ListDomains(ctx, s.pool, &filter, postgres.ListSortHost, ds, nil, lim, backward)
		},
		Key: domainKey(SortHost),
	}, memberItem)
}

// memberItem is the campaign-member wire row: the §4.2 summary plus the
// campaign-side v6_ready verdict.
func memberItem(row *postgres.DomainRow) DomainSummary {
	d := summaryFromRow(row)
	ready := v6ReadyOf(&d.Status)
	d.V6Ready = &ready
	return d
}
