package checker

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
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

	// Fetch the page HTML over IPv6, following same-site redirects.
	body, pageURL, err := c.fetchHTML(ctx, domain, ip)
	if err != nil {
		return Result{
			Status:  StatusError,
			Detail:  &ResourceDiscoveryDetail{CommonDetail: CommonDetail{Error: fmt.Sprintf("fetch failed: %v", err)}},
			Latency: time.Since(start),
		}, nil
	}

	// Parse external hostnames from HTML. The FULL deduped list (≤50,
	// first-seen order) is the result; an empty list is a valid supported
	// outcome (discovery succeeded, no external dependencies). References
	// resolve against the URL the body actually came from — after an
	// apex→www hop that is the www URL, not the apex.
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

// fetchHTML fetches the page body over IPv6 and returns it with the URL it
// finally came from.
//
// Redirects within a host are followed by the pinned client. A redirect to
// a *different* host cannot be: the transport dials one fixed IP with one
// fixed SNI, which is why cross-host following was removed. But an apex
// that 301s to www is the dominant apex configuration, and refusing it left
// discovery parsing the 3xx boilerplate, reporting zero hosts, and folding
// a vacuous not_applicable that unlocked saint (01 §11.9 erratum, review
// issue 01).
//
// So a redirect that stays *in scope* — the apex itself or a subdomain of
// it, over the same scheme and port — gets its own hop: its own AAAA from
// the bulk resolver, its own ValidateIP, its own pin and SNI. Arbitrary
// cross-site redirects stay unfollowed; those really would fetch the wrong
// vhost.
//
// Every hop is over IPv6 or not at all. Phase 2 gates discovery on the apex
// having AAAA, and a hop target with no AAAA is not fetched over v4 to make
// up for it — the last response stands. A v4-only www is the www
// dimension's story to tell, and telling it here would let a v4-only page
// hide behind an apex with AAAA.
func (c *ResourceDiscovery) fetchHTML(ctx context.Context, apex string, ip net.IP) ([]byte, *url.URL, error) {
	host, addr := apex, ip
	remaining := discoveryMaxRedirects

	for {
		hops := 0
		sameHost := sameHostRedirect(host, remaining)
		resp, err := c.probe().get(ctx, addr, host, "https", probeOptions{
			TLS: true,
			Redirect: func(req *http.Request, via []*http.Request) error {
				if err := sameHost(req, via); err != nil {
					return err
				}
				hops = len(via)
				return nil
			},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("http request: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, resourceMaxBodySize))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, nil, fmt.Errorf("reading body: %w", readErr)
		}
		final := resp.Request.URL
		remaining -= hops

		next := inScopeRedirect(resp, apex)
		if next == "" || remaining == 0 {
			return body, final, nil
		}
		nextIP, ok := c.resolveHop(ctx, next)
		if !ok {
			return body, final, nil // no usable AAAA on the target
		}
		remaining--
		host, addr = next, nextIP
	}
}

// inScopeRedirect reports the host of a redirect the pinned client could
// not follow but discovery should: a 3xx whose Location names a different
// host that is still the apex or a subdomain of it, over the same scheme
// and port. Everything else — another site, a scheme downgrade, a port
// change — returns "".
func inScopeRedirect(resp *http.Response, apex string) string {
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return ""
	}
	loc, err := resp.Location()
	if err != nil {
		return ""
	}
	host := strings.ToLower(loc.Hostname())
	current := strings.ToLower(resp.Request.URL.Hostname())
	if host == "" || host == current {
		return "" // same host: the pinned client already had its chance
	}
	if host != apex && !strings.HasSuffix(host, "."+apex) {
		return ""
	}
	if loc.Scheme != resp.Request.URL.Scheme || loc.Port() != resp.Request.URL.Port() {
		return ""
	}
	return host
}

// resolveHop resolves a redirect target's own AAAA and clears it with the
// SSRF blocklist. Failure is not an error: the caller keeps the last
// response, so a hop we cannot reach over IPv6 costs the redirect, not the
// scan.
func (c *ResourceDiscovery) resolveHop(ctx context.Context, host string) (net.IP, bool) {
	ips, _, _, _, err := c.dialer.Resolver().LookupAAAA(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, false
	}
	if err := c.dialer.ValidateIP(ips[0]); err != nil {
		return nil, false
	}
	return ips[0], true
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
