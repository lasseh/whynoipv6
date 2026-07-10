package api

import (
	"net/http"
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
		t.Skipf("openapi.yaml unavailable: %v", err)
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
	router, ok := NewRouter(nil).(chi.Router)
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
// document (07 §2.1).
var operationalRoutes = map[string]bool{
	"GET /livez":  true,
	"GET /readyz": true,
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
