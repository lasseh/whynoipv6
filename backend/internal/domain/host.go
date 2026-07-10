package domain

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
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

// TLD returns the effective TLD (public suffix) of a canonical host —
// e.g. "com", "no", "co.uk", "gov" — the domain.tld pivot written at ingest
// (05-schema.md — add pivots + tags; 06-ingest.md §6.9).
func TLD(canonical string) string {
	suffix, _ := publicsuffix.PublicSuffix(canonical)
	return suffix
}

// ETLDPlusOne returns the registrable domain (eTLD+1, pay-level domain) of a
// canonical host — the Tranco unit and the parent of a campaign subdomain
// (06-ingest.md §3.4).
func ETLDPlusOne(canonical string) (string, error) {
	apex, err := publicsuffix.EffectiveTLDPlusOne(canonical)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrInvalidHost, canonical, err)
	}
	return apex, nil
}
