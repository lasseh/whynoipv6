package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/lasseh/whynoipv6/internal/api/gen"
)

// The §7 discoverability surface: the machine-readable contract at a stable
// path, a Scalar reference, and an llms.txt index. All meta routes — outside
// the OpenAPI document itself, like the health endpoints.

// openAPIJSON renders the embedded contract once per process: the document
// is static, and the kin-openapi load + marshal is not worth repeating for
// every cache-busting query-string variant.
var openAPIJSON = sync.OnceValues(func() ([]byte, error) {
	swagger, err := gen.GetSpec()
	if err != nil {
		return nil, err
	}
	return json.Marshal(swagger)
})

// getOpenAPIJSON serves the embedded contract as JSON.
func (s *Server) getOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	raw, err := openAPIJSON()
	if err != nil {
		InternalError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// scalarPage is a Scalar API reference over /openapi.json — a reader with a
// working request console. Try-it needs no proxy: the page is served from the
// same origin as the API it calls.
//
// The palette mirrors the frontend: the bg-zinc-900/text-slate-200 body in
// frontend/index.html plus the custom gray and fuchsia tokens in
// frontend/src/css/style.css. The site is dark-only, so the reference is
// forced dark and the toggle hidden — Scalar's light mode would match nothing.
// Scalar's own typeface is Inter, which is the site font, so fonts are left
// at their defaults.
const scalarPage = `<!DOCTYPE html>
<html lang="en">
<head>
  <title>WhyNoIPv6 API</title>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    body { margin: 0; padding: 0; }
    .dark-mode {
      --scalar-background-1: #18181b;
      --scalar-background-2: #25282c;
      --scalar-background-3: #33363a;
      --scalar-background-accent: #d946ef1f;
      --scalar-color-1: #e2e8f0;
      --scalar-color-2: #9ba9b4;
      --scalar-color-3: #707d86;
      --scalar-color-accent: #d946ef;
      --scalar-border-color: #33363a;
      --scalar-link-color: #d946ef;
      --scalar-link-color-hover: #e879f9;
      --scalar-button-1: #a21caf;
      --scalar-button-1-hover: #86198f;
      --scalar-button-1-color: #fff;
    }
  </style>
</head>
<body>
  <div id="app"></div>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.67.0/dist/browser/standalone.js"
          integrity="sha384-6c7Vmx+i0yi8gBbltn0x1cavD+zsMGw2xmXXVyacPJLIGBxwaVimW5TW0WiW17Ir"
          crossorigin="anonymous"></script>
  <script nonce="__CSP_NONCE__">
    Scalar.createApiReference('#app', {
      url: '/openapi.json',
      forceDarkModeState: 'dark',
      hideDarkModeToggle: true,
    })
  </script>
</body>
</html>
`

// docsCSP is the /docs policy (07 §1.4 erratum, review issue 45). The page
// is the one third-party-script surface on the api origin, and nginx's
// vhost adds only HSTS to proxied responses — the site's strict CSP does
// not cover api.whynoipv6.com — so the policy has to come from the handler.
//
// Each directive names what the Scalar page actually loads:
//
//	script-src   the SRI-pinned bundle on jsdelivr, plus a per-response
//	             nonce for the one inline createApiReference call. No
//	             'unsafe-inline': a nonce survives edits to that script,
//	             which a hash would not.
//	style-src    'unsafe-inline' is unavoidable — Scalar injects component
//	             styles at runtime, and the page carries the palette inline.
//	font-src     fonts.scalar.com, not the CDN: the bundle's default Inter
//	             and mono faces are fetched from Scalar's own font host.
//	             Found by loading the page under this policy, not by
//	             reading it — the page above says fonts are "left at their
//	             defaults", and the defaults are remote.
//	img-src      spec-referenced images and inline data: URIs.
//	connect-src  'self' only. This deliberately blocks the bundle's calls
//	             to api.scalar.com/vector/registry — a third-party lookup
//	             this reference has no use for. The page renders without
//	             them; only Scalar's own registry search goes dark.
//	worker-src   the bundle spawns workers from blob: URLs.
//
// default-src 'none' means anything not listed is refused, and base-uri /
// object-src / frame-ancestors close the rest. X-Frame-Options: deny from
// securityHeaders stays as the older-browser equivalent of frame-ancestors.
const docsCSP = "default-src 'none'; " +
	"script-src 'nonce-__CSP_NONCE__' https://cdn.jsdelivr.net; " +
	"style-src 'unsafe-inline' https://cdn.jsdelivr.net; " +
	"font-src https://fonts.scalar.com https://cdn.jsdelivr.net data:; " +
	"img-src 'self' data: https:; " +
	"connect-src 'self'; " +
	"worker-src blob:; " +
	"base-uri 'none'; object-src 'none'; frame-ancestors 'none'"

// cspNoncePlaceholder is substituted in both the page and the header, so
// they cannot disagree.
const cspNoncePlaceholder = "__CSP_NONCE__"

func (s *Server) getDocs(w http.ResponseWriter, r *http.Request) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		InternalError(w, r, err)
		return
	}
	nonce := base64.RawStdEncoding.EncodeToString(raw[:])

	// A per-response nonce makes the page uncacheable by shared caches:
	// a cached copy would carry someone else's nonce and its inline script
	// would be refused. private + no-store, not the 1h public max-age the
	// other meta routes use.
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", strings.ReplaceAll(docsCSP, cspNoncePlaceholder, nonce))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(strings.ReplaceAll(scalarPage, cspNoncePlaceholder, nonce)))
}

// llmsTxt is the documentation index for LLM consumers.
const llmsTxt = `# WhyNoIPv6 API

> IPv6 adoption tracking for the Tranco top-1M web: per-domain confirmed
> status across six dimensions (base, www, ns, mx, conn, resources),
> country/network/DNS-provider leaderboards, a public changelog of every
> confirmed transition, and an anonymous live checker.
> License: CC-BY-NC-4.0. No accounts, no API keys.

## Docs

- [OpenAPI contract](/openapi.json): every endpoint, schema, and error shape
- [Interactive reference](/docs): the same contract with a live request console

## Data

- [Domain leaderboard](/domains): filterable, keyset-paginated
- [Recent changes](/changelog): confirmed transitions, also as /changelog.atom
- [Adoption over time](/stats/overview): daily confirmed-state snapshots
- [Bulk datasets](/datasets): nightly CSV.gz + Parquet snapshots with manifest

## Conventions

- Collections use the {items, page, meta} envelope; time series use
  {points, meta}; errors are RFC 9457 application/problem+json.
- Pagination is keyset-only: follow page.next_cursor opaquely.
`

func (s *Server) getLLMsTxt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(llmsTxt))
}
