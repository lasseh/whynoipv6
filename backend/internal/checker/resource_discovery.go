package checker

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	resourceMaxBodySize = 2 << 20 // 2MB
	// discoveryMaxRedirects preserves the previous `len(via) >= 3` policy:
	// at most two same-host hops.
	discoveryMaxRedirects = 2
	resourceMaxHosts      = 50
)

// ResourceDiscovery finds which external hosts the page references.
// Discovery-only (01-engine.md §11.9): the hosts' AAAA status lives in the
// resource_host registry; the `resources` observation comes from the
// registry roll-up, never from this check's status.
type ResourceDiscovery struct {
	dialer  *SafeDialer
	port    string
	rootCAs *x509.CertPool
}

// NewResourceDiscovery creates a new resource_discovery checker.
func NewResourceDiscovery(dialer *SafeDialer) *ResourceDiscovery {
	return &ResourceDiscovery{dialer: dialer, port: "443"}
}

// probe is the pinned fetch; port and rootCAs are this check's test seams.
func (c *ResourceDiscovery) probe() probe {
	return probe{dialer: c.dialer, port: c.port, rootCAs: c.rootCAs}
}

func (c *ResourceDiscovery) Name() string { return NameResourceDiscovery }

func (c *ResourceDiscovery) Check(ctx context.Context, domain string, _ Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Resolve AAAA records for the domain itself. Resolver trouble is
	// transient (error); only an empty answer is "no AAAA" (01 §11.9).
	ips, _, _, _, err := c.dialer.Resolver().LookupAAAA(ctx, domain)
	if err != nil {
		return Result{
			Status:  StatusError,
			Detail:  &ResourceDiscoveryDetail{CommonDetail: CommonDetail{Error: err.Error()}},
			Latency: time.Since(start),
		}, nil
	}
	if len(ips) == 0 {
		return Result{
			Status:  StatusNotApplicable,
			Detail:  &ResourceDiscoveryDetail{CommonDetail: CommonDetail{Reason: errNoAAAARecord}},
			Latency: time.Since(start),
		}, nil
	}

	ip := ips[0]
	if err := c.dialer.ValidateIP(ip); err != nil {
		return Result{
			Status:  StatusError,
			Detail:  &ResourceDiscoveryDetail{CommonDetail: CommonDetail{Error: errAddrBlocked}},
			Latency: time.Since(start),
		}, nil
	}

	// Fetch the page HTML over IPv6.
	body, err := c.fetchHTML(ctx, domain, ip)
	if err != nil {
		return Result{
			Status:  StatusError,
			Detail:  &ResourceDiscoveryDetail{CommonDetail: CommonDetail{Error: fmt.Sprintf("fetch failed: %v", err)}},
			Latency: time.Since(start),
		}, nil
	}

	// Parse external hostnames from HTML. The FULL deduped list (≤50,
	// first-seen order) is the result; an empty list is a valid supported
	// outcome (discovery succeeded, no external dependencies).
	pageURL := &url.URL{Scheme: "https", Host: domain, Path: "/"}
	hosts := extractExternalHosts(body, pageURL, domain)
	if len(hosts) > resourceMaxHosts {
		hosts = hosts[:resourceMaxHosts]
	}
	if hosts == nil {
		hosts = []string{}
	}

	total := len(hosts)
	return Result{
		Status:  StatusSupported, // "discovery succeeded"; never maps to a public dimension
		Detail:  &ResourceDiscoveryDetail{Hosts: hosts, TotalHosts: &total},
		Latency: time.Since(start),
	}, nil
}

// fetchHTML fetches the page body over IPv6 and returns the raw bytes.
func (c *ResourceDiscovery) fetchHTML(ctx context.Context, domain string, ip net.IP) ([]byte, error) {
	resp, err := c.probe().get(ctx, ip, domain, "https", probeOptions{
		TLS:      true,
		Redirect: sameHostRedirect(domain, discoveryMaxRedirects),
	})
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, resourceMaxBodySize))
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	return body, nil
}

// extractExternalHosts parses HTML to find external resource URLs and returns
// a deduplicated slice of hostnames that differ from the page domain.
func extractExternalHosts(body []byte, pageURL *url.URL, domain string) []string {
	tokenizer := html.NewTokenizer(strings.NewReader(string(body)))

	baseURL := pageURL
	seen := make(map[string]struct{})
	var hosts []string

	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}

		tn, hasAttr := tokenizer.TagName()
		if !hasAttr {
			continue
		}
		tag := string(tn)

		switch tag {
		case "base":
			if href := tagAttrs(tokenizer)["href"]; href != "" {
				if parsed, err := url.Parse(href); err == nil && parsed.IsAbs() {
					baseURL = parsed
				}
			}
		case "script", "img", "iframe", "source", "video", "audio", "object", "embed":
			attrs := tagAttrs(tokenizer)
			addHost(attrs["src"], baseURL, domain, seen, &hosts)
			for _, candidate := range srcsetURLs(attrs["srcset"]) {
				addHost(candidate, baseURL, domain, seen, &hosts)
			}
			addHost(attrs["poster"], baseURL, domain, seen, &hosts)
		case "link":
			attrs := tagAttrs(tokenizer)
			if isFetchRel(attrs["rel"]) {
				addHost(attrs["href"], baseURL, domain, seen, &hosts)
			}
		}
	}

	return hosts
}

// addHost resolves a URL reference against the base, extracts the hostname,
// and appends it to hosts if it is external and not yet seen.
func addHost(raw string, base *url.URL, domain string, seen map[string]struct{}, hosts *[]string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}

	// Skip data: and javascript: URIs.
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "javascript:") {
		return
	}

	ref, err := url.Parse(raw)
	if err != nil {
		return
	}

	resolved := base.ResolveReference(ref)
	host := strings.ToLower(resolved.Hostname())
	if host == "" {
		return
	}

	// Skip same-domain resources (including subdomains like cdn.example.com).
	if host == domain || strings.HasSuffix(host, "."+domain) {
		return
	}

	if _, ok := seen[host]; ok {
		return
	}
	seen[host] = struct{}{}
	*hosts = append(*hosts, host)
}

// tagAttrs reads every attribute of the current token. TagAttr consumes as
// it walks, so a tag whose decision needs two attributes — <link>'s rel and
// href — has to take them in one pass.
func tagAttrs(z *html.Tokenizer) map[string]string {
	attrs := map[string]string{}
	for {
		key, val, more := z.TagAttr()
		attrs[string(key)] = string(val)
		if !more {
			return attrs
		}
	}
}

// fetchRels are the <link> relations whose target a browser fetches to
// render the page. Everything else — canonical, alternate (hreflang and
// RSS/Atom), dns-prefetch, preconnect, me, license, author — is metadata or
// a connection hint, so a v4-only sibling site or feed host behind one is
// not a resource the page depends on (01 §11.9 erratum).
var fetchRels = map[string]bool{
	"stylesheet":       true,
	"preload":          true,
	"modulepreload":    true,
	"icon":             true,
	"apple-touch-icon": true,
	"manifest":         true,
	"prefetch":         true,
}

// isFetchRel reports whether a rel attribute names a fetched relation. rel
// is a space-separated token list ("shortcut icon"), and a <link> with no
// rel at all fetches nothing.
func isFetchRel(rel string) bool {
	for _, token := range strings.Fields(strings.ToLower(rel)) {
		if fetchRels[token] {
			return true
		}
	}
	return false
}

// srcsetURLs pulls the URL out of each srcset candidate. The grammar is
// comma-separated "url [descriptor]"; the descriptor (2x, 640w) is dropped.
func srcsetURLs(srcset string) []string {
	if srcset == "" {
		return nil
	}
	var out []string
	for _, candidate := range strings.Split(srcset, ",") {
		if fields := strings.Fields(candidate); len(fields) > 0 {
			out = append(out, fields[0])
		}
	}
	return out
}
