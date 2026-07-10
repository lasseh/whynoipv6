//go:build integration

package api_test

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
)

func fetch(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

// TestFeeds (P4.15 / 07 §5.4): every scope×format feed carries the required
// top-level members, the latest-50 window, and the composite item id.
func TestFeeds(t *testing.T) {
	srv, pool := newAPI(t)
	seedEntities(t, pool)
	seedChangelog(t, pool)

	type atomDoc struct {
		ID      string `xml:"id"`
		Title   string `xml:"title"`
		Updated string `xml:"updated"`
		Links   []struct {
			Rel  string `xml:"rel,attr"`
			Href string `xml:"href,attr"`
		} `xml:"link"`
		Entries []struct {
			ID      string `xml:"id"`
			Title   string `xml:"title"`
			Updated string `xml:"updated"`
			Content string `xml:"content"`
		} `xml:"entry"`
	}

	atomScopes := map[string]struct {
		path, title string
		entries     int
	}{
		"global":   {"/changelog.atom", "WhyNoIPv6 — recent changes", 3},
		"domain":   {"/domains/d3.example/changelog.atom", "WhyNoIPv6 — d3.example", 2},
		"country":  {"/countries/no/changelog.atom", "WhyNoIPv6 — Norway", 2},
		"campaign": {"/campaigns/" + campaignUUID + "/changelog.atom", "WhyNoIPv6 — Norwegian Banks", 2},
	}
	for name, tc := range atomScopes {
		resp, body := fetch(t, srv.URL+tc.path)
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/atom+xml") {
			t.Fatalf("%s atom: %d %s", name, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		var doc atomDoc
		if err := xml.Unmarshal(body, &doc); err != nil {
			t.Fatalf("%s atom parse: %v", name, err)
		}
		if doc.ID == "" || doc.Title != tc.title || doc.Updated == "" {
			t.Errorf("%s atom feed members: id=%q title=%q updated=%q", name, doc.ID, doc.Title, doc.Updated)
		}
		var self, alternate bool
		for _, l := range doc.Links {
			self = self || (l.Rel == "self" && strings.HasSuffix(l.Href, ".atom"))
			alternate = alternate || (l.Rel == "alternate" && strings.HasSuffix(l.Href, "/changelog"))
		}
		if !self || !alternate {
			t.Errorf("%s atom links: self=%t alternate=%t", name, self, alternate)
		}
		if len(doc.Entries) != tc.entries {
			t.Errorf("%s atom entries = %d, want %d", name, len(doc.Entries), tc.entries)
		}
		for _, e := range doc.Entries {
			// The composite (host, ts, field) id.
			if !strings.Contains(e.ID, "ts=") || !strings.Contains(e.ID, "field=") || !strings.Contains(e.ID, "/domains/") {
				t.Errorf("%s entry id %q lacks the composite (host, ts, field)", name, e.ID)
			}
			if e.Title == "" || e.Content == "" {
				t.Errorf("%s entry missing rendered title/content: %+v", name, e)
			}
		}
	}

	jsonScopes := map[string]string{
		"global":   "/changelog.feed.json",
		"domain":   "/domains/d3.example/changelog.feed.json",
		"country":  "/countries/no/changelog.feed.json",
		"campaign": "/campaigns/" + campaignUUID + "/changelog.feed.json",
	}
	for name, path := range jsonScopes {
		resp, body := fetch(t, srv.URL+path)
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/feed+json") {
			t.Fatalf("%s json feed: %d %s", name, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		var feed struct {
			Version     string `json:"version"`
			Title       string `json:"title"`
			HomePageURL string `json:"home_page_url"`
			FeedURL     string `json:"feed_url"`
			Items       []struct {
				ID            string `json:"id"`
				Title         string `json:"title"`
				ContentText   string `json:"content_text"`
				DatePublished string `json:"date_published"`
			} `json:"items"`
		}
		if err := json.Unmarshal(body, &feed); err != nil {
			t.Fatalf("%s json feed parse: %v", name, err)
		}
		if feed.Version != "https://jsonfeed.org/version/1.1" || feed.Title == "" ||
			feed.HomePageURL == "" || !strings.HasSuffix(feed.FeedURL, ".feed.json") {
			t.Errorf("%s json feed members: %+v", name, feed)
		}
		if len(feed.Items) == 0 {
			t.Errorf("%s json feed empty", name)
		}
		for _, it := range feed.Items {
			if !strings.Contains(it.ID, "ts=") || it.Title == "" || it.ContentText == "" || it.DatePublished == "" {
				t.Errorf("%s item %+v", name, it)
			}
		}
	}

	// Rendered copy: the www transition reads as a support gain.
	_, body := fetch(t, srv.URL+"/domains/d3.example/changelog.feed.json")
	if !strings.Contains(string(body), "d3.example now supports IPv6 on www") {
		t.Errorf("rendered title missing from feed: %s", body)
	}
}

// TestCSV (P4.15 / 07 §5.5): defined column set, attachment disposition,
// stable after_rank-anchored URL, format=csv on every list class.
func TestCSV(t *testing.T) {
	srv, pool := newAPI(t)
	seedEntities(t, pool)
	seedChangelog(t, pool)

	resp, body := fetch(t, srv.URL+"/domains?format=csv")
	if resp.StatusCode != 200 ||
		!strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("domains csv: %d %s %s", resp.StatusCode,
			resp.Header.Get("Content-Type"), resp.Header.Get("Content-Disposition"))
	}
	records, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 10 { // header + 9 visible rows
		t.Fatalf("domains csv rows = %d, want 10", len(records)-1+1)
	}
	header := strings.Join(records[0], ",")
	for _, col := range []string{"host", "rank", "classification", "gold", "base_status", "country_code", "asn_number", "hosting_provider", "last_checked_at"} {
		if !strings.Contains(header, col) {
			t.Errorf("domains csv header missing %s: %s", col, header)
		}
	}
	if records[1][0] != "d1.example" || records[1][1] != "1" {
		t.Errorf("domains csv row 1 = %v", records[1][:3])
	}

	// A stable after_rank-anchored URL reproduces the same view.
	_, anchored := fetch(t, srv.URL+"/domains?after_rank=7&format=csv")
	rec2, _ := csv.NewReader(strings.NewReader(string(anchored))).ReadAll()
	if len(rec2) < 2 || rec2[1][0] != "d8.example" {
		t.Errorf("after_rank csv first row = %v, want d8.example", rec2[1][:2])
	}
	_, anchoredAgain := fetch(t, srv.URL+"/domains?after_rank=7&format=csv")
	if string(anchored) != string(anchoredAgain) {
		t.Error("anchored CSV view is not stable across fetches")
	}

	// The other list classes accept format=csv.
	for path, firstCol := range map[string]string{
		"/countries?format=csv": "code",
		"/asns?format=csv":      "number",
		"/providers?format=csv": "id",
		"/changelog?format=csv": "ts",
	} {
		resp, body := fetch(t, srv.URL+path)
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") {
			t.Errorf("%s: %d %s", path, resp.StatusCode, resp.Header.Get("Content-Type"))
			continue
		}
		rec, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
		if err != nil || len(rec) < 2 || rec[0][0] != firstCol {
			t.Errorf("%s csv shape: err=%v header=%v rows=%d", path, err, rec[0], len(rec)-1)
		}
	}

	// Bad format value → 400 invalid-parameter.
	var problem struct{ Type string }
	if resp := getJSON(t, srv.URL+"/domains?format=xml", &problem); resp.StatusCode != 400 {
		t.Errorf("bad format: %d", resp.StatusCode)
	}
}
