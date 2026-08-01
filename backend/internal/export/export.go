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
	"os"
	"path/filepath"
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

// columns is the CSV header and the Table Schema field order.
var columns = []string{
	"host", "rank", "kind", "parent", "classification", "class_flags", "saint",
	"base", "www", "ns", "mx", "conn", "resources",
	"base_since", "www_since", "ns_since", "mx_since", "conn_since", "resources_since",
	"tld", "country", "asn", "dns_provider", "hosting_provider", "last_checked",
}

// Frictionless Table Schema types + the two published formats.
const (
	typeDatetime  = "datetime"
	formatCSVGz   = "csv.gz"
	formatParquet = "parquet"
)

// columnTypes maps column → Frictionless Table Schema type.
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

	// RetentionDays prunes dailies older than this (datasets.retention_days;
	// ≤0 falls back to the spec default 90). First-of-month snapshots are
	// kept forever regardless.
	RetentionDays int

	// Now is injectable for tests; defaults to time.Now.
	Now func() time.Time
}

// Run produces the dated snapshot for today, atomically publishes it
// (tmp-dir rename(2)), rewrites manifest.json + the latest symlink, and
// prunes per retention (dailies 90 d, first-of-month forever).
func (e *Exporter) Run(ctx context.Context, generation int32) error {
	now := time.Now().UTC()
	if e.Now != nil {
		now = e.Now().UTC()
	}
	date := now.Format("2006-01-02")

	if err := os.MkdirAll(e.Dir, 0o755); err != nil {
		return fmt.Errorf("datasets dir: %w", err)
	}
	tmp, err := os.MkdirTemp(e.Dir, ".export-*")
	if err != nil {
		return fmt.Errorf("export tmp: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	var resources []datapackageResource
	var sums []string
	for _, tier := range tiers {
		rows, err := e.fetch(ctx, tier.RankedOnly, tier.MaxRank)
		if err != nil {
			return fmt.Errorf("tier %s: %w", tier.Name, err)
		}
		for _, format := range []string{formatCSVGz, formatParquet} {
			name := fmt.Sprintf("whynoipv6-%s.%s", tier.Name, format)
			path := filepath.Join(tmp, name)
			if format == formatCSVGz {
				err = writeCSVGz(path, rows)
			} else {
				err = writeParquet(path, rows)
			}
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			size, digest, err := fileDigest(path)
			if err != nil {
				return err
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
	listID := e.trancoListID(ctx)
	dp := datapackage{
		Name: "whynoipv6-" + date, Title: "WhyNoIPv6 daily snapshot " + date,
		Licenses:  []dpLicense{{Name: license, Path: "https://creativecommons.org/licenses/by-nc/4.0/"}},
		Created:   now.Format(time.RFC3339),
		Sources:   dpSources(listID),
		Resources: resources,
	}
	if err := writeJSONFile(filepath.Join(tmp, "datapackage.json"), dp); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"), 0o644); err != nil {
		return err
	}

	// Publish atomically: tmp-dir rename(2) to the immutable dated path.
	final := filepath.Join(e.Dir, date)
	_ = os.RemoveAll(final) // same-day re-export replaces the snapshot
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	if err := os.WriteFile(filepath.Join(e.Dir, "DICTIONARY.md"), []byte(dictionaryText(listID)), 0o644); err != nil {
		return err
	}
	if err := e.updateLatest(date); err != nil {
		return err
	}
	if err := e.prune(now); err != nil {
		return err
	}
	return e.writeManifest(ctx, now, generation)
}

func (e *Exporter) fetch(ctx context.Context, rankedOnly bool, maxRank int32) ([]Row, error) {
	dbRows, err := db.New(e.Pool).ExportRows(ctx, db.ExportRowsParams{
		RankedOnly: rankedOnly, MaxRank: maxRank,
	})
	if err != nil {
		return nil, fmt.Errorf("export rows: %w", err)
	}
	status := postgres.StatusPtr
	ts := func(t pgtype.Timestamptz) *string {
		if !t.Valid {
			return nil
		}
		s := t.Time.UTC().Format(time.RFC3339)
		return &s
	}
	out := make([]Row, len(dbRows))
	for i := range dbRows {
		d := &dbRows[i]
		var rank *int64
		if d.Rank != nil {
			r64 := int64(*d.Rank)
			rank = &r64
		}
		out[i] = Row{
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
	return out, nil
}

func (r *Row) csv() []string {
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	rank := ""
	if r.Rank != nil {
		rank = strconv.FormatInt(*r.Rank, 10)
	}
	return []string{
		r.Host, rank, r.Kind, str(r.Parent), r.Classification, r.ClassFlags,
		strconv.FormatBool(r.Saint),
		str(r.Base), str(r.WWW), str(r.NS), str(r.MX), str(r.Conn), str(r.Resources),
		str(r.BaseSince), str(r.WWWSince), str(r.NSSince), str(r.MXSince), str(r.ConnSince), str(r.ResourcesSince),
		str(r.TLD), r.Country, strconv.FormatInt(r.ASN, 10),
		str(r.DNSProvider), str(r.HostingProvider), str(r.LastChecked),
	}
}

func writeCSVGz(path string, rows []Row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	cw := csv.NewWriter(gz)
	if err := cw.Write(columns); err != nil {
		return err
	}
	for i := range rows {
		if err := cw.Write(rows[i].csv()); err != nil {
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
	return f.Close()
}

func writeParquet(path string, rows []Row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w := parquet.NewGenericWriter[Row](f, parquet.Compression(&parquet.Snappy))
	if len(rows) > 0 {
		if _, err := w.Write(rows); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	return f.Close()
}

func fileDigest(path string) (size int64, digest string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, "", fmt.Errorf("digest %s: %w", filepath.Base(path), err)
	}
	return int64(len(b)), fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

// updateLatest repoints the latest symlink atomically.
func (e *Exporter) updateLatest(date string) error {
	link := filepath.Join(e.Dir, "latest")
	tmp := link + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(date, tmp); err != nil {
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
		d, err := time.Parse("2006-01-02", ent.Name())
		if err != nil {
			continue
		}
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
	var dates []string
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		if _, err := time.Parse("2006-01-02", ent.Name()); err == nil {
			dates = append(dates, ent.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates))) // newest first
	if len(dates) == 0 {
		return fmt.Errorf("no snapshots to index")
	}

	snaps := make([]ManifestEntry, len(dates))
	for i, d := range dates {
		snaps[i] = ManifestEntry{
			Date: d, Path: "datasets/" + d + "/",
			Tiers:          []string{"top100k", "top1m", "full"},
			Formats:        []string{formatCSVGz, formatParquet},
			DatapackageURL: "/datasets/" + d + "/datapackage.json",
			SHA256SumsURL:  "/datasets/" + d + "/SHA256SUMS",
		}
	}
	// Cite the specific Tranco list ID (07 §5.3); the generic string only
	// when no import has succeeded yet.
	attribution := "Data: whynoipv6.com (CC-BY-NC-4.0). Ranks: Tranco list."
	if listID := e.trancoListID(ctx); listID != "" {
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
func (e *Exporter) trancoListID(ctx context.Context) string {
	listID, err := db.New(e.Pool).TrancoLatestSuccessListID(ctx)
	if err != nil {
		return ""
	}
	return listID
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
