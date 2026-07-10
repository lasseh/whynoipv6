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

// CampaignListItem is the /campaigns index row (07 §4.7).
type CampaignListItem struct {
	UUID        string   `json:"uuid"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	SourceFile  *string  `json:"source_file"`
	Tags        []string `json:"tags"`
	DomainCount int64    `json:"domain_count"`
}

// CampaignAdoption is the stats-derived adoption block, null pre-first-rollup.
type CampaignAdoption struct {
	V6ReadyPercent float64 `json:"v6_ready_percent"`
	Day            string  `json:"day"`
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
// set — whole list, exact count.
func (s *Server) listCampaigns(w http.ResponseWriter, r *http.Request) {
	generation, asOf, err := s.svc.Generation(r.Context())
	if err != nil {
		InternalError(w, r)
		return
	}
	if CacheList(w, r, generation) {
		return
	}
	rows, err := s.svc.Q.CampaignPublicList(r.Context(), r.URL.Query().Get("tag"))
	if err != nil {
		InternalError(w, r)
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
	row, err := s.svc.Q.CampaignPublicDetail(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Campaign not found", "No such campaign: "+id.String())
		return row, false
	}
	if err != nil {
		InternalError(w, r)
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
	generation, asOf, err := s.svc.Generation(r.Context())
	if err != nil {
		InternalError(w, r)
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
		InternalError(w, r)
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
	if adoption, err := s.svc.Q.CampaignAdoption(r.Context(), row.ID); err == nil &&
		adoption.Domains != nil && *adoption.Domains > 0 && adoption.V6Ready != nil {
		pct := float64(*adoption.V6Ready) * 100 / float64(*adoption.Domains)
		d.Adoption = &CampaignAdoption{
			V6ReadyPercent: math.Round(pct*10) / 10,
			Day:            adoption.Day.Time.Format("2006-01-02"),
		}
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
	generation, asOf, err := s.svc.Generation(r.Context())
	if err != nil {
		InternalError(w, r)
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
		InternalError(w, r)
		return
	}
	meta := NewMeta(asOf, generation)
	meta.Count = &row.DomainCount
	WriteJSON(w, http.StatusOK, ListEnvelope{Items: members, Page: page, Meta: meta})
}

// campaignMembersPage runs one host-ordered keyset page over the members
// (§3.2 — host is unique, so the seek is total despite rank being NULL).
func (s *Server) campaignMembersPage(r *http.Request, campaignID, generation int32, limit int) ([]DomainSummary, Page, error) {
	q := r.URL.Query()
	fingerprint := FilterFingerprint(q)
	var seek *postgres.DomainSeek
	if token := q.Get(paramCursor); token != "" {
		c, err := DecodeCursor(token, SortHost, fingerprint, generation)
		if err != nil {
			return nil, Page{}, err
		}
		st, err := c.SeekTuple()
		if err != nil {
			return nil, Page{}, err
		}
		seek = &postgres.DomainSeek{Host: st.Host}
	}
	filter := postgres.DomainListFilter{CampaignID: &campaignID}
	rows, err := postgres.ListDomains(r.Context(), s.svc.Pool, &filter, postgres.ListSortHost, seek, nil, limit)
	if err != nil {
		return nil, Page{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]DomainSummary, len(rows))
	for i := range rows {
		items[i] = summaryFromRow(&rows[i])
	}
	var lastK []any
	if hasMore {
		lastK = []any{rows[len(rows)-1].Host}
	}
	return items, PageOf(generation, SortHost, fingerprint, hasMore, lastK, nil), nil
}
