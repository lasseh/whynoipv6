// Package geoip implements MaxMind mmdb reading with hot reload and the
// attribution algorithm (06-ingest.md §6): ASN auto-registration input,
// ccTLD-beats-GeoIP country attribution, and the mmdb-free insert-time rule.
package geoip

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/oschwald/geoip2-golang/v2"
)

// Fixed filenames under GEOIP_PATH (06-ingest.md §6.1 — not config).
const (
	asnFile     = "GeoLite2-ASN.mmdb"
	countryFile = "GeoLite2-Country.mmdb"
)

// ReloadInterval is the fixed mtime-check cadence (06-ingest.md §6.8).
const ReloadInterval = time.Hour

// IPMeta is the lookup seam the attributor consumes; *Reader implements it,
// tests fake it.
type IPMeta interface {
	// ASN returns (0, "") on miss.
	ASN(addr netip.Addr) (number uint, org string)
	// CountryCode returns "" on miss.
	CountryCode(addr netip.Addr) string
}

// Reader wraps the two mmdb readers behind atomic pointers so the hourly
// mtime check can swap them without interrupting crawl runs (§6.8).
type Reader struct {
	dir        string
	asn        atomic.Pointer[geoip2.Reader]
	country    atomic.Pointer[geoip2.Reader]
	asnMtime   atomic.Int64
	ctryMtime  atomic.Int64
	buildEpoch atomic.Int64
}

// Open fails fast when either mmdb file is missing or unreadable (§6.1 —
// only the crawler binary calls this).
func Open(dir string) (*Reader, error) {
	r := &Reader{dir: dir}
	if err := r.load(); err != nil {
		return nil, err
	}
	slog.Info("geoip databases loaded", "geoip.build_epoch", r.BuildEpoch().Format(time.RFC3339))
	return r, nil
}

func (r *Reader) load() error {
	asnPath := filepath.Join(r.dir, asnFile)
	ctryPath := filepath.Join(r.dir, countryFile)
	asnInfo, err := os.Stat(asnPath)
	if err != nil {
		return fmt.Errorf("geoip: %w", err)
	}
	ctryInfo, err := os.Stat(ctryPath)
	if err != nil {
		return fmt.Errorf("geoip: %w", err)
	}
	asnRdr, err := geoip2.Open(asnPath)
	if err != nil {
		return fmt.Errorf("geoip: open %s: %w", asnFile, err)
	}
	ctryRdr, err := geoip2.Open(ctryPath)
	if err != nil {
		_ = asnRdr.Close()
		return fmt.Errorf("geoip: open %s: %w", countryFile, err)
	}
	if old := r.asn.Swap(asnRdr); old != nil {
		_ = old.Close()
	}
	if old := r.country.Swap(ctryRdr); old != nil {
		_ = old.Close()
	}
	r.asnMtime.Store(asnInfo.ModTime().UnixNano())
	r.ctryMtime.Store(ctryInfo.ModTime().UnixNano())
	r.buildEpoch.Store(int64(ctryRdr.Metadata().BuildEpoch)) //nolint:gosec // epoch seconds
	return nil
}

// MaybeReload re-opens the readers when either file's mtime changed; called
// hourly by the crawler (§6.8).
func (r *Reader) MaybeReload() error {
	asnInfo, err1 := os.Stat(filepath.Join(r.dir, asnFile))
	ctryInfo, err2 := os.Stat(filepath.Join(r.dir, countryFile))
	if err1 != nil || err2 != nil {
		return errors.Join(err1, err2)
	}
	if asnInfo.ModTime().UnixNano() == r.asnMtime.Load() &&
		ctryInfo.ModTime().UnixNano() == r.ctryMtime.Load() {
		return nil
	}
	if err := r.load(); err != nil {
		return err
	}
	slog.Info("geoip databases reloaded", "geoip.build_epoch", r.BuildEpoch().Format(time.RFC3339))
	return nil
}

// BuildEpoch is the loaded country database's build time — exported into
// crawler_metrics for the staleness alert (§6.8; 09-ops.md §12).
func (r *Reader) BuildEpoch() time.Time { return time.Unix(r.buildEpoch.Load(), 0).UTC() }

func (r *Reader) ASN(addr netip.Addr) (number uint, org string) {
	rec, err := r.asn.Load().ASN(addr)
	if err != nil || rec == nil {
		return 0, ""
	}
	return rec.AutonomousSystemNumber, rec.AutonomousSystemOrganization
}

func (r *Reader) CountryCode(addr netip.Addr) string {
	rec, err := r.country.Load().Country(addr)
	if err != nil || rec == nil {
		return ""
	}
	return rec.Country.ISOCode
}

// Close releases both readers.
func (r *Reader) Close() {
	if old := r.asn.Swap(nil); old != nil {
		_ = old.Close()
	}
	if old := r.country.Swap(nil); old != nil {
		_ = old.Close()
	}
}
