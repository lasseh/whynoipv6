//go:build integration

package api_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedChangelog: d3.example bootstraps (no changelog rows), then flips www
// unsupported→supported and mx supported→not_applicable on later days, plus
// one conn transition on d5 and scan latency rows.
func seedChangelog(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		// d3's bootstrap happened at creation; backdate created_at so the
		// window clamp keeps early days visible.
		`UPDATE domain SET created_at = now() - interval '20 days',
		                   www_status = 'supported', www_since = now() - interval '5 days',
		                   mx_status = 'not_applicable', mx_since = now() - interval '3 days'
		 WHERE host = 'd3.example'`,
		`INSERT INTO changelog (domain_id, ts, field, old_value, new_value)
		 SELECT id, now() - interval '5 days', 'www', 'unsupported', 'supported'
		 FROM domain WHERE host = 'd3.example'`,
		`INSERT INTO changelog (domain_id, ts, field, old_value, new_value)
		 SELECT id, now() - interval '3 days', 'mx', 'supported', 'not_applicable'
		 FROM domain WHERE host = 'd3.example'`,
		`INSERT INTO changelog (domain_id, ts, field, old_value, new_value)
		 SELECT id, now() - interval '2 days', 'conn', 'supported', 'unsupported'
		 FROM domain WHERE host = 'd5.example'`,
		`INSERT INTO scan (domain_id, ts, base, www, ns, mx, conn, resources,
		                   latency_v4_ms, latency_v6_ms, classification)
		 SELECT id, now() - interval '2 days', 'supported', 'supported', 'supported',
		        'not_applicable', 'supported', 'no_record', 41, 38, 'hero'
		 FROM domain WHERE host = 'd3.example'`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("seed changelog: %v\n%s", err, s)
		}
	}
}

// TestChangelog (P4.14): structured rows, all fields served incl. conn and
// not_applicable transitions, keyset walk, field/window filters, scoped
// recent-window feeds, empty-DB 200.
func TestChangelog(t *testing.T) {
	srv, pool := newAPI(t)
	seedEntities(t, pool)

	// Fresh DB: 200 with an empty collection.
	var empty envelope
	if resp := getJSON(t, srv.URL+"/changelog", &empty); resp.StatusCode != 200 || len(empty.Items) != 0 {
		t.Fatalf("empty changelog: %d, %d items", resp.StatusCode, len(empty.Items))
	}

	seedChangelog(t, pool)

	var env struct {
		Items []struct {
			TS       time.Time `json:"ts"`
			Host     string    `json:"host"`
			Field    string    `json:"field"`
			OldValue string    `json:"old_value"`
			NewValue string    `json:"new_value"`
		} `json:"items"`
		Page struct {
			NextCursor *string `json:"next_cursor"`
			HasMore    bool    `json:"has_more"`
		} `json:"page"`
	}
	getJSON(t, srv.URL+"/changelog", &env)
	if len(env.Items) != 3 {
		t.Fatalf("changelog rows = %d, want 3", len(env.Items))
	}
	// ts DESC: conn (2d) → mx not_applicable (3d) → www (5d).
	if env.Items[0].Field != "conn" || env.Items[0].Host != "d5.example" ||
		env.Items[0].NewValue != "unsupported" {
		t.Errorf("items[0] = %+v, want the conn transition (served, no coverage filter)", env.Items[0])
	}
	if env.Items[1].Field != "mx" || env.Items[1].NewValue != "not_applicable" {
		t.Errorf("items[1] = %+v, want the not_applicable transition", env.Items[1])
	}
	for _, it := range env.Items {
		if it.OldValue == "" || it.NewValue == "" || it.OldValue == it.NewValue {
			t.Errorf("row %+v: old/new must be non-null and distinct", it)
		}
	}

	// ?field= filter.
	var wwwOnly envelope
	getJSON(t, srv.URL+"/changelog?field=www", &wwwOnly)
	if len(wwwOnly.Items) != 1 {
		t.Errorf("field=www rows = %d", len(wwwOnly.Items))
	}
	var badField struct{ Type string }
	if resp := getJSON(t, srv.URL+"/changelog?field=tls", &badField); resp.StatusCode != 422 {
		t.Errorf("bad field: %d", resp.StatusCode)
	}

	// ?from= window: only the last 4 days (conn + mx).
	fromDay := time.Now().UTC().AddDate(0, 0, -4).Format("2006-01-02")
	var windowed envelope
	getJSON(t, srv.URL+"/changelog?from="+fromDay, &windowed)
	if len(windowed.Items) != 2 {
		t.Errorf("windowed rows = %d, want 2", len(windowed.Items))
	}

	// Keyset walk at limit=1 covers all three exactly once, newest first.
	var seen []string
	url := srv.URL + "/changelog?limit=1"
	for {
		var page struct {
			Items []struct {
				Field string `json:"field"`
			} `json:"items"`
			Page struct {
				NextCursor *string `json:"next_cursor"`
				HasMore    bool    `json:"has_more"`
			} `json:"page"`
		}
		getJSON(t, url, &page)
		for _, it := range page.Items {
			seen = append(seen, it.Field)
		}
		if !page.Page.HasMore {
			break
		}
		url = srv.URL + "/changelog?limit=1&cursor=" + *page.Page.NextCursor
	}
	if fmt.Sprint(seen) != "[conn mx www]" {
		t.Errorf("walk = %v, want [conn mx www]", seen)
	}

	// Per-domain feed.
	var d3 envelope
	getJSON(t, srv.URL+"/domains/d3.example/changelog", &d3)
	if len(d3.Items) != 2 {
		t.Errorf("d3 feed rows = %d, want 2", len(d3.Items))
	}

	// Scoped recent-window feeds: country (d3 is NO; d5 is UN) and campaign
	// (d3 is a member).
	var no envelope
	getJSON(t, srv.URL+"/countries/no/changelog", &no)
	if len(no.Items) != 2 {
		t.Errorf("NO feed rows = %d, want 2 (d3 only)", len(no.Items))
	}
	var camp envelope
	getJSON(t, srv.URL+"/campaigns/"+campaignUUID+"/changelog", &camp)
	if len(camp.Items) != 2 {
		t.Errorf("campaign feed rows = %d, want 2 (member d3)", len(camp.Items))
	}
	var member envelope
	getJSON(t, srv.URL+"/campaigns/"+campaignUUID+"/domains/d3.example/changelog", &member)
	if len(member.Items) != 2 {
		t.Errorf("member feed rows = %d", len(member.Items))
	}
	var problem struct{ Type string }
	if resp := getJSON(t, srv.URL+"/campaigns/"+campaignUUID+"/domains/d1.example/changelog", &problem); resp.StatusCode != 404 {
		t.Errorf("non-member feed: %d, want 404", resp.StatusCode)
	}

	// ?scope=campaign: campaign-member domains only (d3's 2 rows, never d5's
	// conn row), recent-window envelope with null cursors.
	var scoped struct {
		Items []struct {
			Host string `json:"host"`
		} `json:"items"`
		Page struct {
			NextCursor *string `json:"next_cursor"`
			PrevCursor *string `json:"prev_cursor"`
			HasMore    bool    `json:"has_more"`
		} `json:"page"`
	}
	getJSON(t, srv.URL+"/changelog?scope=campaign", &scoped)
	if len(scoped.Items) != 2 {
		t.Errorf("scope=campaign rows = %d, want 2 (member d3 only)", len(scoped.Items))
	}
	for _, it := range scoped.Items {
		if it.Host != "d3.example" {
			t.Errorf("scope=campaign leaked non-member host %s", it.Host)
		}
	}
	if scoped.Page.NextCursor != nil || scoped.Page.PrevCursor != nil || scoped.Page.HasMore {
		t.Errorf("scope=campaign page = %+v, want null cursors / has_more=false", scoped.Page)
	}
	if resp := getJSON(t, srv.URL+"/changelog?scope=bogus", &problem); resp.StatusCode != 422 {
		t.Errorf("bad scope: %d, want 422", resp.StatusCode)
	}
}

// TestHistory (P4.14 / 07 §4.9): changelog-reconstructed trajectory — the
// ladder per point, error/inconsistent structurally absent, latency overlay
// from scan, empty changelog → empty points.
func TestHistory(t *testing.T) {
	srv, pool := newAPI(t)
	seedEntities(t, pool)

	// No changelog rows → 200 with points: [] (day-1 rule, OPEN-9).
	var fresh struct {
		Host   string `json:"host"`
		Points []any  `json:"points"`
		Meta   struct {
			RetentionDays int `json:"retention_days"`
		} `json:"meta"`
	}
	if resp := getJSON(t, srv.URL+"/domains/d3.example/history", &fresh); resp.StatusCode != 200 || len(fresh.Points) != 0 {
		t.Fatalf("fresh history: %d, %d points", resp.StatusCode, len(fresh.Points))
	}
	if fresh.Meta.RetentionDays != 730 {
		t.Errorf("retention_days = %d", fresh.Meta.RetentionDays)
	}

	seedChangelog(t, pool)

	var hist struct {
		Host   string `json:"host"`
		Points []struct {
			Day            string  `json:"day"`
			Base           *string `json:"base"`
			WWW            *string `json:"www"`
			NS             *string `json:"ns"`
			MX             *string `json:"mx"`
			Conn           *string `json:"conn"`
			Resources      *string `json:"resources"`
			Classification string  `json:"classification"`
			LatencyV4Ms    *int32  `json:"latency_v4_ms"`
		} `json:"points"`
	}
	getJSON(t, srv.URL+"/domains/d3.example/history", &hist)
	if hist.Host != "d3.example" || len(hist.Points) == 0 {
		t.Fatalf("history = %s, %d points", hist.Host, len(hist.Points))
	}
	// The window is clamped to created_at (20 days back), so ~21 points.
	if len(hist.Points) < 19 || len(hist.Points) > 22 {
		t.Errorf("points = %d, want ~21 (clamped to created_at)", len(hist.Points))
	}

	deref := func(s *string) string {
		if s == nil {
			return "<null>"
		}
		return *s
	}
	firstDay, lastDay := hist.Points[0], hist.Points[len(hist.Points)-1]
	// Before the www transition (5d ago) www replays its old_value.
	if deref(firstDay.WWW) != "unsupported" {
		t.Errorf("day[0].www = %s, want unsupported (old_value backfill)", deref(firstDay.WWW))
	}
	if deref(lastDay.WWW) != "supported" {
		t.Errorf("last.www = %s, want supported", deref(lastDay.WWW))
	}
	if deref(lastDay.MX) != "not_applicable" {
		t.Errorf("last.mx = %s, want not_applicable", deref(lastDay.MX))
	}
	// d3's seed has base/ns supported since 90/30 days; conn never
	// confirmed → null; resources null everywhere.
	if deref(lastDay.Base) != "supported" || deref(lastDay.NS) != "supported" {
		t.Errorf("last base/ns = %s/%s", deref(lastDay.Base), deref(lastDay.NS))
	}
	if lastDay.Resources != nil {
		t.Errorf("resources = %s, want null (never confirmed)", *lastDay.Resources)
	}
	// The ladder per point: conn is never confirmed for d3, so even with
	// base+www+ns+mx green the hero bar is not met → partial.
	if lastDay.Classification != "partial" {
		t.Errorf("last.classification = %s, want partial (conn unconfirmed)", lastDay.Classification)
	}
	// error/inconsistent are structurally impossible: every value must be
	// in the 4-value enum.
	valid := map[string]bool{"supported": true, "unsupported": true, "no_record": true, "not_applicable": true, "<null>": true}
	for _, p := range hist.Points {
		for _, v := range []*string{p.Base, p.WWW, p.NS, p.MX, p.Conn, p.Resources} {
			if !valid[deref(v)] {
				t.Fatalf("day %s carries %q — error/inconsistent must never reach history", p.Day, deref(v))
			}
		}
	}
	// Latency overlay from scan on the -2d day.
	overlayDay := time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02")
	found := false
	for _, p := range hist.Points {
		if p.Day == overlayDay {
			found = true
			if p.LatencyV4Ms == nil || *p.LatencyV4Ms != 41 {
				t.Errorf("overlay day latency = %v, want 41", p.LatencyV4Ms)
			}
		}
	}
	if !found {
		t.Errorf("overlay day %s missing from points", overlayDay)
	}

	// Weekly sampling returns only week-boundary points.
	var weekly struct {
		Points []struct {
			Day string `json:"day"`
		} `json:"points"`
	}
	getJSON(t, srv.URL+"/domains/d3.example/history?interval=weekly", &weekly)
	if len(weekly.Points) == 0 || len(weekly.Points) >= len(hist.Points) {
		t.Errorf("weekly points = %d (daily %d)", len(weekly.Points), len(hist.Points))
	}
	for _, p := range weekly.Points {
		d, _ := time.Parse("2006-01-02", p.Day)
		if d.Weekday() != time.Monday {
			t.Errorf("weekly point %s is not a week boundary", p.Day)
		}
	}

	// Validation: bad interval / reversed window → 400.
	var problem struct{ Type string }
	if resp := getJSON(t, srv.URL+"/domains/d3.example/history?interval=hourly", &problem); resp.StatusCode != 400 {
		t.Errorf("bad interval: %d", resp.StatusCode)
	}
	if resp := getJSON(t, srv.URL+"/domains/d3.example/history?from=2026-07-09&to=2026-07-01", &problem); resp.StatusCode != 400 {
		t.Errorf("reversed window: %d", resp.StatusCode)
	}
}
