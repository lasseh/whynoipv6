package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBaseline (P4.1; 07 §1.8, §2.5, §2.7): problem+json on every error with
// status matching the HTTP line, no-store health endpoints, the /ip echo,
// CORS preflight, and the RealIP derivation.
func TestBaseline(t *testing.T) {
	// A nil service is fine for the routes under test except /readyz.
	router := NewRouter(nil, Options{})

	t.Run("livez_no_store", func(t *testing.T) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/livez", nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("livez = %d %q", rec.Code, rec.Header().Get("Cache-Control"))
		}
	})

	t.Run("not_found_is_problem_json", func(t *testing.T) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
			t.Errorf("content-type = %q", ct)
		}
		var p Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if p.Status != http.StatusNotFound || !strings.HasSuffix(p.Type, "/not-found") || p.Instance != "/nope" {
			t.Errorf("problem = %+v", p)
		}
	})

	t.Run("ip_echo_real_ip", func(t *testing.T) {
		// §1.8.1: X-Real-IP wins; bracketless; family derived server-side.
		req := httptest.NewRequest(http.MethodGet, "/ip", nil)
		req.Header.Set("X-Real-IP", "2001:db8::7")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["ip"] != "2001:db8::7" || body["family"] != "ipv6" {
			t.Errorf("ip echo = %v", body)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("ip cache-control = %q", rec.Header().Get("Cache-Control"))
		}

		req = httptest.NewRequest(http.MethodGet, "/ip", nil)
		req.Header.Set("X-Forwarded-For", "192.0.2.7, 10.0.0.1")
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["ip"] != "192.0.2.7" || body["family"] != "ipv4" {
			t.Errorf("xff echo = %v", body)
		}
	})

	t.Run("cors_preflight", func(t *testing.T) {
		// §1.8.2: OPTIONS preflight for POST /check.
		req := httptest.NewRequest(http.MethodOptions, "/check", nil)
		req.Header.Set("Origin", "https://whynoipv6.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code >= 300 {
			t.Fatalf("preflight status = %d", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") == "" {
			t.Error("missing Access-Control-Allow-Origin")
		}
		if !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "POST") {
			t.Errorf("allow-methods = %q", rec.Header().Get("Access-Control-Allow-Methods"))
		}
	})

	t.Run("security_headers", func(t *testing.T) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ip", nil))
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
			rec.Header().Get("X-Frame-Options") != "deny" {
			t.Errorf("security headers missing: %v", rec.Header())
		}
	})
}

// TestCacheHelpers: the generation-seeded ETag honors If-None-Match with a
// query fingerprint (07 §6.1).
func TestCacheHelpers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/domains?class=hero", nil)
	rec := httptest.NewRecorder()
	if CacheList(rec, req, 20260707) {
		t.Fatal("first request must not 304")
	}
	etag := rec.Header().Get("ETag")
	if etag == "" || !strings.Contains(rec.Header().Get("Cache-Control"), "s-maxage") {
		t.Fatalf("headers = %v", rec.Header())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/domains?class=hero", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	if !CacheList(rec2, req2, 20260707) || rec2.Code != http.StatusNotModified {
		t.Errorf("conditional GET should 304: code=%d", rec2.Code)
	}

	// A different query fingerprint must not share the ETag.
	req3 := httptest.NewRequest(http.MethodGet, "/domains?class=sinner", nil)
	req3.Header.Set("If-None-Match", etag)
	rec3 := httptest.NewRecorder()
	if CacheList(rec3, req3, 20260707) {
		t.Error("different query must have a different ETag")
	}
}
