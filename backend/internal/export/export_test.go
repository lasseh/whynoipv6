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
	rows       []Row
	listID     string
	err        error // yielded as the sequence's final element
	calls      int
	generation int32 // 0 -> fakeGeneration
	genAtCalls int   // tier walks completed when Generation was read
}

// fakeGeneration is what a fake stamps unless a test sets its own.
const fakeGeneration = 42

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

func (s *fakeSource) ListID(context.Context) (string, error) { return s.listID, nil }

func (s *fakeSource) Generation(context.Context) (int32, error) {
	s.genAtCalls = s.calls
	if s.generation == 0 {
		return fakeGeneration, nil
	}
	return s.generation, nil
}

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
	if _, err := e.Run(context.Background()); err != nil {
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
	if _, err := e.Run(context.Background()); err == nil {
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
	if _, err := e.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, stale)); !os.IsNotExist(err) {
		t.Errorf("stale daily %s survived retention", stale)
	}
	if _, err := os.Stat(filepath.Join(dir, monthFirst)); err != nil {
		t.Errorf("first-of-month %s was pruned: %v", monthFirst, err)
	}
}

// snapshotName is the one definition of a snapshot directory; publication,
// retention and the manifest all read it.
func TestSnapshotName(t *testing.T) {
	cases := []struct {
		name string
		date string
		rev  int
		ok   bool
	}{
		{"2026-08-20", "2026-08-20", 1, true},
		{"2026-08-20r2", "2026-08-20", 2, true},
		{"2026-08-20r13", "2026-08-20", 13, true},
		{"2026-08-20r1", "", 0, false},  // revision 1 is spelled as the bare date
		{"2026-08-20r0", "", 0, false},  // ditto
		{"2026-08-20r", "", 0, false},   // no number
		{"2026-08-20rx", "", 0, false},  // not a number
		{"latest", "", 0, false},        // the symlink
		{".export-123", "", 0, false},   // staging
		{"manifest.json", "", 0, false}, // a file
		{"2026-13-45", "", 0, false},    // not a date
	}
	for _, tc := range cases {
		d, rev, ok := snapshotName(tc.name)
		if ok != tc.ok {
			t.Errorf("snapshotName(%q) ok = %v, want %v", tc.name, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if got := d.Format("2006-01-02"); got != tc.date || rev != tc.rev {
			t.Errorf("snapshotName(%q) = (%s, %d), want (%s, %d)", tc.name, got, rev, tc.date, tc.rev)
		}
	}
}

// The defect this replaces: publication used to RemoveAll the dated path
// before renaming onto it, so a same-day re-export briefly 404'd a URL
// served immutable for a year. A published snapshot must now keep its
// bytes, and the re-export must land beside it.
func TestReExportNeverMutatesAPublishedSnapshot(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)

	runExport(t, &fakeSource{rows: rankedRows(3), listID: "FIRST"}, dir, now)
	first := filepath.Join(dir, "2026-08-20")
	before, err := os.ReadFile(filepath.Join(first, "SHA256SUMS"))
	if err != nil {
		t.Fatalf("read first SHA256SUMS: %v", err)
	}

	// A re-export on the same day, with different data.
	runExport(t, &fakeSource{rows: rankedRows(9), listID: "SECOND"}, dir, now)

	after, err := os.ReadFile(filepath.Join(first, "SHA256SUMS"))
	if err != nil {
		t.Fatalf("the first snapshot disappeared: %v", err)
	}
	if string(before) != string(after) {
		t.Error("the first snapshot's bytes changed — an immutable URL was rewritten")
	}

	second := filepath.Join(dir, "2026-08-20r2")
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("re-export did not publish a fresh path: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(second, "SHA256SUMS")); err != nil {
		t.Fatalf("read second SHA256SUMS: %v", err)
	} else if string(b) == string(before) {
		t.Error("the re-export published identical digests despite different data")
	}

	// A third lands on r3.
	runExport(t, &fakeSource{rows: rankedRows(4)}, dir, now)
	if _, err := os.Stat(filepath.Join(dir, "2026-08-20r3")); err != nil {
		t.Errorf("third export did not land on r3: %v", err)
	}
}

// latest follows the newest revision, and the manifest indexes every
// revision with the newest first.
func TestReExportRepointsLatestAndIndexesRevisions(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)
	runExport(t, &fakeSource{rows: rankedRows(2)}, dir, now)
	runExport(t, &fakeSource{rows: rankedRows(2)}, dir, now)

	target, err := os.Readlink(filepath.Join(dir, "latest"))
	if err != nil {
		t.Fatalf("readlink latest: %v", err)
	}
	if target != "2026-08-20r2" {
		t.Errorf("latest -> %q, want 2026-08-20r2", target)
	}

	m := readJSON[Manifest](t, filepath.Join(dir, "manifest.json"))
	if len(m.Snapshots) != 2 {
		t.Fatalf("manifest indexed %d snapshots, want 2", len(m.Snapshots))
	}
	if m.Snapshots[0].Path != "datasets/2026-08-20r2/" {
		t.Errorf("newest snapshot path = %q", m.Snapshots[0].Path)
	}
	if m.Snapshots[1].Path != "datasets/2026-08-20/" {
		t.Errorf("older snapshot path = %q", m.Snapshots[1].Path)
	}
	// The revision rides the path; date stays the calendar day so consumers
	// can group re-exports without parsing directory names.
	for _, s := range m.Snapshots {
		if s.Date != "2026-08-20" {
			t.Errorf("snapshot date = %q, want the calendar day", s.Date)
		}
	}
	if m.Latest.Path != "datasets/2026-08-20r2/" {
		t.Errorf("manifest latest = %q", m.Latest.Path)
	}
}

// Ordering across dates as well as revisions.
func TestManifestOrdersDatesThenRevisions(t *testing.T) {
	dir := t.TempDir()
	runExport(t, &fakeSource{rows: rankedRows(1)}, dir, time.Date(2026, 8, 19, 3, 0, 0, 0, time.UTC))
	runExport(t, &fakeSource{rows: rankedRows(1)}, dir, time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC))
	runExport(t, &fakeSource{rows: rankedRows(1)}, dir, time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC))

	m := readJSON[Manifest](t, filepath.Join(dir, "manifest.json"))
	want := []string{"datasets/2026-08-20r2/", "datasets/2026-08-20/", "datasets/2026-08-19/"}
	if len(m.Snapshots) != len(want) {
		t.Fatalf("indexed %d snapshots, want %d", len(m.Snapshots), len(want))
	}
	for i, w := range want {
		if m.Snapshots[i].Path != w {
			t.Errorf("snapshot %d = %q, want %q", i, m.Snapshots[i].Path, w)
		}
	}
}

// Retention is per calendar date: every revision of a pruned day goes, and
// every revision of a kept day stays.
func TestPruneKeepsAllRevisionsOfAKeptDate(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)

	stale := now.AddDate(0, 0, -95)
	if stale.Day() == 1 {
		stale = stale.AddDate(0, 0, -1)
	}
	staleDate := stale.Format("2006-01-02")
	for _, d := range []string{staleDate, staleDate + "r2", "2026-08-19", "2026-08-19r2"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	e := &Exporter{Dir: dir, Source: &fakeSource{rows: rankedRows(1)}, RetentionDays: 90,
		Now: func() time.Time { return now }}
	if _, err := e.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, gone := range []string{staleDate, staleDate + "r2"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s survived retention", gone)
		}
	}
	for _, kept := range []string{"2026-08-19", "2026-08-19r2"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s was pruned but its date is within retention", kept)
		}
	}
}

// The revision ceiling fails loudly rather than filling the volume.
func TestPublishCeiling(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)
	for rev := 1; rev <= maxSnapshotRevisions; rev++ {
		if err := os.MkdirAll(filepath.Join(dir, snapshotDirName("2026-08-20", rev)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e := &Exporter{Dir: dir, Source: &fakeSource{rows: rankedRows(1)},
		Now: func() time.Time { return now }}
	_, err := e.Run(context.Background())
	if err == nil {
		t.Fatal("Run succeeded past the revision ceiling")
	}
	if !strings.Contains(err.Error(), "revisions already exist") {
		t.Errorf("error = %v", err)
	}
}

// TestRunReadsGenerationAfterTheWalk pins the manifest's generation to a read
// that happens after every tier has been walked (issue 43). Reading it first
// let a stats rollup landing mid-export stamp the manifest one day behind the
// rows it describes — a consumer holding that generation would then skip the
// download.
func TestRunReadsGenerationAfterTheWalk(t *testing.T) {
	src := &fakeSource{rows: rankedRows(2), generation: 20260710}
	e := &Exporter{Dir: t.TempDir(), Source: src, RetentionDays: 90,
		Now: func() time.Time { return time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC) }}
	generation, err := e.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generation != 20260710 {
		t.Errorf("Run returned generation %d, want 20260710", generation)
	}
	if src.genAtCalls != len(tiers) {
		t.Errorf("generation read after %d of %d tier walks, want all of them",
			src.genAtCalls, len(tiers))
	}
}
