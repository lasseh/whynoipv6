// Package geoip implements IPinfo Lite mmdb reading with hot reload and the
// attribution algorithm (06-ingest.md §6): ASN auto-registration input,
// ccTLD-beats-GeoIP country attribution, and the mmdb-free insert-time rule.
package geoip

import (
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

// liteFile is the single combined IPinfo Lite database under GEOIP_PATH: one
// file carries both country and ASN for IPv4+IPv6 (06-ingest.md §6.1 — not
// config).
const liteFile = "ipinfo_lite.mmdb"

// ReloadInterval is the fixed mtime-check cadence (06-ingest.md §6.8).
const ReloadInterval = time.Hour

// liteRecord is the subset of the IPinfo Lite record we attribute on. The asn
// field is the textual form "AS13335"; as_name is the AS organization.
type liteRecord struct {
	ASN         string `maxminddb:"asn"`
	ASName      string `maxminddb:"as_name"`
	CountryCode string `maxminddb:"country_code"`
}

// IPMeta is the lookup seam the attributor consumes; *Reader implements it,
// tests fake it.
type IPMeta interface {
	// ASN returns (0, "") on miss.
	ASN(addr netip.Addr) (number uint, org string)
	// CountryCode returns "" on miss.
	CountryCode(addr netip.Addr) string
}

// Reader wraps the mmdb reader behind an atomic pointer so the hourly mtime
// check can swap it without interrupting crawl runs (§6.8).
type Reader struct {
	dir        string
	db         atomic.Pointer[maxminddb.Reader]
	mtime      atomic.Int64
	buildEpoch atomic.Int64
}

// Open fails fast when the mmdb file is missing or unreadable (§6.1 — only the
// crawler binary calls this).
func Open(dir string) (*Reader, error) {
	r := &Reader{dir: dir}
	if err := r.load(); err != nil {
		return nil, err
	}
	slog.Info("geoip databases loaded", "geoip.build_epoch", r.BuildEpoch().Format(time.RFC3339))
	return r, nil
}

func (r *Reader) load() error {
	path := filepath.Join(r.dir, liteFile)
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("geoip: %w", err)
	}
	rdr, err := maxminddb.Open(path)
	if err != nil {
		return fmt.Errorf("geoip: open %s: %w", liteFile, err)
	}
	if old := r.db.Swap(rdr); old != nil {
		_ = old.Close()
	}
	r.mtime.Store(info.ModTime().UnixNano())
	r.buildEpoch.Store(int64(rdr.Metadata.BuildEpoch)) //nolint:gosec // epoch seconds
	return nil
}

// MaybeReload re-opens the reader when the file's mtime changed; called hourly
// by the crawler (§6.8).
func (r *Reader) MaybeReload() error {
	info, err := os.Stat(filepath.Join(r.dir, liteFile))
	if err != nil {
		return err
	}
	if info.ModTime().UnixNano() == r.mtime.Load() {
		return nil
	}
	if err := r.load(); err != nil {
		return err
	}
	slog.Info("geoip databases reloaded", "geoip.build_epoch", r.BuildEpoch().Format(time.RFC3339))
	return nil
}

// BuildEpoch is the loaded database's build time — exported into
// crawler_metrics for the staleness alert (§6.8; 09-ops.md §12).
func (r *Reader) BuildEpoch() time.Time { return time.Unix(r.buildEpoch.Load(), 0).UTC() }

func (r *Reader) ASN(addr netip.Addr) (number uint, org string) {
	var rec liteRecord
	res := r.db.Load().Lookup(addr)
	if !res.Found() || res.Decode(&rec) != nil {
		return 0, ""
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(rec.ASN, "AS"), 10, 64)
	if err != nil {
		return 0, ""
	}
	return uint(n), rec.ASName
}

func (r *Reader) CountryCode(addr netip.Addr) string {
	var rec liteRecord
	res := r.db.Load().Lookup(addr)
	if !res.Found() || res.Decode(&rec) != nil {
		return ""
	}
	return rec.CountryCode
}

// Close releases the reader.
func (r *Reader) Close() {
	if old := r.db.Swap(nil); old != nil {
		_ = old.Close()
	}
}
