package checker

import (
	"context"
	"crypto/tls"
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
	resourceMaxHosts    = 50
)

// ResourceDiscovery finds which external hosts the page references.
// Discovery-only (01-engine.md §11.9): the hosts' AAAA status lives in the
// resource_host registry; the `resources` observation comes from the
// registry roll-up, never from this check's status.
type ResourceDiscovery struct {
	dialer *SafeDialer
}

// NewResourceDiscovery creates a new resource_discovery checker.
func NewResourceDiscovery(dialer *SafeDialer) *ResourceDiscovery {
	return &ResourceDiscovery{dialer: dialer}
}

func (c *ResourceDiscovery) Name() string { return "resource_discovery" }

func (c *ResourceDiscovery) Check(ctx context.Context, domain string, _ Kind) (Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Resolve AAAA records for the domain itself.
	ips, _, _, _, err := c.dialer.Resolver().LookupAAAA(ctx, domain)
	if err != nil || len(ips) == 0 {
		return Result{
			Status:  StatusNotApplicable,
			Details: map[string]any{"reason": errNoAAAARecord},
			Latency: time.Since(start),
		}, nil
	}

	ip := ips[0]
	if err := c.dialer.ValidateIP(ip); err != nil {
		return Result{
			Status:  StatusError,
			Details: map[string]any{"error": errAddrBlocked},
			Latency: time.Since(start),
		}, nil
	}

	// Fetch the page HTML over IPv6.
	body, err := c.fetchHTML(ctx, domain, ip)
	if err != nil {
		return Result{
			Status:  StatusError,
			Details: map[string]any{"error": fmt.Sprintf("fetch failed: %v", err)},
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

	return Result{
		Status:  StatusSupported, // "discovery succeeded"; never maps to a public dimension
		Details: map[string]any{"hosts": hosts, "total_hosts": len(hosts)},
		Latency: time.Since(start),
	}, nil
}

// fetchHTML fetches the page body over IPv6 and returns the raw bytes.
func (c *ResourceDiscovery) fetchHTML(ctx context.Context, domain string, ip net.IP) ([]byte, error) {
	addr := net.JoinHostPort(ip.String(), "443")

	transport := &http.Transport{
		DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
			return c.dialer.dialer.DialContext(dialCtx, "tcp6", addr)
		},
		TLSClientConfig: &tls.Config{
			ServerName: domain,
			MinVersion: tls.VersionTLS12,
		},
		DisableKeepAlives: true,
	}

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	reqURL := fmt.Sprintf("https://%s/", domain)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
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
			if href := getAttr(tokenizer, "href"); href != "" {
				if parsed, err := url.Parse(href); err == nil && parsed.IsAbs() {
					baseURL = parsed
				}
			}
		case "script", "img", "iframe", "source", "video", "audio", "object", "embed":
			if src := getAttr(tokenizer, "src"); src != "" {
				addHost(src, baseURL, domain, seen, &hosts)
			}
		case "link":
			if href := getAttr(tokenizer, "href"); href != "" {
				addHost(href, baseURL, domain, seen, &hosts)
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

// getAttr returns the value of the named attribute from the current token.
func getAttr(z *html.Tokenizer, name string) string {
	for {
		key, val, more := z.TagAttr()
		if string(key) == name {
			return string(val)
		}
		if !more {
			return ""
		}
	}
}
