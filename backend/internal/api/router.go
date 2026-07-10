package api

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/cors"

	"github.com/lasseh/whynoipv6/internal/service"
)

// Server carries the handler dependencies.
type Server struct {
	svc *service.Service
}

// NewRouter builds the chi router with the 07 §1.7 middleware order
// (outermost first): RealIP → RequestID → slog access log → Recoverer →
// Timeout(30s) → CORS → security headers. No trailing-slash redirection.
func NewRouter(svc *service.Service) http.Handler {
	s := &Server{svc: svc}
	r := chi.NewRouter()

	r.Use(realIP)
	r.Use(middleware.RequestID)
	r.Use(accessLog)
	r.Use(recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cors.New(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPost},
		AllowedHeaders:   []string{"Accept", "Content-Type"},
		ExposedHeaders:   []string{"ETag", "Link", "Retry-After", "RateLimit", "RateLimit-Policy"},
		AllowCredentials: false,
		MaxAge:           300,
	}).Handler)
	r.Use(securityHeaders)

	// Health endpoints: root, outside the OpenAPI document, no-store (§2.7).
	r.Get("/livez", s.livez)
	r.Get("/readyz", s.readyz)

	// /ip — client-IP echo (§4.12).
	r.Get("/ip", s.ip)

	// The /domains list family + tier presets (§4.2/§4.4) and the domain
	// detail (§4.3).
	r.Get("/domains", s.listDomains)
	r.Get("/domains/{host}", s.getDomain)
	r.Get("/domains/{host}/subdomains", s.listSubdomains)
	r.Get("/domains/{host}/resources", s.listDomainResources)
	r.Get("/heroes", s.listHeroes)
	r.Get("/sinners", s.listSinners)
	r.Get("/gold", s.listGold)
	r.Get("/almost", s.listAlmost)
	r.Get("/mail", s.listMail)
	r.Get("/shame", s.listShame)

	// Country / ASN / DNS-provider pivots (§4.5/§4.6).
	r.Get("/countries", s.listCountries)
	r.Get("/countries/{code}", s.getCountry)
	r.Get("/countries/{code}/domains", s.listCountryDomains)
	r.Get("/asns", s.listASNs)
	r.Get("/asns/{number}", s.getASN)
	r.Get("/asns/{number}/domains", s.listASNDomains)
	r.Get("/providers", s.listProviders)
	r.Get("/providers/{id}", s.getProvider)
	r.Get("/providers/{id}/domains", s.listProviderDomains)

	// Campaigns (§4.7) and resource dependencies (§4.11).
	r.Get("/campaigns", s.listCampaigns)
	r.Get("/campaigns/{uuid}", s.getCampaign)
	r.Get("/campaigns/{uuid}/domains", s.listCampaignDomains)
	r.Get("/resources/{host}", s.getResource)
	r.Get("/resources/{host}/dependents", s.listResourceDependents)

	// Route inventory grows with the endpoint tasks (P4.3 onward), each
	// implementing the generated strict-server interface.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		NotFound(w, r, "Not found", "No such resource.")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		WriteProblem(w, r, Problem{Type: problemBase + "invalid-parameter",
			Title: "Method not allowed", Status: http.StatusMethodNotAllowed})
	})
	return r
}

// realIP derives the client address from the trusted proxy headers
// (07 §1.2): X-Real-IP first, else the first X-Forwarded-For entry, else the
// peer address. Trusting these is safe only because API_LISTEN defaults to
// loopback behind nginx. (chi's middleware.RealIP is deprecated upstream;
// this is the spec's exact rule, implemented locally.)
func realIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rip := r.Header.Get("X-Real-IP"); rip != "" {
			r.RemoteAddr = rip
		} else if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			r.RemoteAddr = strings.TrimSpace(first)
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets the §1.4 defaults on every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "deny")
		next.ServeHTTP(w, r)
	})
}

// accessLog is the small custom slog request logger (09-ops §13); the health
// endpoints are excluded.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/livez" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("request",
			"request_id", middleware.GetReqID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_ip", clientIP(r),
		)
	})
}

// recoverer converts panics into RFC 9457 500s and logs them at error level.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil && rec != http.ErrAbortHandler { //nolint:errorlint // sentinel per net/http docs
				slog.Error("handler panic", "panic", rec, "path", r.URL.Path)
				InternalError(w, r)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// clientIP is the RealIP-derived address without the port (the single source
// of truth for /ip and the rate limiter — 07 §1.2).
func clientIP(r *http.Request) string {
	addr := r.RemoteAddr
	// RemoteAddr may be host:port or a bare address (after RealIP rewrote it).
	if i := strings.LastIndexByte(addr, ':'); i > 0 && strings.Count(addr, ":") == 1 {
		return addr[:i] // IPv4 host:port
	}
	if strings.HasPrefix(addr, "[") {
		if i := strings.LastIndexByte(addr, ']'); i > 0 {
			return addr[1:i] // bracketed IPv6 host:port
		}
	}
	return addr
}
