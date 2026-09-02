package domain

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"unicode"

	"golang.org/x/net/idna"

	"github.com/weppos/publicsuffix-go/publicsuffix"
)

// ErrInvalidHost is wrapped by every Canonicalize failure.
var ErrInvalidHost = errors.New("invalid host")

// Canonicalize returns the canonical form of a hostname:
// lowercase punycode FQDN, no trailing dot. It is the ONLY
// path by which a hostname may reach a DB write or DB lookup.
// (06-ingest.md §1.)
func Canonicalize(raw string) (string, error) {
	// Step 1: trim, strip exactly one trailing dot.
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(s, ".")

	// Step 2: reject URLs/ports/paths/addresses-in-brackets and whitespace —
	// callers must pass bare hostnames.
	if s == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidHost)
	}
	if strings.ContainsAny(s, `/\:@?#[]`) || strings.IndexFunc(s, unicode.IsSpace) >= 0 {
		return "", fmt.Errorf("%w: %q is not a bare hostname", ErrInvalidHost, raw)
	}

	// Step 3: lowercase. The single sanctioned hostname-lowercasing site in
	// the module (06-ingest.md §9.1 grep gate).
	s = strings.ToLower(s)

	// Step 4: IDNA2008 lookup profile with UTS46 mapping — Unicode→punycode,
	// strict LDH (rejects '_', empty labels, bad hyphens).
	ascii, err := idna.Lookup.ToASCII(s)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrInvalidHost, raw, err)
	}

	// Step 5: explicit post-checks, independent of profile internals.
	if len(ascii) > 253 {
		return "", fmt.Errorf("%w: %q exceeds 253 octets", ErrInvalidHost, raw)
	}
	labels := strings.Split(ascii, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("%w: %q has fewer than 2 labels", ErrInvalidHost, raw)
	}
	for _, l := range labels {
		if len(l) < 1 || len(l) > 63 {
			return "", fmt.Errorf("%w: %q has a label outside 1–63 octets", ErrInvalidHost, raw)
		}
	}
	if net.ParseIP(ascii) != nil {
		return "", fmt.Errorf("%w: %q is an IP literal", ErrInvalidHost, raw)
	}
	return ascii, nil
}

// pslOptions: ICANN section only, NO wildcard default rule — a host under
// an unknown TLD must fail (06-ingest.md §4.2), so `example.unknowntld999`
// is rejected instead of parsing as registrable.
var pslOptions = &publicsuffix.FindOptions{IgnorePrivate: true, DefaultRule: nil}

// PSLParse returns (registrable eTLD+1, eTLD) for a canonical host; an error
// means the host is a public suffix or the TLD is unknown → invalid entry.
//
// This is the ONE public-suffix derivation in the backend (06 §6.9 erratum,
// review issue 34). It used to live in internal/campaign while Tranco's tld
// came from x/net/publicsuffix — two vendored PSL snapshots a month apart,
// which could disagree about a second-level rule and split one suffix into
// two ?tld= facets by ingress.
func PSLParse(host string) (registrable, tld string, err error) {
	dn, err := publicsuffix.ParseFromListWithOptions(publicsuffix.DefaultList, host, pslOptions)
	if err != nil {
		return "", "", err
	}
	if dn.SLD == "" {
		return "", "", fmt.Errorf("%q is a public suffix", host)
	}
	return dn.SLD + "." + dn.TLD, dn.TLD, nil
}

// TLD returns the effective TLD (public suffix) of a canonical host —
// e.g. "com", "no", "co.uk", "gov" — the domain.tld pivot written at ingest
// (05-schema.md — add pivots + tags; 06-ingest.md §6.9).
//
// Where PSLParse refuses, this falls back to the final label. The Tranco
// path needs that: `tld` is NOT NULL, its rows are already admitted by the
// import's own validation, and a suffix the PSL snapshot has not caught up
// with should land in a facet named after its label rather than fail the
// import.
func TLD(canonical string) string {
	if _, tld, err := PSLParse(canonical); err == nil {
		return tld
	}
	if i := strings.LastIndexByte(canonical, '.'); i >= 0 {
		return canonical[i+1:]
	}
	return canonical
}
