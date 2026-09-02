package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/go-chi/chi/v5"
)

// specPaths loads path+method pairs from openapi/openapi.yaml.
func specPaths(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "openapi", "openapi.yaml"))
	if err != nil {
		t.Fatalf("openapi.yaml unavailable: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for p, ops := range doc.Paths {
		for method := range ops {
			switch method {
			case "get", "post", "put", "patch", "delete":
				out[strings.ToUpper(method)+" "+p] = true
			}
		}
	}
	if len(out) < 30 {
		t.Fatalf("parsed only %d operations — spec structure changed?", len(out))
	}
	return out
}

// chiPaths walks the real router.
func chiPaths(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	router, ok := NewRouter(nil, Options{}).(chi.Router)
	if !ok {
		t.Fatal("router is not chi.Router")
	}
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out[method+" "+strings.TrimSuffix(route, "/")] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// operationalRoutes exist on the wire but stay outside the public OpenAPI
// document (07 §2.1 health; §7 discoverability meta routes).
var operationalRoutes = map[string]bool{
	"GET /livez":        true,
	"GET /readyz":       true,
	"GET /openapi.json": true,
	"GET /docs":         true,
	"GET /llms.txt":     true,
}

// specToChi maps documented paths onto their chi registration where the
// wire shape and the router pattern differ (the badge suffix dispatch).
func specToChi(p string) string {
	switch p {
	case "GET /badge/{host}.svg", "GET /badge/{host}.json":
		return "GET /badge/{file}"
	}
	return p
}

// TestOpenAPIRouteCoverage is the contract↔code half of the drift gate:
// every documented operation is registered, every registered route is
// documented, and the cut GET /diff stays absent (P4.14 acceptance).
func TestOpenAPIRouteCoverage(t *testing.T) {
	spec := specPaths(t)
	routes := chiPaths(t)

	for op := range spec {
		if !routes[specToChi(op)] {
			t.Errorf("documented operation %q is not registered in the router", op)
		}
	}

	// Reverse: every registered route must be documented (badge dispatch
	// maps back to its two documented suffix paths).
	documentedChi := map[string]bool{}
	for op := range spec {
		documentedChi[specToChi(op)] = true
	}
	for route := range routes {
		if operationalRoutes[route] {
			continue
		}
		if !documentedChi[route] {
			t.Errorf("registered route %q is undocumented in openapi.yaml", route)
		}
	}

	if spec["GET /diff"] {
		t.Error("GET /diff was cut (OPEN-7) and must stay out of openapi.yaml")
	}
}

// TestDiscoverability (07 §7): the embedded contract, Scalar reference, and
// llms.txt are served DB-free.
func TestDiscoverability(t *testing.T) {
	srv := httptest.NewServer(NewRouter(nil, Options{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var doc struct {
		OpenAPI string         `json:"openapi"`
		Paths   map[string]any `json:"paths"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || doc.OpenAPI != "3.0.3" || len(doc.Paths) < 30 {
		t.Errorf("/openapi.json: %d openapi=%q paths=%d", resp.StatusCode, doc.OpenAPI, len(doc.Paths))
	}

	for path, wantType := range map[string]string{
		"/docs":     "text/html",
		"/llms.txt": "text/plain",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), wantType) {
			t.Errorf("%s: %d %s", path, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
}

// TestDocsCSP (07 §1.4 erratum, review issue 45): /docs is the one
// third-party-script surface on the api origin, and nginx adds only HSTS to
// proxied responses, so the policy has to come from the handler. The origin
// list was verified by loading the page under it in a browser — the
// remaining CSP blocks are Scalar's own api.scalar.com registry calls,
// which are refused on purpose.
func TestDocsCSP(t *testing.T) {
	srv := httptest.NewServer(NewRouter(nil, Options{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/docs")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	csp := resp.Header.Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'none'",
		"https://cdn.jsdelivr.net", // the SRI-pinned bundle
		"https://fonts.scalar.com", // the bundle's default Inter/mono faces
		"connect-src 'self'",       // try-it is same-origin; scalar's registry is not
		"base-uri 'none'",
		"object-src 'none'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q is missing %q", csp, want)
		}
	}
	// style-src needs 'unsafe-inline' (Scalar injects styles at runtime);
	// script-src must not have it — that is what the nonce is for.
	_, scriptSrc, ok := strings.Cut(csp, "script-src ")
	if !ok {
		t.Fatalf("CSP %q has no script-src", csp)
	}
	scriptSrc, _, _ = strings.Cut(scriptSrc, ";")
	if strings.Contains(scriptSrc, "unsafe-inline") || strings.Contains(scriptSrc, "unsafe-eval") {
		t.Errorf("script-src %q relaxes inline script: the page uses a nonce", scriptSrc)
	}

	// The nonce is per-response and must appear in both the header and the
	// page, or the inline createApiReference call is refused.
	_, after, ok := strings.Cut(csp, "'nonce-")
	if !ok {
		t.Fatalf("CSP %q carries no nonce", csp)
	}
	nonce, _, _ := strings.Cut(after, "'")
	if len(nonce) < 16 {
		t.Errorf("nonce %q is too short", nonce)
	}
	if !strings.Contains(string(body), `<script nonce="`+nonce+`">`) {
		t.Error("the page's inline script does not carry the header's nonce")
	}
	if strings.Contains(string(body), cspNoncePlaceholder) {
		t.Error("the nonce placeholder survived into the served page")
	}

	// A shared cache would hand a second visitor the first one's nonce.
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q: a per-response nonce must not be cached", cc)
	}

	// The nonce is fresh per response.
	resp2, err := http.Get(srv.URL + "/docs")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if resp2.Header.Get("Content-Security-Policy") == csp {
		t.Error("two responses shared a nonce")
	}
}
