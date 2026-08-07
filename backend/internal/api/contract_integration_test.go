//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/lasseh/whynoipv6/internal/api"
	"github.com/lasseh/whynoipv6/internal/api/gen"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestContractResponses is the contract↔wire half of the drift gate
// (TestOpenAPIRouteCoverage is the contract↔code half): one real response
// per documented operation, validated against the embedded spec. A
// hand-written wire struct drifting from openapi.yaml — an extra key, a
// missing key, a renamed field, a wrong type — fails HERE, not in the
// frontend's generated types in another build.
func TestContractResponses(t *testing.T) {
	pool := pgtest.NewDB(t)
	seedLeaderboard(t, pool)
	seedEntities(t, pool)
	seedChangelog(t, pool)

	ctx := context.Background()
	// Top-up: the series tables, a done check job, and a stored scan_detail
	// so every stats/check operation has a 200 to record.
	for _, s := range []string{
		`INSERT INTO stats_global_daily (day, domains, heroes, sinners, saints, mx_supported)
		 SELECT current_date - g, 1000 + g, 100 + g, 200, 10, 300
		 FROM generate_series(1, 3) g`,
		`INSERT INTO stats_country_daily (day, country_id, domains, heroes, base_supported)
		 SELECT current_date - g, (SELECT id FROM country WHERE code = 'NO'), 50 + g, 5, 20
		 FROM generate_series(1, 2) g`,
		`INSERT INTO stats_asn_daily (day, asn_id, domains, v6_domains)
		 SELECT (current_date - g)::timestamptz, (SELECT id FROM asn WHERE number = 2119), 3, 2
		 FROM generate_series(1, 2) g`,
	} {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("contract seed: %v\n%s", err, s)
		}
	}
	var jobID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO check_job (host, requester_ip, status, result, completed_at, created_at)
		 VALUES ('jobonly.no', '192.0.2.1', 'done', '{"checks":{}}', now() - interval '3 days', now() - interval '3 days')
		 RETURNING id`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	manifest := `{
	  "schema_version": 1, "generated_at": "2026-07-07T04:10:00Z", "generation": 20260707,
	  "license": "CC-BY-NC-4.0", "attribution": "Data: whynoipv6.com (CC-BY-NC-4.0). Ranks: Tranco list.",
	  "latest": {"date": "2026-07-07", "path": "datasets/2026-07-07/", "datapackage_url": "/datasets/2026-07-07/datapackage.json"},
	  "snapshots": [{"date": "2026-07-07", "path": "datasets/2026-07-07/", "tiers": ["top100k","top1m","full"],
	    "formats": ["csv.gz","parquet"], "datapackage_url": "/datasets/2026-07-07/datapackage.json",
	    "sha256sums_url": "/datasets/2026-07-07/SHA256SUMS"}]
	}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := newServer(t, pool, api.Options{DatasetsDir: dir})

	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec.Servers = openapi3.Servers{&openapi3.Server{URL: srv.URL}}
	specRouter, err := gorillamux.NewRouter(spec)
	if err != nil {
		t.Fatal(err)
	}

	// One probe per documented operation, keyed "METHOD /spec/path". The
	// completeness loop below fails when an operation lands in openapi.yaml
	// without a probe — recorded coverage is the gate, not a sample.
	// JSON Feed is JSON under another media-type name — teach the validator
	// to decode it so the feed schema is actually enforced.
	openapi3filter.RegisterBodyDecoder("application/feed+json",
		func(body io.Reader, _ http.Header, _ *openapi3.SchemaRef, _ openapi3filter.EncodingFn) (any, error) {
			var v any
			if err := json.NewDecoder(body).Decode(&v); err != nil {
				return nil, err
			}
			return v, nil
		})

	type probe struct {
		url      string // concrete path (+query) against the seed
		method   string // "" = GET
		body     string // JSON request body (POST)
		status   int    // expected; 0 = 200
		skipBody bool   // XML bodies: validate status+headers only
	}
	probes := map[string]probe{
		"GET /domains":                    {url: "/domains"},
		"GET /domains/{host}":             {url: "/domains/d1.example"},
		"GET /domains/{host}/subdomains":  {url: "/domains/d1.example/subdomains"},
		"GET /domains/{host}/resources":   {url: "/domains/d1.example/resources"},
		"GET /domains/{host}/changelog":   {url: "/domains/d3.example/changelog"},
		"GET /domains/{host}/history":     {url: "/domains/d3.example/history"},
		"GET /heroes":                     {url: "/heroes"},
		"GET /sinners":                    {url: "/sinners"},
		"GET /saints":                     {url: "/saints"},
		"GET /almost-heroes":              {url: "/almost-heroes"},
		"GET /shame":                      {url: "/shame"},
		"GET /countries":                  {url: "/countries"},
		"GET /countries/{code}":           {url: "/countries/NO"},
		"GET /countries/{code}/domains":   {url: "/countries/NO/domains"},
		"GET /countries/{code}/stats":     {url: "/countries/NO/stats"},
		"GET /countries/{code}/changelog": {url: "/countries/NO/changelog"},
		"GET /asns":                       {url: "/asns"},
		"GET /asns/{number}":              {url: "/asns/2119"},
		"GET /asns/{number}/domains":      {url: "/asns/2119/domains"},
		"GET /asns/{number}/stats":        {url: "/asns/2119/stats"},
		"GET /providers":                  {url: "/providers"},
		"GET /providers/{id}":             {url: "/providers/1"},
		"GET /providers/{id}/domains":     {url: "/providers/1/domains"},
		"GET /campaigns":                  {url: "/campaigns"},
		"GET /mandates":                   {url: "/mandates"},
		"GET /campaigns/{uuid}":           {url: "/campaigns/" + campaignUUID},
		"GET /campaigns/{uuid}/domains":   {url: "/campaigns/" + campaignUUID + "/domains"},
		"GET /campaigns/{uuid}/stats":     {url: "/campaigns/" + campaignUUID + "/stats"},
		"GET /campaigns/{uuid}/changelog": {url: "/campaigns/" + campaignUUID + "/changelog"},
		"GET /campaigns/{uuid}/domains/{host}/changelog": {
			url: "/campaigns/" + campaignUUID + "/domains/d3.example/changelog"},
		"GET /resources/{host}":            {url: "/resources/fonts.example"},
		"GET /resources/{host}/dependents": {url: "/resources/fonts.example/dependents"},
		"GET /changelog":                   {url: "/changelog"},
		"GET /changelog.atom":              {url: "/changelog.atom", skipBody: true},
		"GET /changelog.feed.json":         {url: "/changelog.feed.json"},
		"GET /domains/{host}/changelog.atom":      {url: "/domains/d3.example/changelog.atom", skipBody: true},
		"GET /domains/{host}/changelog.feed.json": {url: "/domains/d3.example/changelog.feed.json"},
		"GET /countries/{code}/changelog.atom":      {url: "/countries/NO/changelog.atom", skipBody: true},
		"GET /countries/{code}/changelog.feed.json": {url: "/countries/NO/changelog.feed.json"},
		"GET /campaigns/{uuid}/changelog.atom":      {url: "/campaigns/" + campaignUUID + "/changelog.atom", skipBody: true},
		"GET /campaigns/{uuid}/changelog.feed.json": {url: "/campaigns/" + campaignUUID + "/changelog.feed.json"},
		// The badge host must carry a real TLD (.example is the reserved-TLD
		// 400); an untracked host renders the "unknown" badge, still in-spec.
		"GET /badge/{host}.svg":  {url: "/badge/jobonly.no.svg", skipBody: true},
		"GET /badge/{host}.json": {url: "/badge/jobonly.no.json"},
		"GET /datasets":          {url: "/datasets"},
		"GET /stats/overview":    {url: "/stats/overview"},
		"GET /stats/crawler":     {url: "/stats/crawler"},
		"GET /stats/networks":    {url: "/stats/networks"},
		"GET /stats/changes":     {url: "/stats/changes"},
		"GET /ip":                {url: "/ip"},
		"POST /check":            {url: "/check", method: http.MethodPost, body: `{"host":"fresh.no"}`, status: http.StatusAccepted},
		"GET /check/{id}":        {url: "/check/" + strconv.FormatInt(jobID, 10)},
		"GET /check/latest":      {url: "/check/latest?host=jobonly.no"},
	}

	for path, item := range spec.Paths.Map() {
		for method := range item.Operations() {
			key := method + " " + path
			if _, ok := probes[key]; !ok {
				t.Errorf("no contract probe for documented operation %q — add one to TestContractResponses", key)
			}
		}
	}

	for key, p := range probes {
		t.Run(key, func(t *testing.T) {
			method := p.method
			if method == "" {
				method = http.MethodGet
			}
			wantStatus := p.status
			if wantStatus == 0 {
				wantStatus = http.StatusOK
			}
			var bodyReader io.Reader
			if p.body != "" {
				bodyReader = strings.NewReader(p.body)
			}
			req, err := http.NewRequest(method, srv.URL+p.url, bodyReader)
			if err != nil {
				t.Fatal(err)
			}
			if p.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != wantStatus {
				t.Fatalf("status = %d, want %d\n%s", resp.StatusCode, wantStatus, raw)
			}

			route, pathParams, err := specRouter.FindRoute(req)
			if err != nil {
				t.Fatalf("request does not match the spec's routes: %v", err)
			}
			input := &openapi3filter.ResponseValidationInput{
				RequestValidationInput: &openapi3filter.RequestValidationInput{
					Request: req, PathParams: pathParams, Route: route,
				},
				Status: resp.StatusCode,
				Header: resp.Header,
				Options: &openapi3filter.Options{
					IncludeResponseStatus: true,
					ExcludeResponseBody:   p.skipBody,
				},
			}
			input.SetBodyBytes(raw)
			if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
				t.Errorf("response does not satisfy openapi.yaml:\n%v", err)
			}
		})
	}
}
