package ingest

import (
	"strings"

	"github.com/lasseh/whynoipv6/internal/checker"
)

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
			for suffix, tag := range checker.CDNSuffixTags {
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
