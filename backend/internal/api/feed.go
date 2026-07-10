package api

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/whynoipv6/internal/domain"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// feedWindow is the fixed feed contract: the latest 50 transitions, no
// pagination (07 §5.4 — bulk consumers go to the datasets).
const feedWindow = 50

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

// dimLabel is the human dimension name used in rendered feed copy.
var dimLabel = map[string]string{
	"base": "the apex", "www": "www", "ns": "nameservers",
	"mx": "mail", "conn": "connectivity", "resources": "page resources",
}

// feedItemTitle derives the human title server-side at render time from
// (field, old_value, new_value) — never from a frozen message table.
func feedItemTitle(it *ChangelogItem) string {
	label := dimLabel[it.Field]
	switch it.NewValue {
	case "supported":
		return fmt.Sprintf("%s now supports IPv6 on %s", it.Host, label)
	case "unsupported":
		return fmt.Sprintf("%s no longer supports IPv6 on %s", it.Host, label)
	case "no_record":
		return fmt.Sprintf("%s no longer publishes records for %s", it.Host, label)
	default: // not_applicable
		return fmt.Sprintf("%s: %s is no longer applicable", it.Host, label)
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

// Scope loaders — each returns the latest-50 window for its scope.

func (s *Server) globalFeedScope(r *http.Request) (*feedScope, error) {
	rows, err := s.svc.Q.ChangelogGlobal(r.Context(), db.ChangelogGlobalParams{Lim: feedWindow})
	if err != nil {
		return nil, err
	}
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
	host, err := domain.Canonicalize(chi.URLParam(r, "host"))
	if err != nil {
		NotFound(w, r, "Domain not found", "The host is not a valid public domain name.")
		return nil, false
	}
	d, err := s.svc.Q.DomainByHost(r.Context(), host)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Domain not found", "No such domain: "+host)
		return nil, false
	}
	if err != nil {
		InternalError(w, r)
		return nil, false
	}
	rows, err := s.svc.Q.ChangelogByDomain(r.Context(), db.ChangelogByDomainParams{DomainID: d.ID, Lim: feedWindow})
	if err != nil {
		InternalError(w, r)
		return nil, false
	}
	items := make([]ChangelogItem, len(rows))
	for i := range rows {
		items[i] = changelogItem((*db.ChangelogGlobalRow)(&rows[i]))
	}
	return &feedScope{
		Title:   "WhyNoIPv6 — " + host,
		ListURL: s.opts.PublicBaseURL + "/domains/" + host + "/changelog",
		Items:   items,
	}, true
}

func (s *Server) countryFeedScope(w http.ResponseWriter, r *http.Request) (*feedScope, bool) {
	code := chi.URLParam(r, "code")
	if len(code) != 2 {
		NotFound(w, r, "Country not found", "Country codes are two-letter ISO 3166-1 alpha-2.")
		return nil, false
	}
	c, err := s.svc.Q.CountryByCode(r.Context(), code)
	if errors.Is(err, pgx.ErrNoRows) {
		NotFound(w, r, "Country not found", "No such country: "+code)
		return nil, false
	}
	if err != nil {
		InternalError(w, r)
		return nil, false
	}
	id, err := s.svc.Q.CountryIDByCode(r.Context(), code)
	if err != nil {
		InternalError(w, r)
		return nil, false
	}
	rows, err := s.svc.Q.ChangelogByCountry(r.Context(), id)
	if err != nil {
		InternalError(w, r)
		return nil, false
	}
	return &feedScope{
		Title:   "WhyNoIPv6 — " + c.Name,
		ListURL: s.opts.PublicBaseURL + "/countries/" + strings.TrimSpace(c.Code) + "/changelog",
		Items:   changelogWindowItems(rows),
	}, true
}

func (s *Server) campaignFeedScope(w http.ResponseWriter, r *http.Request) (*feedScope, bool) {
	row, ok := s.campaignByPathUUID(w, r)
	if !ok {
		return nil, false
	}
	rows, err := s.svc.Q.ChangelogByCampaign(r.Context(), row.ID)
	if err != nil {
		InternalError(w, r)
		return nil, false
	}
	return &feedScope{
		Title:   "WhyNoIPv6 — " + row.Name,
		ListURL: s.opts.PublicBaseURL + "/campaigns/" + chi.URLParam(r, "uuid") + "/changelog",
		Items:   changelogCampaignItems(rows),
	}, true
}

// The eight feed handlers (4 scopes × 2 formats).

func (s *Server) globalAtom(w http.ResponseWriter, r *http.Request) {
	scope, err := s.globalFeedScope(r)
	if err != nil {
		InternalError(w, r)
		return
	}
	s.writeAtom(w, r, scope)
}

func (s *Server) globalJSONFeed(w http.ResponseWriter, r *http.Request) {
	scope, err := s.globalFeedScope(r)
	if err != nil {
		InternalError(w, r)
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
