//go:build integration

package api_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lasseh/whynoipv6/internal/api"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestDatasetsEndpoint (P6.3 / 07 §5.3): GET /datasets serves the on-disk
// manifest re-read per request; missing/unparseable → 503
// manifest-unavailable, the API's only 503.
func TestDatasetsEndpoint(t *testing.T) {
	pool := pgtest.NewDB(t)
	dir := t.TempDir()
	srv := newServer(t, pool, api.Options{DatasetsDir: dir})

	var problem struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	resp := getJSON(t, srv.URL+"/datasets", &problem)
	if resp.StatusCode != 503 || problem.Type != "https://whynoipv6.com/problems/manifest-unavailable" {
		t.Fatalf("missing manifest: %d %+v", resp.StatusCode, problem)
	}

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

	var m struct {
		SchemaVersion int    `json:"schema_version"`
		Generation    int32  `json:"generation"`
		License       string `json:"license"`
		Latest        struct {
			Date string `json:"date"`
		} `json:"latest"`
		Snapshots []struct {
			Tiers []string `json:"tiers"`
		} `json:"snapshots"`
	}
	resp = getJSON(t, srv.URL+"/datasets", &m)
	if resp.StatusCode != 200 || m.SchemaVersion != 1 || m.Generation != 20260707 ||
		m.Latest.Date != "2026-07-07" || len(m.Snapshots) != 1 || len(m.Snapshots[0].Tiers) != 3 {
		t.Errorf("manifest response: %d %+v", resp.StatusCode, m)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=300" {
		t.Errorf("cache-control = %q", cc)
	}

	// Unparseable manifest → 503, not a 500 or a partial body.
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp = getJSON(t, srv.URL+"/datasets", &problem)
	if resp.StatusCode != 503 {
		t.Errorf("unparseable manifest: %d", resp.StatusCode)
	}
}
