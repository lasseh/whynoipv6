//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/whynoipv6/internal/crawler"
)

// TestStatsEndpoints (P6.2 / 07 §4.10): {points,meta} envelope, points
// SELECT-equal with the snapshot tables, source label, weekly sampling,
// window validation, ASN prefix handling, empty → {"points":[]}.
func TestStatsEndpoints(t *testing.T) {
	srv, pool := newAPI(t)
	seedEntities(t, pool)
	ctx := context.Background()

	// 14 daily global snapshots + country/campaign/asn rows on two days.
	stmts := []string{
		`INSERT INTO stats_global_daily (day, domains, heroes, sinners, saints, mx_supported)
		 SELECT current_date - g, 1000 + g, 100 + g, 200, 10, 300
		 FROM generate_series(1, 14) g`,
		`INSERT INTO stats_country_daily (day, country_id, domains, heroes, base_supported)
		 SELECT current_date - g, (SELECT id FROM country WHERE code = 'NO'), 50 + g, 5, 20
		 FROM generate_series(1, 2) g`,
		`INSERT INTO stats_campaign_daily (day, campaign_id, domains, v6_ready)
		 SELECT current_date - g, (SELECT id FROM campaign), 2, 1 FROM generate_series(1, 2) g`,
		`INSERT INTO stats_asn_daily (day, asn_id, domains, v6_domains)
		 SELECT (current_date - g)::timestamptz, (SELECT id FROM asn WHERE number = 2119), 3, 2
		 FROM generate_series(1, 2) g`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("seed stats: %v\n%s", err, s)
		}
	}

	var overview struct {
		Points []struct {
			Day         string `json:"day"`
			Domains     *int32 `json:"domains"`
			Heroes      *int32 `json:"heroes"`
			Saints      *int32 `json:"saints"`
			MxSupported *int32 `json:"mx_supported"`
		} `json:"points"`
		Meta struct {
			Source  string `json:"source"`
			License string `json:"license"`
		} `json:"meta"`
	}
	getJSON(t, srv.URL+"/stats/overview", &overview)
	if overview.Meta.Source != "confirmed_state" {
		t.Fatalf("meta.source = %q", overview.Meta.Source)
	}
	// 14 seeded + the day-0 seed row (today), ascending.
	if len(overview.Points) != 15 {
		t.Fatalf("overview points = %d, want 15", len(overview.Points))
	}
	first := overview.Points[0]
	expectDay := time.Now().UTC().AddDate(0, 0, -14).Format("2006-01-02")
	if first.Day != expectDay || first.Domains == nil || *first.Domains != 1014 {
		t.Errorf("points[0] = %+v, want day %s domains 1014 (ascending, SELECT-equal)", first, expectDay)
	}
	if *overview.Points[13].Domains != 1001 {
		t.Errorf("points[13].domains = %d, want 1001", *overview.Points[13].Domains)
	}

	// Weekly: fewer points, each the latest snapshot of its ISO week.
	var weekly struct {
		Points []struct {
			Day string `json:"day"`
		} `json:"points"`
	}
	getJSON(t, srv.URL+"/stats/overview?interval=weekly", &weekly)
	if len(weekly.Points) == 0 || len(weekly.Points) >= len(overview.Points) {
		t.Errorf("weekly points = %d (daily %d)", len(weekly.Points), len(overview.Points))
	}

	// Country series.
	var country struct {
		Points []struct {
			Day     string `json:"day"`
			Domains *int32 `json:"domains"`
		} `json:"points"`
		Meta struct {
			Source string `json:"source"`
		} `json:"meta"`
	}
	getJSON(t, srv.URL+"/countries/no/stats", &country)
	if len(country.Points) != 2 || country.Meta.Source != "confirmed_state" {
		t.Errorf("country points = %d source %q", len(country.Points), country.Meta.Source)
	}

	// Campaign series carries v6_ready.
	var camp struct {
		Points []struct {
			V6Ready *int32 `json:"v6_ready"`
		} `json:"points"`
	}
	// 2 seeded here + 1 from seedEntities' adoption row (today).
	getJSON(t, srv.URL+"/campaigns/"+campaignUUID+"/stats", &camp)
	if len(camp.Points) != 3 || camp.Points[0].V6Ready == nil || *camp.Points[0].V6Ready != 1 {
		t.Errorf("campaign points = %+v", camp.Points)
	}

	// ASN series uses the canonical wire names; AS prefix accepted.
	for _, path := range []string{"/asns/2119/stats", "/asns/AS2119/stats"} {
		var asn struct {
			Points []struct {
				CountTotal *int32 `json:"count_total"`
				CountV6    *int32 `json:"count_v6"`
			} `json:"points"`
		}
		getJSON(t, srv.URL+path, &asn)
		if len(asn.Points) != 2 || *asn.Points[0].CountTotal != 3 || *asn.Points[0].CountV6 != 2 {
			t.Errorf("%s points = %+v", path, asn.Points)
		}
	}

	// Zero rows → 200 {"points":[]}, not 404.
	var empty struct {
		Points []any `json:"points"`
	}
	if resp := getJSON(t, srv.URL+"/countries/se/stats", &empty); resp.StatusCode != 200 || len(empty.Points) != 0 {
		t.Errorf("empty series: %d, %d points", resp.StatusCode, len(empty.Points))
	}

	// Validation and path-key failures.
	var problem struct{ Type string }
	if resp := getJSON(t, srv.URL+"/stats/overview?interval=hourly", &problem); resp.StatusCode != 400 {
		t.Errorf("bad interval: %d", resp.StatusCode)
	}
	if resp := getJSON(t, srv.URL+"/asns/ASX/stats", &problem); resp.StatusCode != 400 {
		t.Errorf("non-numeric asn: %d", resp.StatusCode)
	}
	if resp := getJSON(t, srv.URL+"/asns/99999/stats", &problem); resp.StatusCode != 404 {
		t.Errorf("unknown asn: %d", resp.StatusCode)
	}
}

// TestChangeStats (000009): the daily gained/lost roll-up.
//
// The field filter is what this test exists for. changelog holds one row per
// confirmed dimension transition, so an unfiltered count would multiply a
// single adoption across base/www/conn and — because shadowTransition
// suppresses the conn/resources loss mirror but not the gain — would bias
// gained upward, drawing net-positive adoption that never happened.
func TestChangeStats(t *testing.T) {
	srv, pool := newAPI(t)
	ctx := context.Background()

	var domainID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM domain WHERE host = 'd1.example'`).Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	// Day -2: one apex gain. Day -1: the same host flips out and back in, so
	// it counts in both columns. Both days also carry www/conn/mx noise that
	// must not reach the totals.
	seed := `INSERT INTO changelog (domain_id, ts, field, old_value, new_value) VALUES
	  ($1, now() - interval '2 days', 'base', 'unsupported', 'supported'),
	  ($1, now() - interval '2 days', 'www',  'unsupported', 'supported'),
	  ($1, now() - interval '2 days', 'conn', 'unsupported', 'supported'),
	  ($1, now() - interval '1 day',  'base', 'supported',   'unsupported'),
	  ($1, now() - interval '1 day' + interval '1 hour', 'base', 'unsupported', 'supported'),
	  ($1, now() - interval '1 day',  'mx',   'supported',   'unsupported')`
	if _, err := pool.Exec(ctx, seed, domainID); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Points []struct {
			Day    string `json:"day"`
			Gained int64  `json:"gained"`
			Lost   int64  `json:"lost"`
		} `json:"points"`
		Meta struct {
			Source string `json:"source"`
		} `json:"meta"`
	}
	resp := getJSON(t, srv.URL+"/stats/changes", &got)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got.Meta.Source != "confirmed_state" {
		t.Errorf("meta.source = %q", got.Meta.Source)
	}
	if len(got.Points) != 2 {
		t.Fatalf("points = %d, want 2 days: %+v", len(got.Points), got.Points)
	}
	if got.Points[0].Day >= got.Points[1].Day {
		t.Errorf("points not ascending: %+v", got.Points)
	}
	// Day -2: one base gain. The www and conn gains on the same day are the
	// regression guard — unfiltered this would read 3.
	if got.Points[0].Gained != 1 || got.Points[0].Lost != 0 {
		t.Errorf("day -2 = %d gained / %d lost, want 1/0 (www and conn must not count)",
			got.Points[0].Gained, got.Points[0].Lost)
	}
	// Day -1: the same host out and back in — both columns. The mx loss is
	// again noise that must not appear.
	if got.Points[1].Gained != 1 || got.Points[1].Lost != 1 {
		t.Errorf("day -1 = %d gained / %d lost, want 1/1 (churn counts both ways, mx excluded)",
			got.Points[1].Gained, got.Points[1].Lost)
	}

	// Changelog cache class: the ETag follows max(changelog.ts), and a new
	// transition must move it. A generation-seeded ETag would not.
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/stats/changes", nil)
	req.Header.Set("If-None-Match", etag)
	notModified, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	notModified.Body.Close()
	if notModified.StatusCode != http.StatusNotModified {
		t.Errorf("unchanged window = %d, want 304", notModified.StatusCode)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO changelog (domain_id, ts, field, old_value, new_value)
		 VALUES ($1, now(), 'base', 'unsupported', 'supported')`, domainID); err != nil {
		t.Fatal(err)
	}
	after, _ := fetch(t, srv.URL+"/stats/changes")
	if after.Header.Get("ETag") == etag {
		t.Error("ETag unchanged after a new transition — endpoint is not on the changelog class")
	}

	// Validation, and an empty window is an empty series rather than a 404.
	var problem struct{ Type string }
	if r := getJSON(t, srv.URL+"/stats/changes?from=2026-02-01&to=2026-01-01", &problem); r.StatusCode != 400 {
		t.Errorf("from after to: %d, want 400", r.StatusCode)
	}
	if r := getJSON(t, srv.URL+"/stats/changes?from=nonsense", &problem); r.StatusCode != 400 {
		t.Errorf("unparseable from: %d, want 400", r.StatusCode)
	}
	if r := getJSON(t, srv.URL+"/stats/changes?interval=hourly", &problem); r.StatusCode != 400 {
		t.Errorf("bad interval: %d, want 400", r.StatusCode)
	}
	var empty struct {
		Points []any `json:"points"`
	}
	if r := getJSON(t, srv.URL+"/stats/changes?from=2001-01-01&to=2001-02-01", &empty); r.StatusCode != 200 || len(empty.Points) != 0 {
		t.Errorf("empty window: %d with %d points, want 200 and []", r.StatusCode, len(empty.Points))
	}
}

// TestNetworkStats (07 §4.10): the top-N multi-network series.
//
// The shared-name case is the reason this endpoint exists as its own route
// rather than a name-keyed aggregate. asn.name is not unique, and grouping on
// it averages unrelated networks — a defect that has already shipped twice.
// Two seeded ASNs share a name here and must come back as two boxes.
func TestNetworkStats(t *testing.T) {
	srv, pool := newAPI(t)
	ctx := context.Background()

	// Two distinct ASNs with the SAME name and different series, plus a big
	// AS0 row that would rank first if the sentinel were not excluded.
	seed := []string{
		`INSERT INTO asn (number, name) VALUES (15169, 'Google LLC'), (396982, 'Google LLC')`,
		`INSERT INTO stats_asn_daily (day, asn_id, domains, v6_domains)
		 SELECT (current_date - g)::timestamptz, (SELECT id FROM asn WHERE number = 15169), 500, 190
		 FROM generate_series(1, 3) g`,
		`INSERT INTO stats_asn_daily (day, asn_id, domains, v6_domains)
		 SELECT (current_date - g)::timestamptz, (SELECT id FROM asn WHERE number = 396982), 400, 45
		 FROM generate_series(1, 3) g`,
		`INSERT INTO stats_asn_daily (day, asn_id, domains, v6_domains)
		 SELECT (current_date - g)::timestamptz, (SELECT id FROM asn WHERE number = 0), 9999, 1
		 FROM generate_series(1, 3) g`,
	}
	for _, s := range seed {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("seed: %v\n%s", err, s)
		}
	}

	type series struct {
		ASN    int64  `json:"asn"`
		Name   string `json:"name"`
		Points []struct {
			Day        string `json:"day"`
			CountTotal *int32 `json:"count_total"`
			CountV6    *int32 `json:"count_v6"`
		} `json:"points"`
	}
	var got struct {
		Networks []series `json:"networks"`
		Meta     struct {
			Source string `json:"source"`
		} `json:"meta"`
	}
	getJSON(t, srv.URL+"/stats/networks", &got)

	if got.Meta.Source != "confirmed_state" {
		t.Errorf("meta.source = %q", got.Meta.Source)
	}
	byASN := map[int64]series{}
	for _, n := range got.Networks {
		if _, dup := byASN[n.ASN]; dup {
			t.Errorf("AS%d returned twice", n.ASN)
		}
		byASN[n.ASN] = n
		if n.ASN == 0 {
			t.Error("the Unknown sentinel (AS0) must never appear, even ranking first by domains")
		}
	}
	// Both Google ASNs present, unmerged, with their own numbers and series.
	a, okA := byASN[15169]
	b, okB := byASN[396982]
	if !okA || !okB {
		t.Fatalf("want both AS15169 and AS396982, got %+v", got.Networks)
	}
	if a.Name != b.Name {
		t.Fatalf("test seed no longer shares a name: %q vs %q", a.Name, b.Name)
	}
	if len(a.Points) != 3 || len(b.Points) != 3 {
		t.Errorf("points = %d / %d, want 3 each", len(a.Points), len(b.Points))
	}
	if a.Points[0].CountTotal == nil || *a.Points[0].CountTotal != 500 ||
		b.Points[0].CountTotal == nil || *b.Points[0].CountTotal != 400 {
		t.Errorf("series merged or mis-keyed: %+v / %+v", a.Points[0], b.Points[0])
	}
	// Ascending within a network.
	if a.Points[0].Day >= a.Points[2].Day {
		t.Errorf("points not ascending: %+v", a.Points)
	}

	// Size ranking: the larger network leads.
	if got.Networks[0].ASN != 15169 {
		t.Errorf("networks[0] = AS%d, want the largest (AS15169)", got.Networks[0].ASN)
	}

	// limit clamps rather than 400s; a non-positive limit is a 400.
	var clamped struct {
		Networks []series `json:"networks"`
	}
	getJSON(t, srv.URL+"/stats/networks?limit=99", &clamped)
	if len(clamped.Networks) > 10 {
		t.Errorf("limit=99 returned %d networks, want clamped to 10", len(clamped.Networks))
	}
	var one struct {
		Networks []series `json:"networks"`
	}
	getJSON(t, srv.URL+"/stats/networks?limit=1", &one)
	if len(one.Networks) != 1 {
		t.Errorf("limit=1 returned %d networks", len(one.Networks))
	}
	var problem struct{ Type string }
	if resp := getJSON(t, srv.URL+"/stats/networks?limit=0", &problem); resp.StatusCode != 400 {
		t.Errorf("limit=0: %d, want 400", resp.StatusCode)
	}
	if resp := getJSON(t, srv.URL+"/stats/networks?interval=hourly", &problem); resp.StatusCode != 400 {
		t.Errorf("bad interval: %d, want 400", resp.StatusCode)
	}

	// A window with no rows is an empty collection, not a 404.
	var empty struct {
		Networks []series `json:"networks"`
	}
	resp := getJSON(t, srv.URL+"/stats/networks?from=2001-01-01&to=2001-02-01", &empty)
	if resp.StatusCode != 200 || len(empty.Networks) != 0 {
		t.Errorf("empty window: %d with %d networks, want 200 and []", resp.StatusCode, len(empty.Networks))
	}
}

// TestCrawlerStats (07 §4.10): aggregate throughput and nothing else.
//
// The negative assertion is the reason this endpoint has its own test.
// crawler_metrics describes how the fleet is deployed — per-worker identity,
// queue depth, lease losses — and a later "while we're here, expose qps too"
// is the failure mode. Serializing the whole row is one careless SELECT away,
// so the test reads the raw body and fails on the column names themselves.
func TestCrawlerStats(t *testing.T) {
	srv, pool := newAPI(t)
	ctx := context.Background()

	// Empty table first: a fresh install has never run the crawler.
	var empty struct {
		Checked24h int64   `json:"checked_24h"`
		Latest     *string `json:"latest"`
		Meta       struct {
			Source string `json:"source"`
		} `json:"meta"`
	}
	if resp := getJSON(t, srv.URL+"/stats/crawler", &empty); resp.StatusCode != 200 {
		t.Fatalf("empty crawler_metrics: %d, want 200 not 404", resp.StatusCode)
	}
	if empty.Checked24h != 0 || empty.Latest != nil {
		t.Errorf("empty table = %d checked / latest %v, want 0 / null", empty.Checked24h, empty.Latest)
	}

	// Two workers checkpointing inside the window, one shutdown row carrying
	// the tail delta, and one row aged out of it. processed is a per-interval
	// delta, so the window sums to 30 and the old row must not contribute.
	seed := `INSERT INTO crawler_metrics (ts, run_id, worker, processed, succeeded, failed,
	                                      qps, p50_ms, p99_ms, queue_depth, is_final)
	 VALUES (now() - interval '2 hours',  gen_random_uuid(), 'host-a:1', 10, 9, 1, 4.5, 30, 90, 5, false),
	        (now() - interval '1 hour',   gen_random_uuid(), 'host-b:2', 15, 15, 0, 5.5, 31, 91, 6, false),
	        (now() - interval '30 minutes', gen_random_uuid(), 'host-b:2', 5, 5, 0, 5.0, 32, 92, 0, true),
	        (now() - interval '30 hours', gen_random_uuid(), 'host-c:3', 999, 999, 0, 9.9, 33, 93, 7, false)`
	if _, err := pool.Exec(ctx, seed); err != nil {
		t.Fatal(err)
	}

	resp, body := fetch(t, srv.URL+"/stats/crawler")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got struct {
		Checked24h int64   `json:"checked_24h"`
		Latest     *string `json:"latest"`
		Meta       struct {
			Source string `json:"source"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	// 10 + 15 + 5 in-window (is_final included); the 30h-old 999 excluded.
	if got.Checked24h != 30 {
		t.Errorf("checked_24h = %d, want 30 (deltas summed across workers, is_final counted)", got.Checked24h)
	}
	if got.Latest == nil {
		t.Error("latest = null with rows present")
	}
	if got.Meta.Source != "telemetry" {
		t.Errorf("meta.source = %q, want telemetry — this is fleet work, not confirmed state", got.Meta.Source)
	}

	// The whole point: internal telemetry must not leak, by any name.
	for _, leak := range []string{
		"worker", "run_id", "queue_depth", "qps", "p50_ms", "p99_ms",
		"dim_counters", "is_final", "succeeded", "failed", "geoip_build_epoch",
	} {
		if strings.Contains(string(body), leak) {
			t.Errorf("response leaks internal telemetry %q: %s", leak, body)
		}
	}

	// Not generation-scoped: a 24h counter behind the stats class would
	// 304-freeze until the next daily tick.
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=60" {
		t.Errorf("Cache-Control = %q, want the short class", cc)
	}
	if etag := resp.Header.Get("ETag"); etag != "" {
		t.Errorf("ETag = %q, want none on a continuously moving counter", etag)
	}
}

// TestStatsLiveSetCounters (000008): the five live-set columns count the
// whole live population, not the ranked subset every other counter in the
// snapshot uses, and stay NULL on rows written before the columns existed.
// The rank-scoped bug this guards is silent — tracked_total simply equals
// domains — so the unranked seed row is the whole point of the test.
func TestStatsLiveSetCounters(t *testing.T) {
	srv, pool := newAPI(t) // seedLeaderboard: 10 ranked (d9 disabled) + 1 unranked
	ctx := context.Background()

	// A pre-000008 row: the five columns did not exist when it was written.
	if _, err := pool.Exec(ctx,
		`INSERT INTO stats_global_daily (day, domains) VALUES (current_date - 1, 7)`); err != nil {
		t.Fatal(err)
	}
	if err := crawler.RunStatsRollup(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var overview struct {
		Points []struct {
			Day           string `json:"day"`
			Domains       *int32 `json:"domains"`
			TrackedTotal  *int32 `json:"tracked_total"`
			PtrSupported  *int32 `json:"ptr_supported"`
			PtrGraded     *int32 `json:"ptr_graded"`
			SmtpSupported *int32 `json:"smtp_supported"`
			SmtpGraded    *int32 `json:"smtp_graded"`
		} `json:"points"`
	}
	getJSON(t, srv.URL+"/stats/overview", &overview)
	if len(overview.Points) < 2 {
		t.Fatalf("points = %d, want the pre-migration row and today's rollup", len(overview.Points))
	}
	old, latest := overview.Points[0], overview.Points[len(overview.Points)-1]

	if old.TrackedTotal != nil || old.PtrSupported != nil || old.PtrGraded != nil ||
		old.SmtpSupported != nil || old.SmtpGraded != nil {
		t.Errorf("pre-migration row must serialize the live-set columns as null, got %+v", old)
	}

	if latest.Domains == nil || latest.TrackedTotal == nil {
		t.Fatalf("rollup left the live-set columns null: %+v", latest)
	}
	// 9 live ranked (10 ranked, d9 disabled) vs 10 live overall (+ the
	// rank-NULL campaign apex). Equality here means the CTE inherited the
	// rank predicate.
	if *latest.Domains != 9 || *latest.TrackedTotal != 10 {
		t.Errorf("domains = %d, tracked_total = %d; want 9 and 10 (ranked subset vs live set)",
			*latest.Domains, *latest.TrackedTotal)
	}

	// d1 is base_status=supported with ptr_observed=partial: graded, and
	// counted as supported because a partial PTR still resolves.
	if latest.PtrSupported == nil || *latest.PtrSupported != 1 ||
		latest.PtrGraded == nil || *latest.PtrGraded != 1 {
		t.Errorf("ptr = %v/%v, want 1/1", latest.PtrSupported, latest.PtrGraded)
	}
	// d1's smtp_observed is 'partial', which production folds to unsupported
	// before storage — so it is neither supported nor gradeable here.
	if latest.SmtpSupported == nil || *latest.SmtpSupported != 0 ||
		latest.SmtpGraded == nil || *latest.SmtpGraded != 0 {
		t.Errorf("smtp = %v/%v, want 0/0 (partial is not a stored SMTP value)",
			latest.SmtpSupported, latest.SmtpGraded)
	}
}

// TestWideStatsWindowServesEverything is review issue 58's `from` half, and
// it pins the behaviour AGAINST the fix that issue proposed.
//
// The issue read the unclamped `from` as an amplifier — "a two-millennium
// scan" — and recommended a floor. Measured, it is not: `to` is clamped
// because a far-future bound makes history synthesize days that do not
// exist, while a far-past `from` only widens an indexed range scan over rows
// that do. On 3 years of daily snapshots, ?from=0001-01-01 returns every row
// in single-digit milliseconds, linear in what it returns.
//
// A floor at historyWindowDays would have silently truncated this series by
// a year: the stats tables carry no retention policy. The test exists so the
// next reader who spots the asymmetry does not "fix" it.
func TestWideStatsWindowServesEverything(t *testing.T) {
	srv, pool := newAPI(t)
	ctx := context.Background()

	// Three years of snapshots — deliberately more than historyWindowDays.
	if _, err := pool.Exec(ctx, `
		INSERT INTO stats_global_daily (day, domains, sinners, partial, heroes, saints,
		  inactive, unknown, disabled, base_supported, www_supported, ns_supported,
		  mx_supported, conn_supported, resources_supported, top_heroes, top_nameserver)
		SELECT d::date, 1000, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1
		FROM generate_series(now() - interval '3 years', now(), interval '1 day') d
		ON CONFLICT (day) DO NOTHING`); err != nil {
		t.Fatal(err)
	}

	var wide, narrow struct {
		Points []map[string]any `json:"points"`
	}
	if resp := getJSON(t, srv.URL+"/stats/overview?from=0001-01-01", &wide); resp.StatusCode != 200 {
		t.Fatalf("wide window = %d, want 200", resp.StatusCode)
	}
	getJSON(t, srv.URL+"/stats/overview", &narrow)

	if len(narrow.Points) == 0 || len(wide.Points) <= len(narrow.Points) {
		t.Fatalf("wide=%d narrow=%d points; the wide window must serve more",
			len(wide.Points), len(narrow.Points))
	}
	// The whole seeded series, not a slice of it. 730 is api.historyWindowDays,
	// spelled out because this test is in the external package — it is the cap
	// history applies to its own window and must NOT be applied here.
	const historyCapDays = 730
	if len(wide.Points) <= historyCapDays {
		t.Errorf("wide window returned %d points, want the full series past the %d-day history cap",
			len(wide.Points), historyCapDays)
	}
}
