package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// ChangelogItem is the structured §4.8 event — raw 4-value enum, always
// non-null and distinct, no message rendering, no synthetic epoch id.
type ChangelogItem struct {
	TS       time.Time `json:"ts"`
	Host     string    `json:"host"`
	Field    string    `json:"field"`
	OldValue string    `json:"old_value"`
	NewValue string    `json:"new_value"`
}

// changelogWindow parses the shared ?field=/?from=/?to= filters. from/to
// accept a bare date or a full RFC 3339 timestamp.
type changelogWindow struct {
	Field    string
	From, To time.Time
	HasFrom  bool
	HasTo    bool
}

func parseChangelogWindow(q url.Values) (changelogWindow, error) {
	var w changelogWindow
	if f := q.Get("field"); f != "" {
		ok := false
		for _, dim := range statusDims {
			if f == dim {
				ok = true
				break
			}
		}
		if !ok {
			return w, validationError{"field", "must be one of base, www, ns, mx, conn, resources"}
		}
		w.Field = f
	}
	parse := func(v string) (time.Time, error) {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			return t, nil
		}
		return time.Parse(time.RFC3339, v)
	}
	if v := q.Get("from"); v != "" {
		t, err := parse(v)
		if err != nil {
			return w, validationError{"from", "must be YYYY-MM-DD or RFC 3339"}
		}
		w.From, w.HasFrom = t, true
	}
	if v := q.Get("to"); v != "" {
		t, err := parse(v)
		if err != nil {
			return w, validationError{"to", "must be YYYY-MM-DD or RFC 3339"}
		}
		w.To, w.HasTo = t, true
	}
	if w.HasFrom && w.HasTo && w.From.After(w.To) {
		return w, validationError{"from", "must not be after to"}
	}
	return w, nil
}

// listChangelog is GET /changelog — the global recent-transitions feed,
// keyset on (ts, domain_id, field) DESC over idx_changelog_ts. ?scope=campaign
// restricts to campaign-member domains as a capped recent window (07 §4.8).
func (s *Server) listChangelog(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("scope") {
	case "":
		s.serveChangelogFeed(w, r, nil)
	case "campaign":
		rows, err := s.svc.Q.ChangelogCampaignScope(r.Context())
		if err != nil {
			InternalError(w, r, err)
			return
		}
		s.writeRecentWindow(w, r, changelogScopeItems(rows))
	default:
		ValidationError(w, r, []FieldError{{Field: "scope", Reason: "must be campaign"}})
	}
}

// listDomainChangelog is GET /domains/{host}/changelog (native PK read).
func (s *Server) listDomainChangelog(w http.ResponseWriter, r *http.Request) {
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
		InternalError(w, r, err)
		return
	}
	s.serveChangelogFeed(w, r, &d.ID)
}

// serveChangelogFeed runs the global or per-domain paginating feed.
func (s *Server) serveChangelogFeed(w http.ResponseWriter, r *http.Request, domainID *int64) {
	q := r.URL.Query()
	win, err := parseChangelogWindow(q)
	var ve validationError
	if errors.As(err, &ve) {
		ValidationError(w, r, []FieldError{{Field: ve.field, Reason: ve.msg}})
		return
	}
	wantCSV, err := parseFormat(q)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}
	limitCap := MaxLimit
	if wantCSV {
		limitCap = s.opts.CSVMaxRows
	}
	limit, err := ParseLimitCap(q, limitCap)
	if err != nil {
		InvalidParameter(w, r, err.Error())
		return
	}

	generation, asOf, err := s.svc.Generation(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	maxTS, err := s.svc.Q.ChangelogMaxTS(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheChangelog(w, r, maxTS.Time) {
		return
	}

	filter := postgres.ChangelogFilter{DomainID: domainID, Field: win.Field}
	if win.HasFrom {
		filter.From = &win.From
	}
	if win.HasTo {
		filter.To = &win.To
	}
	rows, page, err := KeysetPage(r, generation, limit, KeysetSpec[postgres.ChangelogRow]{
		Sort: SortChangelog,
		Fetch: func(ctx context.Context, seek *Seek, lim int, backward bool) ([]postgres.ChangelogRow, error) {
			var cs *postgres.ChangelogSeek
			if seek != nil {
				cs = &postgres.ChangelogSeek{TS: time.Unix(0, seek.TS).UTC(), Domain: seek.ID, Field: seek.Field}
			}
			return postgres.ListChangelog(ctx, s.svc.Pool, &filter, cs, lim, backward)
		},
		Key: func(row *postgres.ChangelogRow) []any {
			return []any{row.Ts.UnixNano(), row.DomainID, row.Field}
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
	items := make([]ChangelogItem, len(rows))
	for i := range rows {
		items[i] = changelogItem(&rows[i])
	}
	if wantCSV {
		writeChangelogCSV(w, items)
		return
	}

	WriteJSON(w, http.StatusOK, ListEnvelope{
		Items: items,
		Page:  page,
		Meta:  NewMeta(asOf, generation),
	})
}

func changelogItem(r *postgres.ChangelogRow) ChangelogItem {
	return ChangelogItem{
		TS:       r.Ts.UTC(),
		Host:     r.Host,
		Field:    r.Field,
		OldValue: r.OldValue,
		NewValue: r.NewValue,
	}
}

// listCountryChangelog is GET /countries/{code}/changelog — capped to the
// latest-50 recent window (OPEN-15: no (scope, ts) index exists).
func (s *Server) listCountryChangelog(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if len(code) != 2 {
		NotFound(w, r, "Country not found", "Country codes are two-letter ISO 3166-1 alpha-2.")
		return
	}
	id, err := s.svc.Q.CountryIDByCode(r.Context(), code)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Country not found", "No such country: "+code)
		return
	}
	if err != nil {
		InternalError(w, r, err)
		return
	}
	rows, err := s.svc.Q.ChangelogByCountry(r.Context(), id)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	s.writeRecentWindow(w, r, changelogWindowItems(rows))
}

// listCampaignChangelog is GET /campaigns/{uuid}/changelog — same recent
// window; members' transitions campaign-wide.
func (s *Server) listCampaignChangelog(w http.ResponseWriter, r *http.Request) {
	row, ok := s.campaignByPathUUID(w, r)
	if !ok {
		return
	}
	rows, err := s.svc.Q.ChangelogByCampaign(r.Context(), row.ID)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	s.writeRecentWindow(w, r, changelogCampaignItems(rows))
}

// listCampaignDomainChangelog is GET /campaigns/{uuid}/domains/{host}/
// changelog — one member's feed; 404 when the host is not a member.
func (s *Server) listCampaignDomainChangelog(w http.ResponseWriter, r *http.Request) {
	c, ok := s.campaignByPathUUID(w, r)
	if !ok {
		return
	}
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
		InternalError(w, r, err)
		return
	}
	member, err := s.svc.Q.CampaignHasMember(r.Context(), db.CampaignHasMemberParams{
		CampaignID: c.ID, DomainID: d.ID,
	})
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if !member {
		NotFound(w, r, "Domain not found", host+" is not a member of this campaign.")
		return
	}
	s.serveChangelogFeed(w, r, &d.ID)
}

// writeRecentWindow emits the capped scoped feed: trivial page, no cursor.
func (s *Server) writeRecentWindow(w http.ResponseWriter, r *http.Request, items []ChangelogItem) {
	generation, asOf, err := s.svc.Generation(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	maxTS, err := s.svc.Q.ChangelogMaxTS(r.Context())
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if CacheChangelog(w, r, maxTS.Time) {
		return
	}
	WriteJSON(w, http.StatusOK, ListEnvelope{Items: items, Page: Page{}, Meta: NewMeta(asOf, generation)})
}

func changelogWindowItems(rows []db.ChangelogByCountryRow) []ChangelogItem {
	items := make([]ChangelogItem, len(rows))
	for i := range rows {
		items[i] = ChangelogItem{
			TS: rows[i].Ts.Time.UTC(), Host: rows[i].Host, Field: rows[i].Field,
			OldValue: string(rows[i].OldValue), NewValue: string(rows[i].NewValue),
		}
	}
	return items
}

func changelogCampaignItems(rows []db.ChangelogByCampaignRow) []ChangelogItem {
	items := make([]ChangelogItem, len(rows))
	for i := range rows {
		items[i] = ChangelogItem{
			TS: rows[i].Ts.Time.UTC(), Host: rows[i].Host, Field: rows[i].Field,
			OldValue: string(rows[i].OldValue), NewValue: string(rows[i].NewValue),
		}
	}
	return items
}

func changelogScopeItems(rows []db.ChangelogCampaignScopeRow) []ChangelogItem {
	items := make([]ChangelogItem, len(rows))
	for i := range rows {
		items[i] = ChangelogItem{
			TS: rows[i].Ts.Time.UTC(), Host: rows[i].Host, Field: rows[i].Field,
			OldValue: string(rows[i].OldValue), NewValue: string(rows[i].NewValue),
		}
	}
	return items
}
