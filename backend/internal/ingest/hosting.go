package ingest

import (
	"strings"
)

// cdnSuffixTags maps the checker's fixed CDN-suffix list (01-engine.md §11.2,
// single-sourced here per 06-ingest.md §6.10) to normalized hosting tags.
//
//nolint:goconst // a literal data table; repeated tags are values
var cdnSuffixTags = map[string]string{
	"cloudfront.net":        "cloudfront",
	"cloudflare.net":        "cloudflare",
	"cdn.cloudflarenet.com": "cloudflare",
	"akamaiedge.net":        "akamai",
	"akamai.net":            "akamai",
	"edgekey.net":           "akamai",
	"fastly.net":            "fastly",
	"azureedge.net":         "azure",
	"edgecastcdn.net":       "edgecast",
	"stackpathdns.com":      "stackpath",
	"googleapis.com":        "google",
}

// hostingASNTags is the launch seed set of hosting/cloud ASN → tag
// (06-ingest.md §6.10 Decision; extended as collected data shows gaps).
var hostingASNTags = map[uint]string{
	16509:  "aws",
	14618:  "aws",
	15169:  "google",
	396982: "google",
	8075:   "azure",
	16276:  "ovh",
	24940:  "hetzner",
	14061:  "digitalocean",
	63949:  "linode",
	13335:  "cloudflare",
}

// NormalizeHosting derives the hosting/CDN provider tag from data the scan
// already collected: CDN via CNAME chain first, else the resolved input IP's
// ASN, else nil (06-ingest.md §6.10).
func NormalizeHosting(cdnDetected bool, cnameChain []string, asn uint) *string {
	if cdnDetected {
		for _, cname := range cnameChain {
			// Folded like the NS hosts in provider.go: a mixed-case CNAME
			// target must still hit its suffix key.
			c := strings.ToLower(strings.TrimSuffix(cname, "."))
			for suffix, tag := range cdnSuffixTags {
				if c == suffix || strings.HasSuffix(c, "."+suffix) {
					t := tag
					return &t
				}
			}
		}
	}
	if tag, ok := hostingASNTags[asn]; ok {
		t := tag
		return &t
	}
	return nil
}
