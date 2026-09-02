// Package export produces the nightly static dataset snapshots (07-api.md
// §5.3): 3 size tiers × CSV.gz + Parquet under $DATASETS_DIR, each snapshot
// self-describing (datapackage.json, SHA256SUMS, DICTIONARY.md), indexed by
// the atomically-rewritten top-level manifest.json. Bulk data is a static
// channel — never the paginated API.
package export

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/parquet-go/parquet-go"

	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// SchemaVersion versions the manifest index shape AND the exported column
// set; bump when either changes (07 §5.3).
const SchemaVersion = 1

// maxSnapshotRevisions bounds same-day re-exports. Publication never
// overwrites, so a wedged loop re-running the export would otherwise fill
// the volume one snapshot at a time; failing loudly at the ceiling is the
// cheaper failure.
const maxSnapshotRevisions = 24

const license = "CC-BY-NC-4.0"

// Row is the exported record — one column set across every tier/format.
type Row struct {
	Host            string  `parquet:"host" json:"host"`
	Rank            *int64  `parquet:"rank,optional" json:"rank"`
	Kind            string  `parquet:"kind" json:"kind"`
	Parent          *string `parquet:"parent,optional" json:"parent"`
	Classification  string  `parquet:"classification" json:"classification"`
	ClassFlags      string  `parquet:"class_flags" json:"class_flags"` // ;-joined
	Saint           bool    `parquet:"saint" json:"saint"`
	Base            *string `parquet:"base,optional" json:"base"`
	WWW             *string `parquet:"www,optional" json:"www"`
	NS              *string `parquet:"ns,optional" json:"ns"`
	MX              *string `parquet:"mx,optional" json:"mx"`
	Conn            *string `parquet:"conn,optional" json:"conn"`
	Resources       *string `parquet:"resources,optional" json:"resources"`
	BaseSince       *string `parquet:"base_since,optional" json:"base_since"`
	WWWSince        *string `parquet:"www_since,optional" json:"www_since"`
	NSSince         *string `parquet:"ns_since,optional" json:"ns_since"`
	MXSince         *string `parquet:"mx_since,optional" json:"mx_since"`
	ConnSince       *string `parquet:"conn_since,optional" json:"conn_since"`
	ResourcesSince  *string `parquet:"resources_since,optional" json:"resources_since"`
	TLD             *string `parquet:"tld,optional" json:"tld"`
	Country         string  `parquet:"country" json:"country"`
	ASN             int64   `parquet:"asn" json:"asn"`
	DNSProvider     *string `parquet:"dns_provider,optional" json:"dns_provider"`
	HostingProvider *string `parquet:"hosting_provider,optional" json:"hosting_provider"`
	LastChecked     *string `parquet:"last_checked,optional" json:"last_checked"`
}

// columns is the CSV header and the Table Schema field order — derived from
// Row's parquet tags, so the struct is the single declaration of the
// exported column set and the header cannot drift from the fields.
var columns = rowColumns()

func rowColumns() []string {
	t := reflect.TypeOf(Row{})
	cols := make([]string, t.NumField())
	for i := range cols {
		cols[i] = strings.SplitN(t.Field(i).Tag.Get("parquet"), ",", 2)[0]
	}
	return cols
}

// Frictionless Table Schema types + the two published formats.
const (
	typeDatetime  = "datetime"
	formatCSVGz   = "csv.gz"
	formatParquet = "parquet"
)

// columnTypes maps column → Frictionless Table Schema type.
//
//nolint:goconst // a literal data table; repeated column names are values
var columnTypes = map[string]string{
	"rank": "integer", "saint": "boolean", "asn": "integer",
	"base_since": typeDatetime, "www_since": typeDatetime, "ns_since": typeDatetime,
	"mx_since": typeDatetime, "conn_since": typeDatetime, "resources_since": typeDatetime,
	"last_checked": typeDatetime,
}

// tiers: parameters per size tier for the one sqlc ExportRows query.
// top100k/top1m use the publicly-ranked predicate; full = every
// non-disabled scannable entity.
var tiers = []struct {
	Name       string
	RankedOnly bool
	MaxRank    int32 // 0 = unbounded
}{
	{"top100k", true, 100000},
	{"top1m", true, 0},
	{"full", false, 0},
}

// Exporter runs one snapshot export.
type Exporter struct {
	Pool *pgxpool.Pool
	Dir  string // $DATASETS_DIR

	// Source overrides the row seam; nil adapts Pool (see source.go).
	// Production sets only Pool.
	Source Source

	// RetentionDays prunes dailies older than this (datasets.retention_days;
	// ≤0 falls back to the spec default 90). First-of-month snapshots are
	// kept forever regardless.
	RetentionDays int

	// Now is injectable for tests; defaults to time.Now.
	Now func() time.Time
}

// Run produces today's snapshot, publishes it with a single rename(2) onto
// a path that did not exist, rewrites manifest.json + the latest symlink,
// and prunes per retention (dailies 90 d, first-of-month forever).
//
// Publication never unlinks. rename(2) onto a non-empty directory fails
// EEXIST, so replacing a snapshot in place would mean removing it first,
// which opens a window where a dated URL 404s — on paths nginx serves
// `immutable, max-age=31536000`. Instead a same-day re-export publishes the
// next revision (2026-08-21, then 2026-08-21r2 …) and repoints latest, so
// every published URL keeps serving the bytes it was first published with.
// The stats generation is read after the walk and returned: it is the value
// the manifest carries.
func (e *Exporter) Run(ctx context.Context) (int32, error) {
	now := time.Now().UTC()
	if e.Now != nil {
		now = e.Now().UTC()
	}
	date := now.Format("2006-01-02")

	if err := os.MkdirAll(e.Dir, 0o755); err != nil {
		return 0, fmt.Errorf("datasets dir: %w", err)
	}
	// A staging dir left by a hard kill is never a snapshot (prune and the
	// manifest ignore it) and would accumulate forever; this run holds the
	// dataset-export lock, so no other export can own one.
	if stale, _ := filepath.Glob(filepath.Join(e.Dir, ".export-*")); len(stale) > 0 {
		for _, p := range stale {
			slog.Warn("removing orphaned export staging dir", "path", p)
			_ = os.RemoveAll(p)
		}
	}
	tmp, err := os.MkdirTemp(e.Dir, ".export-*")
	if err != nil {
		return 0, fmt.Errorf("export tmp: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	// MkdirTemp creates 0700 and rename(2) carries the mode to the published
	// dated dir — which nginx serves as a foreign user, so it must be
	// world-traversable or every file inside 403s.
	if err := os.Chmod(tmp, 0o755); err != nil {
		return 0, fmt.Errorf("export tmp mode: %w", err)
	}

	var resources []datapackageResource
	var sums []string
	for _, tier := range tiers {
		csvName := fmt.Sprintf("whynoipv6-%s.%s", tier.Name, formatCSVGz)
		parquetName := fmt.Sprintf("whynoipv6-%s.%s", tier.Name, formatParquet)
		if err := e.writeTier(ctx, filepath.Join(tmp, csvName), filepath.Join(tmp, parquetName),
			tier.RankedOnly, tier.MaxRank); err != nil {
			return 0, fmt.Errorf("tier %s: %w", tier.Name, err)
		}
		for _, format := range []string{formatCSVGz, formatParquet} {
			name := csvName
			if format == formatParquet {
				name = parquetName
			}
			path := filepath.Join(tmp, name)
			size, digest, err := fileDigest(path)
			if err != nil {
				return 0, err
			}
			resources = append(resources, datapackageResource{
				Name: strings.TrimSuffix(name, filepath.Ext(name)), Path: name,
				Bytes: size, Hash: "sha256:" + digest, Format: format,
				Schema: tableSchema(),
			})
			sums = append(sums, digest+"  "+name)
		}
	}

	// The Tranco list ID rides in all three attribution surfaces —
	// datapackage sources, DICTIONARY.md, and the manifest (07 §5.3).
	listID, err := e.trancoListID(ctx)
	if err != nil {
		return 0, err
	}
	dp := datapackage{
		Name: "whynoipv6-" + date, Title: "WhyNoIPv6 daily snapshot " + date,
		Licenses:  []dpLicense{{Name: license, Path: "https://creativecommons.org/licenses/by-nc/4.0/"}},
		Created:   now.Format(time.RFC3339),
		Sources:   dpSources(listID),
		Resources: resources,
	}
	if err := writeJSONFile(filepath.Join(tmp, "datapackage.json"), dp); err != nil {
		return 0, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"), 0o644); err != nil {
		return 0, err
	}

	// Every snapshot carries its own dictionary (the package contract and
	// 07 §5.3's self-describing bullet); the root copy below is the mutable,
	// short-TTL one nginx serves at /datasets/DICTIONARY.md.
	dictionary := []byte(dictionaryText(listID))
	if err := writeFileAtomic(filepath.Join(tmp, "DICTIONARY.md"), dictionary, 0o644); err != nil {
		return 0, err
	}
	if err := syncDir(tmp); err != nil {
		return 0, fmt.Errorf("fsync staging dir: %w", err)
	}

	// Publish to a path that does not exist yet, so rename(2) is the whole
	// operation and a published snapshot is never unlinked. A same-day
	// re-export lands on the next revision rather than replacing the bytes
	// under a URL that is served immutable for a year.
	name, err := e.freshSnapshotName(date)
	if err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, filepath.Join(e.Dir, name)); err != nil {
		return 0, fmt.Errorf("publish: %w", err)
	}
	if err := syncDir(e.Dir); err != nil {
		return 0, fmt.Errorf("fsync datasets dir: %w", err)
	}

	// The served root copy is rewritten in place on every export: through a
	// temp file and rename, never truncate-then-write under a live reader.
	if err := writeFileAtomic(filepath.Join(e.Dir, "DICTIONARY.md"), dictionary, 0o644); err != nil {
		return 0, err
	}
	if err := e.updateLatest(name); err != nil {
		return 0, err
	}
	if err := e.prune(now); err != nil {
		return 0, err
	}
	// After the walk, not before: a stats rollup landing mid-export would
	// otherwise stamp the manifest one generation behind the rows it
	// describes. generated_at stays the run's start (it names the snapshot
	// directory), so the remaining skew only ever over-states.
	generation, err := e.src().Generation(ctx)
	if err != nil {
		return 0, fmt.Errorf("generation: %w", err)
	}
	if err := e.writeManifest(ctx, now, generation); err != nil {
		return 0, err
	}
	return generation, nil
}

// tsRFC3339 renders a nullable timestamp as RFC 3339 UTC.
func tsRFC3339(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.UTC().Format(time.RFC3339)
	return &s
}

// exportRow maps one scanned ExportRows record to the exported shape.
func exportRow(d *db.ExportRowsRow) Row {
	status := postgres.StatusPtr
	ts := tsRFC3339
	var rank *int64
	if d.Rank != nil {
		r64 := int64(*d.Rank)
		rank = &r64
	}
	return Row{
		Host: d.Host, Rank: rank, Kind: string(d.Kind), Parent: d.Parent,
		Classification: string(d.Classification),
		ClassFlags:     strings.Join(d.ClassFlags, ";"),
		Saint:          d.Saint,
		Base:           status(d.BaseStatus), WWW: status(d.WwwStatus), NS: status(d.NsStatus),
		MX: status(d.MxStatus), Conn: status(d.ConnStatus), Resources: status(d.ResourcesStatus),
		BaseSince: ts(d.BaseSince), WWWSince: ts(d.WwwSince), NSSince: ts(d.NsSince),
		MXSince: ts(d.MxSince), ConnSince: ts(d.ConnSince), ResourcesSince: ts(d.ResourcesSince),
		TLD: d.Tld, Country: strings.TrimSpace(d.Code), ASN: d.Asn,
		DNSProvider: d.DnsProvider, HostingProvider: d.HostingProvider,
		LastChecked: ts(d.LastCheckedAt),
	}
}

// csv renders the row in field order — the same order columns derives — so
// header and cells cannot misalign. An unrenderable field kind is a
// programmer error (a new field needs a case here) and panics; the export
// unit test renders a fully-populated Row to catch it before any nightly run.
func (r *Row) csv() []string {
	v := reflect.ValueOf(*r)
	out := make([]string, v.NumField())
	for i := range out {
		f := v.Field(i)
		if f.Kind() == reflect.Pointer {
			if f.IsNil() {
				continue // empty cell
			}
			f = f.Elem()
		}
		switch f.Kind() {
		case reflect.String:
			out[i] = f.String()
		case reflect.Bool:
			out[i] = strconv.FormatBool(f.Bool())
		case reflect.Int64:
			out[i] = strconv.FormatInt(f.Int(), 10)
		default:
			panic(fmt.Sprintf("export: no CSV rendering for Row field kind %s", f.Kind()))
		}
	}
	return out
}

// parquetChunk batches the streaming parquet writes; small enough to keep
// memory flat, large enough to keep the writer efficient.
const parquetChunk = 1000

// writeTier streams one ExportRows pass into the tier's CSV.gz and Parquet
// files simultaneously — rows are scanned, mapped and written one at a time,
// never materialized as a full-tier slice.
func (e *Exporter) writeTier(ctx context.Context, csvPath, parquetPath string,
	rankedOnly bool, maxRank int32,
) error {
	cf, err := os.Create(csvPath)
	if err != nil {
		return err
	}
	defer func() { _ = cf.Close() }()
	gz := gzip.NewWriter(cf)
	cw := csv.NewWriter(gz)
	if err := cw.Write(columns); err != nil {
		return err
	}

	pf, err := os.Create(parquetPath)
	if err != nil {
		return err
	}
	defer func() { _ = pf.Close() }()
	pw := parquet.NewGenericWriter[Row](pf, parquet.Compression(&parquet.Snappy))

	chunk := make([]Row, 0, parquetChunk)
	for row, err := range e.src().Rows(ctx, rankedOnly, maxRank) {
		if err != nil {
			return err
		}
		if err := cw.Write(row.csv()); err != nil {
			return err
		}
		chunk = append(chunk, row)
		if len(chunk) == parquetChunk {
			if _, err := pw.Write(chunk); err != nil {
				return err
			}
			chunk = chunk[:0]
		}
	}
	if len(chunk) > 0 {
		if _, err := pw.Write(chunk); err != nil {
			return err
		}
	}

	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := cf.Sync(); err != nil {
		return err
	}
	if err := cf.Close(); err != nil {
		return err
	}
	if err := pw.Close(); err != nil {
		return err
	}
	if err := pf.Sync(); err != nil {
		return err
	}
	return pf.Close()
}

// fileDigest streams the artifact through SHA-256 — the tiers were written
// without materializing a row set, and hashing must not undo that.
func fileDigest(path string) (size int64, digest string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", fmt.Errorf("digest %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", fmt.Errorf("digest %s: %w", filepath.Base(path), err)
	}
	return n, fmt.Sprintf("%x", h.Sum(nil)), nil
}

// writeFileAtomic writes data through a same-directory temp file, fsync
// and rename, so a concurrent reader (nginx) never sees a truncated file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	if err := os.Chmod(name, mode); err != nil {
		return fail(err)
	}
	if err := os.Rename(name, path); err != nil {
		return fail(err)
	}
	return nil
}

// syncDir fsyncs a directory so the entries created or renamed into it are
// durable (07 §5.3 "fsync, rename").
func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

// snapshotName parses a published snapshot directory name into its calendar
// date and revision. The bare date is revision 1; a same-day re-export adds
// an "r2", "r3" … suffix. It is the one definition of what counts as a
// snapshot directory — publication, retention and the manifest all read it,
// so they cannot disagree about which directories exist.
func snapshotName(name string) (date time.Time, rev int, ok bool) {
	datePart, revPart, hasRev := strings.Cut(name, "r")
	d, err := time.Parse("2006-01-02", datePart)
	if err != nil {
		return time.Time{}, 0, false
	}
	if !hasRev {
		return d, 1, true
	}
	n, err := strconv.Atoi(revPart)
	if err != nil || n < 2 {
		return time.Time{}, 0, false
	}
	return d, n, true
}

// snapshotDirName renders the inverse of snapshotName.
func snapshotDirName(date string, rev int) string {
	if rev <= 1 {
		return date
	}
	return date + "r" + strconv.Itoa(rev)
}

// freshSnapshotName picks the lowest revision for today that does not exist
// yet, so publication is a pure rename(2) onto a free path. Published
// snapshots are never unlinked, which is what lets the dated URLs be served
// immutable.
func (e *Exporter) freshSnapshotName(date string) (string, error) {
	for rev := 1; rev <= maxSnapshotRevisions; rev++ {
		name := snapshotDirName(date, rev)
		if _, err := os.Lstat(filepath.Join(e.Dir, name)); os.IsNotExist(err) {
			return name, nil
		} else if err != nil {
			return "", fmt.Errorf("publish probe %s: %w", name, err)
		}
	}
	return "", fmt.Errorf("publish: %d revisions already exist for %s", maxSnapshotRevisions, date)
}

// updateLatest repoints the latest symlink atomically.
func (e *Exporter) updateLatest(name string) error {
	link := filepath.Join(e.Dir, "latest")
	tmp := link + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(name, tmp); err != nil {
		return fmt.Errorf("latest symlink: %w", err)
	}
	if err := os.Rename(tmp, link); err != nil {
		return fmt.Errorf("latest symlink swap: %w", err)
	}
	return nil
}

// prune enforces retention: dailies 90 d, first-of-month forever.
func (e *Exporter) prune(now time.Time) error {
	entries, err := os.ReadDir(e.Dir)
	if err != nil {
		return fmt.Errorf("prune readdir: %w", err)
	}
	days := e.RetentionDays
	if days <= 0 {
		days = 90
	}
	cutoff := now.AddDate(0, 0, -days)
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		d, _, ok := snapshotName(ent.Name())
		if !ok {
			continue
		}
		// Retention is per calendar date, so every revision of a pruned day
		// goes together and every revision of a kept day stays: a published
		// URL disappears only when its whole date ages out.
		if d.Day() != 1 && d.Before(cutoff) {
			if err := os.RemoveAll(filepath.Join(e.Dir, ent.Name())); err != nil {
				return fmt.Errorf("prune %s: %w", ent.Name(), err)
			}
		}
	}
	return nil
}

// Manifest is the pinned GET /datasets index schema (07 §5.3).
type Manifest struct {
	SchemaVersion int             `json:"schema_version"`
	GeneratedAt   string          `json:"generated_at"`
	Generation    int32           `json:"generation"`
	License       string          `json:"license"`
	Attribution   string          `json:"attribution"`
	Latest        ManifestLatest  `json:"latest"`
	Snapshots     []ManifestEntry `json:"snapshots"`
}

// ManifestLatest duplicates the newest complete snapshot's entry (07 §5.3).
type ManifestLatest struct {
	Date           string `json:"date"`
	Path           string `json:"path"`
	DatapackageURL string `json:"datapackage_url"`
}

// ManifestEntry is one retained snapshot in the newest-first index (07 §5.3).
type ManifestEntry struct {
	Date           string   `json:"date"`
	Path           string   `json:"path"`
	Tiers          []string `json:"tiers"`
	Formats        []string `json:"formats"`
	DatapackageURL string   `json:"datapackage_url"`
	SHA256SumsURL  string   `json:"sha256sums_url"`
}

func (e *Exporter) writeManifest(ctx context.Context, now time.Time, generation int32) error {
	entries, err := os.ReadDir(e.Dir)
	if err != nil {
		return err
	}
	type snapshot struct {
		name string
		date time.Time
		rev  int
	}
	var found []snapshot
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		if d, rev, ok := snapshotName(ent.Name()); ok {
			found = append(found, snapshot{name: ent.Name(), date: d, rev: rev})
		}
	}
	// Newest first, and within one date the newest revision first — so
	// Latest names the snapshot the export just published.
	sort.Slice(found, func(i, j int) bool {
		if !found[i].date.Equal(found[j].date) {
			return found[i].date.After(found[j].date)
		}
		return found[i].rev > found[j].rev
	})
	if len(found) == 0 {
		return fmt.Errorf("no snapshots to index")
	}

	snaps := make([]ManifestEntry, len(found))
	for i, s := range found {
		snaps[i] = ManifestEntry{
			// date stays the calendar day; path carries the revision, so a
			// consumer can group re-exports without parsing directory names.
			Date: s.date.Format("2006-01-02"), Path: "datasets/" + s.name + "/",
			Tiers:          []string{"top100k", "top1m", "full"},
			Formats:        []string{formatCSVGz, formatParquet},
			DatapackageURL: "/datasets/" + s.name + "/datapackage.json",
			SHA256SumsURL:  "/datasets/" + s.name + "/SHA256SUMS",
		}
	}
	// Cite the specific Tranco list ID (07 §5.3); the generic string only
	// when no import has succeeded yet.
	attribution := "Data: whynoipv6.com (CC-BY-NC-4.0). Ranks: Tranco list."
	listID, err := e.trancoListID(ctx)
	if err != nil {
		return err
	}
	if listID != "" {
		attribution = "Data: whynoipv6.com (CC-BY-NC-4.0). Ranks: Tranco list " + listID + "."
	}
	m := Manifest{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   now.Format(time.RFC3339),
		Generation:    generation,
		License:       license,
		Attribution:   attribution,
		Latest: ManifestLatest{
			Date: snaps[0].Date, Path: snaps[0].Path, DatapackageURL: snaps[0].DatapackageURL,
		},
		Snapshots: snaps,
	}
	// Atomic rewrite: tmp + rename.
	tmp := filepath.Join(e.Dir, ".manifest.json.tmp")
	if err := writeJSONFile(tmp, m); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(e.Dir, "manifest.json"))
}

// Frictionless datapackage.json shapes (OPEN-6).
type datapackage struct {
	Name      string                `json:"name"`
	Title     string                `json:"title"`
	Licenses  []dpLicense           `json:"licenses"`
	Created   string                `json:"created"`
	Sources   []dpSource            `json:"sources"`
	Resources []datapackageResource `json:"resources"`
}

type dpSource struct {
	Title string `json:"title"`
	Path  string `json:"path,omitempty"`
}

// dpSources cites the rank provenance (07 §5.3).
func dpSources(listID string) []dpSource {
	src := dpSource{Title: "Tranco list", Path: "https://tranco-list.eu/"}
	if listID != "" {
		src = dpSource{Title: "Tranco list " + listID, Path: "https://tranco-list.eu/list/" + listID}
	}
	return []dpSource{
		{Title: "whynoipv6.com crawl (CC-BY-NC-4.0)", Path: "https://whynoipv6.com"},
		src,
	}
}

// trancoListID returns the newest successful import's list ID ("" pre-first
// import).
func (e *Exporter) trancoListID(ctx context.Context) (string, error) {
	return e.src().ListID(ctx)
}

type dpLicense struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type datapackageResource struct {
	Name   string   `json:"name"`
	Path   string   `json:"path"`
	Bytes  int64    `json:"bytes"`
	Hash   string   `json:"hash"` // always the sha256: prefix — bare means MD5
	Format string   `json:"format"`
	Schema dpSchema `json:"schema"`
}

type dpSchema struct {
	Fields []dpField `json:"fields"`
}

type dpField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func tableSchema() dpSchema {
	fields := make([]dpField, len(columns))
	for i, c := range columns {
		t := columnTypes[c]
		if t == "" {
			t = "string"
		}
		fields[i] = dpField{Name: c, Type: t}
	}
	return dpSchema{Fields: fields}
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// dictionaryText renders DICTIONARY.md with the rank provenance line
// (07 §5.3 — the list ID is cited in every attribution surface).
func dictionaryText(listID string) string {
	ranks := "Ranks: Tranco list (https://tranco-list.eu/)."
	if listID != "" {
		ranks = "Ranks: Tranco list " + listID + " (https://tranco-list.eu/list/" + listID + ")."
	}
	return dictionaryMD + "\n" + ranks + "\n"
}

// dictionaryMD documents columns + status semantics for bulk consumers.
const dictionaryMD = `# WhyNoIPv6 dataset dictionary

Snapshots of confirmed IPv6 adoption state for the Tranco-ranked web,
exported nightly. License: CC-BY-NC-4.0. https://whynoipv6.com

## Tiers

- ` + "`top100k`" + ` — publicly-ranked domains with rank ≤ 100000
- ` + "`top1m`" + ` — every publicly-ranked domain
- ` + "`full`" + ` — every non-disabled scannable entity (campaign-only
  domains and subdomains carry an empty rank)

## Formats

Each tier ships as gzipped CSV and Parquet. Most tools read both without
extracting: pandas, DuckDB, R (readr), and ClickHouse all load ` + "`.csv.gz`" + `
directly. If you do want the raw CSV, note that macOS's Archive Utility
cannot expand bare .gz files (error 79) — use ` + "`gunzip <file>`" + ` in
Terminal instead.

## Columns

| column | type | notes |
|---|---|---|
| host | string | lowercase punycode FQDN |
| rank | integer | Tranco rank; empty = unranked (campaign/subdomain) |
| kind | string | apex or subdomain |
| parent | string | parent apex for subdomains |
| classification | string | unknown, inactive, sinner, partial, hero |
| class_flags | string | ;-joined: broken_v6, www_missing, ns_missing, mail_missing, resources_v4only |
| saint | boolean | hero with IPv6-clean page resources |
| base/www/ns/mx/conn/resources | string | confirmed status: supported, unsupported, no_record, not_applicable; empty = never confirmed |
| *_since | datetime | when the current confirmed value was established |
| tld | string | bare eTLD suffix |
| country | string | ISO 3166-1 alpha-2 (UN = unknown) |
| asn | integer | hosting AS number (0 = unknown) |
| dns_provider | string | mapped DNS provider name |
| hosting_provider | string | normalized hosting/CDN tag |
| last_checked | datetime | last crawl of this host |

Statuses are confirmed values (N-consecutive-scan anti-flap); raw
per-scan observations are not exported.
`
