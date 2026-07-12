//go:build integration

package api_test

import (
	"context"
	"strings"
	"testing"
)

// TestBadgeHandlers (10-testing §10.6): 200-always on any valid host,
// suffix-less route-miss 404, invalid host 400 problem+json, cache headers,
// the shields JSON variant, and unknown-equivalence across no-row/disabled.
func TestBadgeHandlers(t *testing.T) {
	srv, pool := newAPI(t)
	ctx := context.Background()
	// Real-TLD fixtures (the .example seeds hit the reserved-TLD layer).
	stmts := []string{
		`INSERT INTO domain (host, kind, rank, created_by, asn_id, country_id, tld, classification, saint)
		 VALUES ('badge-hero.no', 'apex', 900, 'tranco', (SELECT id FROM asn WHERE number = 0),
		         (SELECT id FROM country WHERE code = 'NO'), 'no', 'hero', true)`,
		`INSERT INTO domain (host, kind, rank, created_by, asn_id, country_id, tld, classification)
		 VALUES ('badge-sinner.no', 'apex', 901, 'tranco', (SELECT id FROM asn WHERE number = 0),
		         (SELECT id FROM country WHERE code = 'NO'), 'no', 'sinner')`,
		`INSERT INTO domain (host, kind, rank, created_by, asn_id, country_id, tld, classification, disabled, disabled_reason)
		 VALUES ('badge-disabled.no', 'apex', 902, 'tranco', (SELECT id FROM asn WHERE number = 0),
		         (SELECT id FROM country WHERE code = 'NO'), 'no', 'hero', true, 'manual')`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// SVG variants render per the table; headers per §5.2.
	resp, body := fetch(t, srv.URL+"/badge/badge-hero.no.svg")
	if resp.StatusCode != 200 ||
		!strings.HasPrefix(resp.Header.Get("Content-Type"), "image/svg+xml") ||
		resp.Header.Get("Cache-Control") != "public, max-age=86400" ||
		resp.Header.Get("ETag") == "" {
		t.Fatalf("hero badge: %d %s cache=%q etag=%q", resp.StatusCode,
			resp.Header.Get("Content-Type"), resp.Header.Get("Cache-Control"), resp.Header.Get("ETag"))
	}
	if !strings.Contains(string(body), ">full</text>") {
		t.Errorf("hero+saint badge must render the full variant")
	}
	_, sinner := fetch(t, srv.URL+"/badge/badge-sinner.no.svg")
	if !strings.Contains(string(sinner), ">no IPv6</text>") {
		t.Errorf("sinner badge must say 'no IPv6', never ladder branding")
	}

	// Valid-but-unknown host → 200 gray unknown (never a broken image);
	// disabled renders the same bytes.
	respU, unknown := fetch(t, srv.URL+"/badge/never-crawled.no.svg")
	if respU.StatusCode != 200 || !strings.Contains(string(unknown), ">unknown</text>") {
		t.Fatalf("unknown badge: %d", respU.StatusCode)
	}
	_, disabled := fetch(t, srv.URL+"/badge/badge-disabled.no.svg")
	if string(unknown) != string(disabled) {
		t.Error("no-row and disabled must render byte-identical unknown badges")
	}

	// Zero side effects: the unknown lookup inserted no row.
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM domain WHERE host = 'never-crawled.no'").Scan(&n); err != nil || n != 0 {
		t.Errorf("badge lookup must never insert a domain row (n=%d err=%v)", n, err)
	}

	// Invalid host / reserved TLD → 400 problem+json (the declared
	// exception); suffix-less → route-miss 404.
	var problem struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	if resp := getJSON(t, srv.URL+"/badge/not_a_host!.svg", &problem); resp.StatusCode != 400 ||
		problem.Type != "https://whynoipv6.com/problems/invalid-parameter" {
		t.Errorf("invalid badge host: %d %+v", resp.StatusCode, problem)
	}
	if resp := getJSON(t, srv.URL+"/badge/foo.test.svg", &problem); resp.StatusCode != 400 {
		t.Errorf("reserved TLD: %d", resp.StatusCode)
	}
	if resp := getJSON(t, srv.URL+"/badge/badge-hero.no", &problem); resp.StatusCode != 404 {
		t.Errorf("suffix-less badge path: %d, want route-miss 404", resp.StatusCode)
	}

	// The shields endpoint-JSON variant (camelCase, isError semantics).
	var shields struct {
		SchemaVersion int    `json:"schemaVersion"`
		Label         string `json:"label"`
		Message       string `json:"message"`
		Color         string `json:"color"`
		CacheSeconds  int    `json:"cacheSeconds"`
		IsError       bool   `json:"isError"`
	}
	getJSON(t, srv.URL+"/badge/badge-sinner.no.json", &shields)
	if shields.SchemaVersion != 1 || shields.Label != "IPv6" || shields.Message != "no IPv6" ||
		shields.Color != "red" || shields.CacheSeconds != 86400 || shields.IsError {
		t.Errorf("shields json = %+v", shields)
	}
	getJSON(t, srv.URL+"/badge/never-crawled.no.json", &shields)
	if shields.Message != "unknown" || !shields.IsError {
		t.Errorf("unknown shields json = %+v", shields)
	}
}
