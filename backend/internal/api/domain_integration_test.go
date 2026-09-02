//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedLeaderboard: a small mixed population — heroes, sinners, partial, a
// saint hero, a disabled row, a rank-NULL campaign row, and a shame pick.
func seedLeaderboard(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		// Country 'NO' ships in the seed migration; only the ASN is new.
		`INSERT INTO asn (number, name) VALUES (2119, 'Telenor Norge AS')`,
		`INSERT INTO dns_provider (name, ns_suffixes) VALUES ('Cloudflare', '{ns.cloudflare.com}')`,
		// 10 ranked apexes: odd rank = hero, even = sinner; rank 2 partial.
		`INSERT INTO domain (host, kind, rank, created_by, asn_id, country_id, tld,
		                     classification, saint, base_status, base_since, ns_status, ns_since,
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
	row := env.Items[0] // d1: rank 1, hero, saint
	var summary struct {
		Host           string   `json:"host"`
		Rank           *int32   `json:"rank"`
		Classification string   `json:"classification"`
		ClassFlags     []string `json:"class_flags"`
		Saint          bool     `json:"saint"`
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
	if summary.Classification != "hero" || !summary.Saint {
		t.Errorf("d1 classification=%s saint=%t", summary.Classification, summary.Saint)
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
// the saints preset and the partial/mail /domains filters return their
// subsets.
func TestTierEquivalence(t *testing.T) {
	srv, pool := newAPI(t)
	var tier, filtered envelope
	getJSON(t, srv.URL+"/sinners", &tier)
	getJSON(t, srv.URL+"/domains?class=sinner", &filtered)
	th, fh := hosts(t, tier.Items), hosts(t, filtered.Items)
	if fmt.Sprint(th) != fmt.Sprint(fh) || len(th) == 0 {
		t.Errorf("/sinners %v != /domains?class=sinner %v", th, fh)
	}
	var saints envelope
	getJSON(t, srv.URL+"/saints", &saints)
	if g := hosts(t, saints.Items); len(g) != 1 || g[0] != "d1.example" {
		t.Errorf("/saints = %v, want [d1.example]", g)
	}
	var partial envelope
	getJSON(t, srv.URL+"/domains?class=partial", &partial)
	if a := hosts(t, partial.Items); len(a) != 1 || a[0] != "d2.example" {
		t.Errorf("/domains?class=partial = %v, want [d2.example]", a)
	}
	var mail envelope
	getJSON(t, srv.URL+"/domains?class=hero&mx=supported", &mail)
	for _, h := range hosts(t, mail.Items) {
		if h == "d2.example" || h == "d4.example" {
			t.Errorf("the mail track must contain only mx-supported heroes, got %s", h)
		}
	}
	if len(mail.Items) == 0 {
		t.Error("the mail track should list the odd-numbered heroes")
	}
	// Tier + country composition: sinners in Norway (only d2 ≤3 is partial,
	// so no NO sinner; UN holds the rest).
	var no envelope
	getJSON(t, srv.URL+"/sinners?country=no", &no)
	if len(no.Items) != 0 {
		t.Errorf("/sinners?country=no = %v, want empty", hosts(t, no.Items))
	}

	// Almost-heroes (07 §4.4): hero in every dimension except the apex AAAA.
	// Promote one sinner (base unsupported, ns supported) into the cohort;
	// no other fixture row qualifies (the rest lack www/mx support).
	if _, err := pool.Exec(context.Background(),
		`UPDATE domain SET www_status = 'supported', mx_status = 'not_applicable'
		 WHERE host = 'd6.example'`); err != nil {
		t.Fatal(err)
	}
	var almost, almostEq envelope
	getJSON(t, srv.URL+"/almost-heroes", &almost)
	if g := hosts(t, almost.Items); len(g) != 1 || g[0] != "d6.example" {
		t.Errorf("/almost-heroes = %v, want [d6.example]", g)
	}
	getJSON(t, srv.URL+"/domains?almost_hero=true", &almostEq)
	if fmt.Sprint(hosts(t, almostEq.Items)) != fmt.Sprint(hosts(t, almost.Items)) {
		t.Errorf("/domains?almost_hero=true = %v, want %v",
			hosts(t, almostEq.Items), hosts(t, almost.Items))
	}
}

// TestDomainMandates (07 §4.3): the detail lists the enabled
// 'mandate'-tagged campaigns the domain belongs to; plain campaigns and
// non-member domains contribute nothing.
func TestDomainMandates(t *testing.T) {
	srv, pool := newAPI(t)
	ctx := context.Background()
	for _, s := range []string{
		`INSERT INTO campaign (uuid, name, description, source_file, tags)
		 VALUES ('3fa85f64-5717-4562-b3fc-2c963f66afa6', 'EU Mandate', 'd', 'EU.yml', '{mandate,eu-2030}')`,
		`INSERT INTO campaign (uuid, name, description, source_file, tags)
		 VALUES ('7c9e6679-7425-40de-944b-e07fc1f90ae7', 'Plain Campaign', 'd', 'P.yml', '{}')`,
		`INSERT INTO campaign_domain (campaign_id, domain_id)
		 SELECT c.id, d.id FROM campaign c, domain d WHERE d.host = 'd1.example'`,
	} {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	var detail struct {
		Mandates []struct {
			UUID string `json:"uuid"`
			Name string `json:"name"`
		} `json:"mandates"`
	}
	getJSON(t, srv.URL+"/domains/d1.example", &detail)
	if len(detail.Mandates) != 1 || detail.Mandates[0].Name != "EU Mandate" ||
		detail.Mandates[0].UUID != "3fa85f64-5717-4562-b3fc-2c963f66afa6" {
		t.Errorf("d1 mandates = %+v, want the one mandate campaign", detail.Mandates)
	}
	getJSON(t, srv.URL+"/domains/d2.example", &detail)
	if detail.Mandates == nil || len(detail.Mandates) != 0 {
		t.Errorf("d2 mandates = %+v, want present-but-empty", detail.Mandates)
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

	// after_rank deep link skips ahead, and mints a prev_cursor because
	// ranks 1-7 really do precede the window.
	var deep envelope
	getJSON(t, srv.URL+"/domains?after_rank=7", &deep)
	if h := hosts(t, deep.Items); len(h) == 0 || h[0] != "d8.example" {
		t.Errorf("after_rank=7 first row = %v, want d8.example", h)
	}
	if deep.Page.PrevCursor == nil {
		t.Error("after_rank=7 must mint prev_cursor: ranks 1-7 precede the window")
	}

	// …but an after_rank with nothing before it must not (07 §2.4). Both
	// reproducers: the global list from 0, and a scope whose first member
	// sits above the anchor (the first sinner is rank 4).
	for _, url := range []string{
		srv.URL + "/domains?after_rank=0&limit=3",
		srv.URL + "/domains?class=sinner&after_rank=2&limit=3",
	} {
		var edge envelope
		getJSON(t, url, &edge)
		if len(edge.Items) == 0 {
			t.Fatalf("%s returned no rows", url)
		}
		if edge.Page.PrevCursor != nil {
			t.Errorf("%s minted prev_cursor onto an empty previous page", url)
		}
	}
}

// TestSearchOrdersByRank (07 §3.3): ?q= overrides the client sorts and
// orders by rank, NULLS LAST — the rank-NULL rows the search scope pulls in
// sort behind every ranked match.
func TestSearchOrdersByRank(t *testing.T) {
	srv, _ := newAPI(t)
	var env envelope
	getJSON(t, srv.URL+"/domains?q=example", &env)
	want := []string{
		"d1.example", "d2.example", "d3.example", "d4.example", "d5.example",
		"d6.example", "d7.example", "d8.example", "d10.example", // d9 disabled
		"campaign-only.example", // rank NULL → last
	}
	if h := hosts(t, env.Items); !slices.Equal(h, want) {
		t.Errorf("q=example = %v, want rank-ordered %v", h, want)
	}
	// Hosts are stored lowercase; the term is normalised, not the caller.
	getJSON(t, srv.URL+"/domains?q=+Example+", &env)
	if h := hosts(t, env.Items); !slices.Equal(h, want) {
		t.Errorf("q=Example = %v, want the lowercase result %v", h, want)
	}
	// LIKE metacharacters are literal substrings: no host contains % or _.
	for _, raw := range []string{"%25", "d_.example"} {
		getJSON(t, srv.URL+"/domains?q="+raw, &env)
		if n := len(env.Items); n != 0 {
			t.Errorf("q=%s matched %d rows, want 0 (metacharacters must be literal)", raw, n)
		}
	}
	// An explicit sort loses to the search ordering, and the rank deep links
	// stay rejected on it. The ordering is overridden, not the cursor scope:
	// sort= is part of the filter fingerprint, so the two spellings still
	// page separately.
	getJSON(t, srv.URL+"/domains?q=example&sort=host", &env)
	if h := hosts(t, env.Items); !slices.Equal(h, want) {
		t.Errorf("q=example&sort=host = %v, want rank-ordered %v", h, want)
	}
	var paged envelope
	getJSON(t, srv.URL+"/domains?q=example&limit=2", &paged)
	if paged.Page.NextCursor == nil {
		t.Fatal("q=example&limit=2 must mint a next_cursor")
	}
	var problem struct{ Type string }
	resp := getJSON(t, srv.URL+"/domains?q=example&sort=host&limit=2&cursor="+*paged.Page.NextCursor, &problem)
	if resp.StatusCode != 400 {
		t.Errorf("cursor replayed with sort=host = %d, want 400 (fingerprint scope)", resp.StatusCode)
	}
	if resp := getJSON(t, srv.URL+"/domains?q=example&after_rank=3", &problem); resp.StatusCode != 400 {
		t.Errorf("q= with after_rank = %d, want 400", resp.StatusCode)
	}
}

// TestSearchPagesAcrossTheNullTail (07 §3.2): the search cursor is the
// null-flag-first key, so the walk crosses the ranked → rank-NULL partition
// boundary exactly once, and prev_cursor crosses back.
func TestSearchPagesAcrossTheNullTail(t *testing.T) {
	srv, _ := newAPI(t)
	var first envelope
	getJSON(t, srv.URL+"/domains?q=example&limit=9", &first)
	if h := hosts(t, first.Items); len(h) != 9 || h[8] != "d10.example" {
		t.Fatalf("page 1 = %v, want the 9 ranked matches", h)
	}
	if !first.Page.HasMore || first.Page.NextCursor == nil {
		t.Fatal("page 1 must carry a next_cursor onto the null tail")
	}

	var second envelope
	getJSON(t, srv.URL+"/domains?q=example&limit=9&cursor="+*first.Page.NextCursor, &second)
	if h := hosts(t, second.Items); len(h) != 1 || h[0] != "campaign-only.example" {
		t.Fatalf("page 2 = %v, want [campaign-only.example]", h)
	}
	if second.Page.PrevCursor == nil {
		t.Fatal("page 2 must carry a prev_cursor back into the ranked partition")
	}

	var back envelope
	getJSON(t, srv.URL+"/domains?q=example&limit=9&cursor="+*second.Page.PrevCursor, &back)
	if h := hosts(t, back.Items); !slices.Equal(h, hosts(t, first.Items)) {
		t.Errorf("prev walk = %v, want page 1 %v", h, hosts(t, first.Items))
	}
}

// TestSearchSpansRankNull (07 §3.1/§3.3, Decision 2026-07-11): ?q= is the one
// read that surfaces rank-NULL rows (campaign-only hosts, subdomains) outside
// their sub-collections; disabled rows stay excluded.
func TestSearchSpansRankNull(t *testing.T) {
	srv, _ := newAPI(t)

	// campaign-only.example is a rank-NULL apex — invisible on the plain
	// leaderboard, but findable by search.
	var env envelope
	getJSON(t, srv.URL+"/domains?q=campaign-only", &env)
	h := hosts(t, env.Items)
	if len(h) != 1 || h[0] != "campaign-only.example" {
		t.Fatalf("q=campaign-only = %v, want [campaign-only.example]", h)
	}
	var rank *int32
	if err := json.Unmarshal(env.Items[0]["rank"], &rank); err != nil {
		t.Fatal(err)
	}
	if rank != nil {
		t.Errorf("campaign-only rank = %v, want JSON null", *rank)
	}

	// The bare leaderboard (no q) still hides the rank-NULL row.
	var plain envelope
	getJSON(t, srv.URL+"/domains", &plain)
	for _, host := range hosts(t, plain.Items) {
		if host == "campaign-only.example" {
			t.Errorf("rank-NULL host leaked onto the bare leaderboard")
		}
	}

	// Disabled rows (d9) stay excluded even under the q scope.
	var disabled envelope
	getJSON(t, srv.URL+"/domains?q=d9", &disabled)
	if len(disabled.Items) != 0 {
		t.Errorf("q=d9 = %v, want empty (disabled excluded)", hosts(t, disabled.Items))
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

// TestDetailETagTracksTransitions (07 §6.1 row 2, review issue 06): the
// detail ETag is tied to the entity's last confirmed transition, so a
// transition invalidates a CDN copy when it commits. Seeded from the daily
// generation instead, this revalidation 304s and the edge keeps serving the
// pre-transition body until the next stats tick.
func TestDetailETagTracksTransitions(t *testing.T) {
	srv, pool := newAPI(t)
	const url = "/domains/d1.example"

	resp, _ := fetch(t, srv.URL+url)
	before := resp.Header.Get("ETag")
	if before == "" {
		t.Fatal("no ETag on the detail response")
	}
	if code := conditionalGet(t, srv.URL+url, before); code != http.StatusNotModified {
		t.Fatalf("unchanged entity revalidated %d, want 304", code)
	}

	// The crawler confirms a base transition. generation does not move —
	// that is the daily stats tick, which has not run.
	if _, err := pool.Exec(context.Background(),
		`UPDATE domain SET base_status = 'no_record', base_since = now() WHERE host = 'd1.example'`); err != nil {
		t.Fatal(err)
	}

	resp, _ = fetch(t, srv.URL+url)
	after := resp.Header.Get("ETag")
	if after == before {
		t.Errorf("ETag %s survived a confirmed transition", after)
	}
	if code := conditionalGet(t, srv.URL+url, before); code != http.StatusOK {
		t.Errorf("stale validator revalidated %d, want 200", code)
	}
}

// conditionalGet issues an If-None-Match revalidation and returns the status.
func conditionalGet(t *testing.T, url, etag string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("If-None-Match", etag)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
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
	getJSON(t, srv.URL+"/domains?fields=host,rank,classification,saint", &env)
	if len(env.Items) == 0 {
		t.Fatal("no rows")
	}
	row := env.Items[0]
	if len(row) != 4 {
		keys := make([]string, 0, len(row))
		for k := range row {
			keys = append(keys, k)
		}
		t.Errorf("trimmed row has keys %v, want exactly host,rank,classification,saint", keys)
	}
}

// TestAcceptIsIgnored pins the decision behind review issue 05 (07 §2.5,
// §5.5 and §6.2 errata): the API serves its one representation whatever
// Accept says. No 406, and no Vary: Accept — the header would split every
// CDN cache key for a negotiation that does not exist.
func TestAcceptIsIgnored(t *testing.T) {
	srv, _ := newAPI(t)
	for _, accept := range []string{"text/xml", "application/xml", "text/csv", "image/png"} {
		t.Run(accept, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/domains", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Accept", accept)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("content-type = %q, want application/json", ct)
			}
			for _, v := range resp.Header.Values("Vary") {
				for _, part := range strings.Split(v, ",") {
					if strings.EqualFold(strings.TrimSpace(part), "Accept") {
						t.Errorf("Vary = %q, must not list Accept", v)
					}
				}
			}
		})
	}
}
