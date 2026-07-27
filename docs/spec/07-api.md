# 07 — HTTP API Contract

_Status: Round 3.0 — API redesign folded in (docs/history/api-design-research.md, decisions 2026-07-09): clean root API, keyset pagination, RFC 9457, no legacy compat, no history import._

**Purpose:** The complete, self-contained HTTP contract of the public API — a read-only, anonymous, **unversioned** JSON API served at the **root of `api.whynoipv6.com`**. It serves the *real* WhyNoIPv6 data model (the 4-value confirmed `ipv6_status` per dimension, the `classification` enum, `class_flags[]`, `saint`, and the `*_since` provenance timestamps) directly — no projection, no message rendering, no legacy compatibility layer. Every list is a keyset/cursor-paginated collection over an object envelope; every error is RFC 9457 `application/problem+json`; the whole surface is OpenAPI-3.0.3-first. An implementer must be able to build `internal/api` and `openapi/openapi.yaml` from this file alone, using the schema in 05-schema.md and the shared engine mapper in 02-observation-model.md.

**Deliverables:**
- `internal/api/` — chi router + middleware stack (`router.go`), one handler file per route group (`domain.go`, `country.go`, `asn.go` incl. the DNS-provider `provider.go` league table (+ the hosting-tag `?hosting=` filter, §4.6), `campaign.go`, `changelog.go`, `stats.go`, `resource.go`, `check.go`, `badge.go`, `datasets.go`, `feed.go`, `diff.go`, `mandate.go`, `misc.go`), shared helpers (`cursor.go` — the opaque keyset cursor codec and the three seek orderings; `problem.go` — the RFC 9457 problem writer; `http.go` — envelope/pagination/negotiation helpers), and generated code in `internal/api/gen/` (oapi-codegen chi + strict-server output, committed). There is **no** `legacy.go` and **no** `uuid.go`/shortuuid codec.
- `openapi/openapi.yaml` — the hand-authored OpenAPI 3.0.3 source of truth for every endpoint in this file (§7).
- The live-check **contract** (§5.1): POST/GET envelopes, dedupe, consumer/reaper/retention SQL. (The consumer goroutines run inside `cmd/crawler` — process placement, pool wiring, and shutdown are owned by 04-lifecycle-scheduling.md.)
- Nginx location blocks for the API vhost, the change feeds, and `/datasets/` (deployed via 09-ops.md).

**Companion files:** 05-schema.md (every table/column referenced here — this file contains **no DDL**), 04-lifecycle-scheduling.md (check-job consumer placement, frontier scheduling touched by live-check re-entry, the nightly dataset/stats jobs), 02-observation-model.md (the `conn` composition table and the shared engine→public dimension mapper the §5.1 live-check reader imports), 00-overview.md (sizing-constants table, monorepo layout), 09-ops.md (consolidated config-key registry, systemd/nginx deploy, the endpoint-class caching + rate-limit vhost), 10-testing.md (native contract vectors: keyset-cursor codec, RFC 9457 shapes, Atom/JSON-Feed serializers, badge golden SVGs, the `manifest.json`/`datapackage.json` schemas, and the confirmed-state reconstruction).

Reference repos: the crawler engine and the shared `internal/domain` canonicalizer (`Canonicalize`), the `internal/crawler` result mapper (`MapLiveResult`).

---

## 1. Server baseline (applies to every endpoint)

### 1.1 Listen address

Config key `API_LISTEN` (string, default `[::1]:8080`; registry: 09-ops.md). The API binds IPv6 loopback **by design**: it is always fronted by nginx, which terminates TLS and is the only process that can reach it. Override to `:8080` / `0.0.0.0:8080` only for docker-compose/dev.

### 1.2 Real client IP

Because the bind is loopback-only, every request arrives from nginx and the peer address is useless. Apply chi `middleware.RealIP` **first** in the chain: set the request's remote address from `X-Real-IP` if present, else the first entry of `X-Forwarded-For`, else the peer address. This derived address is the **single source of truth** for (a) the `GET /ip` echo body (§4.12) and (b) `check_job.requester_ip` in the §5.1 rate limiter (keyed on the /64 prefix, §6.3). Operator caveat (state in README): trusting these headers is safe only because the default bind is unreachable except via the local proxy; if `API_LISTEN` is opened to a non-loopback interface without a trusted proxy, per-IP rate limits become spoofable.

Required nginx location config (deployed per 09-ops.md):

```nginx
proxy_set_header X-Real-IP        $remote_addr;
proxy_set_header X-Forwarded-For  $proxy_add_x_forwarded_for;
proxy_set_header Host             $host;
```

### 1.3 CORS

The frontend is cross-origin (whynoipv6.com → api.whynoipv6.com). rs/cors (or a chi-compatible equivalent) with all-origin settings plus `POST` (needed by `POST /check`):

- AllowedOrigins: `https://*`, `http://*` (allow-all; API is public and anonymous)
- AllowedMethods: `GET`, `HEAD`, `OPTIONS`, `POST`
- AllowedHeaders: `Accept`, `Content-Type`
- ExposedHeaders: `ETag`, `Link`, `Retry-After`, `RateLimit`, `RateLimit-Policy`
- AllowCredentials: `false`
- MaxAge: `300`

### 1.4 Default headers

All JSON responses: `Content-Type: application/json` (overridden by `GET /badge/{host}.svg` → `image/svg+xml`, `GET /badge/{host}.json` → `application/json`, the feeds → `application/atom+xml` / `application/feed+json`, and CSV → `text/csv`; dataset files are served by nginx, not the API), plus `X-Content-Type-Options: nosniff` and `X-Frame-Options: deny`. Parity assertions test the media-type prefix only (`application/json`), never a `charset` parameter — either form is conformant.

### 1.5 Cache-Control

The blanket `no-cache, no-store` policy is **deleted**. Cache-Control is set **per endpoint class** — see §6.1 for the full table (cache-first `public` + `s-maxage` for the daily-batch read surface; `no-store` only for `POST /check`, the in-flight poll, `/ip`, and health). nginx applies the matching vhost policy (09-ops.md).

### 1.6 Timeouts & graceful shutdown

`http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second}`; per-request `middleware.Timeout(30 * time.Second)`. Graceful shutdown on SIGINT/SIGTERM: `server.Shutdown(ctx)` with a 15 s drain budget. `POST /check` is async job+poll (§5.1), so no handler legitimately exceeds 30 s.

### 1.7 Middleware order (outermost first)

RealIP → RequestID → slog request logger → Recoverer → Timeout(30 s) → CORS → security/content-type headers → per-route Cache-Control (§6.1). No trailing-slash redirection middleware; routes match exactly as written. Logging follows the shared slog conventions (design §11.5; registry of log keys in 09-ops.md).

### 1.8 Baseline acceptance criteria

(Fixture definitions live in 10-testing.md.)
1. `GET /ip` with header `X-Real-IP: 2001:db8::7` returns `{"ip":"2001:db8::7","family":"ipv6"}` — not `::1` (guards the visitor banner and the §5.1 per-IP bucket); the address is bracketless and `family` is derived server-side (§4.12).
2. `OPTIONS /check` preflight with `Origin: https://whynoipv6.com` and `Access-Control-Request-Method: POST` returns 2xx with `Access-Control-Allow-Origin` and `POST` in `Access-Control-Allow-Methods`.
3. Two `POST /check` requests with different `X-Real-IP` values in different /64 prefixes consume different rate-limit buckets (§6.3).

---

## 2. Core conventions

### 2.1 Base URL — no version segment

**Decision: no URL version segment.** All data endpoints sit at the **root of `api.whynoipv6.com`** — the `api.` subdomain already names the role, so there is **no `/v1`** and no doubled `/api/v1`. Real URLs are `https://api.whynoipv6.com/domains/{host}`. The API is frontend-facing with **no external / third-party consumers today**; versioning for outside users is speculative machinery. Changes stay additive by discipline (new fields, new endpoints, new optional query params); a breaking change is a project decision, not a URL concern. If third-party consumers ever appear, a version segment or an `Accept`-header version can be introduced then (an additive move). Operational endpoints (`/livez`, `/readyz`, §2.7) and the static `/datasets/` tree live at the **root** and outside the public OpenAPI document.

*Rejected — URL-path `/v1` versioning* (premature for a no-consumer API; reintroducing a version segment later is additive) and *media-type / `Accept`-header versioning* (optimized for a large multi-consumer mesh; harder to test, uncacheable-by-URL, buys nothing today).

### 2.2 Resource naming

Lowercase **plural** collection nouns, **verb-free**, hierarchy as `/collection/{id}/sub-collection`, kebab-case for any multi-word segment. Item access by **stable natural id**:

| Resource | Collection | Item | Natural id |
|---|---|---|---|
| Domain | `/domains` | `/domains/{host}` | `host` (lowercase punycode FQDN) |
| Country | `/countries` | `/countries/{code}` | ISO-ish `CHAR(2)`, `UN` sentinel |
| ASN / network | `/asns` | `/asns/{number}` | `BIGINT` AS number, `0`=Unknown |
| DNS provider | `/providers` | `/providers/{id}` | `dns_provider.id` (§4.6, OPEN-4) |
| Campaign | `/campaigns` | `/campaigns/{uuid}` | **raw canonical UUID** (OPEN-1) |
| Resource host | `/resources` | `/resources/{host}` | canonicalized host |
| Changelog | `/changelog` | — (feed; addressed by scope + cursor) | — |

Sub-collections: `/countries/{code}/domains`, `/asns/{number}/domains`, `/providers/{id}/domains`, `/campaigns/{uuid}/domains`, `/domains/{host}/subdomains`, `/domains/{host}/resources`, `/resources/{host}/dependents`, `/domains/{host}/changelog`, `/domains/{host}/history`.

The classification tiers are promoted to their own **short, canonical plural collection paths** — the browse URLs the frontend links to, each a **preset filtered view over the `/domains` leaderboard** (§4.4):

| Tier collection | Preset over `/domains` |
|---|---|
| `/heroes` | `class=hero` |
| `/sinners` | `class=sinner` |
| `/saints` | `saint=true` |

(**Decision, ADR 0003:** the former `/gold` tier is renamed `/saints`, and the `/almost` + `/mail` tier paths are removed — the partial and mail views are spelled `/domains?class=partial` and `/domains?class=hero&mx=supported`. This also retires the "almost there"/`partial` one-class-two-names alias.)

Each shares the exact same keyset/cursor pagination, §4.2 row shape, and `?country=`/`?asn=`/`?tld=`/`?provider=` filter composition as `/domains`, under the §3.3 indexed-scope guardrail. `GET /sinners?country=no` ≡ `GET /domains?class=sinner&country=no`. `GET /domains` remains the **general filterable collection** whose `?class=` param spans every tier (`class=partial`, etc.); the tier paths are short aliases over it, not a second vocabulary.

**Host as a path key.** The eTLD+1 `host` is stable, unique, SEO/deep-link friendly, and already the schema's `UNIQUE` natural id. The API canonicalizes the path parameter (§2.8) before lookup; a value that fails canonicalization is a `404 not-found` (exception: the badge returns `400 invalid-parameter`, §5.2). Synthetic `domain.id` is internal-only and never appears on the wire except inside opaque cursors.

*Rejected — `/metric` (singular), `/domain`-means-sinner, shortuuid campaign tokens, synthetic ids in URLs.* All are legacy accidents; self-describing plural nouns keyed by natural id are the AIP/Microsoft/Zalando consensus. The short tier collections (`/heroes` etc.) are transparent aliases over `/domains` (self-describing), **not** the hidden-meaning `/domain`=sinner form.

### 2.3 Field naming — `snake_case`

Emit **`snake_case`** field names on the wire, uniformly, everywhere; **never mix cases within a response.** This is symmetry with the whole Postgres → sqlc → Go → OpenAPI stack and needs no per-field mapping layer. openapi-typescript generates typed TS bindings from whatever case the spec declares, so the rebuilt frontend is transform-free either way.

Two explicit, bounded, **lint-enforced** exceptions:

- **snake_case normalization of smushed column names.** A handful of schema columns are written without a separator (`country.v6sites`); these are normalized to canonical snake_case on the wire (`v6_sites`) — a mechanical, total case-normalization, not a semantic remap. Where the same concept has two schema spellings (ASN `count_v6` in `asn` vs `v6_domains` in `stats_asn_daily`), the API picks **one** wire spelling (`count_v6`) and uses it in both the detail and the time-series (§4.6/§4.10).
- **The shields.io badge JSON (§5.2)** emits camelCase (`schemaVersion`, `cacheSeconds`, `isError`) because those field names are dictated by shields.io's external endpoint schema. The API's *own* surface stays snake_case throughout.

*Rejected — camelCase* (forces a json-tag mapping layer across the domain model and diverges from the rest of the spec, to benefit a type-generated consumer). If ever revisited, it must be decided once and linted so it never drifts.

### 2.4 Envelope + list-response shape

A **thin, ad-hoc envelope**, not JSON:API. There are exactly **two** collection shapes.

**A. Item collections** (leaderboards, sub-collections, feeds) return a top-level object, never a bare array:

```json
{
  "items": [ /* resource objects */ ],
  "page": {
    "next_cursor": "cD1yYW5rOjEwMDI...",
    "prev_cursor": null,
    "has_more": true
  },
  "meta": {
    "as_of": "2026-07-07T03:41:12Z",
    "generation": 20260707,
    "count_estimate": 1003418,
    "license": "CC-BY-NC-4.0"
  }
}
```

**B. Time-series collections** (`/stats/*`, `/domains/{host}/history`, the country/campaign/asn `/stats`) use **`points`** as the collection key, carry **no `page`** (bounded by the date window, never cursor-paged):

```json
{ "points": [ /* time-ordered rows */ ], "meta": { "as_of": "...", "source": "confirmed_state" } }
```

`points` is the **only** sanctioned alternate collection key. **Single resources** return the resource object with a sibling `meta`:

```json
{ "host": "example.com", "...": "...", "meta": { "as_of": "..." } }
```

**Uniform rules:**

- **`page` is always `{ next_cursor, prev_cursor, has_more }`** and `prev_cursor` is **always present** (`null` when there is no previous page). Bounded non-paged item collections (campaign members, `/shame`, forward-resources) still emit a `page` with all three fields (`has_more: false`, cursors `null`) so the type never varies.
- **Counts live only in `meta`.** A collection carries *either* `meta.count_estimate` (approximate — the default for anything derived from a large or filtered `domain` scan) *or* `meta.count` (exact — only the genuinely bounded curated sets: campaign members, `/shame`, forward-resources). A client reads count from `meta`, full stop.
- **`meta` is deliberately thin:** `as_of` (freshness signal), `generation` (integer crawl id, the ETag/cache-key seed), `count`/`count_estimate` where applicable, `license`. Never nest `items`/`points` more than one level. No per-response `stability` marker.

**`generation` and `as_of` sources** (they seed the ETag/cache story, §6.1, so they must be deterministic across backend instances). `generation` is the integer `YYYYMMDD` from **`max(stats_global_daily.day)`** (see 05-schema.md — stats tables) — an O(1) lookup, monotonic, identical on every instance, no schema change. `as_of` is the crawl-rollup completion timestamp: `stats_global_daily.generated_at` for the newest day, written by the daily stats rollup (06-ingest.md — daily stats rollup; DDL 05-schema.md — stats tables); when it is NULL (the day-0 seed row, pre-first-rollup), `as_of` falls back deterministically to `max(stats_global_daily.day)` at `00:00:00Z`. The per-worker `crawler_metrics.run_id` UUID is **not** used.

*Rejected — full JSON:API* (ceremony for a shallow graph), *bare top-level arrays* (force metadata into headers; unextendable), and the *legacy `{"data":[…]}` search envelope* (one-off inconsistency, deleted in favor of the single `{items,page,meta}` / `{points,meta}` shapes).

### 2.5 Error format — RFC 9457

Every 4xx/5xx is **`application/problem+json`** per RFC 9457:

```http
HTTP/1.1 404 Not Found
Content-Type: application/problem+json

{
  "type": "https://whynoipv6.com/problems/not-found",
  "title": "Domain not found",
  "status": 404,
  "detail": "No crawl record for example.invalid.",
  "instance": "/domains/example.invalid"
}
```

The `status` member must equal the HTTP status line. `type` URIs are stable and resolvable on the site. The fixed set — small, because there are no accounts:

| `type` (relative to `/problems/`) | HTTP | When |
|---|---|---|
| `not-found` | 404 | unknown/uncanonicalizable domain, country, asn, provider, campaign, resource |
| `invalid-parameter` | 400 | malformed cursor, bad `format`, malformed host on badge, non-JSON `POST /check` field |
| `validation-error` | 422 | out-of-range/invalid enum filter value (a value not in the enum); adds `errors:[{field,reason}]` |
| `scope-required` | 422 | a *valid* filter value that needs a companion scope to stay indexed (bare `?flag=`, bare per-dimension `?mx=`); `detail` names the scope params that satisfy it |
| `rate-limited` | 429 | `POST /check` over quota; adds `retry_after` + `Retry-After` header (§6.3) |
| `not-acceptable` | 406 | `Accept` cannot be satisfied on a JSON endpoint |
| `unsupported-media-type` | 415 | `POST /check` body not JSON |
| `manifest-unavailable` | 503 | `/datasets` manifest missing/unparseable (the only 503) |
| `internal-error` | 500 | unexpected fault; `detail` is generic, never a stack trace |

Note the deliberate split between `validation-error` (your *value* is invalid) and `scope-required` (your value is valid but needs an indexed companion scope, §3.3). Conflating the two misleads clients. The legacy byte-exact, capitalization-divergent error strings are deleted.

### 2.6 HTTP semantics

The read surface is **GET** (with implicit HEAD/OPTIONS). The *only* mutating verb is **`POST /check`** (enqueue a live check, §5.1) — an explicitly-modelled async job. No PUT/PATCH/DELETE; no CSRF surface on reads. No HATEOAS (REST maturity level 2): the OpenAPI document is the discoverability substitute.

Status codes: `200` with the resource; `404` for an unknown entity; `400` for a malformed request/cursor/filter; `422` for a semantically-invalid enum value or a scope-required filter; `406`/`415` for negotiation failures; `429` for rate-limit; `304` on a conditional-GET cache hit; `202` on `POST /check` enqueue; `5xx` for faults. **Never** a `200`-with-error-body.

**Zero-result is `200`, not `404`.** A search with no matches, a filter that selects nothing, and paging past the end all return `200` with an empty `items` array. A `404` is reserved for *the addressed entity does not exist* (unknown host/code/asn/provider/uuid). The legacy "404 on zero results / page-past-end / by-domain changelog" behavior is deleted.

### 2.7 Health endpoints

At the **root**, outside the public OpenAPI, outside CDN caching (`Cache-Control: no-store`), and outside rate-limiting:

- **`GET /livez`** — `200` whenever the process is running; no dependency checks. A failure means *restart me*.
- **`GET /readyz`** — `200` only when Postgres/TimescaleDB is reachable and the app can serve; a failure means *stop routing traffic without restarting*. Optional JSON body listing individual checks, but orchestration is driven off the status code.

*Rejected — a single `/healthz`* (can't distinguish "restart me" from "don't route to me").

### 2.8 Host path-parameter canonicalization

Every path parameter carrying a hostname — `{host}` in `/domains/{host}*`, `/resources/{host}*`, campaign member routes, the change-feed scopes, and the badge's `{host}` (after stripping `.svg`/`.json`) — passes through the single shared `Canonicalize()` helper before any DB lookup. One implementation, `internal/domain/host.go`, shared with the crawler and v6ctl:

```go
// Canonicalize returns the canonical form of a hostname:
// lowercase punycode (A-label) FQDN, no trailing dot, ≤253 octets, ≥2 labels.
// It is the ONLY path by which a hostname may reach a DB write or DB lookup.
func Canonicalize(raw string) (string, error)

var ErrInvalidHost = errors.New("invalid host") // all failures wrap this
```

Algorithm (in order):
1. `s := strings.TrimSpace(raw)`; strip **exactly one** trailing `.` if present.
2. Reject (`ErrInvalidHost`) if `s == ""` or contains any of `/ \ : @ ? # [ ]` or whitespace.
3. `s = strings.ToLower(s)`.
4. `ascii, err := idna.Lookup.ToASCII(s)` (`golang.org/x/net/idna`, IDNA2008 lookup profile with UTS46 mapping); any error → `ErrInvalidHost`.
5. Post-checks: total length ≤253 octets; ≥2 labels; each label 1–63 octets; `net.ParseIP(ascii) == nil`.
6. Return `ascii`.

**Failure policy for API path params:** `404 not-found` — a lookup miss (`xn--`-input and equivalent Unicode input resolve to the same entity). The **one exception** is the badge (§5.2), which returns `400 invalid-parameter` because a malformed embed is not a legitimate request. The `POST /check` and badge endpoints additionally apply a reserved-TLD policy layer (reject final label ∈ {`test`, `example`, `invalid`, `localhost`, `internal`, `local`}); the read endpoints do not.

---

## 3. Pagination, filtering, sorting

The master list is the Tranco top-1M ranked by rank; offset pagination is deleted.

### 3.1 Keyset/cursor, not offset

**Keyset (seek-method) pagination** is the primary access pattern for *every* collection large enough to page — `/domains`, its tier/country/asn/provider-scoped variants, the reverse `dependents` list, and campaign members. **Offset pagination is not used anywhere.** Exact `COUNT(*)` survives only on the genuinely-bounded curated sets (campaign members, `/shame`, forward-resources), where the row count is a few tens.

Country/asn/provider-scoped lists are **not** "small bounded": a single large country or a hyperscaler ASN can hold tens to hundreds of thousands of eTLD+1s, so they reuse the **rank keyset** (`(rank, id)`, composing with `idx_domain_country`/`idx_domain_asn` plus the `(rank, id)` tiebreaker) and report an **estimated** `count_estimate` — never an exact count. The one truly-small curated set is **campaign members** (typically tens of rows); those get an exact `meta.count` but are paged with the same cursor page type for envelope uniformity.

Rationale: offset degrades linearly with depth (walk-and-discard), is incorrect on a mutating set (the daily crawl re-ranks), whereas keyset uses a `WHERE key > :last` seek on an index — constant cost regardless of depth. Rank is the ideal keyset key on rank-ordered views: monotonic, indexable (`idx_domain_rank`), human-meaningful.

**Default `/domains` scope.** The bare top-level `/domains` collection is **ranked, non-disabled rows only** (`WHERE rank IS NOT NULL AND NOT disabled`; columns on the `domain` table, see 05-schema.md — domain table). Rank-NULL entities (campaign-only hosts, subdomains, live-check hosts) are real `domain` rows reachable **only via their sub-collections** (`/campaigns/{uuid}/domains`, `/domains/{host}/subdomains`, `/resources/{host}/dependents`), never from the top-level leaderboard — with **one exception**: the `?q=` search predicate spans rank-NULL rows (§3.3, Decision 2026-07-11). `meta.count_estimate = max(rank)` is an **upper bound** (disabled ranked rows retain their rank but are filtered out), which is why it is labelled an estimate. Queries spell out `AND rank IS NOT NULL AND NOT disabled` **verbatim** so the planner's partial-index predicate-implication check is trivial (`idx_domain_heroes`/`idx_domain_sinners`/`idx_domain_partial`, see 05-schema.md — domain table).

### 3.2 Cursor design

The cursor is an **opaque base64url token**. Opacity lets the server evolve the scheme and validate staleness. It encodes:

```
{
  "v": 1,                    // cursor schema version
  "g": 20260707,             // crawl generation the cursor was minted against
  "s": "rank",               // sort key
  "f": "a3f9c1",             // filter fingerprint (hash of the normalized filter set)
  "k": [10023, 88142]        // the seek tuple (shape depends on the sort key)
}
```

Each sort binds a **strict total order** and its own seek tuple:

- **`rank` (default, and country/asn/provider scopes):** `(rank, id)` with `id` as the guaranteed-unique tiebreaker. Qualified `rank IS NOT NULL`, so the seek tuple is always total:

```sql
SELECT ... FROM domain
WHERE classification = 'hero' AND rank IS NOT NULL AND NOT disabled
  AND (rank, id) > ($1, $2)          -- seek tuple from the cursor
ORDER BY rank, id
LIMIT $3 + 1;                        -- N+1 fetch → compute has_more, drop the extra
```

- **`host` (campaign members, and the `sort=host` option):** `host` alone is a unique natural id, so `host > $1` is already a strict total order — no tiebreaker, no NULL. This is the ordering for campaign members, whose `rank` is usually NULL.
- **Nullable-rank ordering (`dependents`, §4.11):** the reverse `dependents` set mixes ranked and rank-NULL rows, ordered `rank NULLS LAST`. A plain `(rank, id) > (…)` evaluates to `UNKNOWN` on NULL rank and would drop the null-rank tail, so `dependents` uses a **null-flag-first** key:

```sql
SELECT ... FROM domain
WHERE ... AND (rank IS NULL, rank, id) > ($1, $2, $3)
ORDER BY (rank IS NULL), rank, id
LIMIT $4 + 1;
```

The seek tuple is then `[is_rank_null, last_rank, last_id]`. The `rank IS NOT NULL` global-list seek is **never** reused verbatim for this nullable ordering.

**Staleness handling.** A cursor whose `g` differs from the current crawl generation is *re-anchored*: the server seeks to the same `last_rank` in the current generation and continues. Re-anchoring is **best-effort**, not exact — ranks are reassigned every generation, so rows whose rank crossed the cursor boundary between fetches may be skipped or repeated (a bounded, browse-acceptable anomaly); within a single generation the keyset walk is exact. If the filter fingerprint `f` no longer matches the request's filters, the cursor is rejected with `400 invalid-parameter`. Bidirectional paging (`prev_cursor`) is supported.

**Deep-link escape hatch — rank-ordered views only.** `?after_rank=500000` (and `?around_rank=500000` for a centered window: the `⌈limit/2⌉` rows ranked ≤ N plus the `⌊limit/2⌋` rows ranked > N, honoring the same `?limit` cap) provide shareable, stateless deep links — `WHERE rank > 500000 ORDER BY rank LIMIT n`, an indexed range scan. **Honest scope:** these mean *"rows whose global rank ≥ N, then filtered"* — **not** "the Nth matching row." On a sparse filter (`class=hero` selects ~41k of 1M), `after_rank=N` returns "heroes whose global rank > N," which is not "page N of heroes." The `sort=host` ordering has **no** random-access param — forward/back cursor only. A true dense per-filter rank is **OPEN-14** (skipped).

`?limit` is client-supplied with a sane cap (`default 50`, `max 200`).

### 3.3 Filter / sort / field-selection grammar

Filters are plain query params, aligned with response field names, and **constrained to the indexed axes**:

| Param | Values | Backed by | Notes |
|---|---|---|---|
| `class` | `hero`\|`partial`\|`sinner`\|`inactive`\|`unknown` | `idx_domain_heroes/sinners/partial` | primary filter; presets the `/heroes`,`/sinners` tier paths; predicate spelled `AND rank IS NOT NULL AND NOT disabled` |
| `saint` | `true` | layered on `idx_domain_heroes` | saint ⊂ hero, cheap without its own index; the `/saints` tier path |
| `country` | ISO code | `idx_domain_country` | composes with `class` + rank order |
| `asn` | AS number | `idx_domain_asn` (05-schema.md — `(asn_id, classification, rank)`) | composes with `class` + rank order, same shape as `country` |
| `tld` | eTLD suffix | `idx_domain_tld` (05-schema.md — domain table) | TLD/ccTLD pivot; scope-required unless combined with an indexed prefilter |
| `provider` | `dns_provider.id` | `idx_domain_dns_provider` (05-schema.md) | DNS-provider pivot (OPEN-4); scope-required as `tld`/`flag` |
| `hosting` | hosting tag | `domain.hosting_provider` (05-schema.md) | hosting/CDN text-tag pivot; scope-required |
| `flag` | one of `class_flags` | **expensive** — scope-required (below) | |
| `base`/`www`/`ns`/`mx`/`conn`/`resources` | an `ipv6_status` value | **expensive** — scope-required (below) | per-dimension confirmed-status filters (drives the mail track) |
| `rank_max` / `rank_min` | int | `idx_domain_rank` | cohort ranges (top-1000 etc.) |
| `q` | substring | `idx_domain_host_trgm` | search; **does not** compose with rank ordering — ordered and cursor-paged on the `host` seek key (§3.2); trigram similarity is not a strict total order, so relevance never orders pages. **Scope exception (Decision 2026-07-11):** `?q=` spans **all non-disabled rows including rank-NULL** (campaign-only hosts, subdomains, live-check origins) — the one read that surfaces rank-NULL rows outside their sub-collections; without it, search cannot find campaign-only hosts (12-frontend.md §7.3). The host seek needs no rank, so the ordering is unaffected; `rank` serves as `null` in the row |
| `sort` | `rank` (default) \| `-rank` \| `host` | index-dependent | each sort binds a distinct cursor ordering (§3.2) |
| `fields` | comma list | — | sparse fieldset to trim the leaderboard row |
| `format` | `json` (default) \| `csv` | — | content negotiation (§5.5) |

**Guardrails against expensive predicates (scope-required, §2.5).**

- **`flag=`, the per-dimension status filters (`mx=supported`, …), and the `tld=`/`provider=`/`hosting=` pivots are the same class of unindexed-or-selective predicate.** They are accepted **only** when combined with an indexed prefilter (`class`, `country`, or `asn`). A bare, unscoped one returns **`422 scope-required`** — *not* `validation-error`; the value is valid, it just needs a scope — with a `detail` naming the scope params that satisfy it. (OPEN-2: no `class_flags` GIN index is added now.) **Decision — at most ONE such residual per request.** These predicates are heap residuals inside the indexed scope; stacking several selective residuals (`?tld=com&provider=12&mx=unsupported`) makes each keyset page re-scan a large fraction of the scope. A request combining two or more residuals from this class also returns `422 scope-required`, with a `detail` steering to the indexed axes. Accepted worst case (documented, not hidden): one sparse residual inside one large scope may scan up to that scope per page — bounded by the single-residual rule and the `?limit` cap.
- **The global mail-heroes / mail-sinners lists are served from stats, not a live scan.** A site-wide "all domains missing IPv6 mail" list (`mx=unsupported`) is the unscoped seq-scan case. The **aggregate headline** comes from the daily `mx_supported` stats counter (`domains − mx_supported`); the **list form** is offered only as a `class`/`country`/`asn`-scoped filtered `/domains` view (§4.4).
- **Arbitrary cross-dimension boolean predicates** (`base=supported AND mx=unsupported` with no scope) are not offered — no composite index covers them.
- **No request-time aggregation over the live `domain` table.** Country/ASN/classification rollups are read from the precomputed `stats_*` daily tables, never `GROUP BY domain` live.

**Construction (Decision).** The list-family queries this grammar describes are the one slice **not** served by static sqlc: they are built at request time with squirrel in the hand-written adapter, emitting the scope predicate, the single residual, the seek tuple, and `AND rank IS NOT NULL AND NOT disabled` as **verbatim literals** — required for the planner's partial-index predicate-implication check (05-schema.md — §10.2 builder carve-out; §2 rule 7). Every non-list query stays sqlc.

*Note.* No `-last_change` sort — no `last_change` column or index exists, and the keyset design requires every sort to bind an indexable strict total order. "What changed recently" is served by the changelog feed (§4.8, indexed on `ts`). A materialized `last_change` is **OPEN-14** (skipped).

### 3.4 Count strategy

**No exact `COUNT(*)` on the hot path.** Counts always live in `meta` (§2.4).

- The **global list** `meta.count_estimate` is served from **`max(rank)` of the current generation** — an O(1) lookup, the "your rank / ~N" denominator, labelled an estimate because disabled ranked rows create small gaps.
- **Filtered global views and the country/asn/provider-scoped lists** get an **estimated** `count_estimate` (`reltuples` for the unfiltered table; the EXPLAIN plan row estimate for a filtered query). These scopes can be very large, so an exact count is never taken.
- **Bounded curated sets** (campaign members, `/shame`, forward-resources) get an **exact** `meta.count`.

`has_more` on every page is computed with the **N+1 fetch** (select `limit+1`, return `limit`).

---

## 4. The resource model

Every representation serves the **real** status/classification/saint/flags model. The canonical building block is the domain row; everything else composes from it.

### 4.1 Domain — the status object convention

Each of the six dimensions is a **status object**, so per-dimension provenance rides along:

```json
"status": {
  "base":      { "value": "supported",    "since": "2024-11-03T00:00:00Z" },
  "www":       { "value": "supported",    "since": "2025-01-19T00:00:00Z" },
  "ns":        { "value": "supported",    "since": "2023-08-01T00:00:00Z" },
  "mx":        { "value": "unsupported",  "since": "2025-06-02T00:00:00Z" },
  "conn":      { "value": "supported",    "since": "2025-01-19T00:00:00Z" },
  "resources": { "value": null,           "since": null }
}
```

`value` is the 4-value enum (`supported`/`unsupported`/`no_record`/`not_applicable`) **or JSON `null`** when the dimension has never been confirmed (`resources` is `null` everywhere until `crawler.resources.enabled` flips). `since` is the `*_since` column (05-schema.md — domain table), `null` when never confirmed. No `legacyStatus` collapse, no `ts_aaaa` renaming, no `"0001-01-01T00:00:00Z"` zero-time — real enum, real names, `null` for absent.

### 4.2 Domain summary (list row)

The row returned in every `/domains*` collection:

```json
{
  "host": "example.com",
  "rank": 10023,
  "kind": "apex",
  "parent": null,
  "classification": "partial",
  "class_flags": ["www_missing", "mail_missing"],
  "saint": false,
  "ipv6_only": null,
  "status": {
    "base":      { "value": "supported",   "since": "2024-11-03T00:00:00Z" },
    "www":       { "value": "unsupported", "since": "2025-05-10T00:00:00Z" },
    "ns":        { "value": "supported",   "since": "2023-08-01T00:00:00Z" },
    "mx":        { "value": "unsupported", "since": "2025-06-02T00:00:00Z" },
    "conn":      { "value": "supported",   "since": "2025-01-19T00:00:00Z" },
    "resources": { "value": null,          "since": null }
  },
  "tld": "com",
  "country": { "code": "NO", "name": "Norway" },
  "asn":     { "number": 2119, "name": "Telenor Norge AS" },
  "dns_provider":     { "id": 12, "name": "Cloudflare" },
  "hosting_provider": "Amazon CloudFront",
  "last_checked_at": "2026-07-07T02:14:55Z"
}
```

`rank` is `int` or **JSON `null`** (campaign-only, subdomains, live-check hosts) — never the legacy `0`. `class_flags` is the ordered array (`broken_v6`/`www_missing`/`ns_missing`/`mail_missing`/`resources_v4only`); a `partial` domain may legitimately carry `[]`. `country`/`asn` are **embedded objects**, not display-name strings. `tld` (bare eTLD suffix, `domain.tld`), `dns_provider` (embedded `{id,name}` from the `dns_provider` mapping via `domain.dns_provider_id`, `null` when unmapped), and `hosting_provider` (the normalized `domain.hosting_provider` TEXT tag as a plain string, `null` when unset) are the new per-domain pivots (05-schema.md — domain table; OPEN-4). `?fields=` can trim this to e.g. `host,rank,classification,saint`. `ipv6_only` is the **derived conn+resources fold** (`internal/domain.IPv6Only`, 03 §10): `supported` iff `conn = supported` and `resources ∈ {supported, not_applicable}`; `unsupported` on any definitive negative; `not_applicable` when there is no AAAA to assess; JSON `null` until both dimensions are confirmed (strict — never claimed from `conn` alone). Serialized on both the §4.2 summary and the §4.3 detail, derived at render time from the same confirmed sextet as `status` so the two can never disagree.

### 4.3 Domain detail

`GET /domains/{host}` — the summary plus informational dimensions, children, and a freshness/meta block. The heavy per-check evidence (record sets, TLS cert, per-resolver consensus tuples, resource lists) is served from the latest `scan_detail` under a nested `evidence` object (same shape as the live-check result, §5.1 — the shared `MapLiveResult` mapper), optional via `?include=evidence`.

```json
{
  "host": "example.com",
  "rank": 10023,
  "kind": "apex",
  "parent": null,
  "classification": "partial",
  "class_flags": ["www_missing", "mail_missing"],
  "saint": false,
  "status": { "...": "six status objects as in §4.1" },
  "informational": {
    "dnssec": "supported",
    "ptr":    "partial",
    "smtp":   "unsupported",
    "parity": "supported",
    "latency_v4_ms": 41,
    "latency_v6_ms": 38
  },
  "tld": "com",
  "country": { "code": "NO", "name": "Norway", "tld": ".NO" },
  "asn":     { "number": 2119, "name": "Telenor Norge AS" },
  "dns_provider":     { "id": 12, "name": "Cloudflare" },
  "hosting_provider": "Amazon CloudFront",
  "subdomain_count": 3,
  "disabled": false,
  "last_checked_at": "2026-07-07T02:14:55Z",
  "created_at": "2022-05-01T00:00:00Z",
  "evidence": { "...": "latest scan_detail JSONB, per §5.1 shape (optional, ?include=evidence)" },
  "meta": { "as_of": "2026-07-07T03:41:12Z", "generation": 20260707 }
}
```

`informational` dimensions are **advisory** — they never gate classification. They are served as latest-observation values with the **public-masking rule the trust model requires everywhere** (03 §1 / the 05 enum registry: `error` and `inconsistent` never reach public output; `partial` is public only for `ptr`/`parity`):

| Field | Allowed wire values |
|---|---|
| `dnssec` | `supported` \| `unsupported` \| `no_record` \| `not_applicable` \| `null` |
| `smtp` | `supported` \| `unsupported` \| `no_record` \| `not_applicable` \| `null` |
| `ptr` | above **plus** `partial` |
| `parity` | above **plus** `partial` |

A latest observation of `error` or `inconsistent` maps to **`null`** (never leaks); a raw `partial` on `dnssec`/`smtp` also maps to `null`. `subdomains[]` are a native sub-collection (`/domains/{host}/subdomains`). Observation-level richness (`error`/`inconsistent`) is exposed **only** inside `evidence` (OPEN-3).

### 4.4 The ranked tier lists

Heroes / sinners / saints are **first-class short collection resources** — each a preset filtered view over the `/domains` leaderboard, returning the §4.2 summary row in the §2.4 collection envelope, sharing the exact same keyset/cursor pagination and `?country=`/`?asn=`/`?tld=`/`?provider=` composition:

```
GET /heroes             # preset: class=hero
GET /sinners            # preset: class=sinner
GET /saints             # preset: saint=true
```

`GET /domains` stays the **general filterable collection**; the same views are reachable via `?class=` (`GET /domains?class=hero&sort=rank`, `GET /domains?class=partial`, `GET /domains?class=hero&saint=true`). Tier paths accept the same additional filters, so "sinners in Norway" is `GET /sinners?country=no` (≡ `GET /domains?class=sinner&country=no`, ≡ the scoped `GET /countries/NO/domains?class=sinner`). The partial and mail views have no tier paths (ADR 0003): `GET /domains?class=partial` and `GET /domains?class=hero&mx=supported` are their canonical spellings.

**The mail track** obeys the §3.3 scope-or-stats guardrail:

- **Mail-heroes** — `GET /domains?class=hero&mx=supported` is canonical (scoped so `mx=` is indexed via the `class` prefilter).
- **Mail-sinners** — `GET /domains?class=sinner&mx=unsupported` (likewise scoped). A truly *global* mail-sinners count is **not** a live `/domains` scan — that headline number is read from the `mx_supported` daily stats counter (`domains − mx_supported`). The list form is always class/country/asn-scoped.

**The top-shame editorial pick** (hand-curated `top_shame`, distinct from algorithmic `sinner`) is its own resource, returning the envelope:

```
GET /shame
```
```json
{
  "items": [ { "host": "...", "reason": "...", "added_at": "..." } ],
  "page": { "next_cursor": null, "prev_cursor": null, "has_more": false },
  "meta": { "as_of": "...", "generation": 20260707, "count": 12 }
}
```

A small bounded editorial list (the ~12 curated picks), so no cursor — `page` is trivial and `meta.count` is exact. (`top_shame` must be re-seeded at cutover, 08-migration-cutover.md — it has no crawl-derivable source.)

### 4.5 Country

```json
{
  "code": "NO",
  "name": "Norway",
  "tld": ".NO",
  "sites": 8421,
  "v6_sites": 2103,
  "percent": 24.97,
  "meta": { "as_of": "...", "generation": 20260707 }
}
```

`v6_sites` is the snake_case-normalized wire name for the schema column `country.v6sites` (§2.3). `percent` is served **directly** from the `NUMERIC(5,2)` column (the legacy `÷10` pgtype hack is gone). `GET /countries` is the leaderboard, sortable by `percent`/`v6_sites` (precomputed counters → cheap, no live aggregation). `GET /countries/{code}/domains` is the per-country list (`?class=` filter over `idx_domain_country`) — **keyset-paginated with a `count_estimate`** (a large country can hold hundreds of thousands of domains, §3.1). Time series: `GET /countries/{code}/stats` (§4.10). The `UN` sentinel appears as a normal row. `GET /countries/{code}/changelog` is the per-country feed (§4.8).

### 4.6 ASN / network / provider

```json
{
  "number": 2119,
  "name": "Telenor Norge AS",
  "count_total": 4210,
  "count_v6": 1876,
  "count_v4": 2334,
  "meta": { "as_of": "...", "generation": 20260707 }
}
```

`count_v4` is **synthesized server-side** (`count_total − count_v6`) — not stored. `count_v6`/`count_total` are the **canonical wire names**, used in both this detail representation and the ASN time-series (§4.10); the time-series' underlying `stats_asn_daily.v6_domains` and `domains` columns are mapped onto the same `count_v6`/`count_total` wire names (§2.3). `GET /asns` is the network leaderboard (`?sort=count_v6|count_total`; real columns, no `order=ipv4|ipv6` alias; `?q=` does normal substring match with no AS-prefix bug). `GET /asns/{number}/domains` lists the network's domains — **keyset-paginated with a `count_estimate`** (a hyperscaler ASN can host hundreds of thousands of eTLD+1s, §3.1). The sentinel `0` = Unknown appears as a normal group. This is the **hosting-ASN league table**.

**The DNS-provider league table** (OPEN-4, resolved YES) is **committed**: the `dns_provider` mapping table (`ns_suffixes[] → provider`, 05-schema.md — dns_provider table; `domain.dns_provider_id` set at scan commit by longest ns-suffix match) backs `GET /providers` + `GET /providers/{id}` + `GET /providers/{id}/domains`, keyed by `dns_provider.id`, exposing binary inclusion + counts only (no scores). It is the highest-leverage pivot — one provider's default flips thousands of domains. Provider detail row:

```json
{
  "id": 12,
  "name": "Cloudflare",
  "count_total": 210344,
  "count_v6": 198021,
  "count_v4": 12323,
  "meta": { "as_of": "...", "generation": 20260707 }
}
```

`/providers/{id}/domains` is keyset-paginated with a `count_estimate` (§3.1), like the ASN scope. `?provider=` (§3.3) filters `/domains*` by `dns_provider_id`, backed by `idx_domain_dns_provider`.

**Provider leaderboard count source (resolved — mirror ASN).** `dns_provider` carries stored `count_total`/`count_v6` columns (05-schema.md — dns_provider table), recomputed by the daily tick's counter step exactly like `asn` (06-ingest.md — §10.6 Country/ASN/DNS-provider counter recompute; the provider set is small, so the exact reset+recompute is trivially cheap). The leaderboard therefore serves **exact** `count_total`/`count_v6` with `count_v4` synthesized server-side (`count_total − count_v6`, never stored), identical to the ASN shape above — "counts, not scores." The scoped list `/providers/{id}/domains` still reports `count_estimate` (a large provider can host hundreds of thousands of eTLD+1s, §3.1); only the leaderboard row itself carries exact counts.

The companion **hosting/CDN provider** axis is `domain.hosting_provider` — a normalized TEXT tag (CNAME-chain CDN detection + resolved-IP ASN), **not** an id-keyed resource. It surfaces on the domain row (§4.2/§4.3) as a plain string and as a `?hosting=` pivot on `/domains*` under the same scope-required guardrail (§3.3). A precomputed hosting league table (aggregating the tag) needs a stats source rather than a live `GROUP BY domain` (§3.3); until one exists it is a scoped filter, not a leaderboard collection.

### 4.7 Campaign (composite detail)

`GET /campaigns` lists campaigns (with a `?tag=` filter, §5.6). **Each list row carries the same `adoption` object as the detail** (`{v6_ready_percent, day}` from the latest `stats_campaign_daily` row; `null` before the campaign's first stats tick) alongside `domain_count` — the campaign set is small (tens of rows), so the per-row stats join is trivially cheap, and it spares the frontend's card grid an N+1 detail fan-out (Decision 2026-07-11; 12-frontend.md §8). The detail is a **composite** (metadata + paged members + adoption):

```json
{
  "uuid": "3f2b...",
  "name": "Norwegian Banks",
  "description": "Retail banks operating in Norway",
  "source_file": "campaigns/no-banks.yaml",
  "tags": ["mandate", "eu-2030", "sector-banking"],
  "disabled": false,
  "adoption": { "v6_ready_percent": 41.7, "day": "2026-07-07" },
  "domains": {
    "items": [ /* §4.2 domain summary rows, typically rank:null */ ],
    "page": { "next_cursor": null, "prev_cursor": null, "has_more": false }
  },
  "meta": { "as_of": "2026-07-07T03:41:12Z", "generation": 20260707, "count": 17 }
}
```

Members are ordinary domain rows joined via `campaign_domain`, **ordered by `host` and paged with the same keyset cursor** as every other collection (§3.2 — `host` is a unique key, so the seek is total even though members' `rank` is usually NULL). Campaign members are a genuinely-bounded set, so `meta.count` is **exact**. `adoption.v6_ready_percent` comes from `stats_campaign_daily` (`v6_ready` = base supported ∧ ns supported ∧ www ∈ {supported, not_applicable}), never per-entity scoring; `adoption.day` is the stats date. **Keyed by raw UUID** (OPEN-1), not shortuuid. `tags` backs the mandate surface (OPEN-12; the `campaign.tags` TEXT[] column, 05-schema.md — campaign table). Time series: `GET /campaigns/{uuid}/stats`.

### 4.8 Changelog event (the trust surface)

Served **structured**, one row per confirmed dimension transition — no message rendering, no synthetic epoch id, no `domain_url` string:

```json
{
  "ts": "2026-07-06T03:32:10Z",
  "host": "example.com",
  "field": "www",
  "old_value": "unsupported",
  "new_value": "supported"
}
```

`ts` is a full RFC 3339 **timestamp** (event time). `old_value`/`new_value` are the raw 4-value enum, always non-null, always distinct. **All native fields are served**, including `conn` and `resources` transitions and `not_applicable` transitions — the legacy coverage filter (which admitted only `base/www/ns/mx` with 3 legacy strings) is deleted. Feeds:

- `GET /changelog` — global recent-transitions feed, cursor on `ts DESC` (`idx_changelog_ts`, see 05-schema.md — changelog table), `?field=` and `?from=/&to=` windows. **`?scope=campaign`** (Decision, 2026-07-11 — frontend parity, 12-frontend.md §7.4): restricts the feed to transitions of domains that are members of *any* campaign. Implementation is driven from the **domain side**, never a global-index probe: the bounded `campaign_domain` set (curated, low thousands) feeds a lateral per-domain read of the `(domain_id, ts)` PK, merged `ts DESC` — k index probes, no sparse scan of `idx_changelog_ts`. Like the per-campaign/per-country feeds it is **capped to the fixed recent window** (latest 50 within the last 90 days, no pagination — uniform envelope, cursors `null`, `has_more: false`); lifting the cap rides the OPEN-15 denormalization if it ever lands. Any other `scope` value → `422 validation-error`.
- `GET /domains/{host}/changelog` — per-domain (native PK read).
- `GET /campaigns/{uuid}/changelog` — campaign-wide (members' transitions).
- `GET /campaigns/{uuid}/domains/{host}/changelog` — one member.
- `GET /countries/{code}/changelog` — per-country feed (no legacy equivalent).

**Scoped-feed cost (§3.3 discipline).** The `changelog` hypertable is keyed `(domain_id, ts, field)` with a global `idx_changelog_ts` and the per-domain PK, but **no `(scope_id, ts)` index**. A naive per-country / per-campaign feed can scan far back through the global `ts` index probing a sparse scope. So the scoped changelog feeds (per-country, per-campaign) are **capped to a fixed recent window** — the latest 50 transitions **within the last 90 days** (matching §5.4), never deep-paginated; bulk consumers are steered to the datasets (§5.3). The 90-day `ts` floor is load-bearing, not cosmetic: it bounds the walk via chunk exclusion — without it a scope with <50 recent transitions scans past the 60d columnstore boundary (where the btree indexes no longer exist) into the forever-retained history. A denormalized `(country_id/campaign, ts)` path is **OPEN-15** (skipped); until it exists, the recent-window cap is the guardrail. The global and per-domain feeds are index-backed and paginate normally.

The item id for feed serializations (§5.4) is the composite `(host, ts, field)` (stable, immutable), not a synthetic epoch id.

### 4.9 Per-domain timeline / history

`GET /domains/{host}/history?from=&to=&interval=daily|weekly` — the trajectory graph.

**Trust-consistent sourcing.** The per-dimension trajectory is **reconstructed from the `changelog`** (the authoritative confirmed transitions, the same source as §4.8), **not** read from the raw `scan` hypertable. Reading `scan` would (a) leak `error`/`inconsistent` into public output — locked as "never reach public output" (03 §1) — and (b) mix a noisy per-scan observation trajectory with the smooth confirmed one. Instead the API **replays the changelog** to reconstruct the confirmed per-dimension state as of each day in the window, and applies the deterministic classification ladder to those confirmed values to stamp `classification` per point — a pure function of confirmed state, fully trust-consistent. The only value taken from `scan` is the **latency overlay** (`latency_v4_ms`/`latency_v6_ms`), a genuinely per-scan measurement carrying no confirmed-status semantics.

```json
{
  "host": "example.com",
  "points": [
    {
      "day": "2026-07-01",
      "base": "supported", "www": "supported", "ns": "supported",
      "mx": "unsupported", "conn": "supported", "resources": null,
      "classification": "partial",
      "latency_v4_ms": 41, "latency_v6_ms": 38
    }
  ],
  "meta": { "retention_days": 730, "as_of": "..." }
}
```

`day` is date granularity. `resources` is `null` (never confirmed, per §4.1) — the confirmed-never `null`, correct here because the source is the confirmed reconstruction. No pagination (`points` collection); `interval=weekly` samples the reconstructed state at each week boundary, not averaging.

**Baseline seeding (Decision 2026-07-11).** Bootstrap confirmations (NULL→value) write **no** changelog row, so a pure changelog replay renders every never-flipped domain — the overwhelmingly common stable case — as `points: []` forever (a permanently blank frontend tracker, 12-frontend.md §7.3). The reconstruction therefore **seeds each dimension from the domain row's confirmed `(value, *_since)` pair**: from `since` (clamped to the window/created_at) onward the dimension holds its confirmed value; days before `since` are `null` (unconfirmed — never fabricated). Dimensions with changelog transitions replay exactly as before, including the first transition's `old_value` backfill toward `created_at`; where both sources describe a day, the changelog replay wins (it is the authoritative transition record — the seed only fills dimensions the replay never touches). This uses only the new system's own confirmed state — OPEN-9 (no legacy history import) is untouched. **Day-1 note (amended):** the trajectory starts at the first confirmed baseline rather than empty, and deepens as the fresh crawl accumulates transitions; the latency overlay fills as scans accumulate.

### 4.10 Stats / overview (adoption over time)

Five routes, one query contract (`?from=&to=&interval=daily|weekly`, default `to=today` UTC, `from=to−90d`, ascending, no pagination, `≤366 rows/yr`, zero rows → `200 {"points":[]}`), sourced from the confirmed-state `stats_*_daily` tables so graphs match public lists exactly. All use the `{points, meta}` time-series envelope (§2.4). `interval=weekly` returns one row per ISO week — the latest snapshot in that week (`SELECT DISTINCT ON (date_trunc('week', day)) ... ORDER BY date_trunc('week', day), day DESC`, re-sorted ascending), not averaging. Validation: unparseable date, `from > to`, or bad `interval` → `400 invalid-parameter`.

- `GET /stats/overview` — `stats_global_daily` (the headline dashboard).
- `GET /countries/{code}/stats` — `stats_country_daily`.
- `GET /campaigns/{uuid}/stats` — `stats_campaign_daily` (incl. `v6_ready%`).
- `GET /asns/{number}/stats` — `stats_asn_daily` (exposes the `count_v6`/`count_total` wire names, §4.6).
- `GET /domains/{host}/history` — per-domain, changelog-reconstructed (§4.9).

The overview point carries the full `stats_global_daily` payload (see 05-schema.md — stats tables):

```json
{
  "points": [
    {
      "day": "2026-07-06",
      "domains": 1003418, "heroes": 41022, "partial": 88310, "sinners": 210144,
      "inactive": 512900, "unknown": 151042, "saints": 3110, "disabled": 8933,
      "base_supported": 260113, "www_supported": 241889, "ns_supported": 402231,
      "mx_supported": 180334, "conn_supported": 251004, "resources_supported": 0,
      "top_heroes": 512, "top_nameserver": 690
    }
  ],
  "meta": { "as_of": "...", "source": "confirmed_state" }
}
```

`meta.source: "confirmed_state"` is deliberate: if the measurement-flavored `scan_daily_adoption` cagg is ever exposed (OPEN-5 — resolved NO), it must be labelled `source: "measurement"` and never reconciled with these numbers.

The country / campaign / asn `/stats` points carry their table's columns (`stats_country_daily`, `stats_campaign_daily` incl. `v6_ready`, `stats_asn_daily` with `v6_domains` mapped to `count_v6` and `domains` mapped to `count_total`, §4.6); `day` serializes as `"YYYY-MM-DD"` (UTC date part for TIMESTAMPTZ `day` columns). `GET /campaigns/{uuid}/stats` uses the raw-UUID resolver; unknown/disabled → `404 not-found`. `GET /asns/{number}/stats` accepts a leading `AS`/`as` prefix (stripped); non-numeric after stripping → `400 invalid-parameter`; unknown AS → `404 not-found`.

### 4.11 Resource dependencies

- `GET /domains/{host}/resources` — forward: what this domain depends on. Bounded small (a handful of resource hosts per domain), so no cursor is needed — but it still returns the uniform collection envelope (`items`, not a bare `resources` key), `ORDER BY host`, with an exact `meta.count` (tables `resource_host` + `domain_resource`, see 05-schema.md — resources tables):

```json
{
  "items": [
    { "host": "fonts.googleapis.com", "aaaa_status": "unsupported",
      "source": "discovered", "required": true,
      "first_seen": "2025-02-01", "last_seen": "2026-07-06",
      "last_checked_at": "2026-07-07T01:00:00Z" }
  ],
  "page": { "next_cursor": null, "prev_cursor": null, "has_more": false },
  "meta": { "as_of": "...", "generation": 20260707, "count": 4 }
}
```

`aaaa_status` is `resource_host.aaaa_status` (the 4-value enum or `null`). Zero links → `items: []` (also the constant answer while `crawler.resources.enabled=false`, pre-phase-5).

- `GET /resources/{host}/dependents` — reverse: **the advocacy surface** ("this v4-only host breaks N sites"), keyset-paginated over the **null-flag-first** ordering (`ORDER BY (rank IS NULL), rank, id`, §3.2 — the set mixes ranked and rank-NULL dependents):

```json
{
  "resource": { "host": "fonts.googleapis.com", "aaaa_status": "unsupported",
                "dependent_count": 41022, "last_checked_at": "..." },
  "items": [ /* §4.2 domain summary rows depending on it, + link attrs source/required */ ],
  "page": { "next_cursor": "...", "prev_cursor": null, "has_more": true },
  "meta": { "as_of": "...", "generation": 20260707, "count_estimate": 41022 }
}
```

`resource.dependent_count` (`resource_host.dependent_count`) is the headline counter; the paged `items` carry a `meta.count_estimate` because the dependent set can be very large. This powers the `resources_v4only` flag narrative. `{host}` through Canonicalize; no `resource_host` row → `404 not-found`. Empty/dormant until `crawler.resources.enabled` flips.

### 4.12 Client-IP echo (visitor banner)

`GET /ip` — a genuine product feature (the "are you reaching us over IPv6?" banner). Echoes the caller's real IP (§1.2), bracketless, as an object:

```json
{ "ip": "2001:df0:...:1", "family": "ipv6" }
```

`Cache-Control: no-store` (per-caller). `family` (`ipv6`|`ipv4`) is derived server-side so the frontend doesn't sniff for `:`. No other keys. This replaces the legacy `{"ip":"..."}` shape.

---

## 5. Special endpoints

### 5.1 Live check — `POST /check` / `GET /check/{id}`

Anonymous, no auth; the only write path. **Async enqueue + poll**, never synchronous (a full engine run is 60–90 s; holding an anonymous request open invites resource exhaustion).

```http
POST /check
Content-Type: application/json

{ "host": "example.com" }
```

```http
HTTP/1.1 202 Accepted
Location: /check/481529
Cache-Control: no-store

{ "id": 481529, "host": "example.com", "status": "pending", "created_at": "..." }
```

```http
GET /check/481529     → 200 { "id": 481529, "host": "...", "status": "done", "cached": false, "result": { ... }, "confirmed": { ... } }
```

**Rule 0 (locked):** the live check writes only `check_job.result` and **never advances confirmed state** — no `scan` rows, no `*_pending`/`*_observed`, no `last_checked_at`, no `classification`, no `changelog`, no country/ASN counters. Anonymous POSTs must not accelerate the N-consecutive-scan confirmation. The POST handler's lifecycle re-entry (§5.1.6) may set `last_requested_at`/`next_check_at` and re-enable dead/delisted rows (it *schedules* frontier work, never *advances* confirmed state), and the consumer inserts the initial `domain` row for unknown hosts (§5.1.5 step 2). `check_job` rows are public data; sequential BIGINT ids are enumerable and that is accepted.

**Job id type.** The job `id` is the schema's `BIGINT GENERATED ALWAYS AS IDENTITY` (`check_job.id`, see 05-schema.md — check_job table), served as an integer. If unguessable poll tokens are later judged worthwhile, changing `check_job.id` to UUIDv7/ULID is a deliberate schema change to list in 05 with that rationale — not assumed here.

#### 5.1.1 `POST /check` — processing order

Body: `{"host": "<host>"}`.

1. **Parse + validate.** A body that is not JSON → `415 unsupported-media-type`. A body that lacks the `host` key or has a non-string value → `400 invalid-parameter`. Then `Canonicalize(host)` (§2.8), then the `POST /check`-only policy layer: reject IP literals (already dead at Canonicalize) and reserved TLDs (final label ∈ {`test`, `example`, `invalid`, `localhost`, `internal`, `local`}). Failure → `400 invalid-parameter`. (SSRF is already handled by the engine's pinned dialer; these rejections are the API-boundary layer on top.)
2. **Rate limit (§6.3).** Count `check_job` rows for this `requester_ip` /64 prefix with `created_at > now() - interval '1 hour'`; limit `live_check.rate_ip_per_hour` (default 10). Then the global count over the same window; limit `live_check.rate_global_per_hour` (default 500). Exceeded → `429 rate-limited` (problem+json with `retry_after`) + `Retry-After` header. `retry_after = ceil(3600 − (now − min(created_at)))` seconds over the counted window rows.
3. **Lifecycle re-entry** (runs after rate limiting, before dedupe, on every POST whose host already has a `domain` row — including dedupe hits): apply §5.1.6.
4. **Dedupe, domain-side.** If a `domain` row exists and `last_checked_at >= now() - interval '1 hour'` (`live_check.dedupe_window`), load its latest `scan_detail`, run the shared mapper (§5.1.4) over `details`, and return `200` with a **synthetic done envelope**: `id: null`, `host`, `status:"done"`, `cached:true`, `created_at`/`completed_at` = the scan_detail `ts`, `error:null`, `result` = mapper output, `confirmed` = §5.1.3's object from the domain row. No job row is created.
5. **Dedupe, job-side.** Else if a `check_job` for the same canonical host has `status='done' AND completed_at >= now() - interval '1 hour'`, return `200` with that job's §5.1.3 envelope, `cached` overridden to `true`.
6. Else `INSERT INTO check_job (host, requester_ip) VALUES ($1, $2)` → status `pending`; return `202 { "id":..., "host":..., "status":"pending", "created_at":... }` (exactly these four keys) + `Location: /check/{id}` + `Cache-Control: no-store`.

#### 5.1.2 `GET /check/{id}`

`{id}` must parse as a positive int64; non-numeric or no row → `404 not-found`. Success → `200`:

```json
{
  "id": 123,
  "host": "example.com",
  "status": "pending",           // pending|processing|done|failed
  "cached": false,               // false on every job row; true only in §5.1.1 dedupe envelopes
  "created_at": "2026-07-06T10:00:00Z",
  "completed_at": null,          // set when done|failed
  "error": null,                 // short string when failed
  "result": null,                // object (§5.1.3) when done
  "confirmed": null              // object (§5.1.3) when a domain row exists
}
```

**Poll caching.** A terminal (`done`/`failed`) job is immutable, so it is served `Cache-Control: public, max-age=60` (a job id is minted per request, so this is safe); `pending`/`processing` stay `no-store`. Recommended client poll interval is **2 s** (documented in the OpenAPI). This closes the one anonymous read path the CDN otherwise cannot absorb without adding a limiter to a read path.

#### 5.1.3 Result + confirmed objects

`result` (produced by the shared mapper §5.1.4; statuses use the raw-observation vocabulary `supported|unsupported|no_record|not_applicable|error` — plus `inconsistent` for base/www when the resolver quorum split; live results are raw observations, explicitly NOT confirmed state):

```json
{
  "checked_at": "2026-07-06T10:00:41Z", "duration_ms": 4183,
  "checks": {
    "base": {"status": "supported"}, "www": {"status": "supported"},
    "ns": {"status": "supported"},   "mx": {"status": "not_applicable"},
    "conn": {"status": "supported"}, "resources": {"status": "unsupported"},
    "tls": {"status": "supported"},  "smtp": {"status": "not_applicable"},
    "parity": {"status": "supported"}, "dnssec": {"status": "unsupported"},
    "ptr": {"status": "supported"},  "spf": {"status": "supported"}
  },
  "latency": {"v4_ms": 12, "v6_ms": 14}
}
```

`confirmed` (from the `domain` row; `null` if no row exists or nothing confirmed yet — all six statuses NULL), computed at **read time** on every `GET /check/{id}` and on dedupe responses:

```json
{"classification":"partial","class_flags":["mail_missing"],"saint":false,
 "status":{"base":{"value":"supported","since":"..."}, "...": "six §4.1 status objects"},
 "as_of":"2026-07-06T04:12:09.331Z"}
```

(`as_of` = `domain.last_checked_at`.)

#### 5.1.4 Shared result mapper (one implementation, four consumers)

`MapLiveResult(sr checker.ScanResult) → result JSON`. Applies the engine→public dimension mapping exactly (keys are the PUBLIC dimension names, not engine check names):

- `base` ← `dns_aaaa_base`; `www` ← `dns_aaaa_www` (consensus-composite observations, including `inconsistent` on quorum split)
- `ns` ← `dns_ns_ipv6` (engine `partial` → `supported`); `mx` ← `dns_mx_ipv6` (`partial` → `supported`)
- `conn` ← the worker-side https/http composition table (02-observation-model.md — `conn` composition; `https_ipv6` with `http_ipv6` fallback)
- `tls`, `parity`, `dnssec`, `ptr`, `spf` ← informational, raw engine status; `smtp` ← informational with `partial` → `unsupported`
- `latency` ← `latency_ipv4`/`latency_ipv6` (`{"v4_ms":int|null,"v6_ms":int|null}`)
- `resources` is NOT engine-mapped: it is the registry roll-up, computed **read-only** over the run's `resource_discovery` host list against confirmed `resource_host.aaaa_status` (discovery `error` → `error`; `not_applicable` → `not_applicable`; missing/unswept → NULL → `error`; while `crawler.resources.enabled=false` → `not_applicable`). No registry rows are written on this path, per Rule 0.

Because `scan_detail.details` stores the engine ScanResult serialization, the same mapper serves the domain-side dedupe path (§5.1.1 step 4) and the domain-detail `evidence` block (§4.3). It is ALSO the mapping the frontier worker uses before the confirmed-status commit — one mapping, four consumers. Implementation lives with the crawler-facing mapping code (02-observation-model.md — `internal/crawler/observe.go`); the API imports it.

#### 5.1.5 Consumer (contract; runs in `cmd/crawler` — placement/pool/shutdown in 04-lifecycle-scheduling.md)

Dedicated goroutine pool: `live_check.workers` (default 4) slots; poll every 2 s when idle.

1. Claim one job:

```sql
UPDATE check_job SET status='processing', claimed_at = now()
WHERE id = (
  SELECT id FROM check_job
  WHERE status = 'pending'
     OR (status = 'processing' AND claimed_at < now() - interval '5 minutes')
  ORDER BY created_at
  LIMIT 1 FOR UPDATE SKIP LOCKED
) RETURNING id, host;
```

2. Ensure a `domain` row: `INSERT INTO domain (host, kind, parent_id, rank, created_by, last_requested_at) VALUES ($host, $kind, $parent_id, NULL, 'live_check', now()) ON CONFLICT (host) DO NOTHING`. `kind` via the PSL helper; `parent_id` set only if the registrable parent row ALREADY exists — live checks never auto-ensure parents. New rows keep the default `next_check_at = now()`, so the frontier scans the host promptly.
3. Run the full engine with a 60 s context budget (`live_check.job_budget`), panic-recovered.
4. On success: `UPDATE check_job SET status='done', result=$1, completed_at=now() WHERE id=$2`. On error/timeout: `UPDATE check_job SET status='failed', error=$2, completed_at=now() WHERE id=$3`. Nothing else is written (Rule 0).

**Reaper** (same goroutine, every 60 s) — guarantees every poller terminates ≤15 min:

```sql
UPDATE check_job SET status='failed', error='timed out', completed_at=now()
WHERE status IN ('pending','processing') AND created_at < now() - interval '15 minutes';
```

**Retention** (daily tick, owned by 04-lifecycle-scheduling.md): `$1` = `live_check.retention` (duration, default 720 h = 30 d).

```sql
DELETE FROM check_job WHERE created_at < now() - $1;
```

#### 5.1.6 Lifecycle re-entry (POST-handler writes on existing rows)

Every `POST /check` for an existing host sets `last_requested_at = now()` — the "live-check origin within 7 days" linkage evaluated by the daily lifecycle sweep (`lifecycle.live_check_linkage`, default 168 h), extending the frontier life of any rank-NULL row a user watches. Additionally:

- `disabled_reason = 'delisted'` → re-enable: clear `disabled`, `disabled_reason`, `disabled_at`; `orphaned_at = NULL`; `next_check_at = now()`.
- `disabled_reason = 'dead'` → leave disabled but set `next_check_at = now()`: recovery happens via the pulled-in frontier scan.
- `disabled_reason IN ('service','manual')` → the live check runs and returns its result, but never re-enables.

Config keys (crawler config; registry: 09-ops.md): `live_check.workers`, `live_check.job_budget`, `live_check.reclaim_after`, `live_check.fail_after`, `live_check.retention`, `live_check.rate_ip_per_hour`, `live_check.rate_global_per_hour`, `live_check.dedupe_window`, `live_check.link_ttl`.

*Rejected — synchronous inline check:* a full engine run can take 60–90 s; holding an anonymous request open that long invites trivial resource-exhaustion abuse. Job + poll matches ready.chair6.net-class tools.

#### 5.1.7 `GET /check/latest?host=` — the shareable-link read side

The frontend's shareable check URLs are `/check/{domain}` (12-frontend §10.1); this endpoint is their read side: the freshest stored result for a canonical host **within `live_check.link_ttl`** (duration, default `168h`; registry: 09-ops.md). Lookup order mirrors the POST dedupe pair, with the TTL replacing the dedupe window:

1. **Domain-side** — a `domain` row with `last_checked_at >= now() - link_ttl`: the §5.1.1-step-4 synthetic done envelope from the latest `scan_detail` (tracked domains crawl daily, so this branch serves virtually all of them).
2. **Job-side** — else the newest `check_job` with `status='done' AND completed_at >= now() - link_ttl`, `cached` overridden to `true`.
3. Neither → `404 not-found`. The client decides whether to enqueue a fresh check via `POST /check` — the frontend does so automatically, which is what makes an expired link self-heal into a recheck.

Read-only, never rate-limited, `Cache-Control: public, max-age=60`. An invalid host → `400 invalid-parameter`. The route is the static `/check/latest`, registered alongside `/check/{id}` (static wins).

### 5.2 Embeddable SVG badge

`GET /badge/{host}.svg` — a shields.io-flat SVG rendered from confirmed status. Feature-research ranks the badge **#1** (viral distribution; every badge is a backlink) — committed, not optional.

- `Content-Type: image/svg+xml; charset=utf-8`; `Cache-Control: public, max-age=86400`; `X-Content-Type-Options: nosniff`; an `ETag` from the crawl generation; **no rate-limit** (CDN-cacheable).
- **Read-only, zero side effects:** never inserts a domain row, never enqueues a check_job, never touches `last_requested_at`. Not rank-scoped (any kind/origin resolves).
- The `.svg` suffix is part of the route pattern, not the `{host}` param; a suffix-less path is a route-miss `404`.
- **Declared exception to the 404-on-canonicalize-failure rule:** an invalid host (Canonicalize failure or reserved-TLD, §2.8) → `400 invalid-parameter` JSON (a malformed embed is not a legitimate request). A *valid* host is **always `200`** (a `404` renders as a broken image); disabled/unknown → the gray `IPv6: unknown` badge. XML-escape the host label into the SVG to prevent markup injection.

Six precompiled byte-deterministic variants, one per `classification`(+`saint`) input. Copy is **public status vocabulary**, never ladder branding — a README badge never says "sinner"/"hero"/"saint" (which is why the saint variant's label is the neutral `full`, ADR 0003). The mapping is normative:

| `classification` (+ `saint`) | SVG message label | shields color | `isError` (JSON variant) |
|---|---|---|---|
| `hero` | `IPv6: supported` | `brightgreen` | `false` |
| `hero` + `saint: true` | `IPv6: full` | `brightgreen` (gold accent) | `false` |
| `partial` | `IPv6: partial` | `yellow` | `false` |
| `sinner` | `IPv6: no IPv6` | `red` | `false` |
| `inactive` | `IPv6: inactive` | `lightgrey` | `false` |
| no row / `disabled` / `unknown` | `IPv6: unknown` | `lightgrey` | `true` |

`sinner`'s label is `no IPv6` (not "unsupported" or "no record"). `unknown` sets `isError:true` so shields renders it as a genuine error. First match wins: no row / `disabled` / `unknown` → gray `unknown`; then `hero+saint` → full; `hero` → supported; `partial` → partial; `sinner` → no IPv6; `inactive` → inactive. The six SVGs are shields.io-flat, label `IPv6`, fixed-geometry `textLength` (byte-deterministic, no font measurement, no dependencies); the exact template and per-label geometry constants are **golden-file fixtures in 10-testing.md** (§7.4 badge goldens, re-scoped to this six-variant table). The copy/color table is ONE Go constant table — the single place to reword.

**Shields.io endpoint-JSON variant** for users who want shields styling: `GET /badge/{host}.json` → `{"schemaVersion":1,"label":"IPv6","message":"supported","color":"brightgreen","cacheSeconds":86400,"isError":false}`. `message`/`color`/`isError` come from the table above. This JSON deliberately uses shields.io's **camelCase** field names (the one sanctioned camelCase exception, §2.3). Users embed `https://img.shields.io/endpoint?url=https://api.whynoipv6.com/badge/example.com.json`. Documented usage string: `![IPv6](https://api.whynoipv6.com/badge/example.com.svg)`.

**Pre-phase-5 interaction:** `domain.saint` is false for everyone (`crawler.resources.enabled=false`), so heroes render `supported` — correct, no special case.

### 5.3 Datasets — static bulk + manifest + citation

Bulk data is a **separate static channel**, not the paginated API (keeps scrapers off the pagination path; avoids the BigQuery-only mistake that spawns unofficial mirrors).

- **Served statically by nginx** under `/datasets/` (not the API). Config key `DATASETS_DIR` (string, default `/var/lib/whynoipv6/datasets`; registry: 09-ops.md), shared by the API binary and `v6ctl export`. Nightly `v6ctl export` (owned by 04-lifecycle-scheduling.md, after the stats tick) produces, atomically (tmp-dir `rename(2)`), 3 size tiers (`top100k`, `top1m`, `full`) × formats **CSV.gz + Parquet** (exactly the two formats the manifest `formats` array lists; no JSONL artifact in this build). Columns: `host, rank, kind, parent, classification, class_flags, saint, {6 confirmed statuses}, {6 since-timestamps}, tld, country, asn, dns_provider, hosting_provider, last_checked`. `top100k`/`top1m` use the publicly-ranked predicate; `full` = all non-disabled scannable entities. Dailies retained 90 d, first-of-month forever.
- **Self-describing + verifiable (OPEN-6):** each snapshot ships a Frictionless **`datapackage.json`** — a `resources[]` array with per-file `path`, `bytes`, `hash: "sha256:<digest>"` (always the `sha256:` prefix; a bare hash means MD5 to the spec), and a Table Schema of column names/types — plus a `SHA256SUMS` file and a `DICTIONARY.md`.

On-disk layout:

```
/var/lib/whynoipv6/datasets/
├── manifest.json                      # the one file the API serves; rewritten atomically after every export
├── DICTIONARY.md                      # column + status-semantics docs
├── latest -> 2026-07-07               # symlink to newest COMPLETE snapshot
├── 2026-07-07/                        # immutable once published
│   ├── datapackage.json               # Frictionless: files, hashes, Table Schema
│   ├── whynoipv6-top100k.csv.gz
│   ├── whynoipv6-top100k.parquet
│   ├── whynoipv6-top1m.csv.gz
│   ├── whynoipv6-top1m.parquet
│   ├── whynoipv6-full.csv.gz
│   ├── whynoipv6-full.parquet
│   └── SHA256SUMS                     # sha256sum -c compatible
└── 2026-07-06/ ...
```

- **The top-level `manifest.json` is a distinct index over snapshots, not an alias of a per-snapshot `datapackage.json`.** Its schema is pinned (this is the response schema of `GET /datasets`, added to the OpenAPI `components` so the drift gate §7 contract-tests it):

```json
{
  "schema_version": 1,
  "generated_at": "2026-07-07T04:10:00Z",
  "generation": 20260707,
  "license": "CC-BY-NC-4.0",
  "attribution": "Data: whynoipv6.com (CC-BY-NC-4.0). Ranks: Tranco list <id>.",
  "latest": {
    "date": "2026-07-07",
    "path": "datasets/2026-07-07/",
    "datapackage_url": "/datasets/2026-07-07/datapackage.json"
  },
  "snapshots": [
    {
      "date": "2026-07-07",
      "path": "datasets/2026-07-07/",
      "tiers": ["top100k", "top1m", "full"],
      "formats": ["csv.gz", "parquet"],
      "datapackage_url": "/datasets/2026-07-07/datapackage.json",
      "sha256sums_url": "/datasets/2026-07-07/SHA256SUMS"
    }
  ]
}
```

`schema_version` (int) versions this index shape and the exported column set, and bumps whenever either changes. Each `snapshots[]` entry points at that snapshot's `datapackage.json` (which lists the actual files, hashes, and Table Schema). `snapshots` is sorted newest-first and lists every snapshot currently retained; `latest` duplicates the newest complete snapshot's entry.

- **Citable:** the **dated (immutable) path** is the citation anchor; a stable `/datasets/latest/whynoipv6-top1m.csv.gz` alias is the convenience URL for scripts. An optional **Zenodo Concept DOI** + per-snapshot **Version DOI** (monthly/quarterly cadence) is a later additive step gated on demand (OPEN-6), outside the daily crawler.
- **License in-band:** **CC-BY-NC-4.0** stated in the manifest, the OpenAPI `info` block, and `DICTIONARY.md`, with the required attribution string. **Tranco rank redistribution (OPEN-13):** CC-BY-NC-4.0 covers *our* derived measurements; it does not by itself grant the right to redistribute Tranco's ranking, which the datasets re-publish as a `rank` column. **Decision (OPEN-13): the bulk exports ship WITH the `rank` column.** Cite the specific Tranco list ID/permalink in `DICTIONARY.md` + `datapackage.json` + the manifest `attribution` and honor Tranco's attribution requirement. The redistribution-terms check is a post-launch action item, not a build gate; if it finds bulk rank redistribution restricted, `rank` is dropped from subsequent exports (a `schema_version` bump) while per-entity `rank` stays on the live API.

**Atomic publish** (nightly): write all files + `datapackage.json` + `SHA256SUMS` into `{date}.tmp/`, fsync, `rename({date}.tmp, {date})`; repoint `latest` via `ln -sfn` + `mv -T` (rename(2), no missing window); prune per retention; regenerate `manifest.json` from the directory tree, write `manifest.json.tmp`, rename over `manifest.json`. On any failure before the snapshot rename, delete the `.tmp` dir and fire the ops webhook; the previous manifest/latest stay correct.

**The only dataset piece the API serves:** `GET /datasets` re-reads `$DATASETS_DIR/manifest.json` from disk every request and returns it verbatim as `application/json`, `Cache-Control: public, max-age=300`. Missing/unparseable → `503 manifest-unavailable` (the only 503).

nginx location split (sibling locations in the api.whynoipv6.com server block; deployed per 09-ops.md):

```nginx
# exact match: manifest endpoint → API (§1.2 proxy_set_header block applies)
location = /datasets {
    proxy_pass http://[::1]:8080;
    proxy_set_header X-Real-IP       $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header Host            $host;
}
# dated snapshots: immutable forever
location ~ ^/datasets/\d{4}-\d{2}-\d{2}/ {
    root /var/lib/whynoipv6;
    add_header Cache-Control "public, max-age=31536000, immutable";
    add_header Access-Control-Allow-Origin "*";
    gzip off;   # payloads are pre-compressed (.csv.gz) or binary (.parquet)
}
# latest/ symlink + DICTIONARY.md: mutable, short TTL
location /datasets/ {
    root /var/lib/whynoipv6;
    autoindex off;
    add_header Cache-Control "public, max-age=3600";
    add_header Access-Control-Allow-Origin "*";
    gzip off;
}
```

### 5.4 Change feeds — Atom + JSON Feed per scope

The changelog is the only account-free **push** channel and near-zero cost (the structured changelog rendered as a feed). Feed representations are exposed as **distinct suffix URLs** (keeps CDN cache keys clean; embeddable in READMEs/feed readers), per scope:

| Scope | Atom | JSON Feed 1.1 |
|---|---|---|
| Global | `/changelog.atom` | `/changelog.feed.json` |
| Per-domain | `/domains/{host}/changelog.atom` | `.feed.json` |
| Per-campaign | `/campaigns/{uuid}/changelog.atom` | `.feed.json` |
| Per-country | `/countries/{code}/changelog.atom` | `.feed.json` |

The extension-less path stays the paginated JSON-API list (§4.8).

**Feed-level contract.** Every feed is a **fixed recent window of the latest 50 transitions, no pagination** (bulk consumers → datasets §5.3; honors the scoped-feed cap in §4.8). Required top-level members:

- **Atom (RFC 4287, `application/atom+xml`):** feed `<id>` = the scope's canonical API URL (e.g. `https://api.whynoipv6.com/countries/NO/changelog`); `<updated>` = `max(ts)` in the window; `<title>` = the scope name ("WhyNoIPv6 — Norway", "WhyNoIPv6 — example.com", global "WhyNoIPv6 — recent changes"); `<link rel="self">` = the `.atom` URL, `<link rel="alternate">` = the extension-less JSON list URL.
- **JSON Feed 1.1 (`application/feed+json`):** `version` (`https://jsonfeed.org/version/1.1`), `title`, `home_page_url` (the list URL), `feed_url` (the `.feed.json` URL), `items`.

Per item: `id`/`guid` = the composite `(host, ts, field)`; `date_published` = `ts` (RFC 3339); the human `title`/`content_text` is derived **server-side at render time** from `(field, old_value, new_value)` — e.g. "example.com now supports IPv6 on www" — freshly, not from any frozen message table.

### 5.5 CSV export via content negotiation

**`?format=csv`** on the list endpoints (`/domains*`, `/countries`, `/asns`, `/providers`, `/changelog`, search results) — a query param keeps the CDN cache key clean and the shareable URL stable. `Content-Type: text/csv; charset=utf-8`, `Content-Disposition: attachment`. A defined column set per list (the §4.2 summary-row columns for `/domains`). The stable, cursor- or `after_rank`-anchored URL means a shared link reproduces the same view. **Cap:** a CSV request honors the same filters and cursor as the JSON view but raises the `?limit` cap from 200 to `export.csv_max_rows` (int, default 10000; registry: 09-ops.md); larger "give me everything" pulls are steered to the static datasets (§5.3), not deep CSV pagination.

*Rejected — `Accept: text/csv` negotiation* (needs `Vary: Accept`, invisible in a shared link). `Accept` is reserved for the JSON core (default `application/json`; `406 not-acceptable` when unsatisfiable).

### 5.6 Methodology, mandates

- **Diff / "who went green"** (OPEN-7, re-resolved: **cut from this build**). A dedicated `GET /diff` endpoint added no information the confirmed-transition surfaces don't already carry — "who went green between A and B" is the `/changelog` list (§4.8) filtered client-side, and "what changed recently" is the change feeds (§5.4). Its response contract was never pinned; rather than invent one, the endpoint is dropped. It can return later as a purely additive endpoint if a real consumer appears.
- **Methodology** (trust lever): `GET /methodology` returns the deterministic Hero/Partial/Sinner ladder + Saint rule + flag definitions as structured JSON, plus a `criteria_changelog[]` of every rule change (recalibrate Saint in the open). Mostly static content; the `class_flags` vocabulary it documents is the join key the frontend uses to select fix guides.
- **Government-mandate compliance tracking** (OPEN-12, resolved YES — explicitly wanted): the campaign `tags`/mandate capability (the `campaign.tags` TEXT[] column with GIN index `idx_campaign_tags`, 05-schema.md — campaign table) backs a **`?tag=`** filter on `GET /campaigns` and a **`GET /mandates`** surface. **Decision — the mandate predicate and shape:** a campaign is a mandate iff it carries the literal tag `mandate` (`'mandate' = ANY(tags)`, GIN-served); descriptive companion tags (`eu-2030`, `sector-banking`) are ordinary kebab-case tags per the 06-ingest.md tag grammar (no namespace colons). `GET /mandates` ≡ `GET /campaigns?tag=mandate` — the standard campaign list envelope, nothing bespoke; there is no `citations` field (legal/citation copy is frontend content keyed by campaign `name`/`description`, not API data). The `campaign` resource (§4.7) exposes `tags`.
- **Contact-discovery / notification toolkit** (OPEN-8, resolved NO) — **not built**; templates remain static.
- **OG/social cards** — deferred; pairs with the badge renderer (same image pipeline), unverified impact.

---

## 6. Caching, content negotiation, rate limiting, compression

Daily-batch, fully-public data behind a CDN is the ideal caching case; caching + a CDN is the single biggest performance lever for an anonymous read-heavy site facing viral traffic.

### 6.1 Caching by endpoint class

| Class | `Cache-Control` | Validators |
|---|---|---|
| List / leaderboard / stats (`/domains*`, `/countries*`, `/asns*`, `/providers*`, `/stats/*`) | `public, max-age=300, s-maxage=<until-next-crawl>, stale-while-revalidate=600, stale-if-error=86400` | `ETag` from crawl `generation` (+ query fingerprint); `Last-Modified` from crawl ts; honor `If-None-Match` → `304` |
| Domain / country / asn / provider / campaign detail | as above; ETag tied to that entity's last confirmed transition | `304` on `If-None-Match` |
| Changelog lists (`/changelog` + scoped) and change feeds (`*.atom`, `*.feed.json`) | `public, max-age=300` | `ETag` from the scope window's `max(changelog.ts)` (+ query fingerprint) — **not** the daily `generation`: the crawler commits transitions continuously, and a generation-seeded ETag would 304-freeze the live surface until the next stats tick |
| Badge SVG/JSON (`/badge/*`) | `public, max-age=86400` | `ETag` from generation |
| Datasets manifest (`/datasets`) | `public, max-age=300` | — |
| Static datasets (nginx) | dated dirs `immutable, max-age=31536000`; `latest/` + `DICTIONARY.md` `max-age=3600` | nginx auto ETag |
| Live check poll (`GET /check/{id}`) — **terminal** (`done`/`failed`) | `public, max-age=60` (immutable job) | — |
| Live check (`POST /check`, `GET /check/{id}` while `pending`/`processing`), `/ip` | `no-store` | — |
| Health (`/livez`,`/readyz`) | `no-store` | — |

Use `public` (never `private`) — there is no per-user data, and `public` is what unlocks CDN edge caching. **ETags must be deterministic across backend instances** — derive from the crawl `generation` (`= YYYYMMDD` of `max(stats_global_daily.day)`, §2.4) + query fingerprint (for lists), the entity's last confirmed-transition ts (for detail), or the scope window's `max(changelog.ts)` (for changelog lists and feeds), never from a per-process hash. The badge and manifest `max-age` literals above are the defaults of `badge.cache_ttl` (24h) and `datasets.manifest_cache_ttl` (5m) respectively (registry: 09-ops.md) — override the key and the header follows.

### 6.2 Content negotiation

`Accept`-header negotiation for the JSON core (default `application/json`; `406 not-acceptable` when unsatisfiable, as a Problem Detail). Genuinely-different representations are **distinct URLs**, not `Accept` variants: badge `.svg`/`.json`, feeds `.atom`/`.feed.json`, CSV `?format=csv`. Set `Vary: Accept` on any Accept-negotiated endpoint and `Vary: Accept-Encoding` everywhere compression applies.

### 6.3 Rate limiting

**Only `POST /check` is rate-limited** — cacheable GETs (including the now-cacheable terminal poll responses, §5.1.2) are absorbed by the CDN and need no limiter. Keyed on the **CDN-forwarded real client IP** (§1.2), on the **/64 prefix** (not the /128): per-IP `live_check.rate_ip_per_hour` (default 10) and global `live_check.rate_global_per_hour` (default 500), counted over `check_job` rows in a 1 h window. On breach: `429 rate-limited` (Problem Detail with `retry_after`) + `Retry-After` header. Emit the IETF-draft **`RateLimit`** + **`RateLimit-Policy`** structured-field headers (RFC 9651 syntax); optionally mirror legacy `X-RateLimit-*`. Limiting on the /64 stops address rotation and avoids over-throttling NAT'd CGNAT IPv4 users; keep limits generous — the endpoint is a public good. Encourage (don't require) a descriptive `User-Agent` / optional `?sourceapp=` for high-volume research clients.

### 6.4 Compression

gzip + Brotli at the **nginx/CDN edge** for all JSON / CSV / SVG-text / Atom responses over ~256 bytes, with `Vary: Accept-Encoding` (Brotli preferred, gzip fallback). The Go origin emits uncompressed bodies (simpler; origin sits behind nginx+CDN). BREACH-class risk is a non-issue — responses are public data with no secrets or tokens. Don't double-compress the pre-compressed dataset files or the tiny SVG badge.

---

## 7. OpenAPI-first workflow

A hand-authored **`openapi.yaml` (OpenAPI 3.0.3)** at `openapi/openapi.yaml` (monorepo root `openapi/` directory, 00-overview.md §4) is the single source of truth; both sides generate from it, and CI blocks drift.

- **Version 3.0.3, not 3.1.** oapi-codegen mainline (the chi-server + strict-server generators) does not yet support 3.1. Since chi v5 is locked, oapi-codegen `chi-server` + `strict-server` is the natural spec-first Go path. This API's payloads — the status/classification enums, aggregate percentages, cursor pagination, RFC 9457 errors — are all expressible in 3.0.3 (using `nullable: true` for the `null` status values), so the 3.1 gap costs nothing.
- **Go side:** `github.com/oapi-codegen/oapi-codegen/v2` (latest release) generates server interface + types + request validation in `internal/api/gen/` (committed); handlers implement the generated strict-server interface; hand-written code never redefines wire types. *Rejected — ogen* (native 3.1 but owns the router/server, displacing the locked chi v5).
- **Frontend side:** **openapi-typescript** (types, zero runtime deps) + **openapi-fetch** (tiny typed fetch wrapper) — the thin gold standard for a read-only public API, composing with Vue 3.5 + Pinia. The snake_case wire (§2.3) yields snake_case TS property names — non-idiomatic but fully type-safe. *Rejected — Orval/Hey-API/Kubb* (batteries this API doesn't need yet).
- **Drift gate:** `make generate` runs oapi-codegen (Go) + openapi-typescript (TS); CI regenerates and `git diff --exit-code`s to block staleness, and lints the spec with Spectral/vacuum (enforcing the snake_case rule, the `{items,page,meta}`/`{points,meta}` envelopes, and the `problem+json` error schema). The `manifest.json` schema (§5.3), the keyset cursor grammar, the RFC 9457 problem shapes, and the badge/feed representations are all in `components` so the gate covers them. This replaces the deleted API-compat parity testing against the old backend.
- **Discoverability:** serve `openapi.json` at a stable path with Redoc/Swagger UI; additionally publish an `llms.txt` docs index. A read-only MCP server is a cheap later add. Do **not** add a free-account+API-key gate — it violates the anonymous lock.
