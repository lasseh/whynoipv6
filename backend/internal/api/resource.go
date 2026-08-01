package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// ResourceLink is a forward-dependency row (07 §4.11): what one domain
// depends on. first/last_seen are dates on the wire.
type ResourceLink struct {
	Host          string     `json:"host"`
	AAAAStatus    *string    `json:"aaaa_status"`
	Source        string     `json:"source"`
	Required      bool       `json:"required"`
	FirstSeen     string     `json:"first_seen"`
	LastSeen      string     `json:"last_seen"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
}

// ResourceHostBody is the reverse-list headline block (07 §4.11).
type ResourceHostBody struct {
	Host           string     `json:"host"`
	AAAAStatus     *string    `json:"aaaa_status"`
	DependentCount int32      `json:"dependent_count"`
	LastCheckedAt  *time.Time `json:"last_checked_at"`
}

func resourceHostBody(row *db.ResourceHostByHostRow) ResourceHostBody {
	return ResourceHostBody{
		Host:           row.Host,
		AAAAStatus:     postgres.StatusPtr(row.AaaaStatus),
		DependentCount: row.DependentCount,
		LastCheckedAt:  postgres.TimePtr(row.LastCheckedAt),
	}
}

// listDomainResources is GET /domains/{host}/resources — bounded small,
// exact count, trivial page; items: [] while resources are dormant.
func (s *Server) listDomainResources(w http.ResponseWriter, r *http.Request) {
	d, ok := s.domainByPathHost(w, r)
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
	rows, err := s.q.DomainResourceList(r.Context(), d.ID)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	items := make([]ResourceLink, len(rows))
	for i := range rows {
		items[i] = ResourceLink{
			Host:          rows[i].Host,
			AAAAStatus:    postgres.StatusPtr(rows[i].AaaaStatus),
			Source:        string(rows[i].Source),
			Required:      rows[i].Required,
			FirstSeen:     rows[i].FirstSeen.Time.UTC().Format("2006-01-02"),
			LastSeen:      rows[i].LastSeen.Time.UTC().Format("2006-01-02"),
			LastCheckedAt: postgres.TimePtr(rows[i].LastCheckedAt),
		}
	}
	count := int64(len(items))
	meta := NewMeta(asOf, generation)
	meta.Count = &count
	WriteJSON(w, http.StatusOK, ListEnvelope{Items: items, Page: Page{}, Meta: meta})
}

// getResource is GET /resources/{host} — the resource-host headline.
func (s *Server) getResource(w http.ResponseWriter, r *http.Request) {
	row, ok := s.resourceByPathHost(w, r)
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
	WriteJSON(w, http.StatusOK, struct {
		ResourceHostBody
		Meta DetailMeta `json:"meta"`
	}{resourceHostBody(&row), DetailMeta{AsOf: asOf.UTC(), Generation: generation}})
}

// listResourceDependents is GET /resources/{host}/dependents — the advocacy
// surface: §4.2 rows + link attrs, null-flag-first keyset (07 §4.11/§3.2).
func (s *Server) listResourceDependents(w http.ResponseWriter, r *http.Request) {
	row, ok := s.resourceByPathHost(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, err := ParseLimit(q)
	if err != nil {
		invalidParam(w, r, err)
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

	rows, page, err := KeysetPage(r, generation, limit, KeysetSpec[postgres.DependentRow]{
		Sort: SortDependents,
		Fetch: func(ctx context.Context, seek *Seek, lim int, backward bool) ([]postgres.DependentRow, error) {
			var ds *postgres.DependentSeek
			if seek != nil {
				ds = &postgres.DependentSeek{RankNull: seek.RankNull, Rank: seek.Rank, ID: seek.ID}
			}
			return postgres.ListDependents(ctx, s.pool, row.ID, ds, lim, backward)
		},
		Key: func(d *postgres.DependentRow) []any {
			isNull := d.Rank == nil
			var rank int32
			if d.Rank != nil {
				rank = *d.Rank
			}
			return []any{isNull, rank, d.ID}
		},
	})
	if errors.Is(err, ErrCursorInvalid) {
		InvalidParameter(w, r, err.Error())
		return
	}
	if err != nil {
		InternalError(w, r, err)
		return
	}

	type dependentItem struct {
		DomainSummary
		Source   string `json:"source"`
		Required bool   `json:"required"`
	}
	items := make([]dependentItem, len(rows))
	for i := range rows {
		items[i] = dependentItem{summaryFromRow(&rows[i].DomainRow), rows[i].Source, rows[i].Required}
	}

	est := int64(row.DependentCount)
	meta := NewMeta(asOf, generation)
	meta.CountEstimate = &est
	WriteJSON(w, http.StatusOK, struct {
		Resource ResourceHostBody `json:"resource"`
		Items    any              `json:"items"`
		Page     Page             `json:"page"`
		Meta     Meta             `json:"meta"`
	}{resourceHostBody(&row), items, page, meta})
}

func (s *Server) resourceByPathHost(w http.ResponseWriter, r *http.Request) (db.ResourceHostByHostRow, bool) {
	host, err := domain.Canonicalize(chi.URLParam(r, "host"))
	if err != nil {
		NotFound(w, r, "Resource not found", "The host is not a valid public domain name.")
		return db.ResourceHostByHostRow{}, false
	}
	row, err := s.q.ResourceHostByHost(r.Context(), host)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Resource not found", "No such resource host: "+host)
		return row, false
	}
	if err != nil {
		InternalError(w, r, err)
		return row, false
	}
	return row, true
}
