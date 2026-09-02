package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// goldenFeedItems is the one fixture set behind testdata/feed/**: four
// structured changelog rows covering an em dash and the `&` in a composite
// item id (both need escaping, differently, in each format), a fractional
// timestamp, and the conn/resources/not_applicable transitions §7.7
// requires to appear in feeds.
func goldenFeedItems() []ChangelogItem {
	ts := func(s string) time.Time {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			panic(err)
		}
		return t
	}
	return []ChangelogItem{
		{TS: ts("2026-08-20T09:15:00Z"), Host: "example.no", Field: "www",
			OldValue: "not_applicable", NewValue: "unsupported"},
		{TS: ts("2026-08-19T23:04:05.123456789Z"), Host: "sub.example.no", Field: "conn",
			OldValue: "unsupported", NewValue: "supported"},
		{TS: ts("2026-08-19T08:00:00Z"), Host: "example.no", Field: "resources",
			OldValue: "supported", NewValue: "not_applicable"},
		{TS: ts("2026-08-18T12:30:45Z"), Host: "other.no", Field: "mx",
			OldValue: "supported", NewValue: "no_record"},
	}
}

// goldenFeedScopes is the §7.7 four-scope matrix over that one fixture set;
// the titles and list URLs are the shapes the scope loaders build. The
// per-domain scope carries only its own rows, as its query would.
func goldenFeedScopes() map[string]*feedScope {
	all := goldenFeedItems()
	var domainOnly []ChangelogItem
	for _, it := range all {
		if it.Host == "example.no" {
			domainOnly = append(domainOnly, it)
		}
	}
	return map[string]*feedScope{
		"global": {
			Title:   "WhyNoIPv6 — recent changes",
			ListURL: "https://whynoipv6.com/changelog",
			Items:   all,
		},
		"domain": {
			Title:   "WhyNoIPv6 — example.no",
			ListURL: "https://whynoipv6.com/domains/example.no/changelog",
			Items:   domainOnly,
		},
		"country": {
			Title:   "WhyNoIPv6 — Norway",
			ListURL: "https://whynoipv6.com/countries/NO/changelog",
			Items:   all,
		},
		"campaign": {
			Title:   "WhyNoIPv6 — Norwegian Government",
			ListURL: "https://whynoipv6.com/campaigns/8f1c2a5e-3b0d-4a7e-9c11-6d2f8a4b7e30/changelog",
			Items:   all,
		},
	}
}

// TestFeedGoldens is 10-testing §7.7's byte-exact feed bodies over the
// four-scope × two-format matrix. TestFeeds checks members and counts
// behaviourally, which a change to writeAtom's element order, the Atom
// namespace, or a JSON Feed member name would pass — these are documents
// other people's readers parse, so the bytes are the contract.
func TestFeedGoldens(t *testing.T) {
	s := &Server{opts: Options{PublicBaseURL: "https://whynoipv6.com"}}
	formats := []struct {
		ext         string
		contentType string
		write       func(w *httptest.ResponseRecorder, scope *feedScope)
	}{
		{
			ext:         ".atom",
			contentType: "application/atom+xml; charset=utf-8",
			write: func(w *httptest.ResponseRecorder, scope *feedScope) {
				s.writeAtom(w, httptest.NewRequest("GET", "/changelog.atom", nil), scope)
			},
		},
		{
			ext:         ".feed.json",
			contentType: "application/feed+json; charset=utf-8",
			write: func(w *httptest.ResponseRecorder, scope *feedScope) {
				s.writeJSONFeed(w, httptest.NewRequest("GET", "/changelog.feed.json", nil), scope)
			},
		},
	}
	for name, scope := range goldenFeedScopes() {
		for _, f := range formats {
			golden := name + f.ext
			t.Run(golden, func(t *testing.T) {
				want, err := os.ReadFile(filepath.Join("testdata", "feed", golden))
				if err != nil {
					t.Fatal(err)
				}
				w := httptest.NewRecorder()
				f.write(w, scope)
				if got := w.Header().Get("Content-Type"); got != f.contentType {
					t.Errorf("Content-Type = %q, want %q", got, f.contentType)
				}
				if got := w.Body.String(); got != string(want) {
					t.Errorf("body does not match testdata/feed/%s\n got: %s\nwant: %s",
						golden, got, want)
				}
			})
		}
	}
}
