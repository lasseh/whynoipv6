//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/api"
	"github.com/lasseh/whynoipv6/internal/crawler"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestAroundRank (07 §3.2): the centered window — ⌈limit/2⌉ rows ranked ≤ N
// plus ⌊limit/2⌋ rows ranked > N — and its cursor continuations.
func TestAroundRank(t *testing.T) {
	srv, _ := newAPI(t) // d1..d10, d9 disabled → visible ranks 1..8,10
	var env envelope
	getJSON(t, srv.URL+"/domains?around_rank=5&limit=4", &env)
	if h := hosts(t, env.Items); fmt.Sprint(h) != "[d4.example d5.example d6.example d7.example]" {
		t.Fatalf("centered window = %v", h)
	}
	// Both cursors exist: truncation above rank 4 and below rank 7.
	if env.Page.NextCursor == nil || env.Page.PrevCursor == nil {
		t.Fatalf("around window must mint both cursors: %+v", env.Page)
	}
	// Forward continuation.
	var next envelope
	getJSON(t, srv.URL+"/domains?limit=4&cursor="+*env.Page.NextCursor, &next)
	if h := hosts(t, next.Items); len(h) == 0 || h[0] != "d8.example" {
		t.Errorf("forward from window = %v, want d8.example first", h)
	}
	// Backward continuation.
	var prev envelope
	getJSON(t, srv.URL+"/domains?limit=4&cursor="+*env.Page.PrevCursor, &prev)
	if h := hosts(t, prev.Items); fmt.Sprint(h) != "[d1.example d2.example d3.example]" {
		t.Errorf("backward from window = %v", h)
	}

	// Edge: window at the very top has no rows above.
	var top envelope
	getJSON(t, srv.URL+"/domains?around_rank=1&limit=4", &top)
	if top.Page.PrevCursor != nil {
		t.Error("around_rank=1 has nothing above; prev_cursor must be null")
	}
	// Only on the rank ordering.
	var problem struct{ Type string }
	if resp := getJSON(t, srv.URL+"/domains?sort=host&around_rank=5", &problem); resp.StatusCode != 400 {
		t.Errorf("around_rank on host sort: %d", resp.StatusCode)
	}
}

// TestAroundRankPresetCursors (07 §3.2): cursors minted by a centered
// window on a preset route fingerprint the preset-MERGED query, so they
// can be followed on that same route — and nowhere the preset does not hold.
func TestAroundRankPresetCursors(t *testing.T) {
	srv, _ := newAPI(t) // heroes at visible ranks 1,3,5,7; NO = d1..d3

	// /heroes: follow the around_rank window forward.
	var window envelope
	getJSON(t, srv.URL+"/heroes?around_rank=3&limit=2", &window)
	if h := hosts(t, window.Items); fmt.Sprint(h) != "[d3.example d5.example]" {
		t.Fatalf("heroes centered window = %v", h)
	}
	if window.Page.NextCursor == nil || window.Page.PrevCursor == nil {
		t.Fatalf("heroes window must mint both cursors: %+v", window.Page)
	}
	var next envelope
	if resp := getJSON(t, srv.URL+"/heroes?limit=2&cursor="+*window.Page.NextCursor, &next); resp.StatusCode != 200 {
		t.Fatalf("following the heroes next_cursor: %d", resp.StatusCode)
	}
	if h := hosts(t, next.Items); fmt.Sprint(h) != "[d7.example]" {
		t.Errorf("heroes forward continuation = %v, want [d7.example]", h)
	}

	// /countries/{code}/domains: the same follow on a path-scoped preset.
	var no envelope
	getJSON(t, srv.URL+"/countries/NO/domains?around_rank=1&limit=2", &no)
	if h := hosts(t, no.Items); fmt.Sprint(h) != "[d1.example d2.example]" {
		t.Fatalf("NO centered window = %v", h)
	}
	if no.Page.NextCursor == nil {
		t.Fatal("NO window must mint next_cursor")
	}
	var noNext envelope
	if resp := getJSON(t, srv.URL+"/countries/NO/domains?limit=2&cursor="+*no.Page.NextCursor, &noNext); resp.StatusCode != 200 {
		t.Fatalf("following the NO next_cursor: %d", resp.StatusCode)
	}
	if h := hosts(t, noNext.Items); fmt.Sprint(h) != "[d3.example]" {
		t.Errorf("NO forward continuation = %v, want [d3.example]", h)
	}

	// Cursors are scope-bound: a /heroes cursor replays neither on the
	// bare leaderboard nor on a sibling preset.
	var problem struct{ Type string }
	if resp := getJSON(t, srv.URL+"/domains?limit=2&cursor="+*window.Page.NextCursor, &problem); resp.StatusCode != 400 {
		t.Errorf("heroes cursor on /domains: %d, want 400", resp.StatusCode)
	}
	if resp := getJSON(t, srv.URL+"/sinners?limit=2&cursor="+*window.Page.NextCursor, &problem); resp.StatusCode != 400 {
		t.Errorf("heroes cursor on /sinners: %d, want 400", resp.StatusCode)
	}
}

// TestPrevCursor (07 §3.2): bidirectional paging — walking forward then
// back reproduces the earlier page exactly.
func TestPrevCursor(t *testing.T) {
	srv, pool := newAPI(t)

	// /domains: page 1 → page 2 → back to page 1.
	var page1 envelope
	getJSON(t, srv.URL+"/domains?limit=3", &page1)
	if page1.Page.PrevCursor != nil {
		t.Error("the unpositioned first page has no prev_cursor")
	}
	var page2 envelope
	getJSON(t, srv.URL+"/domains?limit=3&cursor="+*page1.Page.NextCursor, &page2)
	if page2.Page.PrevCursor == nil {
		t.Fatal("a positioned page must mint prev_cursor")
	}
	var back envelope
	getJSON(t, srv.URL+"/domains?limit=3&cursor="+*page2.Page.PrevCursor, &back)
	if fmt.Sprint(hosts(t, back.Items)) != fmt.Sprint(hosts(t, page1.Items)) {
		t.Errorf("prev walk = %v, want page 1 %v", hosts(t, back.Items), hosts(t, page1.Items))
	}
	if !back.Page.HasMore || back.Page.NextCursor == nil {
		t.Error("the backward page must carry a forward cursor")
	}
	if back.Page.PrevCursor != nil {
		t.Error("page 1 reached backward has nothing before it")
	}

	// /changelog: same round trip on the ts ordering.
	seedEntities(t, pool)
	seedChangelog(t, pool)
	var cl1 struct {
		Items []struct {
			Field string `json:"field"`
		} `json:"items"`
		Page struct {
			NextCursor *string `json:"next_cursor"`
			PrevCursor *string `json:"prev_cursor"`
		} `json:"page"`
	}
	getJSON(t, srv.URL+"/changelog?limit=1", &cl1)
	cl2 := cl1
	getJSON(t, srv.URL+"/changelog?limit=1&cursor="+*cl1.Page.NextCursor, &cl2)
	if cl2.Page.PrevCursor == nil {
		t.Fatal("changelog page 2 must mint prev_cursor")
	}
	clBack := cl1
	getJSON(t, srv.URL+"/changelog?limit=1&cursor="+*cl2.Page.PrevCursor, &clBack)
	if len(clBack.Items) != 1 || clBack.Items[0].Field != cl1.Items[0].Field {
		t.Errorf("changelog prev walk = %+v, want %+v", clBack.Items, cl1.Items)
	}
}

// TestReviewGaps covers the acceptance bullets flagged by the review:
// /heroes request, bare ?flag= 422, inconsistent masking, resources
// changelog transitions, the feed latest-50 window, the global rate cap,
// and the reaper via its sqlc query.
func TestReviewGaps(t *testing.T) {
	srv, pool := newAPI(t)
	ctx := context.Background()

	// /heroes ≡ /domains?class=hero.
	var tier, filtered envelope
	getJSON(t, srv.URL+"/heroes", &tier)
	getJSON(t, srv.URL+"/domains?class=hero", &filtered)
	if fmt.Sprint(hosts(t, tier.Items)) != fmt.Sprint(hosts(t, filtered.Items)) || len(tier.Items) == 0 {
		t.Errorf("/heroes %v != ?class=hero %v", hosts(t, tier.Items), hosts(t, filtered.Items))
	}

	// Bare ?flag= → 422 scope-required.
	var problem struct{ Type string }
	if resp := getJSON(t, srv.URL+"/domains?flag=broken_v6", &problem); resp.StatusCode != 422 ||
		problem.Type != "https://whynoipv6.com/problems/scope-required" {
		t.Errorf("bare flag: %d %+v", resp.StatusCode, problem)
	}

	// `inconsistent` masks to null on the detail informational block.
	if _, err := pool.Exec(ctx,
		"UPDATE domain SET ptr_observed = 'inconsistent', parity_observed = 'inconsistent' WHERE host = 'd2.example'"); err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Informational struct {
			PTR    *string `json:"ptr"`
			Parity *string `json:"parity"`
		} `json:"informational"`
	}
	getJSON(t, srv.URL+"/domains/d2.example", &detail)
	if detail.Informational.PTR != nil || detail.Informational.Parity != nil {
		t.Errorf("inconsistent must mask to null: %+v", detail.Informational)
	}

	// A `resources` transition is served (no coverage filter) in the list
	// and the feed; 60 rows prove the latest-50 feed window.
	if _, err := pool.Exec(ctx, `
		INSERT INTO changelog (domain_id, ts, field, old_value, new_value)
		SELECT (SELECT id FROM domain WHERE host = 'd1.example'),
		       now() - (g || ' minutes')::interval,
		       CASE WHEN g = 1 THEN 'resources' ELSE 'www' END,
		       CASE WHEN g % 2 = 0 THEN 'supported' ELSE 'unsupported' END::ipv6_status,
		       CASE WHEN g % 2 = 0 THEN 'unsupported' ELSE 'supported' END::ipv6_status
		FROM generate_series(1, 60) g`); err != nil {
		t.Fatal(err)
	}
	var cl envelope
	getJSON(t, srv.URL+"/changelog?field=resources", &cl)
	if len(cl.Items) != 1 {
		t.Errorf("resources transitions = %d, want 1 served", len(cl.Items))
	}
	_, feedBody := fetch(t, srv.URL+"/changelog.feed.json")
	var feed struct {
		Items []struct {
			ContentText string `json:"content_text"`
		} `json:"items"`
	}
	if err := json.Unmarshal(feedBody, &feed); err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 50 {
		t.Errorf("feed window = %d items, want exactly 50", len(feed.Items))
	}
	foundResources := false
	for _, it := range feed.Items {
		if strings.Contains(it.ContentText, "resources for d1.example") {
			foundResources = true
		}
	}
	if !foundResources {
		t.Error("the resources transition must appear in the feed")
	}

	// Reaper through its real sqlc query.
	if resp := postCheck(t, srv, "192.0.2.77", `{"host":"reapme.no"}`); resp.StatusCode != 202 {
		t.Fatal("enqueue failed")
	}
	if _, err := pool.Exec(ctx, "UPDATE check_job SET created_at = now() - interval '20 minutes'"); err != nil {
		t.Fatal(err)
	}
	n, err := db.New(pool).CheckJobReap(ctx, pgtype.Interval{Microseconds: (15 * time.Minute).Microseconds(), Valid: true})
	if err != nil || n != 1 {
		t.Errorf("CheckJobReap = %d, %v", n, err)
	}
	var status string
	if err := pool.QueryRow(ctx, "SELECT status::text FROM check_job WHERE host = 'reapme.no'").Scan(&status); err != nil || status != "failed" {
		t.Errorf("reaped job status = %s, %v", status, err)
	}
}

// TestGlobalRateCap: the site-wide hourly cap 429s independently of the
// per-/64 bucket.
func TestGlobalRateCap(t *testing.T) {
	srv, _ := newCheckAPI(t, api.Options{RateIPPerHour: 100, RateGlobalPerHour: 2})
	if resp := postCheck(t, srv, "192.0.2.1", `{"host":"g1.no"}`); resp.StatusCode != 202 {
		t.Fatal("g1 enqueue failed")
	}
	if resp := postCheck(t, srv, "198.51.100.1", `{"host":"g2.no"}`); resp.StatusCode != 202 {
		t.Fatal("g2 enqueue failed")
	}
	over := postCheck(t, srv, "203.0.113.1", `{"host":"g3.no"}`)
	if over.StatusCode != 429 || over.Header.Get("Retry-After") == "" {
		t.Errorf("global cap: %d retry=%q", over.StatusCode, over.Header.Get("Retry-After"))
	}
}

// TestGraphsEqualLists (P6.2 invariant): the stats rollup's counters equal
// what the public lists report for the same day.
func TestGraphsEqualLists(t *testing.T) {
	pool := pgtest.NewDB(t)
	srv := newServerOver(t, pool)
	seedLeaderboard(t, pool)

	if err := crawler.RunStatsRollup(context.Background(), pool); err != nil {
		t.Fatal(err)
	}

	var overview struct {
		Points []struct {
			Day     string `json:"day"`
			Domains *int32 `json:"domains"`
			Heroes  *int32 `json:"heroes"`
			Sinners *int32 `json:"sinners"`
		} `json:"points"`
	}
	getJSON(t, srv.URL+"/stats/overview", &overview)
	if len(overview.Points) == 0 {
		t.Fatal("no stats points after rollup")
	}
	latest := overview.Points[len(overview.Points)-1]
	if latest.Day != time.Now().UTC().Format("2006-01-02") {
		t.Fatalf("latest point day = %s", latest.Day)
	}

	var heroes, sinners envelope
	getJSON(t, srv.URL+"/heroes?limit=200", &heroes)
	getJSON(t, srv.URL+"/sinners?limit=200", &sinners)
	if latest.Heroes == nil || int(*latest.Heroes) != len(heroes.Items) {
		t.Errorf("graph heroes = %v, list shows %d", latest.Heroes, len(heroes.Items))
	}
	if latest.Sinners == nil || int(*latest.Sinners) != len(sinners.Items) {
		t.Errorf("graph sinners = %v, list shows %d", latest.Sinners, len(sinners.Items))
	}
}

// newServerOver serves the API over an existing (caller-seeded) pool.
func newServerOver(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(api.NewRouter(pool, api.Options{}))
	t.Cleanup(srv.Close)
	return srv
}
