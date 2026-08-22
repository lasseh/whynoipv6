package export

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

// fakeSource is the test adapter at the Source seam: row volume and the
// Tranco list ID become inputs, so the publication protocol runs with no
// container. The integration test still covers the real SQL.
type fakeSource struct {
	rows   []Row
	listID string
	err    error // yielded as the sequence's final element
	calls  int
}

func (s *fakeSource) Rows(_ context.Context, rankedOnly bool, maxRank int32) iter.Seq2[Row, error] {
	return func(yield func(Row, error) bool) {
		s.calls++
		if s.err != nil {
			yield(Row{}, s.err)
			return
		}
		for _, r := range s.rows {
			// Mirror the tier predicates so per-tier row counts differ the
			// way production's do.
			if rankedOnly && r.Rank == nil {
				continue
			}
			if maxRank > 0 && r.Rank != nil && *r.Rank > int64(maxRank) {
				continue
			}
			if !yield(r, nil) {
				return
			}
		}
	}
}

func (s *fakeSource) ListID(context.Context) string { return s.listID }

// rankedRows builds n ranked apex rows, ranks 1..n.
func rankedRows(n int) []Row {
	rows := make([]Row, n)
	for i := range rows {
		rank := int64(i + 1)
		rows[i] = Row{
			Host: fmt.Sprintf("h%06d.example", i), Rank: &rank, Kind: "apex",
			Classification: "hero", Country: "UN", ASN: 0,
		}
	}
	return rows
}

func runExport(t *testing.T, src Source, dir string, now time.Time) {
	t.Helper()
	e := &Exporter{Dir: dir, Source: src, Now: func() time.Time { return now }}
	if err := e.Run(context.Background(), 42); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func readJSON[T any](t *testing.T, path string) T {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return v
}

// The whole publication protocol runs off a slice — the twelve filesystem
// functions no longer need a database to reach.
func TestRunPublishesSnapshotWithoutDatabase(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)
	src := &fakeSource{rows: rankedRows(5), listID: "K2XYZ"}
	runExport(t, src, dir, now)

	snap := filepath.Join(dir, "2026-08-20")
	for _, tier := range []string{"top100k", "top1m", "full"} {
		for _, ext := range []string{formatCSVGz, formatParquet} {
			name := fmt.Sprintf("whynoipv6-%s.%s", tier, ext)
			if _, err := os.Stat(filepath.Join(snap, name)); err != nil {
				t.Errorf("missing %s: %v", name, err)
			}
		}
	}
	for _, name := range []string{"SHA256SUMS", "datapackage.json"} {
		if _, err := os.Stat(filepath.Join(snap, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "DICTIONARY.md")); err != nil {
		t.Errorf("missing DICTIONARY.md: %v", err)
	}

	// nginx serves this as a foreign user: the published dir must stay
	// world-traversable, not MkdirTemp's 0700.
	fi, err := os.Stat(snap)
	if err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o755 {
		t.Errorf("published mode = %o, want 755", perm)
	}

	// One Rows call per tier, and no leftover staging directories.
	if src.calls != len(tiers) {
		t.Errorf("Rows called %d times, want %d", src.calls, len(tiers))
	}
	ents, _ := os.ReadDir(dir)
	for _, ent := range ents {
		if strings.HasPrefix(ent.Name(), ".export-") {
			t.Errorf("staging dir survived: %s", ent.Name())
		}
	}
}

// parquetChunk is 1000 and the integration seed is six rows, so the
// mid-stream flush had never executed. A 1001-row fake reaches it, and the
// parquet file must still carry every row exactly once.
func TestRunFlushesParquetMidStream(t *testing.T) {
	dir := t.TempDir()
	const n = parquetChunk + 1
	runExport(t, &fakeSource{rows: rankedRows(n)}, dir, time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC))

	path := filepath.Join(dir, "2026-08-20", "whynoipv6-full."+formatParquet)
	rows, err := parquet.ReadFile[Row](path)
	if err != nil {
		t.Fatalf("read parquet: %v", err)
	}
	if len(rows) != n {
		t.Fatalf("parquet carried %d rows, want %d — the mid-stream flush dropped or duplicated a chunk", len(rows), n)
	}
	// Boundary rows: the last of the first chunk and the first of the second.
	if rows[parquetChunk-1].Host != fmt.Sprintf("h%06d.example", parquetChunk-1) {
		t.Errorf("row at the chunk boundary = %q", rows[parquetChunk-1].Host)
	}
	if rows[parquetChunk].Host != fmt.Sprintf("h%06d.example", parquetChunk) {
		t.Errorf("first row after the flush = %q", rows[parquetChunk].Host)
	}
}

// The list ID rides all three attribution surfaces (07 §5.3). Their
// populated form had never been exercised together.
func TestRunListIDReachesAllAttributionSurfaces(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)
	runExport(t, &fakeSource{rows: rankedRows(2), listID: "K2XYZ"}, dir, now)

	dp := readJSON[datapackage](t, filepath.Join(dir, "2026-08-20", "datapackage.json"))
	var citedTranco bool
	for _, s := range dp.Sources {
		if strings.Contains(s.Title, "K2XYZ") {
			citedTranco = true
		}
	}
	if !citedTranco {
		t.Errorf("datapackage sources do not cite the list ID: %+v", dp.Sources)
	}

	dict, err := os.ReadFile(filepath.Join(dir, "DICTIONARY.md"))
	if err != nil {
		t.Fatalf("read dictionary: %v", err)
	}
	if !strings.Contains(string(dict), "K2XYZ") {
		t.Error("DICTIONARY.md does not cite the list ID")
	}

	m := readJSON[Manifest](t, filepath.Join(dir, "manifest.json"))
	if !strings.Contains(m.Attribution, "K2XYZ") {
		t.Errorf("manifest attribution = %q, want the list ID", m.Attribution)
	}
	if m.Generation != 42 {
		t.Errorf("manifest generation = %d, want 42", m.Generation)
	}
}

// No successful import yet degrades attribution to the generic string
// rather than failing the export.
func TestRunWithoutListIDDegrades(t *testing.T) {
	dir := t.TempDir()
	runExport(t, &fakeSource{rows: rankedRows(2)}, dir, time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC))

	m := readJSON[Manifest](t, filepath.Join(dir, "manifest.json"))
	if m.Attribution != "Data: whynoipv6.com (CC-BY-NC-4.0). Ranks: Tranco list." {
		t.Errorf("attribution = %q", m.Attribution)
	}
}

// A failing row read aborts the export and publishes nothing.
func TestRunSourceErrorPublishesNothing(t *testing.T) {
	dir := t.TempDir()
	e := &Exporter{
		Dir:    dir,
		Source: &fakeSource{err: errors.New("relation domain does not exist")},
		Now:    func() time.Time { return time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC) },
	}
	if err := e.Run(context.Background(), 1); err == nil {
		t.Fatal("Run succeeded despite the row read failing")
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-08-20")); !os.IsNotExist(err) {
		t.Error("a snapshot was published despite the failure")
	}
	ents, _ := os.ReadDir(dir)
	for _, ent := range ents {
		if strings.HasPrefix(ent.Name(), ".export-") {
			t.Errorf("staging dir survived the failure: %s", ent.Name())
		}
	}
}

// Retention: dailies age out, first-of-month snapshots are kept forever.
func TestRunPrunesRetention(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)

	stale := now.AddDate(0, 0, -95).Format("2006-01-02")
	if strings.HasSuffix(stale, "-01") {
		stale = now.AddDate(0, 0, -96).Format("2006-01-02")
	}
	monthFirst := "2026-01-01"
	for _, d := range []string{stale, monthFirst} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	e := &Exporter{Dir: dir, Source: &fakeSource{rows: rankedRows(1)}, RetentionDays: 90,
		Now: func() time.Time { return now }}
	if err := e.Run(context.Background(), 1); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, stale)); !os.IsNotExist(err) {
		t.Errorf("stale daily %s survived retention", stale)
	}
	if _, err := os.Stat(filepath.Join(dir, monthFirst)); err != nil {
		t.Errorf("first-of-month %s was pruned: %v", monthFirst, err)
	}
}
