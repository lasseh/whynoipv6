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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/parquet-go/parquet-go"
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
	Gold            bool    `parquet:"gold" json:"gold"`
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
	"host", "rank", "kind", "parent", "classification", "class_flags", "gold",
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
	"rank": "integer", "gold": "boolean", "asn": "integer",
	"base_since": typeDatetime, "www_since": typeDatetime, "ns_since": typeDatetime,
	"mx_since": typeDatetime, "conn_since": typeDatetime, "resources_since": typeDatetime,
	"last_checked": typeDatetime,
}

// tiers: predicate per size tier. top100k/top1m use the publicly-ranked
// predicate; full = every non-disabled scannable entity.
var tiers = []struct {
	Name  string
	Where string
}{
	{"top100k", "d.rank IS NOT NULL AND d.rank <= 100000 AND NOT d.disabled"},
	{"top1m", "d.rank IS NOT NULL AND NOT d.disabled"},
	{"full", "NOT d.disabled"},
}

const exportSelect = `
SELECT d.host, d.rank::bigint, d.kind::text, p.host AS parent,
       d.classification::text, d.class_flags, d.gold,
       d.base_status::text, d.www_status::text, d.ns_status::text,
       d.mx_status::text, d.conn_status::text, d.resources_status::text,
       d.base_since, d.www_since, d.ns_since, d.mx_since, d.conn_since, d.resources_since,
       d.tld, c.code::text AS country, a.number AS asn,
       dp.name AS dns_provider, d.hosting_provider, d.last_checked_at
FROM domain d
JOIN country c ON c.id = d.country_id
JOIN asn a ON a.id = d.asn_id
LEFT JOIN dns_provider dp ON dp.id = d.dns_provider_id
LEFT JOIN domain p ON p.id = d.parent_id
WHERE %s
ORDER BY d.rank ASC NULLS LAST, d.id ASC`

// Exporter runs one snapshot export.
type Exporter struct {
	Pool *pgxpool.Pool
	Dir  string // $DATASETS_DIR

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
		rows, err := e.fetch(ctx, tier.Where)
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

	dp := datapackage{
		Name: "whynoipv6-" + date, Title: "WhyNoIPv6 daily snapshot " + date,
		Licenses:  []dpLicense{{Name: license, Path: "https://creativecommons.org/licenses/by-nc/4.0/"}},
		Created:   now.Format(time.RFC3339),
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

	if err := os.WriteFile(filepath.Join(e.Dir, "DICTIONARY.md"), []byte(dictionaryMD), 0o644); err != nil {
		return err
	}
	if err := e.updateLatest(date); err != nil {
		return err
	}
	if err := e.prune(now); err != nil {
		return err
	}
	return e.writeManifest(now, generation)
}

func (e *Exporter) fetch(ctx context.Context, where string) ([]Row, error) {
	q := fmt.Sprintf(exportSelect, where)
	pgRows, err := e.Pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer pgRows.Close()
	var out []Row
	for pgRows.Next() {
		var r Row
		var flags []string
		var since [6]*time.Time
		var lastChecked *time.Time
		if err := pgRows.Scan(&r.Host, &r.Rank, &r.Kind, &r.Parent, &r.Classification, &flags, &r.Gold,
			&r.Base, &r.WWW, &r.NS, &r.MX, &r.Conn, &r.Resources,
			&since[0], &since[1], &since[2], &since[3], &since[4], &since[5],
			&r.TLD, &r.Country, &r.ASN, &r.DNSProvider, &r.HostingProvider, &lastChecked); err != nil {
			return nil, err
		}
		r.ClassFlags = strings.Join(flags, ";")
		r.Country = strings.TrimSpace(r.Country)
		ts := func(t *time.Time) *string {
			if t == nil {
				return nil
			}
			s := t.UTC().Format(time.RFC3339)
			return &s
		}
		r.BaseSince, r.WWWSince, r.NSSince = ts(since[0]), ts(since[1]), ts(since[2])
		r.MXSince, r.ConnSince, r.ResourcesSince = ts(since[3]), ts(since[4]), ts(since[5])
		r.LastChecked = ts(lastChecked)
		out = append(out, r)
	}
	return out, pgRows.Err()
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
		strconv.FormatBool(r.Gold),
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
		return 0, "", err
	}
	return int64(len(b)), fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

// updateLatest repoints the latest symlink atomically.
func (e *Exporter) updateLatest(date string) error {
	link := filepath.Join(e.Dir, "latest")
	tmp := link + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(date, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, link)
}

// prune enforces retention: dailies 90 d, first-of-month forever.
func (e *Exporter) prune(now time.Time) error {
	entries, err := os.ReadDir(e.Dir)
	if err != nil {
		return err
	}
	cutoff := now.AddDate(0, 0, -90)
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
				return err
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

type ManifestLatest struct {
	Date           string `json:"date"`
	Path           string `json:"path"`
	DatapackageURL string `json:"datapackage_url"`
}

type ManifestEntry struct {
	Date           string   `json:"date"`
	Path           string   `json:"path"`
	Tiers          []string `json:"tiers"`
	Formats        []string `json:"formats"`
	DatapackageURL string   `json:"datapackage_url"`
	SHA256SumsURL  string   `json:"sha256sums_url"`
}

func (e *Exporter) writeManifest(now time.Time, generation int32) error {
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
	m := Manifest{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   now.Format(time.RFC3339),
		Generation:    generation,
		License:       license,
		Attribution:   "Data: whynoipv6.com (CC-BY-NC-4.0). Ranks: Tranco list.",
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
	Resources []datapackageResource `json:"resources"`
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
| gold | boolean | hero with IPv6-clean page resources |
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
