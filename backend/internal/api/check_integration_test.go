//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/api"
	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

func newCheckAPI(t *testing.T, opts api.Options) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.NewDB(t)
	srv := httptest.NewServer(api.NewRouter(pool, opts))
	t.Cleanup(srv.Close)
	return srv, pool
}

func postCheck(t *testing.T, srv *httptest.Server, ip, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/check", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Real-IP", ip)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}

// fakeScanResult builds a stored engine ScanResult for the dedupe mapper.
func fakeScanResult(host string) []byte {
	res := func(status checker.CheckStatus) checker.Result { return checker.Result{Status: status} }
	sr := checker.ScanResult{
		Domain:    host,
		ScannedAt: time.Now().UTC().Add(-10 * time.Minute),
		Duration:  4 * time.Second,
		Results: map[string]checker.Result{
			"dns_aaaa_base": res(checker.StatusSupported), "dns_aaaa_www": res(checker.StatusSupported),
			"dns_ns_ipv6": res(checker.StatusSupported), "dns_mx_ipv6": res(checker.StatusNotApplicable),
			"https_ipv6": res(checker.StatusSupported), "http_ipv6": res(checker.StatusSupported),
			"tls_ipv6": res(checker.StatusSupported), "smtp_ipv6": res(checker.StatusNotApplicable),
			"http_response_parity": res(checker.StatusSupported), "dns_dnssec": res(checker.StatusUnsupported),
			"dns_ptr_ipv6": res(checker.StatusSupported), "spf_ipv6": res(checker.StatusSupported),
			"latency_ipv4": res(checker.StatusUnsupported), "latency_ipv6": res(checker.StatusUnsupported),
			"resource_discovery": res(checker.StatusSupported),
		},
	}
	raw, _ := json.Marshal(sr)
	return raw
}

// TestLiveCheck (P6.1): validation, enqueue/poll lifecycle, dedupe (domain-
// and job-side), rate limits (/64 keying), reaper, Rule 0, poll caching.
func TestLiveCheck(t *testing.T) {
	srv, pool := newCheckAPI(t, api.Options{RateIPPerHour: 3, RateGlobalPerHour: 100})
	ctx := context.Background()

	// Validation layer.
	if resp := postCheck(t, srv, "192.0.2.1", "not json"); resp.StatusCode != 415 {
		t.Errorf("non-JSON body: %d, want 415", resp.StatusCode)
	}
	if resp := postCheck(t, srv, "192.0.2.1", `{"other":"x"}`); resp.StatusCode != 400 {
		t.Errorf("missing host: %d, want 400", resp.StatusCode)
	}
	if resp := postCheck(t, srv, "192.0.2.1", `{"host":42}`); resp.StatusCode != 400 {
		t.Errorf("non-string host: %d, want 400", resp.StatusCode)
	}
	if resp := postCheck(t, srv, "192.0.2.1", `{"host":"foo.test"}`); resp.StatusCode != 400 {
		t.Errorf("reserved TLD: %d, want 400", resp.StatusCode)
	}

	// Enqueue: 202 + Location + no-store + exactly the four keys.
	resp := postCheck(t, srv, "192.0.2.1", `{"host":"live1.no"}`)
	if resp.StatusCode != 202 || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("enqueue: %d cc=%q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	var created map[string]any
	decodeBody(t, resp, &created)
	if len(created) != 4 || created["status"] != "pending" || created["host"] != "live1.no" {
		t.Errorf("202 body = %v (want exactly id/host/status/created_at)", created)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/check/") {
		t.Fatalf("Location = %q", loc)
	}

	// Poll: pending is no-store; bad ids are 404.
	var job struct {
		ID     *int64 `json:"id"`
		Status string `json:"status"`
		Cached bool   `json:"cached"`
	}
	pollResp, _ := http.Get(srv.URL + loc)
	if pollResp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("in-flight poll cc = %q", pollResp.Header.Get("Cache-Control"))
	}
	decodeBody(t, pollResp, &job)
	if job.Status != "pending" || job.Cached {
		t.Errorf("poll = %+v", job)
	}
	var problem struct{ Type string }
	if r := getJSON(t, srv.URL+"/check/abc", &problem); r.StatusCode != 404 {
		t.Errorf("non-numeric id: %d", r.StatusCode)
	}
	if r := getJSON(t, srv.URL+"/check/999999", &problem); r.StatusCode != 404 {
		t.Errorf("unknown id: %d", r.StatusCode)
	}

	// Simulate the consumer: claim → complete; terminal poll caches.
	if _, err := pool.Exec(ctx,
		`UPDATE check_job SET status='done', result='{"checks":{}}', completed_at=now() WHERE host='live1.no'`); err != nil {
		t.Fatal(err)
	}
	doneResp, _ := http.Get(srv.URL + loc)
	if doneResp.Header.Get("Cache-Control") != "public, max-age=60" {
		t.Errorf("terminal poll cc = %q", doneResp.Header.Get("Cache-Control"))
	}
	var done struct {
		Status      string          `json:"status"`
		Result      json.RawMessage `json:"result"`
		CompletedAt *time.Time      `json:"completed_at"`
	}
	decodeBody(t, doneResp, &done)
	if done.Status != "done" || done.Result == nil || done.CompletedAt == nil {
		t.Errorf("done poll = %+v", done)
	}

	// Job-side dedupe: another POST within the window replays it, cached.
	resp = postCheck(t, srv, "192.0.2.1", `{"host":"live1.no"}`)
	var dedup struct {
		ID     *int64 `json:"id"`
		Cached bool   `json:"cached"`
		Status string `json:"status"`
	}
	if resp.StatusCode != 200 {
		t.Fatalf("job dedupe: %d", resp.StatusCode)
	}
	decodeBody(t, resp, &dedup)
	if !dedup.Cached || dedup.Status != "done" || dedup.ID == nil {
		t.Errorf("job dedupe = %+v", dedup)
	}

	// Rate limit: 3/h per /64. Two more jobs from the same /64 hit the cap;
	// a different /64 still passes; Retry-After + RateLimit headers ride.
	if resp := postCheck(t, srv, "2001:db8:0:1::1", `{"host":"rl1.no"}`); resp.StatusCode != 202 {
		t.Fatalf("rl1: %d", resp.StatusCode)
	}
	if resp := postCheck(t, srv, "2001:db8:0:1::2", `{"host":"rl2.no"}`); resp.StatusCode != 202 {
		t.Fatalf("rl2: %d", resp.StatusCode)
	}
	if resp := postCheck(t, srv, "2001:db8:0:1::3", `{"host":"rl3.no"}`); resp.StatusCode != 202 {
		t.Fatalf("rl3: %d", resp.StatusCode)
	}
	over := postCheck(t, srv, "2001:db8:0:1:ffff::9", `{"host":"rl4.no"}`)
	if over.StatusCode != 429 || over.Header.Get("Retry-After") == "" ||
		over.Header.Get("RateLimit-Policy") == "" {
		t.Fatalf("over quota: %d retry=%q policy=%q", over.StatusCode,
			over.Header.Get("Retry-After"), over.Header.Get("RateLimit-Policy"))
	}
	var rlProblem struct {
		Type       string `json:"type"`
		RetryAfter int    `json:"retry_after"`
	}
	decodeBody(t, over, &rlProblem)
	if rlProblem.Type != "https://whynoipv6.com/problems/rate-limited" || rlProblem.RetryAfter < 1 {
		t.Errorf("429 body = %+v", rlProblem)
	}
	if resp := postCheck(t, srv, "2001:db8:0:2::1", `{"host":"rl5.no"}`); resp.StatusCode != 202 {
		t.Errorf("different /64 must not share the bucket: %d", resp.StatusCode)
	}
}

// TestLiveCheckDedupeAndRule0: domain-side dedupe serves the mapper output
// from scan_detail with no new job rows; re-entry touches only the
// lifecycle columns.
func TestLiveCheckDedupeAndRule0(t *testing.T) {
	srv, pool := newCheckAPI(t, api.Options{})
	ctx := context.Background()

	stmts := []string{
		`INSERT INTO domain (host, kind, rank, created_by, asn_id, country_id, tld,
		                     classification, base_status, base_since, last_checked_at)
		 VALUES ('fresh.no', 'apex', 10, 'tranco', (SELECT id FROM asn WHERE number = 0),
		         (SELECT id FROM country WHERE code = 'NO'), 'no', 'partial',
		         'supported', now() - interval '30 days', now() - interval '10 minutes')`,
		// A delisted row for the §5.1.6 re-entry path.
		`INSERT INTO domain (host, kind, created_by, asn_id, country_id, tld, disabled, disabled_reason, disabled_at)
		 VALUES ('delisted.no', 'apex', 'tranco', (SELECT id FROM asn WHERE number = 0),
		         (SELECT id FROM country WHERE code = 'NO'), 'no', true, 'delisted', now())`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO scan_detail (domain_id, ts, details)
		 SELECT id, now() - interval '10 minutes', $1 FROM domain WHERE host = 'fresh.no'`,
		fakeScanResult("fresh.no")); err != nil {
		t.Fatal(err)
	}

	// Domain-side dedupe: 200 synthetic done envelope, id null, no job row.
	resp := postCheck(t, srv, "192.0.2.9", `{"host":"fresh.no"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("domain dedupe: %d", resp.StatusCode)
	}
	var env struct {
		ID     *int64 `json:"id"`
		Cached bool   `json:"cached"`
		Status string `json:"status"`
		Result struct {
			Checks map[string]struct {
				Status string `json:"status"`
			} `json:"checks"`
		} `json:"result"`
		Confirmed *struct {
			Classification string `json:"classification"`
		} `json:"confirmed"`
	}
	decodeBody(t, resp, &env)
	if env.ID != nil || !env.Cached || env.Status != "done" {
		t.Errorf("dedupe envelope = id=%v cached=%v status=%s", env.ID, env.Cached, env.Status)
	}
	if env.Result.Checks["base"].Status != "supported" || env.Result.Checks["mx"].Status != "not_applicable" {
		t.Errorf("mapped checks = %+v", env.Result.Checks)
	}
	if env.Confirmed == nil || env.Confirmed.Classification != "partial" {
		t.Errorf("confirmed = %+v", env.Confirmed)
	}
	var jobs int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM check_job").Scan(&jobs); err != nil || jobs != 0 {
		t.Errorf("domain dedupe must create no job rows (n=%d)", jobs)
	}

	// Rule 0 + §5.1.6: the dedupe POST set last_requested_at but left the
	// confirmed state untouched.
	var lastReq *time.Time
	var baseStatus string
	if err := pool.QueryRow(ctx,
		"SELECT last_requested_at, base_status::text FROM domain WHERE host = 'fresh.no'").
		Scan(&lastReq, &baseStatus); err != nil {
		t.Fatal(err)
	}
	if lastReq == nil || time.Since(*lastReq) > time.Minute {
		t.Error("re-entry must stamp last_requested_at")
	}
	if baseStatus != "supported" {
		t.Errorf("confirmed state changed: base=%s", baseStatus)
	}
	var scans, changelogs int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM scan").Scan(&scans)
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM changelog").Scan(&changelogs)
	if scans != 0 || changelogs != 0 {
		t.Errorf("Rule 0 violated: scan=%d changelog=%d", scans, changelogs)
	}

	// Delisted re-enable on POST.
	if resp := postCheck(t, srv, "192.0.2.9", `{"host":"delisted.no"}`); resp.StatusCode != 202 {
		t.Fatalf("delisted enqueue: %d", resp.StatusCode)
	}
	var disabled bool
	var reason *string
	if err := pool.QueryRow(ctx,
		"SELECT disabled, disabled_reason::text FROM domain WHERE host = 'delisted.no'").
		Scan(&disabled, &reason); err != nil {
		t.Fatal(err)
	}
	if disabled || reason != nil {
		t.Errorf("delisted row must re-enable on POST: disabled=%v reason=%v", disabled, reason)
	}
}

// TestLiveCheckReaper: stale pending/processing jobs flip to failed.
func TestLiveCheckReaper(t *testing.T) {
	srv, pool := newCheckAPI(t, api.Options{})
	ctx := context.Background()
	if resp := postCheck(t, srv, "192.0.2.1", `{"host":"stale.no"}`); resp.StatusCode != 202 {
		t.Fatal("enqueue failed")
	}
	if _, err := pool.Exec(ctx, "UPDATE check_job SET created_at = now() - interval '20 minutes'"); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := pool.QueryRow(ctx,
		`WITH reaped AS (
		   UPDATE check_job SET status='failed', error='timed out', completed_at=now()
		   WHERE status IN ('pending','processing') AND created_at < now() - interval '15 minutes'
		   RETURNING 1)
		 SELECT count(*) FROM reaped`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("reaper caught %d jobs, want 1", n)
	}
	var status, errMsg string
	if err := pool.QueryRow(ctx, "SELECT status::text, error FROM check_job WHERE host='stale.no'").
		Scan(&status, &errMsg); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || errMsg != "timed out" {
		t.Errorf("reaped job = %s %q", status, errMsg)
	}
}
