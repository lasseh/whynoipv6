//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/api"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
	"github.com/lasseh/whynoipv6/internal/service"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

// seedLeaderboard: a small mixed population — heroes, sinners, partial, a
// gold hero, a disabled row, a rank-NULL campaign row, and a shame pick.
func seedLeaderboard(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		// Country 'NO' ships in the seed migration; only the ASN is new.
		`INSERT INTO asn (number, name) VALUES (2119, 'Telenor Norge AS')`,
		`INSERT INTO dns_provider (name, ns_suffixes) VALUES ('Cloudflare', '{ns.cloudflare.com}')`,
		// 10 ranked apexes: odd rank = hero, even = sinner; rank 2 partial.
		`INSERT INTO domain (host, kind, rank, created_by, asn_id, country_id, tld,
		                     classification, gold, base_status, base_since, ns_status, ns_since,
		                     mx_status, dns_provider_id, hosting_provider, last_checked_at)
		 SELECT 'd' || g || '.example', 'apex', g, 'tranco',
		        (SELECT id FROM asn WHERE number = CASE WHEN g <= 3 THEN 2119 ELSE 0 END),
		        (SELECT id FROM country WHERE code = CASE WHEN g <= 3 THEN 'NO' ELSE 'UN' END),
		        'example',
		        CASE WHEN g = 2 THEN 'partial' WHEN g % 2 = 1 THEN 'hero' ELSE 'sinner' END::classification,
		        g = 1,
		        CASE WHEN g % 2 = 1 THEN 'supported' ELSE 'unsupported' END::ipv6_status,
		        now() - interval '30 days',
		        'supported'::ipv6_status, now() - interval '90 days',
		        CASE WHEN g % 2 = 1 THEN 'supported' ELSE 'unsupported' END::ipv6_status,
		        CASE WHEN g = 1 THEN (SELECT id FROM dns_provider) END,
		        CASE WHEN g = 1 THEN 'Amazon CloudFront' END,
		        now()
		 FROM generate_series(1, 10) g`,
		`UPDATE domain SET class_flags = '{www_missing,mail_missing}' WHERE host = 'd2.example'`,
		// Disabled ranked row: must vanish from collections, resolve on detail.
		`UPDATE domain SET disabled = true, disabled_reason = 'manual' WHERE host = 'd9.example'`,
		// Rank-NULL campaign apex: same visibility rule.
		`INSERT INTO domain (host, kind, created_by, asn_id, country_id, tld, classification)
		 VALUES ('campaign-only.example', 'apex', 'campaign',
		         (SELECT id FROM asn WHERE number = 0), (SELECT id FROM country WHERE code = 'UN'),
		         'example', 'sinner')`,
		// Informational dims on d1: error/partial masking material (§4.3).
		`UPDATE domain SET dnssec_observed = 'error', ptr_observed = 'partial',
		                   smtp_observed = 'partial', parity_observed = 'supported',
		                   latency_v4_ms = 41, latency_v6_ms = 38
		 WHERE host = 'd1.example'`,
		// Shame picks: d4 (sinner, ranked → visible); campaign-only (rank NULL → hidden).
		`INSERT INTO top_shame (domain_id, reason)
		 SELECT id, 'legacy bank' FROM domain WHERE host = 'd4.example'`,
		`INSERT INTO top_shame (domain_id, reason)
		 SELECT id, NULL FROM domain WHERE host = 'campaign-only.example'`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("seed: %v\n%s", err, s)
		}
	}
}

func newAPI(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.NewDB(t)
	seedLeaderboard(t, pool)
	srv := httptest.NewServer(api.NewRouter(service.New(pool)))
	t.Cleanup(srv.Close)
	return srv, pool
}

func getJSON(t *testing.T, url string, out any) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return resp
}

type envelope struct {
	Items []map[string]json.RawMessage `json:"items"`
	Page  struct {
		NextCursor *string `json:"next_cursor"`
		PrevCursor *string `json:"prev_cursor"`
		HasMore    bool    `json:"has_more"`
	} `json:"page"`
	Meta struct {
		AsOf          string `json:"as_of"`
		Generation    int32  `json:"generation"`
		Count         *int64 `json:"count"`
		CountEstimate *int64 `json:"count_estimate"`
		License       string `json:"license"`
	} `json:"meta"`
}

func hosts(t *testing.T, items []map[string]json.RawMessage) []string {
	t.Helper()
	out := make([]string, len(items))
	for i, it := range items {
		if err := json.Unmarshal(it["host"], &out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// TestDomainRowShape (07 §4.2): real 4-value enum + JSON null, rank
// int-or-null, embedded country/asn objects, class_flags never null.
func TestDomainRowShape(t *testing.T) {
	srv, _ := newAPI(t)
	var env envelope
	resp := getJSON(t, srv.URL+"/domains", &env)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if len(env.Items) != 9 { // 10 ranked − 1 disabled; rank-NULL invisible
		t.Fatalf("want 9 visible rows, got %d: %v", len(env.Items), hosts(t, env.Items))
	}
	row := env.Items[0] // d1: rank 1, hero, gold
	var summary struct {
		Host           string   `json:"host"`
		Rank           *int32   `json:"rank"`
		Classification string   `json:"classification"`
		ClassFlags     []string `json:"class_flags"`
		Gold           bool     `json:"gold"`
		Status         map[string]struct {
			Value *string `json:"value"`
			Since *string `json:"since"`
		} `json:"status"`
		Country struct {
			Code string `json:"code"`
			Name string `json:"name"`
		} `json:"country"`
		ASN struct {
			Number int64  `json:"number"`
			Name   string `json:"name"`
		} `json:"asn"`
		DNSProvider *struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"dns_provider"`
		HostingProvider *string `json:"hosting_provider"`
	}
	raw, _ := json.Marshal(row)
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Host != "d1.example" || summary.Rank == nil || *summary.Rank != 1 {
		t.Errorf("row 0 = %s rank %v, want d1.example rank 1", summary.Host, summary.Rank)
	}
	if summary.Classification != "hero" || !summary.Gold {
		t.Errorf("d1 classification=%s gold=%t", summary.Classification, summary.Gold)
	}
	if summary.ClassFlags == nil {
		t.Error("class_flags must be [] not null")
	}
	if v := summary.Status["base"].Value; v == nil || *v != "supported" {
		t.Errorf("base.value = %v, want supported", v)
	}
	if v := summary.Status["resources"].Value; v != nil {
		t.Errorf("resources.value = %q, want JSON null (never confirmed)", *v)
	}
	if summary.Country.Code != "NO" || summary.Country.Name != "Norway" {
		t.Errorf("country = %+v", summary.Country)
	}
	if summary.ASN.Number != 2119 {
		t.Errorf("asn = %+v", summary.ASN)
	}
	if summary.DNSProvider == nil || summary.DNSProvider.Name != "Cloudflare" {
		t.Errorf("dns_provider = %+v", summary.DNSProvider)
	}
	if summary.HostingProvider == nil || *summary.HostingProvider != "Amazon CloudFront" {
		t.Errorf("hosting_provider = %v", summary.HostingProvider)
	}
	// d2 (sinner→partial with flags) sanity: legacy 0-rank must not appear.
	var d2 struct {
		Rank       *int32   `json:"rank"`
		ClassFlags []string `json:"class_flags"`
	}
	raw, _ = json.Marshal(env.Items[1])
	_ = json.Unmarshal(raw, &d2)
	if d2.Rank == nil || *d2.Rank != 2 || len(d2.ClassFlags) != 2 {
		t.Errorf("d2 rank=%v flags=%v", d2.Rank, d2.ClassFlags)
	}
	if env.Meta.CountEstimate == nil || *env.Meta.CountEstimate != 10 { // max(rank), disabled gap included
		t.Errorf("count_estimate = %v, want 10 (max rank)", env.Meta.CountEstimate)
	}
	if env.Meta.Generation == 0 || env.Meta.License != "CC-BY-NC-4.0" {
		t.Errorf("meta = %+v", env.Meta)
	}
}

// TestVisibility: disabled and rank-NULL rows are absent from collections
// but resolve on the entity endpoint (07 §2.2 / 05 §1.7).
func TestVisibility(t *testing.T) {
	srv, _ := newAPI(t)
	var env envelope
	getJSON(t, srv.URL+"/domains?limit=100", &env)
	for _, h := range hosts(t, env.Items) {
		if h == "d9.example" || h == "campaign-only.example" {
			t.Errorf("%s must be invisible in the leaderboard", h)
		}
	}
	for _, h := range []string{"d9.example", "campaign-only.example"} {
		var detail struct {
			Host     string `json:"host"`
			Rank     *int32 `json:"rank"`
			Disabled bool   `json:"disabled"`
		}
		resp := getJSON(t, srv.URL+"/domains/"+h, &detail)
		if resp.StatusCode != 200 || detail.Host != h {
			t.Errorf("detail %s: status %d host %s", h, resp.StatusCode, detail.Host)
		}
	}
	var problem struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	resp := getJSON(t, srv.URL+"/domains/missing.example", &problem)
	if resp.StatusCode != 404 || problem.Type != "https://whynoipv6.com/problems/not-found" {
		t.Errorf("missing domain: %d %+v", resp.StatusCode, problem)
	}
}

// TestTierEquivalence (07 §4.4): GET /sinners ≡ GET /domains?class=sinner;
// gold/almost/mail presets return their subsets.
func TestTierEquivalence(t *testing.T) {
	srv, _ := newAPI(t)
	var tier, filtered envelope
	getJSON(t, srv.URL+"/sinners", &tier)
	getJSON(t, srv.URL+"/domains?class=sinner", &filtered)
	th, fh := hosts(t, tier.Items), hosts(t, filtered.Items)
	if fmt.Sprint(th) != fmt.Sprint(fh) || len(th) == 0 {
		t.Errorf("/sinners %v != /domains?class=sinner %v", th, fh)
	}
	var gold envelope
	getJSON(t, srv.URL+"/gold", &gold)
	if g := hosts(t, gold.Items); len(g) != 1 || g[0] != "d1.example" {
		t.Errorf("/gold = %v, want [d1.example]", g)
	}
	var almost envelope
	getJSON(t, srv.URL+"/almost", &almost)
	if a := hosts(t, almost.Items); len(a) != 1 || a[0] != "d2.example" {
		t.Errorf("/almost = %v, want [d2.example]", a)
	}
	var mail envelope
	getJSON(t, srv.URL+"/mail", &mail)
	for _, h := range hosts(t, mail.Items) {
		if h == "d2.example" || h == "d4.example" {
			t.Errorf("/mail must contain only mx-supported heroes, got %s", h)
		}
	}
	if len(mail.Items) == 0 {
		t.Error("/mail should list the odd-numbered heroes")
	}
	// Tier + country composition: sinners in Norway (only d2 ≤3 is partial,
	// so no NO sinner; UN holds the rest).
	var no envelope
	getJSON(t, srv.URL+"/sinners?country=no", &no)
	if len(no.Items) != 0 {
		t.Errorf("/sinners?country=no = %v, want empty", hosts(t, no.Items))
	}
}

// TestScopeGuardrail (07 §3.3): a bare residual → 422 scope-required; two
// residuals → 422 even when scoped; scoped single residual → 200.
func TestScopeGuardrail(t *testing.T) {
	srv, _ := newAPI(t)
	for _, path := range []string{"/domains?mx=unsupported", "/domains?tld=example",
		"/domains?class=sinner&tld=example&mx=unsupported"} {
		var problem struct {
			Type   string `json:"type"`
			Status int    `json:"status"`
		}
		resp := getJSON(t, srv.URL+path, &problem)
		if resp.StatusCode != 422 || problem.Type != "https://whynoipv6.com/problems/scope-required" {
			t.Errorf("%s: %d %+v, want 422 scope-required", path, resp.StatusCode, problem)
		}
	}
	var env envelope
	resp := getJSON(t, srv.URL+"/domains?class=sinner&mx=unsupported", &env)
	if resp.StatusCode != 200 || len(env.Items) == 0 {
		t.Errorf("scoped residual: %d, %d items", resp.StatusCode, len(env.Items))
	}
	// Invalid enum value → 422 validation-error, not scope-required (§2.5).
	var problem struct{ Type string }
	resp = getJSON(t, srv.URL+"/domains?class=awesome", &problem)
	if resp.StatusCode != 422 || problem.Type != "https://whynoipv6.com/problems/validation-error" {
		t.Errorf("bad class: %d %+v", resp.StatusCode, problem)
	}
}

// TestKeysetPagination (07 §3.2): N+1 fetch, opaque cursor walk covers the
// set exactly once; a cursor replayed with different filters → 400.
func TestKeysetPagination(t *testing.T) {
	srv, _ := newAPI(t)
	seen := []string{}
	url := srv.URL + "/domains?limit=4"
	for {
		var env envelope
		getJSON(t, url, &env)
		seen = append(seen, hosts(t, env.Items)...)
		if !env.Page.HasMore {
			if env.Page.NextCursor != nil {
				t.Error("has_more=false must not carry a next_cursor")
			}
			break
		}
		if env.Page.NextCursor == nil {
			t.Fatal("has_more=true without next_cursor")
		}
		url = srv.URL + "/domains?limit=4&cursor=" + *env.Page.NextCursor
	}
	if len(seen) != 9 {
		t.Fatalf("walk covered %d rows (%v), want 9", len(seen), seen)
	}
	uniq := map[string]bool{}
	for _, h := range seen {
		if uniq[h] {
			t.Errorf("duplicate row %s across pages", h)
		}
		uniq[h] = true
	}

	// Cursor bound to different filters is rejected.
	var first envelope
	getJSON(t, srv.URL+"/domains?limit=4", &first)
	var problem struct{ Type string }
	resp := getJSON(t, srv.URL+"/domains?class=hero&cursor="+*first.Page.NextCursor, &problem)
	if resp.StatusCode != 400 || problem.Type != "https://whynoipv6.com/problems/invalid-parameter" {
		t.Errorf("filter-mismatched cursor: %d %+v", resp.StatusCode, problem)
	}

	// after_rank deep link skips ahead.
	var deep envelope
	getJSON(t, srv.URL+"/domains?after_rank=7", &deep)
	if h := hosts(t, deep.Items); len(h) == 0 || h[0] != "d8.example" {
		t.Errorf("after_rank=7 first row = %v, want d8.example", h)
	}
}

// TestSearchForcesHostOrder (07 §3.3): ?q= never composes with rank order.
func TestSearchForcesHostOrder(t *testing.T) {
	srv, _ := newAPI(t)
	var env envelope
	getJSON(t, srv.URL+"/domains?q=d1", &env)
	h := hosts(t, env.Items)
	if len(h) != 2 || h[0] != "d1.example" || h[1] != "d10.example" {
		t.Errorf("q=d1 = %v, want host-ordered [d1.example d10.example]", h)
	}
}

// TestDetailMasking (07 §4.3): error/inconsistent → null everywhere; partial
// survives only on ptr/parity; evidence only with ?include=evidence.
func TestDetailMasking(t *testing.T) {
	srv, _ := newAPI(t)
	var detail struct {
		Informational struct {
			DNSSEC      *string `json:"dnssec"`
			PTR         *string `json:"ptr"`
			SMTP        *string `json:"smtp"`
			Parity      *string `json:"parity"`
			LatencyV4Ms *int32  `json:"latency_v4_ms"`
		} `json:"informational"`
		SubdomainCount int64           `json:"subdomain_count"`
		Evidence       json.RawMessage `json:"evidence"`
		Meta           struct {
			Generation int32 `json:"generation"`
		} `json:"meta"`
	}
	getJSON(t, srv.URL+"/domains/d1.example", &detail)
	info := detail.Informational
	if info.DNSSEC != nil {
		t.Errorf("dnssec error must mask to null, got %q", *info.DNSSEC)
	}
	if info.SMTP != nil {
		t.Errorf("smtp partial must mask to null, got %q", *info.SMTP)
	}
	if info.PTR == nil || *info.PTR != "partial" {
		t.Errorf("ptr = %v, want partial (public)", info.PTR)
	}
	if info.Parity == nil || *info.Parity != "supported" {
		t.Errorf("parity = %v, want supported", info.Parity)
	}
	if info.LatencyV4Ms == nil || *info.LatencyV4Ms != 41 {
		t.Errorf("latency_v4_ms = %v", info.LatencyV4Ms)
	}
	if detail.Evidence != nil {
		t.Error("evidence must be absent without ?include=evidence")
	}
	if detail.Meta.Generation == 0 {
		t.Error("detail meta.generation missing")
	}
}

// TestShameList (07 §4.4): visibility computed read-side, exact meta.count,
// trivial page block.
func TestShameList(t *testing.T) {
	srv, _ := newAPI(t)
	var env struct {
		Items []struct {
			Host    string  `json:"host"`
			Reason  *string `json:"reason"`
			AddedAt string  `json:"added_at"`
		} `json:"items"`
		Page struct {
			NextCursor *string `json:"next_cursor"`
			HasMore    bool    `json:"has_more"`
		} `json:"page"`
		Meta struct {
			Count *int64 `json:"count"`
		} `json:"meta"`
	}
	getJSON(t, srv.URL+"/shame", &env)
	if len(env.Items) != 1 || env.Items[0].Host != "d4.example" {
		t.Fatalf("shame items = %+v, want just d4.example (campaign-only is rank-NULL → hidden)", env.Items)
	}
	if env.Items[0].Reason == nil || *env.Items[0].Reason != "legacy bank" {
		t.Errorf("reason = %v", env.Items[0].Reason)
	}
	if env.Meta.Count == nil || *env.Meta.Count != 1 {
		t.Errorf("meta.count = %v, want exact 1", env.Meta.Count)
	}
	if env.Page.HasMore || env.Page.NextCursor != nil {
		t.Errorf("page must be trivial: %+v", env.Page)
	}
}

// TestFieldsTrim (07 §3.3): sparse fieldset trims the row.
func TestFieldsTrim(t *testing.T) {
	srv, _ := newAPI(t)
	var env envelope
	getJSON(t, srv.URL+"/domains?fields=host,rank,classification,gold", &env)
	if len(env.Items) == 0 {
		t.Fatal("no rows")
	}
	row := env.Items[0]
	if len(row) != 4 {
		keys := make([]string, 0, len(row))
		for k := range row {
			keys = append(keys, k)
		}
		t.Errorf("trimmed row has keys %v, want exactly host,rank,classification,gold", keys)
	}
}
