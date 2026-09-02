//go:build integration

package api_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
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
	requireDocker(t)

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
		skipOrFail(t, "docker run failed (offline?): %v\n%s", err, out)
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

	// The ?q= search zone (review issue 44): 1 r/s burst 5, keyed by client
	// address only when the parameter is present. There is no upstream, so
	// a proxied request answers 502 and a throttled one answers 429 — the
	// two are unambiguous.
	//
	// The general api_perip zone is 10 r/s burst 30 and the requests above
	// leave that far from exhausted, so a 429 here can only come from the
	// search zone.
	statuses := func(path string, n int) map[int]int {
		t.Helper()
		seen := map[int]int{}
		for range n {
			req, _ := http.NewRequest(http.MethodGet, "https://"+hostPort+path, nil)
			req.Host = "api.whynoipv6.com"
			r, err := client.Do(req)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			_ = r.Body.Close()
			seen[r.StatusCode]++
		}
		return seen
	}
	if got := statuses("/domains?q=example", 12); got[429] == 0 {
		t.Errorf("12 rapid ?q= requests were all admitted (%v): the search zone did not fire", got)
	}
	if got := statuses("/domains?class=hero", 12); got[429] != 0 {
		t.Errorf("non-search requests were throttled (%v): the map must key on $arg_q alone", got)
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
	requireDocker(t)

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

	name := fmt.Sprintf("wni6-nginx-site-test-%d", time.Now().UnixNano())
	run := exec.Command("docker", "run", "-d", "--rm", "--name", name,
		"-p", "127.0.0.1:0:443",
		// The drill-free vhost: redirects, headers and the SPA fallback are the
		// same in both files, and TestNginxVhostsAgree pins that they stay so.
		"-v", writeBaseVhost(t, dir)+":/etc/nginx/conf.d/site.conf:ro",
		"-v", certDir+":/etc/ssl/cloudflare:ro",
		"-v", root+":/var/www/whynoipv6.com:ro",
		"nginx:alpine")
	if out, err := run.CombinedOutput(); err != nil {
		skipOrFail(t, "docker run failed (offline?): %v\n%s", err, out)
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

// vhostPath resolves one of the two shipped frontend vhosts. Ansible picks
// which of them lands on the server: whynoipv6.com.conf is the plain site,
// whynoipv6.com.drill.conf is the same site plus the IPv4-outage machinery.
func vhostPath(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "deploy", "nginx", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// writeBaseVhost copies the drill-free vhost into dir and returns the new path.
// Nothing to patch: it carries no outage machinery at all, so it cannot start
// failing on the 6th.
func writeBaseVhost(t *testing.T, dir string) string {
	t.Helper()
	body, err := os.ReadFile(vhostPath(t, "whynoipv6.com.conf"))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "site.conf")
	if err := os.WriteFile(out, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

// writeVhost copies the drill vhost into dir with the IPv4-outage switch forced
// to mode ("off", "on" or "schedule") and returns the new path. The coordinated
// window is gated on $time_iso8601, and a test cannot move the container's clock
// onto the 6th — so the drill is exercised through the same maps via the manual
// switch instead. Callers that are not testing the drill pin themselves to "off"
// so they do not start failing on the 6th.
func writeVhost(t *testing.T, dir, mode string) string {
	t.Helper()
	body, err := os.ReadFile(vhostPath(t, "whynoipv6.com.drill.conf"))
	if err != nil {
		t.Fatal(err)
	}
	const knob = "\n    default schedule;\n"
	if n := strings.Count(string(body), knob); n != 1 {
		t.Fatalf("drill switch appears %d times in the vhost, want 1 — writeVhost needs updating", n)
	}
	out := filepath.Join(dir, "site.conf")
	patched := strings.Replace(string(body), knob, "\n    default "+mode+";\n", 1)
	if err := os.WriteFile(out, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestNginxVhostsAgree pins that whynoipv6.com.drill.conf is still the plain
// vhost plus the outage machinery, and nothing else. The two files are 90%
// identical and Ansible ships one or the other, so an edit applied to only one
// of them is the failure mode that costs a deploy: the drill file quietly falls
// behind on a redirect or a security header, and nobody finds out until the
// month it is the one on the server.
//
// The drill vhost marks its additions with `# >>> ipv4 drill` / `# <<< ipv4
// drill`. Cut those out and what remains must equal the plain vhost byte for
// byte. Equality and not "every base line appears somewhere in the drill file":
// that weaker check passes when a line is *deleted* from the plain vhost, so
// the plain site could quietly ship without its ssl_protocols line while the
// drill file kept it. It needs no docker, so it runs even where the container
// tests skip.
func TestNginxVhostsAgree(t *testing.T) {
	read := func(name string) []string {
		body, err := os.ReadFile(vhostPath(t, name))
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(string(body), "\n")
	}
	base, drill := read("whynoipv6.com.conf"), read("whynoipv6.com.drill.conf")

	var stripped []string
	inDrillBlock, blocks := false, 0
	for n, line := range drill {
		switch strings.TrimSpace(line) {
		case "# >>> ipv4 drill":
			if inDrillBlock {
				t.Fatalf("whynoipv6.com.drill.conf:%d: nested drill block", n+1)
			}
			inDrillBlock = true
		case "# <<< ipv4 drill":
			if !inDrillBlock {
				t.Fatalf("whynoipv6.com.drill.conf:%d: drill block closed but never opened", n+1)
			}
			inDrillBlock, blocks = false, blocks+1
		default:
			if !inDrillBlock {
				stripped = append(stripped, line)
			}
		}
	}
	if inDrillBlock {
		t.Fatal("whynoipv6.com.drill.conf: unterminated drill block")
	}
	if blocks == 0 {
		t.Fatal("whynoipv6.com.drill.conf carries no drill blocks — has the machinery been removed?")
	}

	for i := range max(len(base), len(stripped)) {
		var b, s string
		if i < len(base) {
			b = base[i]
		}
		if i < len(stripped) {
			s = stripped[i]
		}
		if b != s {
			t.Fatalf("the two vhosts have diverged at line %d — apply the edit to both\n"+
				"\twhynoipv6.com.conf:       %q\n"+
				"\twhynoipv6.com.drill.conf: %q", i+1, b, s)
		}
	}
}

// writeRealIPShim emulates the host-level conf.d/cloudflare.conf that the
// nginx role clones from lasseh/nginx-conf. Production is Cloudflare-fronted,
// so the socket peer is an edge PoP and $remote_addr means nothing until
// real_ip_header rewrites it from CF-Connecting-IP. Every consumer of the
// visitor's address depends on it — the rate limiters, X-Umami-Client-IP, and
// now the drill's address-family test. Driving the tests through the same
// header is the point: a family check written against the socket peer would
// pass locally and misfire behind the CDN.
func writeRealIPShim(t *testing.T, dir string) string {
	t.Helper()
	out := filepath.Join(dir, "realip.conf")
	body := "set_real_ip_from 0.0.0.0/0;\nset_real_ip_from ::/0;\nreal_ip_header CF-Connecting-IP;\n"
	if err := os.WriteFile(out, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

// browserUA stands in for an ordinary visitor. It matters that this is set
// explicitly: Go's default User-Agent contains no crawler token, but relying on
// that would make every "human" assertion depend on a default staying benign.
const browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36"

// TestNginxIPv4OutageDrill pins the planned IPv4 unavailability signalling
// (draft-martin-retry-over-ipv6): during a window, IPv4 visitors get 503 plus
// Retry-Over-IPv6 and a body that explains it, while IPv6 visitors are served
// normally.
//
// Status codes are asserted explicitly, not just headers. The failure mode this
// guards is a silent 200 carrying correct signal headers — and the one that
// actually happened in development: error_page does an internal redirect that
// re-runs the server rewrite phase, so the gate fired a second time and, with
// recursive_error_pages off, nginx answered with its own 503 body and none of
// the headers. Asserting the body is therefore load-bearing. Skips when docker
// is unavailable.
func TestNginxIPv4OutageDrill(t *testing.T) {
	requireDocker(t)

	dir := t.TempDir()
	certDir := filepath.Join(dir, "cloudflare")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSelfSigned(t, certDir)

	// The helper page ships from frontend/public/, so it reaches the webroot
	// with the bundle rsync rather than with the vhost. Use the real file: a
	// build that stops emitting it turns the drill into a 404.
	helper, err := filepath.Abs(filepath.Join("..", "..", "..", "frontend", "public", "ipv4-unavailable.html"))
	if err != nil {
		t.Fatal(err)
	}
	helperBody, err := os.ReadFile(helper)
	if err != nil {
		t.Fatalf("helper page missing: %v", err)
	}
	root := filepath.Join(dir, "www")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		"index.html":            "<!doctype html><title>Why No IPv6</title>",
		"ipv4-unavailable.html": string(helperBody),
		"robots.txt":            "User-agent: *\nAllow: /\n",
		"sitemap.xml":           "<urlset/>",
		// Bait for the bypass case below. It has to be a real file: for a URI
		// with nothing behind it, try_files internally redirects to the shell,
		// that redirect re-runs the rewrite phase, and the gate fires again —
		// so an unanchored exemption would still answer 503 and the test would
		// prove nothing.
		"__ipv4-unavailable-x": "served despite the drill",
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	name := fmt.Sprintf("wni6-nginx-drill-test-%d", time.Now().UnixNano())
	run := exec.Command("docker", "run", "-d", "--rm", "--name", name,
		"-p", "127.0.0.1:0:443",
		"-v", writeVhost(t, dir, "on")+":/etc/nginx/conf.d/site.conf:ro",
		"-v", writeRealIPShim(t, dir)+":/etc/nginx/conf.d/00-realip.conf:ro",
		"-v", certDir+":/etc/ssl/cloudflare:ro",
		"-v", root+":/var/www/whynoipv6.com:ro",
		"nginx:alpine")
	if out, err := run.CombinedOutput(); err != nil {
		skipOrFail(t, "docker run failed (offline?): %v\n%s", err, out)
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
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       5 * time.Second,
	}
	// getAs drives the visitor's address family through CF-Connecting-IP,
	// exactly as Cloudflare does in production, and the crawler exemption
	// through User-Agent.
	getAs := func(path, clientIP, accept, ua string) (*http.Response, string) {
		t.Helper()
		var resp *http.Response
		var lastErr error
		for i := 0; i < 20; i++ {
			req, _ := http.NewRequest(http.MethodGet, "https://"+hostPort+path, nil)
			req.Host = "whynoipv6.com"
			req.Header.Set("CF-Connecting-IP", clientIP)
			req.Header.Set("Accept", accept)
			req.Header.Set("User-Agent", ua)
			resp, lastErr = client.Do(req)
			if lastErr == nil {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				return resp, string(body)
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Fatalf("GET %s as %s: %v", path, clientIP, lastErr)
		return nil, ""
	}
	get := func(path, clientIP, accept string) (*http.Response, string) {
		t.Helper()
		return getAs(path, clientIP, accept, browserUA)
	}

	const (
		v4       = "203.0.113.9"
		v6       = "2001:db8::1"
		acceptHT = "text/html,application/xhtml+xml"
		acceptPJ = "application/problem+json"
	)

	// Search engines sit the drill out. Googlebot is IPv4-almost-exclusively,
	// so without the exemption every window is a site-wide outage in Search
	// Console, and a monthly all-day 5xx is the shape that suppresses crawl
	// rate. Nothing here is a security boundary: the UA is spoofable, and
	// opting yourself out of a voluntary drill costs nobody anything.
	t.Run("search engines are served normally over IPv4", func(t *testing.T) {
		for name, ua := range map[string]string{
			"Googlebot": "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			"bingbot":   "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
			"Applebot":  "Mozilla/5.0 (compatible; Applebot/0.1; +http://www.apple.com/go/applebot)",
		} {
			resp, _ := getAs("/domains", v4, acceptHT, ua)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s: status = %d, want 200", name, resp.StatusCode)
			}
			if got := resp.Header.Get("Retry-Over-IPv6"); got != "" {
				t.Errorf("%s got the signal header %q", name, got)
			}
		}
	})

	// A 5xx on robots.txt does not read as "briefly unavailable" — Google
	// takes it as disallow-everything and stops crawling until it can fetch
	// one again. Gating it would turn a one-day drill into a deindexing risk,
	// so it is exempt for every client, crawler or not.
	t.Run("crawler control files are never gated", func(t *testing.T) {
		for _, path := range []string{"/robots.txt", "/sitemap.xml"} {
			resp, _ := get(path, v4, acceptHT)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s: status = %d, want 200 even during a window", path, resp.StatusCode)
			}
		}
	})

	t.Run("IPv4 gets the signal and the helper page", func(t *testing.T) {
		resp, body := get("/", v4, acceptHT)
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", resp.StatusCode)
		}
		if got := resp.Header.Get("Retry-Over-IPv6"); got != "?1" {
			t.Errorf("Retry-Over-IPv6 = %q, want %q", got, "?1")
		}
		if got := resp.Header.Get("Cache-Control"); got != "private, no-store" {
			t.Errorf("Cache-Control = %q, want %q", got, "private, no-store")
		}
		if resp.Header.Get("Retry-After") == "" {
			t.Error("Retry-After missing")
		}
		if got := resp.Header.Get("Retry-Over-IPv6-Token"); !strings.HasPrefix(got, `"`) {
			t.Errorf("Retry-Over-IPv6-Token = %q, want a quoted-string", got)
		}
		// nginx's own 503 page instead of ours means the error_page handler was
		// never reached — see the note on the rewrite phase above.
		if !strings.Contains(body, "IPv4") || strings.Contains(body, "503 Service Temporarily Unavailable") {
			t.Errorf("body is not the helper page (%d bytes): %.200q", len(body), body)
		}
	})

	t.Run("problem+json for machines", func(t *testing.T) {
		resp, body := get("/domains", v4, acceptPJ)
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
			t.Errorf("Content-Type = %q, want application/problem+json", ct)
		}
		var problem struct {
			Type                 string `json:"type"`
			Status               int    `json:"status"`
			IPv4UnavailableUntil string `json:"ipv4UnavailableUntil"`
		}
		if err := json.Unmarshal([]byte(body), &problem); err != nil {
			t.Fatalf("body is not JSON (%d bytes): %v: %.200q", len(body), err, body)
		}
		if problem.Type != "urn:ietf:params:problem:ipv4-unavailable" || problem.Status != 503 {
			t.Errorf("problem = %+v", problem)
		}
		// Built by interpolating a named capture into a map value, which nginx
		// -t cannot check: an empty string here means the capture did not
		// survive into the response.
		if _, err := time.Parse(time.RFC3339, problem.IPv4UnavailableUntil); err != nil {
			t.Errorf("ipv4UnavailableUntil = %q, want RFC 3339: %v", problem.IPv4UnavailableUntil, err)
		}
	})

	t.Run("IPv6 is served normally", func(t *testing.T) {
		resp, _ := get("/", v6, acceptHT)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		if got := resp.Header.Get("Retry-Over-IPv6"); got != "" {
			t.Errorf("Retry-Over-IPv6 = %q on IPv6, want it absent", got)
		}
	})

	t.Run("loopback is skipped for health checks", func(t *testing.T) {
		resp, _ := get("/", "127.0.0.1", acceptHT)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	// The helper page is only ever reached through error_page. Served directly
	// it would tell an IPv6 visitor they are on IPv4, at an indexable URL.
	t.Run("helper page is not a public URL", func(t *testing.T) {
		resp, _ := get("/ipv4-unavailable.html", v6, acceptHT)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	// The gate clears itself for the error_page handlers so the rewrite phase
	// does not fire twice. That exemption is anchored to the two real handler
	// URIs — an unanchored prefix would hand every IPv4 client a way to opt
	// out of the drill by guessing the stem.
	t.Run("the handler exemption cannot be used to bypass the gate", func(t *testing.T) {
		resp, body := get("/__ipv4-unavailable-x", v4, acceptHT)
		if resp.StatusCode == http.StatusOK {
			t.Errorf("bypassed the drill: 200 with %.60q", body)
		}
	})

	// The gate sits ahead of the canonicalisation rewrites in the rewrite
	// phase, so it must not have displaced them for everyone else.
	t.Run("canonical redirects survive on IPv6", func(t *testing.T) {
		resp, _ := get("/domain/reddit.com", v6, acceptHT)
		if resp.StatusCode != http.StatusMovedPermanently {
			t.Errorf("status = %d, want 301", resp.StatusCode)
		}
	})
}

// TestNginxIPv4OutageOff pins the resting state: with the switch off, nothing
// about the vhost changes for an IPv4 visitor. This is the rollback assertion —
// the drill has to be one line away from gone.
func TestNginxIPv4OutageOff(t *testing.T) {
	requireDocker(t)

	dir := t.TempDir()
	certDir := filepath.Join(dir, "cloudflare")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSelfSigned(t, certDir)
	root := filepath.Join(dir, "www")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html>shell"), 0o644); err != nil {
		t.Fatal(err)
	}

	name := fmt.Sprintf("wni6-nginx-nodrill-test-%d", time.Now().UnixNano())
	run := exec.Command("docker", "run", "-d", "--rm", "--name", name,
		"-p", "127.0.0.1:0:443",
		"-v", writeVhost(t, dir, "off")+":/etc/nginx/conf.d/site.conf:ro",
		"-v", writeRealIPShim(t, dir)+":/etc/nginx/conf.d/00-realip.conf:ro",
		"-v", certDir+":/etc/ssl/cloudflare:ro",
		"-v", root+":/var/www/whynoipv6.com:ro",
		"nginx:alpine")
	if out, err := run.CombinedOutput(); err != nil {
		skipOrFail(t, "docker run failed (offline?): %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

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
	var resp *http.Response
	for i := 0; i < 20; i++ {
		req, _ := http.NewRequest(http.MethodGet, "https://"+hostPort+"/", nil)
		req.Host = "whynoipv6.com"
		req.Header.Set("CF-Connecting-IP", "203.0.113.9")
		var err error
		if resp, err = client.Do(req); err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if resp == nil {
		t.Fatal("no response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 with the drill off", resp.StatusCode)
	}
	if got := resp.Header.Get("Retry-Over-IPv6"); got != "" {
		t.Errorf("Retry-Over-IPv6 = %q with the drill off, want it absent", got)
	}
}

// requireDocker gates the nginx tests on a docker daemon. Locally that is a
// skip; in CI (GitHub sets CI=true) a missing daemon or a failed `docker run`
// is a failure, or the vhost gate can stop running without anyone noticing.
func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		skipOrFail(t, "docker unavailable: %v", err)
	}
}

// skipOrFail skips outside CI and fails inside it.
func skipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}
