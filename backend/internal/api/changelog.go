package api

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/whynoipv6/internal/domain"
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

	fingerprint := FilterFingerprint(q)
	params := db.ChangelogGlobalParams{
		Field:    win.Field,
		WithFrom: win.HasFrom, FromTs: pgTS(win.From, win.HasFrom),
		WithTo: win.HasTo, ToTs: pgTS(win.To, win.HasTo),
		Lim: int32(limit + 1),
	}
	backward := false
	if token := q.Get(paramCursor); token != "" {
		c, err := DecodeCursor(token, SortChangelog, fingerprint, generation)
		if err != nil {
			InvalidParameter(w, r, err.Error())
			return
		}
		st, err := c.SeekTuple()
		if err != nil {
			InvalidParameter(w, r, err.Error())
			return
		}
		params.WithSeek = true
		params.SeekTs = pgTS(time.Unix(0, st.TS).UTC(), true)
		params.SeekDomain = st.ID
		params.SeekField = st.Field
		backward = c.Backward()
	}

	rows, err := s.changelogRows(r, domainID, &params, backward)
	if err != nil {
		InternalError(w, r, err)
		return
	}

	rows, forwardMore, backwardMore := trimWindow(rows, limit, backward, params.WithSeek)
	items := make([]ChangelogItem, len(rows))
	for i := range rows {
		items[i] = changelogItem(&rows[i])
	}
	if wantCSV {
		writeChangelogCSV(w, items)
		return
	}
	clKey := func(row *db.ChangelogGlobalRow) []any {
		return []any{row.Ts.Time.UnixNano(), row.DomainID, row.Field}
	}
	var firstK, lastK []any
	if len(rows) > 0 {
		firstK, lastK = clKey(&rows[0]), clKey(&rows[len(rows)-1])
	}

	WriteJSON(w, http.StatusOK, ListEnvelope{
		Items: items,
		Page:  BuildPage(generation, SortChangelog, fingerprint, forwardMore, backwardMore, firstK, lastK),
		Meta:  NewMeta(asOf, generation),
	})
}

// changelogRows dispatches the scope × direction query matrix; backward
// rows come back re-reversed into ts-DESC display order.
func (s *Server) changelogRows(r *http.Request, domainID *int64, params *db.ChangelogGlobalParams, backward bool) ([]db.ChangelogGlobalRow, error) {
	var rows []db.ChangelogGlobalRow
	switch {
	case domainID != nil && backward:
		dr, err := s.svc.Q.ChangelogByDomainPrev(r.Context(), db.ChangelogByDomainPrevParams{
			DomainID: *domainID, Field: params.Field,
			WithFrom: params.WithFrom, FromTs: params.FromTs,
			WithTo: params.WithTo, ToTs: params.ToTs,
			SeekTs: params.SeekTs, SeekDomain: params.SeekDomain, SeekField: params.SeekField,
			Lim: params.Lim,
		})
		if err != nil {
			return nil, err
		}
		rows = make([]db.ChangelogGlobalRow, len(dr))
		for i := range dr {
			rows[i] = db.ChangelogGlobalRow(dr[i])
		}
	case domainID != nil:
		dr, err := s.svc.Q.ChangelogByDomain(r.Context(), db.ChangelogByDomainParams{
			DomainID: *domainID, Field: params.Field,
			WithFrom: params.WithFrom, FromTs: params.FromTs,
			WithTo: params.WithTo, ToTs: params.ToTs,
			WithSeek: params.WithSeek, SeekTs: params.SeekTs,
			SeekDomain: params.SeekDomain, SeekField: params.SeekField,
			Lim: params.Lim,
		})
		if err != nil {
			return nil, err
		}
		rows = make([]db.ChangelogGlobalRow, len(dr))
		for i := range dr {
			rows[i] = db.ChangelogGlobalRow(dr[i])
		}
	case backward:
		gr, err := s.svc.Q.ChangelogGlobalPrev(r.Context(), db.ChangelogGlobalPrevParams{
			Field:    params.Field,
			WithFrom: params.WithFrom, FromTs: params.FromTs,
			WithTo: params.WithTo, ToTs: params.ToTs,
			SeekTs: params.SeekTs, SeekDomain: params.SeekDomain, SeekField: params.SeekField,
			Lim: params.Lim,
		})
		if err != nil {
			return nil, err
		}
		rows = make([]db.ChangelogGlobalRow, len(gr))
		for i := range gr {
			rows[i] = db.ChangelogGlobalRow(gr[i])
		}
	default:
		var err error
		rows, err = s.svc.Q.ChangelogGlobal(r.Context(), *params)
		if err != nil {
			return nil, err
		}
	}
	if backward { // ASC fetch → re-reverse into the ts-DESC feed order
		reverseSlice(rows)
	}
	return rows, nil
}

func changelogItem(r *db.ChangelogGlobalRow) ChangelogItem {
	return ChangelogItem{
		TS:       r.Ts.Time.UTC(),
		Host:     r.Host,
		Field:    r.Field,
		OldValue: string(r.OldValue),
		NewValue: string(r.NewValue),
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
