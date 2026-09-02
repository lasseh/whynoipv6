package api

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/lasseh/whynoipv6/internal/postgres"
)

// The feed contract: the latest feed.recent_window transitions, no
// pagination (07 §5.4 — bulk consumers go to the datasets). NewRouter
// clamps a ≤0 override back to the spec default 50.

// feedScope names one changelog scope for rendering.
type feedScope struct {
	Title   string          // "WhyNoIPv6 — Norway"
	ListURL string          // the extension-less JSON list URL (feed id / alternate)
	Items   []ChangelogItem // newest first, ≤ feedWindow
}

// Atom (RFC 4287) document model — only the required members (07 §5.4).
type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
	Href string `xml:"href,attr"`
}

type atomEntry struct {
	ID      string   `xml:"id"`
	Title   string   `xml:"title"`
	Updated string   `xml:"updated"`
	Link    atomLink `xml:"link"`
	Content string   `xml:"content"`
}

type atomFeed struct {
	XMLName xml.Name `xml:"feed"`
	XMLNS   string   `xml:"xmlns,attr"`
	ID      string   `xml:"id"`
	Title   string   `xml:"title"`
	Updated string   `xml:"updated"`
	Author  struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Links   []atomLink  `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}

// jsonFeed is JSON Feed 1.1 with the required top-level members.
type jsonFeed struct {
	Version     string         `json:"version"`
	Title       string         `json:"title"`
	HomePageURL string         `json:"home_page_url"`
	FeedURL     string         `json:"feed_url"`
	Items       []jsonFeedItem `json:"items"`
}

type jsonFeedItem struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	Title         string `json:"title"`
	ContentText   string `json:"content_text"`
	DatePublished string `json:"date_published"`
}

// dimLabel is the human dimension name used in rendered feed copy — the
// exact §7.4 labels; the frontend's utils/changelog.ts renders from the
// same table and the goldens on both sides pin them together.
var dimLabel = map[string]string{
	"base": "the base domain", "www": "www", "ns": "nameservers", "mx": "mail",
}

// feedItemTitle derives the human title server-side at render time from
// (field, old_value, new_value) — never from a frozen message table. conn
// and resources get bespoke reachability wording (§7.4): the generic
// "{host} verb {label}" template misdescribes derived dimensions.
func feedItemTitle(it *ChangelogItem) string {
	fromNA := it.OldValue == "not_applicable"
	switch it.Field {
	case "conn":
		switch it.NewValue {
		case "supported":
			return it.Host + " is now reachable over IPv6"
		case "unsupported":
			if fromNA {
				return it.Host + " published IPv6 addresses — but connections fail"
			}
			return it.Host + " is no longer reachable over IPv6"
		default: // not_applicable — suppressed at write (03 §11); defensive only
			return it.Host + " has no IPv6 addresses left to test"
		}
	case "resources":
		switch it.NewValue {
		case "supported":
			return it.Host + " now passes the page-resource IPv6 grade"
		case "unsupported":
			return it.Host + " uses some page-resource hosts without IPv6"
		default: // not_applicable — suppressed at write (03 §11); defensive only
			return it.Host + " no longer has its page resources checked"
		}
	}
	label := dimLabel[it.Field]
	switch it.NewValue {
	case "supported":
		return fmt.Sprintf("%s now supports IPv6 on %s", it.Host, label)
	case "unsupported":
		if fromNA {
			return fmt.Sprintf("%s started using %s — without IPv6", it.Host, label)
		}
		return fmt.Sprintf("%s lost IPv6 on %s", it.Host, label)
	case "no_record":
		if fromNA {
			return fmt.Sprintf("%s started publishing %s — without IPv6 records", it.Host, label)
		}
		return fmt.Sprintf("%s no longer publishes records for %s", it.Host, label)
	default: // not_applicable
		return fmt.Sprintf("%s no longer uses %s", it.Host, label)
	}
}

func feedItemContent(it *ChangelogItem) string {
	return fmt.Sprintf("%s for %s changed from %s to %s.", it.Field, it.Host, it.OldValue, it.NewValue)
}

// feedItemID is the composite (host, ts, field) as a stable IRI.
func (s *Server) feedItemID(it *ChangelogItem) string {
	return fmt.Sprintf("%s/domains/%s/changelog?ts=%s&field=%s",
		s.opts.PublicBaseURL, it.Host, url.QueryEscape(it.TS.Format(time.RFC3339Nano)), it.Field)
}

// writeAtom renders the scope as application/atom+xml.
func (s *Server) writeAtom(w http.ResponseWriter, r *http.Request, scope *feedScope) {
	updated := time.Unix(0, 0).UTC()
	if len(scope.Items) > 0 {
		updated = scope.Items[0].TS
	}
	if CacheChangelog(w, r, updated) {
		return
	}
	f := atomFeed{
		XMLNS:   "http://www.w3.org/2005/Atom",
		ID:      scope.ListURL,
		Title:   scope.Title,
		Updated: updated.Format(time.RFC3339),
		Links: []atomLink{
			{Rel: "self", Type: "application/atom+xml", Href: scope.ListURL + ".atom"},
			{Rel: "alternate", Type: "application/json", Href: scope.ListURL},
		},
	}
	f.Author.Name = "WhyNoIPv6"
	for i := range scope.Items {
		it := &scope.Items[i]
		f.Entries = append(f.Entries, atomEntry{
			ID:      s.feedItemID(it),
			Title:   feedItemTitle(it),
			Updated: it.TS.Format(time.RFC3339),
			Link:    atomLink{Rel: "alternate", Type: "application/json", Href: s.opts.PublicBaseURL + "/domains/" + it.Host},
			Content: feedItemContent(it),
		})
	}
	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(f)
}

// writeJSONFeed renders the scope as application/feed+json.
func (s *Server) writeJSONFeed(w http.ResponseWriter, r *http.Request, scope *feedScope) {
	updated := time.Unix(0, 0).UTC()
	if len(scope.Items) > 0 {
		updated = scope.Items[0].TS
	}
	if CacheChangelog(w, r, updated) {
		return
	}
	f := jsonFeed{
		Version:     "https://jsonfeed.org/version/1.1",
		Title:       scope.Title,
		HomePageURL: scope.ListURL,
		FeedURL:     scope.ListURL + ".feed.json",
		Items:       []jsonFeedItem{},
	}
	for i := range scope.Items {
		it := &scope.Items[i]
		f.Items = append(f.Items, jsonFeedItem{
			ID:            s.feedItemID(it),
			URL:           s.opts.PublicBaseURL + "/domains/" + it.Host,
			Title:         feedItemTitle(it),
			ContentText:   feedItemContent(it),
			DatePublished: it.TS.Format(time.RFC3339),
		})
	}
	w.Header().Set("Content-Type", "application/feed+json; charset=utf-8")
	WriteJSONBody(w, f)
}

// Scope loaders — each returns the recent window for its scope.
//
// The global and per-domain scopes are index-backed (idx_changelog_ts, the
// per-domain PK) and take their size from opts.FeedRecentWindow. The country
// and campaign scopes deliberately do NOT: their queries carry a literal
// LIMIT 50 within 90 days, which 07 §3.3 and §4.8 make the guardrail for a
// scope that has no (scope_id, ts) index. An operator override must not be
// able to lift it — that lift rides OPEN-15 (09-ops §2 erratum).

func (s *Server) globalFeedScope(r *http.Request) (*feedScope, error) {
	rows, err := postgres.ListChangelog(r.Context(), s.pool, &postgres.ChangelogFilter{}, nil, s.opts.FeedRecentWindow, false)
	if err != nil {
		return nil, err
	}
	rows = rows[:min(len(rows), s.opts.FeedRecentWindow)] // drop the builder's N+1 probe row
	items := make([]ChangelogItem, len(rows))
	for i := range rows {
		items[i] = changelogItem(&rows[i])
	}
	return &feedScope{
		Title:   "WhyNoIPv6 — recent changes",
		ListURL: s.opts.PublicBaseURL + "/changelog",
		Items:   items,
	}, nil
}

func (s *Server) domainFeedScope(w http.ResponseWriter, r *http.Request) (*feedScope, bool) {
	d, ok := s.domainByPathHost(w, r)
	if !ok {
		return nil, false
	}
	rows, err := postgres.ListChangelog(r.Context(), s.pool,
		&postgres.ChangelogFilter{DomainID: &d.ID}, nil, s.opts.FeedRecentWindow, false)
	if err != nil {
		InternalError(w, r, err)
		return nil, false
	}
	rows = rows[:min(len(rows), s.opts.FeedRecentWindow)] // drop the builder's N+1 probe row
	items := make([]ChangelogItem, len(rows))
	for i := range rows {
		items[i] = changelogItem(&rows[i])
	}
	return &feedScope{
		Title:   "WhyNoIPv6 — " + d.Host,
		ListURL: s.opts.PublicBaseURL + "/domains/" + d.Host + "/changelog",
		Items:   items,
	}, true
}

func (s *Server) countryFeedScope(w http.ResponseWriter, r *http.Request) (*feedScope, bool) {
	c, ok := s.countryByPathCode(w, r)
	if !ok {
		return nil, false
	}
	id, err := s.q.CountryIDByCode(r.Context(), strings.TrimSpace(c.Code))
	if err != nil {
		InternalError(w, r, err)
		return nil, false
	}
	rows, err := s.q.ChangelogByCountry(r.Context(), id)
	if err != nil {
		InternalError(w, r, err)
		return nil, false
	}
	return &feedScope{
		Title:   "WhyNoIPv6 — " + c.Name,
		ListURL: s.opts.PublicBaseURL + "/countries/" + strings.TrimSpace(c.Code) + "/changelog",
		Items:   scopedFeedItems(rows),
	}, true
}

func (s *Server) campaignFeedScope(w http.ResponseWriter, r *http.Request) (*feedScope, bool) {
	row, ok := s.campaignByPathUUID(w, r)
	if !ok {
		return nil, false
	}
	rows, err := s.q.ChangelogByCampaign(r.Context(), row.ID)
	if err != nil {
		InternalError(w, r, err)
		return nil, false
	}
	// The canonical lowercase form, not the path segment as typed:
	// uuid.Parse accepts braces, urn: and uppercase, and the feed <id> and
	// self link must not vary with the spelling (07 §5.4).
	return &feedScope{
		Title:   "WhyNoIPv6 — " + row.Name,
		ListURL: s.opts.PublicBaseURL + "/campaigns/" + uuid.UUID(row.Uuid.Bytes).String() + "/changelog",
		Items:   scopedFeedItems(rows),
	}, true
}

// The eight feed handlers (4 scopes × 2 formats).

func (s *Server) globalAtom(w http.ResponseWriter, r *http.Request) {
	scope, err := s.globalFeedScope(r)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	s.writeAtom(w, r, scope)
}

func (s *Server) globalJSONFeed(w http.ResponseWriter, r *http.Request) {
	scope, err := s.globalFeedScope(r)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	s.writeJSONFeed(w, r, scope)
}

func (s *Server) domainAtom(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.domainFeedScope(w, r); ok {
		s.writeAtom(w, r, scope)
	}
}

func (s *Server) domainJSONFeed(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.domainFeedScope(w, r); ok {
		s.writeJSONFeed(w, r, scope)
	}
}

func (s *Server) countryAtom(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.countryFeedScope(w, r); ok {
		s.writeAtom(w, r, scope)
	}
}

func (s *Server) countryJSONFeed(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.countryFeedScope(w, r); ok {
		s.writeJSONFeed(w, r, scope)
	}
}

func (s *Server) campaignAtom(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.campaignFeedScope(w, r); ok {
		s.writeAtom(w, r, scope)
	}
}

func (s *Server) campaignJSONFeed(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.campaignFeedScope(w, r); ok {
		s.writeJSONFeed(w, r, scope)
	}
}
