//go:build integration

package export

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/parquet-go/parquet-go"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

func seedExport(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		// 5 ranked apexes (one above 100k), one disabled, one rank-NULL.
		`INSERT INTO domain (host, kind, rank, created_by, asn_id, country_id, tld,
		                     classification, saint, base_status, base_since, last_checked_at)
		 SELECT 'e' || g || '.example', 'apex', CASE WHEN g = 5 THEN 200000 ELSE g END, 'tranco',
		        (SELECT id FROM asn WHERE number = 0), (SELECT id FROM country WHERE code = 'UN'),
		        'example', 'hero', g = 1, 'supported', now() - interval '10 days', now()
		 FROM generate_series(1, 5) g`,
		`UPDATE domain SET disabled = true, disabled_reason = 'manual' WHERE host = 'e4.example'`,
		`INSERT INTO domain (host, kind, created_by, asn_id, country_id, tld, classification, class_flags)
		 VALUES ('camp.example', 'apex', 'campaign', (SELECT id FROM asn WHERE number = 0),
		         (SELECT id FROM country WHERE code = 'UN'), 'example', 'partial', '{www_missing,mail_missing}')`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// TestDatasetExport (P6.3): tiers × formats, datapackage hashes verify,
// SHA256SUMS verifies, manifest conforms, retention prunes, latest symlink.
func TestDatasetExport(t *testing.T) {
	pool := pgtest.NewDB(t)
	seedExport(t, pool)
	dir := t.TempDir()

	// A stale snapshot (95 days old, not month-first) and a month-first
	// snapshot (kept forever) to prove retention.
	old := time.Now().UTC().AddDate(0, 0, -95)
	if old.Day() == 1 {
		old = old.AddDate(0, 0, -1)
	}
	monthFirst := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -2, 0)
	for _, d := range []string{old.Format("2006-01-02"), monthFirst.Format("2006-01-02")} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	e := &Exporter{Pool: pool, Dir: dir}
	if err := e.Run(context.Background(), 20260710); err != nil {
		t.Fatal(err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	snap := filepath.Join(dir, today)

	// Tier row counts: top100k/top1m exclude rank>100k, disabled, and
	// rank-NULL; full excludes only disabled.
	// e1–e3 ranked ≤100k; e5 at 200k; e4 disabled; camp rank-NULL.
	wantRows := map[string]int{"top100k": 3, "top1m": 4, "full": 5}
	for tier, want := range wantRows {
		path := filepath.Join(snap, fmt.Sprintf("whynoipv6-%s.csv.gz", tier))
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		gz, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		records, err := csv.NewReader(gz).ReadAll()
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(records)-1 != want {
			t.Errorf("%s rows = %d, want %d", tier, len(records)-1, want)
		}
		if strings.Join(records[0], ",") != strings.Join(columns, ",") {
			t.Errorf("%s header = %v", tier, records[0])
		}
		// Parquet mirror carries the same row count.
		prows, err := parquet.ReadFile[Row](filepath.Join(snap, fmt.Sprintf("whynoipv6-%s.parquet", tier)))
		if err != nil {
			t.Fatalf("%s parquet: %v", tier, err)
		}
		if len(prows) != want {
			t.Errorf("%s parquet rows = %d, want %d", tier, len(prows), want)
		}
	}

	// The full tier carries the rank-NULL campaign row with empty rank and
	// ;-joined flags.
	full, _ := os.Open(filepath.Join(snap, "whynoipv6-full.csv.gz"))
	gz, _ := gzip.NewReader(full)
	records, _ := csv.NewReader(gz).ReadAll()
	full.Close()
	foundCamp := false
	for _, rec := range records[1:] {
		if rec[0] == "camp.example" {
			foundCamp = true
			if rec[1] != "" || rec[5] != "www_missing;mail_missing" {
				t.Errorf("camp row rank=%q flags=%q", rec[1], rec[5])
			}
		}
		if rec[0] == "e4.example" {
			t.Error("disabled row leaked into the full tier")
		}
	}
	if !foundCamp {
		t.Error("rank-NULL campaign row missing from full tier")
	}

	// datapackage.json: per-file bytes + sha256:-prefixed hash verify.
	var dp struct {
		Resources []struct {
			Path   string `json:"path"`
			Bytes  int64  `json:"bytes"`
			Hash   string `json:"hash"`
			Schema struct {
				Fields []struct {
					Name string `json:"name"`
				} `json:"fields"`
			} `json:"schema"`
		} `json:"resources"`
	}
	raw, err := os.ReadFile(filepath.Join(snap, "datapackage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &dp); err != nil {
		t.Fatal(err)
	}
	if len(dp.Resources) != 6 {
		t.Fatalf("datapackage resources = %d, want 6", len(dp.Resources))
	}
	for _, res := range dp.Resources {
		b, err := os.ReadFile(filepath.Join(snap, res.Path))
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(b)) != res.Bytes {
			t.Errorf("%s bytes = %d, datapackage says %d", res.Path, len(b), res.Bytes)
		}
		want := fmt.Sprintf("sha256:%x", sha256.Sum256(b))
		if res.Hash != want {
			t.Errorf("%s hash mismatch (must carry the sha256: prefix)", res.Path)
		}
		if len(res.Schema.Fields) != len(columns) {
			t.Errorf("%s table schema fields = %d", res.Path, len(res.Schema.Fields))
		}
	}

	// SHA256SUMS is sha256sum -c compatible.
	sums, err := os.ReadFile(filepath.Join(snap, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(sums)), "\n") {
		parts := strings.SplitN(line, "  ", 2)
		b, err := os.ReadFile(filepath.Join(snap, parts[1]))
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(b)) != parts[0] {
			t.Errorf("SHA256SUMS mismatch for %s", parts[1])
		}
	}

	// manifest.json conforms to the pinned schema; latest = today.
	var m Manifest
	raw, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != 1 || m.Generation != 20260710 || m.License != "CC-BY-NC-4.0" ||
		m.Latest.Date != today || m.Attribution == "" {
		t.Errorf("manifest = %+v", m)
	}
	if len(m.Snapshots) != 2 { // today + the kept month-first snapshot
		t.Errorf("snapshots = %d, want 2 (stale daily pruned, month-first kept)", len(m.Snapshots))
	}
	if m.Snapshots[0].Date != today || len(m.Snapshots[0].Tiers) != 3 ||
		strings.Join(m.Snapshots[0].Formats, ",") != "csv.gz,parquet" {
		t.Errorf("snapshots[0] = %+v", m.Snapshots[0])
	}

	// Retention: the 95-day-old daily is gone; the month-first survives.
	if _, err := os.Stat(filepath.Join(dir, old.Format("2006-01-02"))); !os.IsNotExist(err) {
		t.Error("stale daily snapshot must be pruned")
	}
	if _, err := os.Stat(filepath.Join(dir, monthFirst.Format("2006-01-02"))); err != nil {
		t.Error("month-first snapshot must be retained")
	}

	// latest symlink points at today.
	target, err := os.Readlink(filepath.Join(dir, "latest"))
	if err != nil || target != today {
		t.Errorf("latest -> %q err=%v", target, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "DICTIONARY.md")); err != nil {
		t.Error("DICTIONARY.md missing")
	}

	// Re-running the same day replaces the snapshot idempotently.
	if err := e.Run(context.Background(), 20260710); err != nil {
		t.Fatalf("same-day re-export: %v", err)
	}
}
