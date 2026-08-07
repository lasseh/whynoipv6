//go:build integration

package api_test

import (
	"context"
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
