package api

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/cors"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// RequestTimeout is the longest a request may run (07 §1.6). Three things
// have to agree on it: the chi middleware that enforces it, the server's
// WriteTimeout, and cmd/api's shutdown drain — a drain shorter than this
// severs a legitimately-long request mid-body on every deploy (review
// issue 39), so it is one constant rather than three literals.
const RequestTimeout = 30 * time.Second

// Options are the serving knobs plumbed from the config registry (09-ops).
type Options struct {
	PublicBaseURL string // feed ids/links; default https://api.whynoipv6.com
	CSVMaxRows    int    // export.csv_max_rows; default 10000
	DatasetsDir   string // DATASETS_DIR; the manifest.json location (§5.3)

	// The §5.1/§6.3 live-check knobs.
	RateIPPerHour     int           // live_check.rate_ip_per_hour; default 10
	RateGlobalPerHour int           // live_check.rate_global_per_hour; default 500
	DedupeWindow      time.Duration // live_check.dedupe_window; default 1h
	LinkTTL           time.Duration // live_check.link_ttl; default 168h (§5.1.7)
	ResourcesEnabled  bool          // crawler.resources.enabled

	BadgeCacheTTL    time.Duration // badge.cache_ttl; default 24h
	ManifestCacheTTL time.Duration // datasets.manifest_cache_ttl; default 5m
	FeedRecentWindow int           // feed.recent_window; default 50 (≤0 falls back)

	TrustedProxies []string // api.trusted_proxies; CIDRs whose client-IP headers are honored (default loopback)
}

// Server carries the handler dependencies: the pool for the builder-built
// queries, the sqlc queries for everything else (the one data seam,
// 05-schema §10.3).
type Server struct {
	pool   *pgxpool.Pool
	q      *db.Queries
	opts   Options
	meta   metaSource // the list rim's freshness seam (list.go); pgMeta in production
	checks checkStore // the live-check data seam (checkstore.go); pgCheckStore in production
}

// NewRouter builds the chi router with the 07 §1.7 middleware order
// (outermost first): RealIP → RequestID → slog access log → Recoverer →
// Timeout(30s) → CORS → security headers. No trailing-slash redirection.
func NewRouter(pool *pgxpool.Pool, opts Options) http.Handler { //nolint:gocritic // one-shot config bag at startup; by-value keeps call sites simple
	s := &Server{pool: pool, q: db.New(pool), opts: opts}
	s.meta = pgMeta{q: s.q}
	s.checks = pgCheckStore{pool: pool, q: s.q}
	if s.opts.PublicBaseURL == "" {
		s.opts.PublicBaseURL = "https://api.whynoipv6.com"
	}
	if s.opts.CSVMaxRows == 0 {
		s.opts.CSVMaxRows = 10000
	}
	if s.opts.DatasetsDir == "" {
		s.opts.DatasetsDir = "/var/lib/whynoipv6/datasets"
	}
	if s.opts.RateIPPerHour == 0 {
		s.opts.RateIPPerHour = 10
	}
	if s.opts.RateGlobalPerHour == 0 {
		s.opts.RateGlobalPerHour = 500
	}
	if s.opts.DedupeWindow == 0 {
		s.opts.DedupeWindow = time.Hour
	}
	if s.opts.LinkTTL == 0 {
		s.opts.LinkTTL = 168 * time.Hour
	}
	if s.opts.BadgeCacheTTL == 0 {
		s.opts.BadgeCacheTTL = 24 * time.Hour
	}
	if s.opts.ManifestCacheTTL == 0 {
		s.opts.ManifestCacheTTL = 5 * time.Minute
	}
	if s.opts.FeedRecentWindow <= 0 {
		s.opts.FeedRecentWindow = 50
	}
	if len(s.opts.TrustedProxies) == 0 {
		s.opts.TrustedProxies = []string{"127.0.0.0/8", "::1/128"}
	}
	trusted := make([]netip.Prefix, 0, len(s.opts.TrustedProxies))
	for _, c := range s.opts.TrustedProxies {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			slog.Warn("ignoring invalid api.trusted_proxies CIDR", "cidr", c, "err", err.Error())
			continue
		}
		trusted = append(trusted, p)
	}
	r := chi.NewRouter()

	r.Use(realIP(trusted))
	r.Use(middleware.RequestID)
	r.Use(accessLog)
	r.Use(recoverer)
	r.Use(middleware.Timeout(RequestTimeout))
	r.Use(cors.New(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPost},
		AllowedHeaders:   []string{"Accept", "Content-Type"},
		ExposedHeaders:   []string{"ETag", "Link", "Retry-After", "RateLimit", "RateLimit-Policy"},
		AllowCredentials: false,
		MaxAge:           300,
	}).Handler)
	r.Use(securityHeaders)
	// HEAD is implicit on every GET (§2.6): chi routes only the registered
	// method, so without this a HEAD lands on the 405 handler below.
	r.Use(middleware.GetHead)

	// Health endpoints: root, outside the OpenAPI document, no-store (§2.7).
	r.Get("/livez", s.livez)
	r.Get("/readyz", s.readyz)

	// /ip — client-IP echo (§4.12).
	r.Get("/ip", s.ip)

	// Discoverability (§7): the contract, its reader, and the LLM index —
	// meta routes outside the OpenAPI document like the health endpoints.
	r.Get("/openapi.json", s.getOpenAPIJSON)
	r.Get("/docs", s.getDocs)
	r.Get("/llms.txt", s.getLLMsTxt)

	// The /domains list family + tier presets (§4.2/§4.4) and the domain
	// detail (§4.3).
	r.Get("/domains", s.listDomains)
	r.Get("/domains/{host}", s.getDomain)
	r.Get("/domains/{host}/subdomains", s.listSubdomains)
	r.Get("/domains/{host}/resources", s.listDomainResources)
	r.Get("/heroes", s.listHeroes)
	r.Get("/sinners", s.listSinners)
	r.Get("/saints", s.listSaints)
	r.Get("/almost-heroes", s.listAlmostHeroes)
	r.Get("/shame", s.listShame)

	// Country / ASN / DNS-provider pivots (§4.5/§4.6).
	r.Get("/countries", s.listCountries)
	r.Get("/countries/{code}", s.getCountry)
	r.Get("/countries/{code}/domains", s.listCountryDomains)
	r.Get("/asns", s.listASNs)
	r.Get("/asns/{number}", s.getASN)
	r.Get("/asns/{number}/domains", s.listASNDomains)
	r.Get("/providers", s.listProviders)
	r.Get("/hosting", s.listHosting)
	r.Get("/providers/{id}", s.getProvider)
	r.Get("/providers/{id}/domains", s.listProviderDomains)

	// Campaigns (§4.7), the mandate view (§5.6), resource deps (§4.11).
	r.Get("/campaigns", s.listCampaigns)
	r.Get("/mandates", s.listMandates)
	r.Get("/campaigns/{uuid}", s.getCampaign)
	r.Get("/campaigns/{uuid}/domains", s.listCampaignDomains)
	r.Get("/resources/{host}", s.getResource)
	r.Get("/resources/{host}/dependents", s.listResourceDependents)

	// The feed matrix (§5.4): 4 scopes × Atom + JSON Feed suffix URLs.
	r.Get("/changelog.atom", s.globalAtom)
	r.Get("/changelog.feed.json", s.globalJSONFeed)
	r.Get("/domains/{host}/changelog.atom", s.domainAtom)
	r.Get("/domains/{host}/changelog.feed.json", s.domainJSONFeed)
	r.Get("/countries/{code}/changelog.atom", s.countryAtom)
	r.Get("/countries/{code}/changelog.feed.json", s.countryJSONFeed)
	r.Get("/campaigns/{uuid}/changelog.atom", s.campaignAtom)
	r.Get("/campaigns/{uuid}/changelog.feed.json", s.campaignJSONFeed)

	// The embeddable badge (§5.2): .svg/.json are route suffixes; a
	// suffix-less path is a route-miss 404. Hosts contain dots, so the
	// suffix is split in the dispatcher, not the chi pattern.
	r.Get("/badge/{file}", s.getBadge)

	// The datasets index (§5.3); the files themselves are nginx-served.
	r.Get("/datasets", s.getDatasets)

	// The live check (§5.1) — the only write path: async enqueue + poll.
	r.Post("/check", s.postCheck)
	r.Get("/check/latest", s.getLatestCheck) // static beats the {id} pattern in chi
	r.Get("/check/{id}", s.getCheck)

	// Stats / adoption-over-time (§4.10) — confirmed-state snapshots only.
	r.Get("/stats/overview", s.getStatsOverview)
	r.Get("/stats/crawler", s.getCrawlerStats) // telemetry, not confirmed state
	r.Get("/stats/networks", s.getNetworkStats)
	r.Get("/stats/changes", s.getChangeStats) // changelog cache class, not stats
	r.Get("/countries/{code}/stats", s.getCountryStats)
	r.Get("/campaigns/{uuid}/stats", s.getCampaignStats)
	r.Get("/asns/{number}/stats", s.getASNStats)

	// The changelog trust surface (§4.8) + per-domain history (§4.9).
	r.Get("/changelog", s.listChangelog)
	r.Get("/domains/{host}/changelog", s.listDomainChangelog)
	r.Get("/domains/{host}/history", s.getDomainHistory)
	r.Get("/countries/{code}/changelog", s.listCountryChangelog)
	r.Get("/campaigns/{uuid}/changelog", s.listCampaignChangelog)
	r.Get("/campaigns/{uuid}/domains/{host}/changelog", s.listCampaignDomainChangelog)

	// Handlers are hand-written against openapi.yaml; TestOpenAPIRouteCoverage
	// is the drift gate holding this route inventory to the contract.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		NotFound(w, r, "Not found", "No such resource.")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		// RFC 9110 §15.5.6: a 405 carries Allow. POST /check is the only
		// write route; every other path is GET with implicit HEAD/OPTIONS.
		allow := "GET, HEAD, OPTIONS"
		if r.URL.Path == "/check" {
			allow = "POST, OPTIONS"
		}
		w.Header().Set("Allow", allow)
		WriteProblem(w, r, Problem{Type: problemBase + "invalid-parameter",
			Title: "Method not allowed", Status: http.StatusMethodNotAllowed})
	})
	return r
}

// realIP derives the client address from the proxy headers (07 §1.2):
// X-Real-IP first, else the first X-Forwarded-For entry, else the peer
// address — but only when the transport peer is inside a trusted-proxy
// CIDR (api.trusted_proxies; loopback by default, matching the
// nginx-in-front deployment). Any other peer keeps its socket address, so
// a directly-reachable API cannot be fed spoofed client IPs. (chi's
// middleware.RealIP is deprecated upstream; this is the spec's exact
// rule, implemented locally.)
func realIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if trustedPeer(r.RemoteAddr, trusted) {
				if rip := r.Header.Get("X-Real-IP"); rip != "" {
					r.RemoteAddr = rip
				} else if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
					first, _, _ := strings.Cut(xff, ",")
					r.RemoteAddr = strings.TrimSpace(first)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// trustedPeer reports whether the pre-rewrite socket address falls inside
// one of the trusted-proxy prefixes.
func trustedPeer(remoteAddr string, trusted []netip.Prefix) bool {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
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
// endpoints are excluded. Debug per request — the full access log is noise at
// the default info level (and would flood the Taillight shipper) — with 5xx
// raised to warn so server errors stay visible.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/livez" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		level := slog.LevelDebug
		if ww.Status() >= 500 {
			level = slog.LevelWarn
		}
		slog.Log(r.Context(), level, "request",
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
				InternalError(w, r, nil) // the panic is already logged above
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
