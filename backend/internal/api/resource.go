package api

import (
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
		AAAAStatus:     statusPtr(row.AaaaStatus),
		DependentCount: row.DependentCount,
		LastCheckedAt:  pgTimePtr(row.LastCheckedAt),
	}
}

// listDomainResources is GET /domains/{host}/resources — bounded small,
// exact count, trivial page; items: [] while resources are dormant.
func (s *Server) listDomainResources(w http.ResponseWriter, r *http.Request) {
	host, err := domain.Canonicalize(chi.URLParam(r, "host"))
	if err != nil {
		NotFound(w, r, "Domain not found", "The host is not a valid public domain name.")
		return
	}
	d, err := s.svc.Q.DomainByHost(r.Context(), host)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Domain not found", "No such domain: "+host)
		return
	}
	if err != nil {
		InternalError(w, r)
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
	rows, err := s.svc.Q.DomainResourceList(r.Context(), d.ID)
	if err != nil {
		InternalError(w, r)
		return
	}
	items := make([]ResourceLink, len(rows))
	for i := range rows {
		items[i] = ResourceLink{
			Host:          rows[i].Host,
			AAAAStatus:    statusPtr(rows[i].AaaaStatus),
			Source:        string(rows[i].Source),
			Required:      rows[i].Required,
			FirstSeen:     rows[i].FirstSeen.Time.UTC().Format("2006-01-02"),
			LastSeen:      rows[i].LastSeen.Time.UTC().Format("2006-01-02"),
			LastCheckedAt: pgTimePtr(rows[i].LastCheckedAt),
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
	generation, asOf, err := s.svc.Generation(r.Context())
	if err != nil {
		InternalError(w, r)
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
		InvalidParameter(w, r, err.Error())
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

	fingerprint := FilterFingerprint(q)
	var seek *postgres.DependentSeek
	if token := q.Get(paramCursor); token != "" {
		c, err := DecodeCursor(token, SortDependents, fingerprint, generation)
		if err != nil {
			InvalidParameter(w, r, err.Error())
			return
		}
		st, err := c.SeekTuple()
		if err != nil {
			InvalidParameter(w, r, err.Error())
			return
		}
		seek = &postgres.DependentSeek{RankNull: st.RankNull, Rank: st.Rank, ID: st.ID}
	}

	rows, err := postgres.ListDependents(r.Context(), s.svc.Pool, row.ID, seek, limit)
	if err != nil {
		InternalError(w, r)
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
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

	var lastK []any
	if hasMore {
		last := &rows[len(rows)-1]
		isNull := last.Rank == nil
		var rank int32
		if last.Rank != nil {
			rank = *last.Rank
		}
		lastK = []any{isNull, rank, last.ID}
	}

	est := int64(row.DependentCount)
	meta := NewMeta(asOf, generation)
	meta.CountEstimate = &est
	WriteJSON(w, http.StatusOK, struct {
		Resource ResourceHostBody `json:"resource"`
		Items    any              `json:"items"`
		Page     Page             `json:"page"`
		Meta     Meta             `json:"meta"`
	}{resourceHostBody(&row), items, PageOf(generation, SortDependents, fingerprint, hasMore, lastK, nil), meta})
}

func (s *Server) resourceByPathHost(w http.ResponseWriter, r *http.Request) (db.ResourceHostByHostRow, bool) {
	host, err := domain.Canonicalize(chi.URLParam(r, "host"))
	if err != nil {
		NotFound(w, r, "Resource not found", "The host is not a valid public domain name.")
		return db.ResourceHostByHostRow{}, false
	}
	row, err := s.svc.Q.ResourceHostByHost(r.Context(), host)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Resource not found", "No such resource host: "+host)
		return row, false
	}
	if err != nil {
		InternalError(w, r)
		return row, false
	}
	return row, true
}

// listSubdomains is GET /domains/{host}/subdomains — a native
// sub-collection (07 §4.3): host-ordered, exact count, rank-NULL rows
// visible (sub-collection visibility, §2.2).
func (s *Server) listSubdomains(w http.ResponseWriter, r *http.Request) {
	host, err := domain.Canonicalize(chi.URLParam(r, "host"))
	if err != nil {
		NotFound(w, r, "Domain not found", "The host is not a valid public domain name.")
		return
	}
	d, err := s.svc.Q.DomainByHost(r.Context(), host)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Domain not found", "No such domain: "+host)
		return
	}
	if err != nil {
		InternalError(w, r)
		return
	}
	q := r.URL.Query()
	limit, err := ParseLimit(q)
	if err != nil {
		InvalidParameter(w, r, err.Error())
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

	fingerprint := FilterFingerprint(q)
	var seek *postgres.DomainSeek
	if token := q.Get(paramCursor); token != "" {
		c, err := DecodeCursor(token, SortHost, fingerprint, generation)
		if err != nil {
			InvalidParameter(w, r, err.Error())
			return
		}
		st, err := c.SeekTuple()
		if err != nil {
			InvalidParameter(w, r, err.Error())
			return
		}
		seek = &postgres.DomainSeek{Host: st.Host}
	}
	filter := postgres.DomainListFilter{ParentID: &d.ID}
	rows, err := postgres.ListDomains(r.Context(), s.svc.Pool, &filter, postgres.ListSortHost, seek, nil, limit)
	if err != nil {
		InternalError(w, r)
		return
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
	count, err := s.svc.Q.SubdomainExactCount(r.Context(), &d.ID)
	if err != nil {
		InternalError(w, r)
		return
	}
	meta := NewMeta(asOf, generation)
	meta.Count = &count
	WriteJSON(w, http.StatusOK, ListEnvelope{
		Items: items,
		Page:  PageOf(generation, SortHost, fingerprint, hasMore, lastK, nil),
		Meta:  meta,
	})
}
