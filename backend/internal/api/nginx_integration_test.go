//go:build integration

package api_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNginxDatasetsSplit (P6.3 acceptance): nginx -t passes on the shipped
// vhost; a dated snapshot file is served from disk with the immutable
// Cache-Control; the latest/ tree gets the mutable TTL; exact =/datasets
// proxies to the API upstream. Skips when docker is unavailable.
func TestNginxDatasetsSplit(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker unavailable")
	}

	dir := t.TempDir()
	// Self-signed stand-in at the Cloudflare Origin CA path the vhost expects
	// (09-ops.md §7.1). Production serves one apex + wildcard cert from here for
	// both vhosts, so this is a single pair rather than a per-hostname
	// letsencrypt tree.
	certDir := filepath.Join(dir, "cloudflare")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSelfSigned(t, certDir)

	// A dated snapshot + the latest symlink under the datasets root.
	dsDir := filepath.Join(dir, "whynoipv6", "datasets", "2026-07-10")
	if err := os.MkdirAll(dsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dsDir, "whynoipv6-top1m.csv.gz"), []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("2026-07-10", filepath.Join(dir, "whynoipv6", "datasets", "latest")); err != nil {
		t.Fatal(err)
	}

	conf, err := filepath.Abs(filepath.Join("..", "..", "..", "deploy", "nginx", "api.whynoipv6.com.conf"))
	if err != nil {
		t.Fatal(err)
	}

	name := fmt.Sprintf("wni6-nginx-test-%d", time.Now().UnixNano())
	run := exec.Command("docker", "run", "-d", "--rm", "--name", name,
		"-p", "127.0.0.1:0:443",
		"-v", conf+":/etc/nginx/conf.d/api.conf:ro",
		"-v", certDir+":/etc/ssl/cloudflare:ro",
		"-v", filepath.Join(dir, "whynoipv6")+":/var/lib/whynoipv6:ro",
		"nginx:alpine")
	if out, err := run.CombinedOutput(); err != nil {
		t.Skipf("docker run failed (offline?): %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	// nginx -t inside the running container proves the config parses.
	if out, err := exec.Command("docker", "exec", name, "nginx", "-t").CombinedOutput(); err != nil {
		t.Fatalf("nginx -t: %v\n%s", err, out)
	}

	// Resolve the mapped port and wait for readiness.
	portOut, err := exec.Command("docker", "port", name, "443/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	hostPort := strings.TrimSpace(strings.Split(string(portOut), "\n")[0])
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed test cert
		},
		Timeout: 5 * time.Second,
	}
	get := func(path string) *http.Response {
		t.Helper()
		var resp *http.Response
		var lastErr error
		for i := 0; i < 20; i++ {
			req, _ := http.NewRequest(http.MethodGet, "https://"+hostPort+path, nil)
			req.Host = "api.whynoipv6.com"
			resp, lastErr = client.Do(req)
			if lastErr == nil {
				return resp
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Fatalf("GET %s: %v", path, lastErr)
		return nil
	}

	// Dated snapshot: from disk, immutable.
	resp := get("/datasets/2026-07-10/whynoipv6-top1m.csv.gz")
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("dated snapshot: %d cc=%q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}

	// latest/ alias: mutable short TTL.
	resp2 := get("/datasets/latest/whynoipv6-top1m.csv.gz")
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 || resp2.Header.Get("Cache-Control") != "public, max-age=3600" {
		t.Errorf("latest alias: %d cc=%q", resp2.StatusCode, resp2.Header.Get("Cache-Control"))
	}

	// Exact /datasets proxies to the API upstream — no upstream in this
	// container, so a 502 proves it left the static tree.
	resp3 := get("/datasets")
	defer resp3.Body.Close()
	if resp3.StatusCode != 502 {
		t.Errorf("=/datasets should proxy (502 without upstream), got %d", resp3.StatusCode)
	}
}

// writeSelfSigned mints a throwaway cert/key pair for the vhost, named and
// scoped like the Cloudflare Origin CA pair production serves: apex plus a
// first-level wildcard, one pair shared by both vhosts.
func writeSelfSigned(t *testing.T, dir string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "whynoipv6.com"},
		DNSNames:     []string{"whynoipv6.com", "*.whynoipv6.com"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(filepath.Join(dir, "whynoipv6.com.pem"), certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "whynoipv6.com.key"), keyPEM, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestNginxSiteCanonicalRedirects pins the public vhost's URL canonicalisation
// (GSC coverage, 2026-08-23). Every route this vhost serves is the same
// client-rendered shell, so a URL shape that answers 200 is a byte-identical
// duplicate until JS runs — these 301s are the only canonical signal a
// non-rendering crawler gets. The negative cases matter as much as the
// positive ones: the SPA fallback reaches index.html through try_files, and a
// canonicalisation rule written against $uri instead of $request_uri turns
// every route into a 301 to /. Skips when docker is unavailable.
func TestNginxSiteCanonicalRedirects(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker unavailable")
	}

	dir := t.TempDir()
	certDir := filepath.Join(dir, "cloudflare")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSelfSigned(t, certDir)

	// A webroot shaped like a real build: the SPA shell, the blog's flat
	// prerendered files, a hashed asset, and the two crawler control files.
	root := filepath.Join(dir, "www")
	for _, d := range []string{"assets", "blog"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		"index.html":                     "<!doctype html><title>Why No IPv6</title>",
		"blog.html":                      "blog list",
		"blog/index.html":                "blog list",
		"blog/two-ipv6-nameservers.html": "post",
		"assets/index-abc123.js":         "console.log(1)",
		"robots.txt":                     "User-agent: *\nAllow: /\n",
		"sitemap.xml":                    "<urlset/>",
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	conf, err := filepath.Abs(filepath.Join("..", "..", "..", "deploy", "nginx", "whynoipv6.com.conf"))
	if err != nil {
		t.Fatal(err)
	}

	name := fmt.Sprintf("wni6-nginx-site-test-%d", time.Now().UnixNano())
	run := exec.Command("docker", "run", "-d", "--rm", "--name", name,
		"-p", "127.0.0.1:0:443",
		"-v", conf+":/etc/nginx/conf.d/site.conf:ro",
		"-v", certDir+":/etc/ssl/cloudflare:ro",
		"-v", root+":/var/www/whynoipv6.com:ro",
		"nginx:alpine")
	if out, err := run.CombinedOutput(); err != nil {
		t.Skipf("docker run failed (offline?): %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	if out, err := exec.Command("docker", "exec", name, "nginx", "-t").CombinedOutput(); err != nil {
		t.Fatalf("nginx -t: %v\n%s", err, out)
	}

	portOut, err := exec.Command("docker", "port", name, "443/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	hostPort := strings.TrimSpace(strings.Split(string(portOut), "\n")[0])
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed test cert
		},
		// Assert on the redirect itself, never on where it lands.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       5 * time.Second,
	}
	get := func(host, path string) *http.Response {
		t.Helper()
		var resp *http.Response
		var lastErr error
		for i := 0; i < 20; i++ {
			req, _ := http.NewRequest(http.MethodGet, "https://"+hostPort+path, nil)
			req.Host = host
			resp, lastErr = client.Do(req)
			if lastErr == nil {
				return resp
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Fatalf("GET %s%s: %v", host, path, lastErr)
		return nil
	}

	// --- duplicate shapes 301 to the one canonical URL. `want` is the
	// Location value; the apex is implied for path-only entries.
	for _, tc := range []struct {
		host, path, want string
	}{
		// www is a whole second copy of the site.
		{"www.whynoipv6.com", "/", "https://whynoipv6.com/"},
		{"www.whynoipv6.com", "/domains/reddit.com", "https://whynoipv6.com/domains/reddit.com"},
		// Legacy singular nouns, with and without the trailing slash the old
		// router forced on — one hop, not two.
		{"whynoipv6.com", "/domain/reddit.com", "/domains/reddit.com"},
		{"whynoipv6.com", "/domain/reddit.com/", "/domains/reddit.com"},
		{"whynoipv6.com", "/country/no/", "/countries/no"},
		{"whynoipv6.com", "/campaign/abc/", "/campaigns/abc"},
		// Trailing slashes on the current canonical shapes.
		{"whynoipv6.com", "/domains/", "/domains"},
		{"whynoipv6.com", "/domains/reddit.com/", "/domains/reddit.com"},
		{"whynoipv6.com", "/blog/", "/blog"},
		// The tier collections are presets over the leaderboard (src/tiers.ts).
		{"whynoipv6.com", "/sinners", "/domains?filter=sinners"},
		{"whynoipv6.com", "/almost-heroes", "/domains?filter=almost-heroes"},
		// The shell's own filename is the same document as /.
		{"whynoipv6.com", "/index.html", "/"},
	} {
		t.Run("301 "+tc.host+tc.path, func(t *testing.T) {
			resp := get(tc.host, tc.path)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusMovedPermanently {
				t.Errorf("status = %d, want 301", resp.StatusCode)
			}
			if got := resp.Header.Get("Location"); got != tc.want && !strings.HasSuffix(got, tc.want) {
				t.Errorf("Location = %q, want %q", got, tc.want)
			}
		})
	}

	// --- the canonical shapes still serve the app. This is the regression
	// guard: a rule matching $uri rather than $request_uri sends every one of
	// these to / instead, and nginx -t cannot see it.
	for _, path := range []string{
		"/", "/domains", "/domains/reddit.com", "/countries/no", "/campaigns/abc",
		"/metrics", "/faq", "/domains?filter=heroes", "/blog", "/blog/two-ipv6-nameservers",
		"/no-such-route",
	} {
		t.Run("200 "+path, func(t *testing.T) {
			resp := get("whynoipv6.com", path)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200 (Location: %q)", resp.StatusCode, resp.Header.Get("Location"))
			}
		})
	}

	// --- the not-found space answers 200 (the SPA renders the explanation)
	// but must never be indexed. try_files' last argument is a URI, and that
	// internal redirect leaves the location and drops its add_header set — so
	// this also pins that the shell is served in place.
	for _, path := range []string{
		"/domains/nope.example/not-found",
		"/campaigns/abc/nope.example/not-found",
	} {
		t.Run("noindex "+path, func(t *testing.T) {
			resp := get("whynoipv6.com", path)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", resp.StatusCode)
			}
			if got := resp.Header.Get("X-Robots-Tag"); got != "noindex" {
				t.Errorf("X-Robots-Tag = %q, want %q", got, "noindex")
			}
		})
	}

	// --- crawler control files come off disk, so a build that fails to emit
	// one 404s instead of serving the HTML shell as XML.
	for _, path := range []string{"/robots.txt", "/sitemap.xml"} {
		t.Run("served "+path, func(t *testing.T) {
			resp := get("whynoipv6.com", path)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", resp.StatusCode)
			}
		})
	}
	t.Run("unnamed xml falls through to the shell", func(t *testing.T) {
		resp := get("whynoipv6.com", "/sitemap-index.xml")
		defer resp.Body.Close()
		// Not one of the named files, so it falls through to the SPA shell —
		// documented here so a future move to a sitemap index notices.
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200 (SPA fallback)", resp.StatusCode)
		}
	})
}
