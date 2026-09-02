//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/crawler"
)

const campaignUUID = "11111111-1111-1111-1111-111111111111"

// seedEntities layers countries/asn counters, a campaign with members, a
// subdomain, and a resource host with mixed-rank dependents on top of
// seedLeaderboard.
func seedEntities(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		`UPDATE country SET sites = 3, v6sites = 2, percent = 66.67 WHERE code = 'NO'`,
		`UPDATE asn SET count_total = 3, count_v6 = 2 WHERE number = 2119`,
		`UPDATE dns_provider SET count_total = 1, count_v6 = 1 WHERE name = 'Cloudflare'`,
		// A subdomain of d1 (rank NULL — visible only on the sub-collection).
		`INSERT INTO domain (host, kind, parent_id, created_by, asn_id, country_id, tld, classification)
		 VALUES ('sub.d1.example', 'subdomain', (SELECT id FROM domain WHERE host = 'd1.example'),
		         'parent_link', (SELECT id FROM asn WHERE number = 2119),
		         (SELECT id FROM country WHERE code = 'NO'), 'example', 'hero')`,
		// Campaign with two members: one ranked, one rank-NULL.
		`INSERT INTO campaign (uuid, name, description, source_file, tags)
		 VALUES ('` + campaignUUID + `', 'Norwegian Banks', 'Retail banks operating in Norway',
		         'campaigns/no-banks.yaml', '{mandate,sector-banking}')`,
		`INSERT INTO campaign_domain (campaign_id, domain_id)
		 SELECT c.id, d.id FROM campaign c, domain d
		 WHERE c.uuid = '` + campaignUUID + `' AND d.host IN ('d3.example', 'campaign-only.example')`,
		`INSERT INTO stats_campaign_daily (day, campaign_id, domains, v6_ready)
		 SELECT current_date, id, 2, 1 FROM campaign WHERE uuid = '` + campaignUUID + `'`,
		// Resource host with three dependents: two ranked + one rank-NULL.
		`INSERT INTO resource_host (host, aaaa_status, dependent_count, last_checked_at)
		 VALUES ('fonts.example', 'unsupported', 3, now())`,
		`INSERT INTO domain_resource (domain_id, resource_host_id, source, required)
		 SELECT d.id, rh.id, 'discovered', true FROM domain d, resource_host rh
		 WHERE rh.host = 'fonts.example' AND d.host IN ('d1.example', 'd3.example', 'campaign-only.example')`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("seed entities: %v\n%s", err, s)
		}
	}
}

func newEntityAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv, pool := newAPI(t)
	seedEntities(t, pool)
	return srv
}

// TestCountries (07 §4.5): bounded leaderboard with exact count, detail with
// direct NUMERIC percent, scoped domain list ≡ ?country=.
func TestCountries(t *testing.T) {
	srv := newEntityAPI(t)
	var env envelope
	getJSON(t, srv.URL+"/countries", &env)
	if env.Meta.Count == nil || *env.Meta.Count < 200 {
		t.Fatalf("countries count = %v, want the full ISO set", env.Meta.Count)
	}
	var top struct {
		Code    string  `json:"code"`
		Percent float64 `json:"percent"`
	}
	raw, _ := json.Marshal(env.Items[0])
	_ = json.Unmarshal(raw, &top)
	if top.Code != "NO" || top.Percent != 66.67 {
		t.Errorf("top country = %+v, want NO at 66.67 (percent sort)", top)
	}

	var detail struct {
		Code    string  `json:"code"`
		Name    string  `json:"name"`
		TLD     *string `json:"tld"`
		Sites   int32   `json:"sites"`
		V6Sites int32   `json:"v6_sites"`
		Percent float64 `json:"percent"`
		Meta    *struct {
			Generation int32 `json:"generation"`
		} `json:"meta"`
	}
	resp := getJSON(t, srv.URL+"/countries/no", &detail)
	if resp.StatusCode != 200 || detail.Code != "NO" || detail.Name != "Norway" ||
		detail.V6Sites != 2 || detail.Percent != 66.67 || detail.Meta == nil {
		t.Errorf("country detail = %+v", detail)
	}
	if detail.TLD == nil || *detail.TLD != ".NO" {
		t.Errorf("country tld = %v", detail.TLD)
	}

	var scoped, filtered envelope
	getJSON(t, srv.URL+"/countries/no/domains", &scoped)
	getJSON(t, srv.URL+"/domains?country=NO", &filtered)
	if fmt.Sprint(hosts(t, scoped.Items)) != fmt.Sprint(hosts(t, filtered.Items)) || len(scoped.Items) != 3 {
		t.Errorf("/countries/no/domains %v != /domains?country=NO %v",
			hosts(t, scoped.Items), hosts(t, filtered.Items))
	}

	var problem struct{ Type string }
	resp = getJSON(t, srv.URL+"/countries/xq", &problem)
	if resp.StatusCode != 404 {
		t.Errorf("unknown country: %d", resp.StatusCode)
	}
}

// TestAsns (07 §4.6): count_v4 synthesized, sort/q variants, scoped list.
func TestAsns(t *testing.T) {
	srv := newEntityAPI(t)
	var detail struct {
		Number     int64 `json:"number"`
		CountTotal int32 `json:"count_total"`
		CountV6    int32 `json:"count_v6"`
		CountV4    int32 `json:"count_v4"`
	}
	getJSON(t, srv.URL+"/asns/2119", &detail)
	if detail.CountTotal != 3 || detail.CountV6 != 2 || detail.CountV4 != 1 {
		t.Errorf("asn detail = %+v (count_v4 must be total−v6)", detail)
	}

	var env envelope
	getJSON(t, srv.URL+"/asns", &env)
	var first struct {
		Number int64 `json:"number"`
	}
	raw, _ := json.Marshal(env.Items[0])
	_ = json.Unmarshal(raw, &first)
	if first.Number != 2119 { // count_v6=2 beats the 0-sentinel
		t.Errorf("asns[0] = %+v, want AS2119 leading count_v6 sort", first)
	}

	var searched envelope
	getJSON(t, srv.URL+"/asns?q=telenor", &searched)
	if len(searched.Items) != 1 {
		t.Errorf("q=telenor matched %d networks", len(searched.Items))
	}

	// Keyset page walk with limit=1 covers both networks exactly once.
	seen := map[int64]bool{}
	url := srv.URL + "/asns?limit=1"
	for {
		var page envelope
		getJSON(t, url, &page)
		for _, it := range page.Items {
			var row struct {
				Number int64 `json:"number"`
			}
			raw, _ := json.Marshal(it)
			_ = json.Unmarshal(raw, &row)
			if seen[row.Number] {
				t.Fatalf("AS%d repeated across pages", row.Number)
			}
			seen[row.Number] = true
		}
		if !page.Page.HasMore {
			break
		}
		url = srv.URL + "/asns?limit=1&cursor=" + *page.Page.NextCursor
	}
	if len(seen) != 2 {
		t.Errorf("asn walk covered %d networks, want 2", len(seen))
	}

	var scoped, filtered envelope
	getJSON(t, srv.URL+"/asns/2119/domains", &scoped)
	getJSON(t, srv.URL+"/domains?asn=2119", &filtered)
	if fmt.Sprint(hosts(t, scoped.Items)) != fmt.Sprint(hosts(t, filtered.Items)) || len(scoped.Items) != 3 {
		t.Errorf("asn-scoped %v != ?asn= %v", hosts(t, scoped.Items), hosts(t, filtered.Items))
	}

	var problem struct{ Type string }
	if resp := getJSON(t, srv.URL+"/asns/99999", &problem); resp.StatusCode != 404 {
		t.Errorf("unknown asn: %d", resp.StatusCode)
	}
}

// TestProviders (07 §4.6): exact-count league table + scoped list.
// TestHosting (07 §4.6/§3.4): the hosting league is a bounded curated set
// with exact meta.count, served from counters the tick recomputes rather
// than a live GROUP BY over the largest table.
func TestHosting(t *testing.T) {
	srv, pool := newAPI(t)
	ctx := context.Background()

	// seedLeaderboard stamps one domain with an unmapped hosting value; give
	// it a second so the fallback covers more than a single row, and confirm
	// a curated slug keeps its display name.
	if _, err := pool.Exec(ctx,
		`UPDATE domain SET hosting_provider = 'cloudflare' WHERE host IN ('d3.example', 'd5.example')`); err != nil {
		t.Fatal(err)
	}
	if err := crawler.RunStatsRollup(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Items []struct {
			Slug       string `json:"slug"`
			Name       string `json:"name"`
			CountTotal int32  `json:"count_total"`
			CountV6    int32  `json:"count_v6"`
			CountV4    int32  `json:"count_v4"`
		} `json:"items"`
		Page struct {
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		} `json:"page"`
		Meta struct {
			Count *int64 `json:"count"`
		} `json:"meta"`
	}
	getJSON(t, srv.URL+"/hosting", &got)

	if got.Meta.Count == nil || int(*got.Meta.Count) != len(got.Items) {
		t.Errorf("meta.count = %v with %d items — bounded sets get an exact count",
			got.Meta.Count, len(got.Items))
	}
	// Bounded, but the page object still ships so the type never varies.
	if got.Page.HasMore || got.Page.NextCursor != nil {
		t.Errorf("page = %+v, want has_more false and null cursors", got.Page)
	}

	bySlug := map[string]int{}
	for i, item := range got.Items {
		bySlug[item.Slug] = i
		if item.CountTotal != item.CountV6+item.CountV4 {
			t.Errorf("%s: %d != %d + %d", item.Slug, item.CountTotal, item.CountV6, item.CountV4)
		}
		if item.Slug == "" || item.Name == "" {
			t.Errorf("slug and name are both required: %+v", item)
		}
	}

	// A curated slug carries its display name, not the raw join key.
	cf, ok := bySlug["cloudflare"]
	if !ok {
		t.Fatalf("cloudflare missing from the league: %+v", got.Items)
	}
	if got.Items[cf].Name != "Cloudflare" {
		t.Errorf("cloudflare name = %q, want the display string", got.Items[cf].Name)
	}
	// d3 and d5 are ranked and live; d3 is a hero, d5 is a hero (odd ranks).
	if got.Items[cf].CountTotal != 2 {
		t.Errorf("cloudflare count_total = %d, want 2", got.Items[cf].CountTotal)
	}

	// An unmapped slug appears under its own slug rather than vanishing — a
	// newly attributed CDN must not silently drop out of the league.
	unmapped, ok := bySlug["Amazon CloudFront"]
	if !ok {
		t.Errorf("unmapped slug dropped from the league: %+v", got.Items)
	} else if got.Items[unmapped].Name != "Amazon CloudFront" {
		t.Errorf("unmapped name = %q, want it to fall back to the slug", got.Items[unmapped].Name)
	}

	// NULL hosting_provider contributes nothing; there is no sentinel value.
	for _, item := range got.Items {
		if item.Slug == "unattributed" {
			t.Error("there is no unattributed sentinel — unattributed hosting is NULL")
		}
	}

	// CSV parity with the other league endpoints.
	resp, body := fetch(t, srv.URL+"/hosting?format=csv")
	if resp.StatusCode != 200 {
		t.Fatalf("csv: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("csv content-type = %q", ct)
	}
	if !strings.HasPrefix(string(body), "slug,name,count_total,count_v6,count_v4") {
		t.Errorf("csv header = %.60q", body)
	}
}

func TestProviders(t *testing.T) {
	srv := newEntityAPI(t)
	var env envelope
	getJSON(t, srv.URL+"/providers", &env)
	if env.Meta.Count == nil || *env.Meta.Count != 1 {
		t.Fatalf("providers count = %v", env.Meta.Count)
	}
	var row struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		CountV4 int32  `json:"count_v4"`
	}
	raw, _ := json.Marshal(env.Items[0])
	_ = json.Unmarshal(raw, &row)
	if row.Name != "Cloudflare" || row.CountV4 != 0 {
		t.Errorf("provider row = %+v", row)
	}

	// The path form is an indexed scope in itself (§4.6); the ?provider=
	// filter form is a residual and needs a class/country/asn scope (§3.3).
	var scoped, filtered envelope
	getJSON(t, srv.URL+fmt.Sprintf("/providers/%d/domains", row.ID), &scoped)
	getJSON(t, srv.URL+fmt.Sprintf("/domains?class=hero&provider=%d", row.ID), &filtered)
	if fmt.Sprint(hosts(t, scoped.Items)) != fmt.Sprint(hosts(t, filtered.Items)) || len(scoped.Items) != 1 {
		t.Errorf("provider-scoped %v != scoped ?provider= %v", hosts(t, scoped.Items), hosts(t, filtered.Items))
	}
	var problem2 struct{ Type string }
	if resp := getJSON(t, srv.URL+fmt.Sprintf("/domains?provider=%d", row.ID), &problem2); resp.StatusCode != 422 {
		t.Errorf("bare ?provider= must be scope-required, got %d", resp.StatusCode)
	}

	var problem struct{ Type string }
	if resp := getJSON(t, srv.URL+"/providers/424242", &problem); resp.StatusCode != 404 {
		t.Errorf("unknown provider: %d", resp.StatusCode)
	}
}

// TestCampaigns (07 §4.7): ?tag= filter, composite detail with adoption and
// host-ordered members (rank-NULL visible), exact count.
func TestCampaigns(t *testing.T) {
	srv, pool := newAPI(t)
	seedEntities(t, pool)

	// A second campaign with no stats_campaign_daily row: its list-row
	// adoption must be JSON null (pre-first-rollup).
	const bareUUID = "22222222-2222-2222-2222-222222222222"
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO campaign (uuid, name, description, source_file, tags)
		 VALUES ($1, 'Fresh Campaign', 'no stats yet', 'campaigns/fresh.yaml', '{sector-tech}')`,
		bareUUID); err != nil {
		t.Fatalf("seed bare campaign: %v", err)
	}

	// The list rows carry the same adoption block as the detail (07 §4.7).
	var list struct {
		Items []struct {
			UUID     string `json:"uuid"`
			Adoption *struct {
				V6ReadyPercent float64 `json:"v6_ready_percent"`
				Day            string  `json:"day"`
			} `json:"adoption"`
		} `json:"items"`
		Meta struct {
			Count *int64 `json:"count"`
		} `json:"meta"`
	}
	getJSON(t, srv.URL+"/campaigns", &list)
	if list.Meta.Count == nil || *list.Meta.Count != 2 {
		t.Fatalf("campaigns count = %v, want 2", list.Meta.Count)
	}
	adoptionByUUID := map[string]*struct {
		V6ReadyPercent float64 `json:"v6_ready_percent"`
		Day            string  `json:"day"`
	}{}
	for _, it := range list.Items {
		adoptionByUUID[it.UUID] = it.Adoption
	}
	if a := adoptionByUUID[campaignUUID]; a == nil || a.V6ReadyPercent != 50.0 || a.Day == "" {
		t.Errorf("list adoption for seeded campaign = %+v, want 50.0 with a day", a)
	}
	if a, ok := adoptionByUUID[bareUUID]; !ok || a != nil {
		t.Errorf("list adoption for stats-less campaign = %+v, want JSON null", a)
	}

	var tagged envelope
	getJSON(t, srv.URL+"/campaigns?tag=mandate", &tagged)
	if len(tagged.Items) != 1 {
		t.Errorf("tag=mandate matched %d", len(tagged.Items))
	}
	getJSON(t, srv.URL+"/campaigns?tag=absent", &tagged)
	if len(tagged.Items) != 0 {
		t.Errorf("tag=absent matched %d", len(tagged.Items))
	}

	var detail struct {
		UUID     string   `json:"uuid"`
		Name     string   `json:"name"`
		Tags     []string `json:"tags"`
		Disabled bool     `json:"disabled"`
		Adoption *struct {
			V6ReadyPercent float64 `json:"v6_ready_percent"`
			Day            string  `json:"day"`
		} `json:"adoption"`
		Domains struct {
			Items []struct {
				Host    string `json:"host"`
				Rank    *int32 `json:"rank"`
				V6Ready *bool  `json:"v6_ready"`
			} `json:"items"`
			Page struct {
				HasMore bool `json:"has_more"`
			} `json:"page"`
		} `json:"domains"`
		Meta struct {
			Count *int64 `json:"count"`
		} `json:"meta"`
	}
	getJSON(t, srv.URL+"/campaigns/"+campaignUUID, &detail)
	if detail.UUID != campaignUUID || detail.Name != "Norwegian Banks" || len(detail.Tags) != 2 {
		t.Errorf("campaign detail = %+v", detail)
	}
	if detail.Adoption == nil || detail.Adoption.V6ReadyPercent != 50.0 {
		t.Errorf("adoption = %+v, want 50.0", detail.Adoption)
	}
	if detail.Meta.Count == nil || *detail.Meta.Count != 2 {
		t.Errorf("campaign count = %v, want exact 2", detail.Meta.Count)
	}
	// Members: host-ordered, the rank-NULL member visible.
	if len(detail.Domains.Items) != 2 ||
		detail.Domains.Items[0].Host != "campaign-only.example" ||
		detail.Domains.Items[1].Host != "d3.example" {
		t.Errorf("members = %+v, want host-ordered [campaign-only.example d3.example]", detail.Domains.Items)
	}
	if detail.Domains.Items[0].Rank != nil {
		t.Errorf("campaign-only rank = %v, want JSON null", *detail.Domains.Items[0].Rank)
	}
	// Campaign membership rows carry the server-derived v6_ready flag
	// (the row highlight consumes it; the frontend never re-derives).
	for _, m := range detail.Domains.Items {
		if m.V6Ready == nil {
			t.Errorf("member %s: v6_ready missing", m.Host)
		}
	}

	var members envelope
	getJSON(t, srv.URL+"/campaigns/"+campaignUUID+"/domains", &members)
	if h := hosts(t, members.Items); len(h) != 2 || h[0] != "campaign-only.example" {
		t.Errorf("/campaigns/{uuid}/domains = %v", h)
	}

	var problem struct{ Type string }
	if resp := getJSON(t, srv.URL+"/campaigns/not-a-uuid", &problem); resp.StatusCode != 404 {
		t.Errorf("malformed campaign uuid: %d", resp.StatusCode)
	}

	// A disabled member (dead, delisted, operator-disabled) leaves the
	// walkable set and the adoption denominator; domain_count and
	// meta.count follow, so the exact count never exceeds the walk.
	if _, err := pool.Exec(context.Background(),
		`UPDATE domain SET disabled = true, disabled_reason = 'dead', disabled_at = now() WHERE host = 'd3.example'`); err != nil {
		t.Fatalf("disable member: %v", err)
	}
	getJSON(t, srv.URL+"/campaigns/"+campaignUUID, &detail)
	if detail.Meta.Count == nil || *detail.Meta.Count != 1 || len(detail.Domains.Items) != 1 {
		t.Errorf("after disabling a member: count = %v with %d members, want 1 and 1",
			detail.Meta.Count, len(detail.Domains.Items))
	}
	var rows struct {
		Items []struct {
			UUID        string `json:"uuid"`
			DomainCount int64  `json:"domain_count"`
		} `json:"items"`
	}
	getJSON(t, srv.URL+"/campaigns", &rows)
	for _, it := range rows.Items {
		if it.UUID == campaignUUID && it.DomainCount != 1 {
			t.Errorf("list domain_count after disabling a member = %d, want 1", it.DomainCount)
		}
	}
}

// TestSubdomains: the native sub-collection resolves rank-NULL children.
func TestSubdomains(t *testing.T) {
	srv := newEntityAPI(t)
	var env envelope
	getJSON(t, srv.URL+"/domains/d1.example/subdomains", &env)
	if h := hosts(t, env.Items); len(h) != 1 || h[0] != "sub.d1.example" {
		t.Fatalf("subdomains = %v", h)
	}
	if env.Meta.Count == nil || *env.Meta.Count != 1 {
		t.Errorf("subdomain count = %v, want exact 1", env.Meta.Count)
	}
	// The parent detail's subdomain_count agrees.
	var detail struct {
		SubdomainCount int64 `json:"subdomain_count"`
	}
	getJSON(t, srv.URL+"/domains/d1.example", &detail)
	if detail.SubdomainCount != 1 {
		t.Errorf("detail subdomain_count = %d", detail.SubdomainCount)
	}
}

// TestResources (07 §4.11): forward list exact count + link attrs; reverse
// dependents null-flag-first walk covers ranked rows before the NULL tail.
func TestResources(t *testing.T) {
	srv := newEntityAPI(t)
	var fwd struct {
		Items []struct {
			Host       string  `json:"host"`
			AAAAStatus *string `json:"aaaa_status"`
			Source     string  `json:"source"`
			Required   bool    `json:"required"`
			FirstSeen  string  `json:"first_seen"`
		} `json:"items"`
		Meta struct {
			Count *int64 `json:"count"`
		} `json:"meta"`
	}
	getJSON(t, srv.URL+"/domains/d1.example/resources", &fwd)
	if len(fwd.Items) != 1 || fwd.Items[0].Host != "fonts.example" ||
		fwd.Items[0].AAAAStatus == nil || *fwd.Items[0].AAAAStatus != "unsupported" ||
		fwd.Items[0].Source != "discovered" || !fwd.Items[0].Required {
		t.Fatalf("forward resources = %+v", fwd.Items)
	}
	if fwd.Meta.Count == nil || *fwd.Meta.Count != 1 {
		t.Errorf("forward count = %v", fwd.Meta.Count)
	}
	if len(fwd.Items[0].FirstSeen) != 10 {
		t.Errorf("first_seen = %q, want a bare date", fwd.Items[0].FirstSeen)
	}
	// Zero links → empty items, not an error.
	var empty struct {
		Items []any `json:"items"`
	}
	getJSON(t, srv.URL+"/domains/d2.example/resources", &empty)
	if len(empty.Items) != 0 {
		t.Errorf("d2 resources = %v, want []", empty.Items)
	}

	// Resource headline.
	var head struct {
		Host           string `json:"host"`
		DependentCount int32  `json:"dependent_count"`
	}
	getJSON(t, srv.URL+"/resources/fonts.example", &head)
	if head.Host != "fonts.example" || head.DependentCount != 3 {
		t.Fatalf("resource head = %+v", head)
	}

	// Dependents: ranked rows first (rank order), rank-NULL tail last; a
	// limit=1 cursor walk covers all three exactly once.
	var seen []string
	url := srv.URL + "/resources/fonts.example/dependents?limit=1"
	for {
		var page struct {
			Resource struct {
				DependentCount int32 `json:"dependent_count"`
			} `json:"resource"`
			Items []struct {
				Host   string `json:"host"`
				Rank   *int32 `json:"rank"`
				Source string `json:"source"`
			} `json:"items"`
			Page struct {
				NextCursor *string `json:"next_cursor"`
				HasMore    bool    `json:"has_more"`
			} `json:"page"`
			Meta struct {
				CountEstimate *int64 `json:"count_estimate"`
			} `json:"meta"`
		}
		getJSON(t, url, &page)
		if page.Meta.CountEstimate == nil || *page.Meta.CountEstimate != 3 {
			t.Fatalf("dependents count_estimate = %v", page.Meta.CountEstimate)
		}
		for _, it := range page.Items {
			if it.Source != "discovered" {
				t.Errorf("dependent %s missing link attrs: %+v", it.Host, it)
			}
			seen = append(seen, it.Host)
		}
		if !page.Page.HasMore {
			break
		}
		url = srv.URL + "/resources/fonts.example/dependents?limit=1&cursor=" + *page.Page.NextCursor
	}
	want := []string{"d1.example", "d3.example", "campaign-only.example"}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Errorf("dependents walk = %v, want %v (ranked first, NULL tail last)", seen, want)
	}

	var problem struct{ Type string }
	if resp := getJSON(t, srv.URL+"/resources/nope.example", &problem); resp.StatusCode != 404 {
		t.Errorf("unknown resource: %d", resp.StatusCode)
	}
}

// TestMandates (P4.16 / 07 §5.6): /mandates ≡ /campaigns?tag=mandate; an
// unknown tag is 200 with an empty collection.
func TestMandates(t *testing.T) {
	srv := newEntityAPI(t)
	var mandates, tagged envelope
	getJSON(t, srv.URL+"/mandates", &mandates)
	getJSON(t, srv.URL+"/campaigns?tag=mandate", &tagged)
	if len(mandates.Items) != 1 || len(tagged.Items) != 1 {
		t.Fatalf("mandates = %d, tagged = %d, want 1 each", len(mandates.Items), len(tagged.Items))
	}
	var m struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	raw, _ := json.Marshal(mandates.Items[0])
	_ = json.Unmarshal(raw, &m)
	if m.Name != "Norwegian Banks" {
		t.Errorf("mandate = %+v", m)
	}
	if mandates.Meta.Count == nil || *mandates.Meta.Count != 1 {
		t.Errorf("mandates count = %v", mandates.Meta.Count)
	}
	var unknown envelope
	if resp := getJSON(t, srv.URL+"/campaigns?tag=no-such-tag", &unknown); resp.StatusCode != 200 || len(unknown.Items) != 0 {
		t.Errorf("unknown tag: %d with %d items, want 200 empty", resp.StatusCode, len(unknown.Items))
	}
}

// TestCountryLeaderboardSurvivesANaN is review issue 61's real stake. The
// handlers spelled the conversion `pct, _ := row.Percent.Float64Value()`, and
// pgtype hands a NaN NUMERIC back as {Valid: true, Float64: NaN} with a nil
// error — so checking that discarded error would not have caught it either.
// The NaN reached encoding/json, which refuses to marshal it, and one bad row
// took the whole 251-row leaderboard with it.
//
// Postgres accepts 'NaN' in a NUMERIC(5,2) column, so this is reachable from
// any arithmetic in the daily rollup that divides by a zero site count.
func TestCountryLeaderboardSurvivesANaN(t *testing.T) {
	srv, pool := newAPI(t)
	if _, err := pool.Exec(context.Background(),
		`UPDATE country SET percent = 'NaN'::numeric WHERE code = 'SE'`); err != nil {
		t.Fatalf("seed NaN: %v", err)
	}

	var env struct {
		Items []struct {
			Code    string  `json:"code"`
			Percent float64 `json:"percent"`
		} `json:"items"`
	}
	resp := getJSON(t, srv.URL+"/countries", &env)
	if resp.StatusCode != 200 {
		t.Fatalf("/countries with a NaN row = %d, want 200", resp.StatusCode)
	}
	var found bool
	for _, it := range env.Items {
		if strings.TrimSpace(it.Code) == "SE" {
			found = true
			if it.Percent != 0 {
				t.Errorf("SE percent = %v, want the 0 fallback", it.Percent)
			}
		}
	}
	if !found {
		t.Error("SE missing from the leaderboard")
	}

	// The detail path converts separately and must not 500 either.
	var detail struct {
		Percent float64 `json:"percent"`
	}
	if resp := getJSON(t, srv.URL+"/countries/se", &detail); resp.StatusCode != 200 {
		t.Errorf("/countries/se with a NaN row = %d, want 200", resp.StatusCode)
	}
}
