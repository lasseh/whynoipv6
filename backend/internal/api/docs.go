package api

import (
	"encoding/json"
	"net/http"

	"github.com/lasseh/whynoipv6/internal/api/gen"
)

// The §7 discoverability surface: the machine-readable contract at a stable
// path, a Redoc reader, and an llms.txt index. All meta routes — outside
// the OpenAPI document itself, like the health endpoints.

// getOpenAPIJSON serves the embedded contract as JSON.
func (s *Server) getOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	swagger, err := gen.GetSpec()
	if err != nil {
		InternalError(w, r, err)
		return
	}
	raw, err := json.Marshal(swagger)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// redocPage is a minimal Redoc reader over /openapi.json.
const redocPage = `<!DOCTYPE html>
<html>
<head>
  <title>WhyNoIPv6 API</title>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>body { margin: 0; padding: 0; }</style>
</head>
<body>
  <redoc spec-url="/openapi.json"></redoc>
  <script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
</body>
</html>
`

func (s *Server) getDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(redocPage))
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
- [Interactive reference](/docs): the same contract rendered with Redoc

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
