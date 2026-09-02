package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestBaseline (P4.1; 07 §1.8, §2.5, §2.7): problem+json on every error with
// status matching the HTTP line, no-store health endpoints, the /ip echo,
// CORS preflight, and the RealIP derivation.
func TestBaseline(t *testing.T) {
	// A nil service is fine for the routes under test except /readyz.
	router := NewRouter(nil, Options{})

	// Review issue 39: the drain, WriteTimeout and the middleware timeout
	// are the same number by construction. A drain shorter than the
	// request timeout truncates long responses on every deploy and leaves
	// the unit failed; cmd/api reads this constant for both.
	t.Run("request_timeout_is_one_constant", func(t *testing.T) {
		if RequestTimeout != 30*time.Second {
			t.Errorf("RequestTimeout = %s, want 30s (07 §1.6)", RequestTimeout)
		}
	})

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

	t.Run("method_not_allowed_problem", func(t *testing.T) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/ip", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
			t.Errorf("content-type = %q", ct)
		}
		if allow := rec.Header().Get("Allow"); allow != "GET, HEAD, OPTIONS" {
			t.Errorf("Allow = %q", allow)
		}
		var p Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Status != http.StatusMethodNotAllowed {
			t.Errorf("problem = %+v err=%v", p, err)
		}
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/check", nil))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST, OPTIONS" {
			t.Errorf("GET /check = %d Allow=%q", rec.Code, rec.Header().Get("Allow"))
		}
	})

	t.Run("head_serves_get", func(t *testing.T) {
		// §2.6: HEAD is implicit on every GET route. (The recorder keeps the
		// body the handler wrote; a real net/http server drops it for HEAD.)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/livez", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("HEAD /livez = %d, want 200", rec.Code)
		}
	})

	t.Run("ip_echo_real_ip", func(t *testing.T) {
		// §1.8.1: X-Real-IP wins; bracketless; family derived server-side.
		// Proxy headers are honored only from a trusted (loopback) peer.
		req := httptest.NewRequest(http.MethodGet, "/ip", nil)
		req.RemoteAddr = "127.0.0.1:9999"
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
		req.RemoteAddr = "127.0.0.1:9999"
		req.Header.Set("X-Forwarded-For", "192.0.2.7, 10.0.0.1")
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["ip"] != "192.0.2.7" || body["family"] != "ipv4" {
			t.Errorf("xff echo = %v", body)
		}
	})

	t.Run("ip_echo_untrusted_peer_ignores_headers", func(t *testing.T) {
		// A peer outside api.trusted_proxies cannot spoof its client IP
		// (httptest's default RemoteAddr 192.0.2.1 is untrusted).
		req := httptest.NewRequest(http.MethodGet, "/ip", nil)
		req.Header.Set("X-Real-IP", "2001:db8::7")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["ip"] != "192.0.2.1" || body["family"] != "ipv4" {
			t.Errorf("untrusted peer echo = %v", body)
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
// query fingerprint (07 §6.1). ETags are weak so nginx's gzip filter
// preserves them.
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
	if !strings.HasPrefix(etag, `W/"`) {
		t.Errorf("ETag %q must be weak (nginx gzip strips strong ETags)", etag)
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

// TestIfNoneMatch pins the RFC 9110 §13.1.2 comparison: comma-separated
// candidate lists, weak comparison, and the "*" wildcard.
func TestIfNoneMatch(t *testing.T) {
	const etag = `W/"g1-abc"`
	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{"empty", "", false},
		{"exact_weak", `W/"g1-abc"`, true},
		{"strong_candidate_weak_compares", `"g1-abc"`, true},
		{"list_match", `"other", W/"g1-abc", "another"`, true},
		{"star", "*", true},
		{"miss", `W/"g2-def"`, false},
		{"list_miss", `"a", "b"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ifNoneMatch(tt.header, etag); got != tt.want {
				t.Errorf("ifNoneMatch(%q, %q) = %v, want %v", tt.header, etag, got, tt.want)
			}
		})
	}
}

// TestProblemNotCacheable: an error emitted after a handler already set the
// public list cache headers must go out no-store with no ETag, so a CDN can
// never pin a problem+json body (RFC 9111 explicit-directive caching).
func TestProblemNotCacheable(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/domains", nil)
	rec := httptest.NewRecorder()
	if CacheList(rec, req, 20260707) {
		t.Fatal("unexpected 304")
	}
	InternalError(rec, req, errors.New("db blip"))
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("error Cache-Control = %q, want no-store", cc)
	}
	if etag := rec.Header().Get("ETag"); etag != "" {
		t.Errorf("error response must not carry an ETag, got %q", etag)
	}
}
