# 07 — HTTP API Contract

**Purpose:** The complete, self-contained HTTP contract of the public API: server baseline, the frozen legacy surface the production Vue frontend depends on (byte-level quirks included), all new endpoints, the badge, the live-check lifecycle, the datasets manifest, and the OpenAPI conventions. An implementer must be able to build `internal/api` and `openapi/openapi.yaml` from this file alone, capturing golden fixtures from the named production source files.

**Deliverables:**
- `internal/api/` — chi router + middleware stack (`router.go`), one handler file per route group (`domain.go`, `country.go`, `changelog.go`, `campaign.go`, `metric.go`, `stats.go`, `resource.go`, `check.go`, `badge.go`, `datasets.go`, `misc.go`), legacy serialization helpers (`legacy.go`: `legacyStatus`, timestamp mapping, `renderChangelog`), shortuuid codec (`uuid.go`), shared pagination/error helpers (`http.go`), generated code in `internal/api/gen/` (oapi-codegen output, committed).
- `openapi/openapi.yaml` — spec-first source of truth for every endpoint in this file.
- The live-check **contract** (§6): POST/GET envelopes, dedupe, consumer/reaper/retention SQL. (The consumer goroutines run inside `cmd/crawler` — process placement, pool wiring, and shutdown are owned by 04-lifecycle-scheduling.md.)
- Nginx location blocks for the API vhost and `/datasets/` (deployed via 09-ops.md).

**Companion files:** 05-schema.md (every table/column referenced here — this file contains **no DDL**), 04-lifecycle-scheduling.md (check-job consumer placement, frontier scheduling touched by live-check re-entry), 02-observation-model.md (the `conn` composition table and the shared engine→public dimension mapper the §6.4 live-check reader imports), 00-overview.md (sizing-constants table, monorepo layout), 09-ops.md (consolidated config-key registry, systemd/nginx deploy), 10-testing.md (golden parity fixtures and synthetic fixtures for every rule in this file).

Reference repos: production backend `whynoipv6` (golden-fixture source, file references given per endpoint), frozen frontend `whynoipv6-web/src/services/*.ts` (caller inventory), campaign repo (live campaign UUIDs used as codec test vectors).

---

## 1. Server baseline (applies to every endpoint)

### 1.1 Listen address

Config key `API_LISTEN` (string, default `[::1]:8080`; registry: 09-ops.md). The API binds IPv6 loopback **by design**: it is always fronted by nginx, which terminates TLS and is the only process that can reach it. Document in the README: "Old API bound [::1]:PORT — kept intentional but documented." Override to `:8080` / `0.0.0.0:8080` only for docker-compose/dev.

### 1.2 Real client IP

Because the bind is loopback-only, every request arrives from nginx and the peer address is useless. Apply chi `middleware.RealIP` **first** in the chain: set the request's remote address from `X-Real-IP` if present, else the first entry of `X-Forwarded-For`, else leave the peer address. This derived address is the **single source of truth** for (a) the `GET /ip` response body and (b) `check_job.requester_ip` in the §6 rate limiter. Operator caveat (state in README): trusting these headers is safe only because the default bind is unreachable except via the local proxy; if `API_LISTEN` is opened to a non-loopback interface without a trusted proxy, per-IP rate limits become spoofable.

Required nginx location config (deployed per 09-ops.md):

```nginx
proxy_set_header X-Real-IP        $remote_addr;
proxy_set_header X-Forwarded-For  $proxy_add_x_forwarded_for;
proxy_set_header Host             $host;
```

### 1.3 CORS

The frontend is cross-origin (whynoipv6.com → api.whynoipv6.com). rs/cors (or chi-compatible equivalent) with production's settings plus `POST` (needed by `POST /check`; production allowed only GET/HEAD/OPTIONS — `whynoipv6/internal/rest/server.go:19-27`):

- AllowedOrigins: `https://*`, `http://*` (allow-all; API is public and anonymous)
- AllowedMethods: `GET`, `HEAD`, `OPTIONS`, `POST`
- AllowedHeaders: `Accept`, `Authorization`, `Content-Type`, `X-CSRF-Token`
- ExposedHeaders: `Link`
- AllowCredentials: `false`
- MaxAge: `300`

### 1.4 Default headers

All responses: `Content-Type: application/json` (default; overridden by `GET /badge/{domain}.svg` → `image/svg+xml`; dataset files are served by nginx, not the API), `X-Content-Type-Options: nosniff`, `X-Frame-Options: deny`. **Decision:** parity tests assert the media type prefix only (`application/json`), never a `charset` parameter — production's go-chi/render emitted `application/json; charset=utf-8`; either form is conformant.

### 1.5 Cache-Control by endpoint class

| Class | Header |
|---|---|
| All JSON API endpoints (legacy §3, new §4, live check §6) | `Cache-Control: no-cache, no-store, no-transform, must-revalidate, private, max-age=0` (chi `middleware.NoCache`, as production) |
| `GET /badge/{domain}.svg` | `Cache-Control: public, max-age=3600` |
| `GET /datasets` (manifest) | `Cache-Control: public, max-age=300` |
| Dataset files | served statically by nginx (§7), not by the API |

### 1.6 Timeouts & graceful shutdown

Production had none; this is a declared cleanup. `http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second}`; per-request `middleware.Timeout(30 * time.Second)`. Graceful shutdown on SIGINT/SIGTERM: `server.Shutdown(ctx)` with a 15s drain budget. `POST /check` is async job+poll (§6), so no handler legitimately exceeds 30s.

### 1.7 Middleware order (outermost first)

RealIP → RequestID → slog request logger → Recoverer → Timeout(30s) → CORS → security/content-type headers → per-route Cache-Control.

Logging follows the shared slog conventions (design §11.5; registry of log keys in 09-ops.md).

### 1.8 Baseline acceptance criteria

(Fixture definitions live in 10-testing.md.)
1. `GET /ip` with header `X-Real-IP: 2001:db8::7` returns `{"ip":"2001:db8::7"}` — not `::1` (guards the frontend `Notification.vue` `ip.includes(":")` IPv4-banner check and the §6 per-IP bucket).
2. `OPTIONS /check` preflight with `Origin: https://whynoipv6.com` and `Access-Control-Request-Method: POST` returns 2xx with `Access-Control-Allow-Origin` and `POST` in `Access-Control-Allow-Methods`.
3. Two `POST /check` requests with different `X-Real-IP` values consume different rate-limit buckets.

---

## 2. Cross-cutting conventions

### 2.1 Route inventory (complete)

Paths mount at the **API root** — no `/api/v1` prefix. `/metric` is **singular**. Legacy (L) shapes are frozen; new (N) endpoints serve the 4-value public enum.

| # | Method | Path | Class |
|---|---|---|---|
| 1 | GET | `/` | L health |
| 2 | GET | `/ip` | L |
| 3 | GET | `/domain` | L |
| 4 | GET | `/domain/heroes` | L |
| 5 | GET | `/domain/topsinner` | L |
| 6 | GET | `/domain/almost` | N |
| 7 | GET | `/domain/search/{q}` | L |
| 8 | GET | `/domain/{domain}` | L (+2 additive keys) |
| 9 | GET | `/domain/{domain}/log` | L |
| 10 | GET | `/domain/{domain}/subdomains` | N |
| 11 | GET | `/domain/{domain}/resources` | N |
| 12 | GET | `/resource/{host}/dependents` | N |
| 13 | GET | `/country` | L |
| 14 | GET | `/country/{code}` | L |
| 15 | GET | `/country/{code}/sinners` | L |
| 16 | GET | `/country/{code}/heroes` | L |
| 17 | GET | `/changelog` | L |
| 18 | GET | `/changelog/campaign` | L |
| 19 | GET | `/changelog/campaign/{uuid}` | L |
| 20 | GET | `/changelog/campaign/{uuid}/{domain}` | L |
| 21 | GET | `/changelog/{domain}` | L |
| 22 | GET | `/campaign` | L |
| 23 | GET | `/campaign/search/{q}` | L |
| 24 | GET | `/campaign/{uuid}` | L |
| 25 | GET | `/campaign/{uuid}/{domain}` | L |
| 26 | GET | `/campaign/{uuid}/{domain}/log` | L |
| 27 | GET | `/metric/overview` | L |
| 28 | GET | `/metric/asn` | L |
| 29 | GET | `/metric/asn/search/{q}` | L |
| 30 | GET | `/stats/overview` | N |
| 31 | GET | `/stats/country/{code}` | N |
| 32 | GET | `/stats/campaign/{uuid}` | N |
| 33 | GET | `/stats/asn/{number}` | N |
| 34 | GET | `/stats/domain/{domain}` | N |
| 35 | POST | `/check` | N |
| 36 | GET | `/check/{id}` | N |
| 37 | GET | `/badge/{domain}.svg` | N |
| 38 | GET | `/datasets` | N |

chi resolves static segments before params, so `/domain/heroes`, `/domain/topsinner`, `/domain/almost`, `/domain/search/{q}` win over `/domain/{domain}` regardless of registration order; same for `/changelog/campaign...` over `/changelog/{domain}`. No trailing-slash redirection middleware (production disabled `RedirectSlashes`); routes match exactly as written.

### 2.2 Pagination

Query params `?offset=` and `?limit=` on every **list** endpoint that declares them below. Defaults: `offset=0`, `limit=50`; `limit` is clamped to max 100 (production `whynoipv6/internal/rest/server.go:63-67` + per-handler clamp). The frontend only ever sends `offset` and hard-assumes page size 50 in its Next-button logic — the default of 50 is wire-frozen.

**Decision:** input sanitization (production passed raw values through and could 500 on `LIMIT -1`): non-integer `offset`/`limit` → use the default; `offset < 0` → `0`; `limit < 1` → `50`; `limit > 100` → `100`. Never an error status for pagination params.

There is no total-count key, no `Link` header, no page envelope: list responses are bare JSON arrays (except the two `{"data":[...]}` search envelopes, §3.8/§3.16).

### 2.3 Error envelope and status codes

Every non-2xx response is `application/json` with an object carrying an `error` string. Legacy endpoints use the **byte-exact production bodies** pinned per endpoint in §3 (capitalization differs between endpoints — that is deliberate bug-compatibility). New endpoints use:

- `404` → `{"error":"not_found"}`
- `400` → `{"error":"invalid_parameter","message":"<human-readable>"}` (exceptions with their own pinned bodies: `POST /check` and the badge use `{"error":"invalid_host","message":"..."}`; rate limiting uses `{"error":"rate_limited",...}` — §6)
- `503` → `{"error":"manifest_unavailable"}` (only `GET /datasets`)

**Decision:** all internal failures (DB down, query error) return `500 {"error":"internal server error"}` on every endpoint, legacy and new. Production mixed `"internal server error"` and `"Internal server error"`; 500 bodies are not part of the frozen contract (the frontend treats any 5xx generically), so one lowercase body is used everywhere.

### 2.4 JSON encoding rules

- Timestamps serialize via Go `time.Time` default marshaling (RFC 3339, UTC, sub-second digits as stored). NULL source timestamps on legacy endpoints serialize as the Go zero time `"0001-01-01T00:00:00Z"` (rule R3, §2.8).
- `DATE` columns on new endpoints serialize as `"YYYY-MM-DD"` strings.
- Empty lists are `[]`, never JSON `null` (the []-cleanup; status-code map in §2.11).
- `NUMERIC(5,2)` (`country.percent`) serializes as a plain JSON number (e.g. `17.42`) — the new column type kills production's pgtype ÷10 hack (`country.go:61-66`).
- Legacy `rank` is a JSON integer. **Decision:** entities with `rank IS NULL` (campaign-only domains, subdomains, live-check hosts — reachable via entity/detail endpoints only) serialize `"rank": 0`, matching production's zero-value encoding of unset struct fields.

### 2.5 Hostname path-parameter canonicalization

Every path parameter carrying a hostname — `{domain}` in routes 8, 9, 10, 11, 21, 25, 26, 34 and 20's second param, `{host}` in route 12, and the badge's `{domain}` (route 37, after stripping the `.svg` suffix) — passes through the single shared `Canonicalize()` helper before any DB lookup. One implementation, `internal/domain/host.go`, shared with the crawler and v6ctl:

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

**Failure policy for API path params:** plain **404** — it is a lookup miss, not a client contract violation. **Decision:** the 404 body on Canonicalize failure is the same body that route returns for an unknown host (pinned per endpoint in §3/§4), so clients cannot distinguish "malformed" from "not in the database". The **one exception** is `GET /badge/{domain}.svg`, which returns `400 {"error":"invalid_host","message":"..."}` per §5. This canonicalization intentionally supersedes production's mixed behavior (production `domain.go:193` and `campaign.go:280` lowercase; the changelog handlers don't, and `campaign.go:220`/`changelog.go:141` applied a regex with a 400): previously-404 mixed-case URLs now resolve, and the regex-400 paths become Canonicalize-404s. This is NOT a bug-compat quirk — the §3 quirks cover response shapes, not lookup normalization.

Where an error message echoes the domain (§3.13, §3.14), it echoes the **raw path parameter as given in the URL**, not the canonical form (production parity).

### 2.6 Publicly-ranked visibility predicate

```sql
rank IS NOT NULL AND NOT disabled
```

(Columns on the `domain` table — see 05-schema.md — domain table. Only the Tranco importer writes `rank`, and Tranco is eTLD+1, so `rank IS NOT NULL` implies `kind='apex'`. `created_by` is irrelevant to visibility.)

Endpoints that select ONLY rows matching this predicate, in addition to their classification filter:

- `GET /domain` (sinners), `GET /domain/heroes`, `GET /domain/almost`
- `GET /domain/topsinner` (the `top_shame` join additionally requires the predicate)
- `GET /country/{code}/sinners`, `GET /country/{code}/heroes`
- `GET /domain/search/{q}`
- `GET /changelog` (global feed; see §3.12)

Ordering on every ranked list: `ORDER BY rank ASC` (NULLs are excluded by the predicate). Queries must spell out `AND rank IS NOT NULL AND NOT disabled` **verbatim** so the planner's partial-index predicate-implication check is trivial (indexes `idx_domain_sinners`/`idx_domain_heroes`/`idx_domain_partial`/`idx_domain_country`, see 05-schema.md — domain table).

Entity/detail endpoints are NOT rank-scoped: `GET /domain/{domain}`, `/domain/{domain}/log`, `/domain/{domain}/subdomains`, `/domain/{domain}/resources`, `/stats/domain/{domain}`, `/changelog/{domain}`, and the campaign detail/log endpoints serve any entity regardless of rank (this is how campaign domains, subdomains, and live-check hosts are viewed). Disabled entities are excluded everywhere: `GET /domain/{domain}` returns 404 for disabled rows (production parity: ViewDomain read the `disabled=FALSE` view), and every list excludes `disabled` rows.

### 2.7 shortuuid codec (wire-frozen)

All campaign UUIDs crossing the API boundary are encoded with `github.com/lithammer/shortuuid/v4` `DefaultEncoder` — base57 alphabet `23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz`, fixed 22-character output. Use the latest v4.x release (production runs v4.2.0). MUST be the v4 major: v3 produces different, variable-length tokens and would 404 every previously shared campaign link. Do not hand-roll the codec.

```go
func encodeUUID(id uuid.UUID) string {           // uuid = github.com/google/uuid
    return shortuuid.DefaultEncoder.Encode(id)   // always 22 chars
}
func decodeUUID(s string) (uuid.UUID, error) {
    return shortuuid.DefaultEncoder.Decode(s)
}
```

**Surfaces (exhaustive):**
1. `uuid` field of campaign list/detail responses (`GET /campaign`, `GET /campaign/{uuid}`).
2. `campaign_uuid` field of `GET /campaign/search/{q}` rows. **Decision:** changelog rows carry NO `campaign_uuid` key — verified against production `changelog.go:22-29` (the struct has exactly six fields); the campaign token appears only inside `domain_url`.
3. `domain_url` in changelog responses: `"/campaign/{token}/{host}"`.
4. Path params: routes 19, 20, 24, 25, 26, 32.

The database stores only the canonical `UUID` column; encode/decode happens exclusively in the HTTP layer.

**Decode-failure behavior (uniform, all `{uuid}` path params including `GET /stats/campaign/{uuid}`):**
- `decodeUUID` error (character outside base57 alphabet, overflow, etc.) → `404 {"error":"Invalid UUID"}` (byte-exact production body; production `campaign.go:117-122`, `changelog.go:196-201`. Production's other handlers used 400 for the same condition — the new backend uniformly uses 404, the behavior the frontend actually exercises).
- Token decodes but no matching non-disabled campaign row → `404 {"error":"Campaign not found"}` (production `campaign.go:132-136`).

No extra length/shape validation beyond what `DefaultEncoder.Decode` performs.

**Shared campaign resolver** (one helper used by routes 19, 20, 24, 25, 26, 32): decode shortuuid → `SELECT id, uuid, name, description FROM campaign WHERE uuid = $1 AND NOT disabled` → no row → `404 {"error":"Campaign not found"}`. Disabled campaigns are invisible on every UUID-addressed endpoint (announced fix; production accidentally kept them listed with zeroed counts because `campaign.disabled = FALSE` sat in a LEFT JOIN condition — `whynoipv6/db/query/campaign.sql` ListCampaign/GetCampaignByUUID).

OpenAPI representation: `type: string`, `pattern: ^[2-9A-HJ-NP-Za-km-z]{22}$`, `example: bHTMghm9txZFhwMKVCiBey`.

Codec test vectors (10-testing.md owns the fixture files; verified against lithammer/shortuuid/v4 v4.2.0; the first two are live campaign UUIDs from the campaign repo):

| canonical UUID | shortuuid token |
|---|---|
| baff94c3-c4b2-4f19-be66-3247250f7868 | bHTMghm9txZFhwMKVCiBey |
| 9b587e73-7694-46f7-b3dc-96f6a1c15317 | VeT2mCvhzny4kAiQ9oLe2r |
| 00000000-0000-0000-0000-000000000000 | 2222222222222222222222 |

Negative vectors: `GET /campaign/not-a-token` and `GET /campaign/baff94c3-c4b2-4f19-be66-3247250f7868` (raw UUID in the path — `-` is outside the alphabet) both → `404 {"error":"Invalid UUID"}`.

### 2.8 Legacy serialization rules R1–R5 (normative; part of openapi.yaml + parity fixtures)

**R1. Status projection.** Every field carrying an `ipv6_status` on a legacy endpoint (`base_domain`, `www_domain`, `nameserver`, `mx_record`, `v6_only` in domain/campaign detail and list rows, campaign-domain composite rows, changelog `ipv6_status`, and log rows) is serialized through ONE shared function:

```go
// legacyStatus projects the 4-value public enum + NULL onto the frozen
// 3-string wire contract. not_applicable and never-confirmed both render
// as "no_record" (frontend shows the amber "no record" marker).
func legacyStatus(s *ipv6Status) string {
    switch {
    case s == nil:                 return "no_record" // never confirmed (NULL column)
    case *s == NotApplicable:      return "no_record"
    default:                       return string(*s)  // supported|unsupported|no_record
    }
}
```

No legacy endpoint may ever emit `not_applicable`, `error`, `inconsistent`, `unknown`, or empty string. New endpoints (§4, §6) are exempt and serve the real 4-value enum (`supported|unsupported|no_record|not_applicable`) plus JSON `null` for never-confirmed.

**R2. Scan-log projection** (`GET /domain/{domain}/log` and `GET /campaign/{uuid}/{domain}/log`). Source: last 90 `scan` rows by `ts DESC` **after filtering out non-definitive rows** — a documented exception that *enforces* "error/inconsistent never become public" by exclusion, not remapping:

```sql
SELECT ts, base, www, ns, mx FROM scan
WHERE domain_id = $1
  AND base NOT IN ('error','inconsistent') AND www NOT IN ('error','inconsistent')
  AND ns   NOT IN ('error','inconsistent') AND mx  NOT IN ('error','inconsistent')
ORDER BY ts DESC LIMIT 90;
```

Per-field values then pass through R1 (`not_applicable` → `"no_record"`). Response row:

```json
{"id": 1751791845, "time": "2026-07-06T08:50:45.123456Z",
 "base_domain": "supported", "www_domain": "supported",
 "nameserver": "supported", "mx_record": "no_record"}
```

`id` = `extract(epoch from ts)::bigint` — synthetic (the frontend uses it as a list key only); epoch seconds is stable across requests. `time` = `ts` as RFC 3339.

**R3. Timestamp key mapping** (`GET /domain/{domain}`, `GET /campaign/{uuid}/{domain}`, and every legacy domain-shaped row):

| JSON key | Source column (`domain` table, 05-schema.md) |
|---|---|
| `ts_aaaa` | `base_since` |
| `ts_www` | `www_since` |
| `ts_ns` | `ns_since` |
| `ts_mx` | `mx_since` |
| `ts_curl` | `conn_since` |
| `ts_check` | `last_checked_at` |
| `ts_updated` | `updated_at` |

NULL source columns serialize as `"0001-01-01T00:00:00Z"` (bug-compatible with production's nullable-timestamp encoding; the frontend tolerates it). No fallback substitution — do NOT substitute `last_checked_at` for a NULL `*_since`.

**R4. `v6_ready` (amended formula — announced under the OPEN-6 methodology-v2 note).** For `GET /campaign` list counts, the `{campaign}` object in the composite, and `stats_campaign_daily.v6_ready`:

```sql
v6_ready := base_status = 'supported'
        AND ns_status   = 'supported'
        AND www_status IN ('supported', 'not_applicable')
```

Rationale (recorded): subdomain entities force `www = not_applicable`; production's strict `www = 'supported'` test would permanently pin subdomain-heavy campaigns at 0%. NULL (unconfirmed) `www` does NOT count as ready. `mx`/`conn` remain excluded from `v6_ready`, as in production.

**R5. Legacy changelog collapse.** The five legacy `/changelog*` endpoints serve only rows passing the §3.12 coverage filter (`field IN ('base','www','ns','mx')`, `old_value`/`new_value` in the 3 legacy strings, `old_value IS NOT NULL`), plus `field='legacy'` passthrough rows. Because the filter admits only the 3 production strings — on which R1 is the identity — and native rows always have `old_value <> new_value`, **the SQL filter alone implements R5**: any transition involving `not_applicable` (and all `conn`/`resources` rows) is excluded before rendering, and the `renderChangelog` ladder only ever sees production's 3 strings.

**Parity-test note:** golden fixtures captured from production cannot exercise R1's not_applicable/NULL branches or R2's filter (production never produces those values); 10-testing.md defines synthetic fixtures for them, keyed to this section.

### 2.9 Legacy domain row shape (shared by routes 3, 4, 5, 7, 15, 16, 23; route 8 adds two keys)

Production struct: `whynoipv6/internal/rest/domain.go:22-40` (`DomainResponse`). Exact keys, types, and source columns (all statuses through R1, all timestamps through R3):

```json
{
  "rank":        1234,                       // domain.rank (int; 0 when NULL — §2.4)
  "domain":      "example.com",              // domain.host
  "base_domain": "supported",                // legacyStatus(domain.base_status)
  "www_domain":  "supported",                // legacyStatus(domain.www_status)
  "nameserver":  "supported",                // legacyStatus(domain.ns_status)
  "mx_record":   "no_record",                // legacyStatus(domain.mx_status)
  "v6_only":     "unsupported",              // legacyStatus(domain.conn_status) — now REAL data
                                             //   (production served a dead column)
  "asn":         "TELENOR-AS",               // asn.name via domain.asn_id (AS *name* string;
                                             //   sentinel row name "Unknown")
  "country":     "Norway",                   // country.name via domain.country_id
                                             //   (name, not code; sentinel "Unknown")
  "ts_aaaa":    "2024-03-01T00:00:00Z",      // R3 table
  "ts_www":     "2024-03-01T00:00:00Z",
  "ts_ns":      "2024-03-01T00:00:00Z",
  "ts_mx":      "0001-01-01T00:00:00Z",
  "ts_curl":    "2024-03-01T00:00:00Z",
  "ts_check":   "2026-07-06T04:12:09.331Z",
  "ts_updated": "2026-07-06T04:12:09.331Z"
}
```

`campaign_uuid` (string, shortuuid) appears ONLY on `GET /campaign/search/{q}` rows (Go tag `omitempty`; production `domain.go:39`). `GET /domain/topsinner` rows omit `asn`/`country`? No — production set neither, so they serialize as `""` (no omitempty); see §3.5.

The campaign-domain row shape (routes 24 list, 25 single) is the same **minus `rank` and `campaign_uuid`** — production `campaign.go:23-39` (`CampaignResponse`) has no rank field at all.

### 2.10 Legacy changelog row shape (routes 17–21)

Production struct: `whynoipv6/internal/rest/changelog.go:22-29`. Exactly six keys, always all present:

```json
{
  "id": 1751791845123,                  // synthetic: epoch MILLISECONDS of ts (int64) — §3.12
  "ts": "2026-07-06T08:50:45.123Z",     // changelog.ts
  "domain": "example.com",              // domain.host via changelog.domain_id
  "domain_url": "/domain/example.com",  // per-endpoint rule (§3.12); "" on by-domain feeds
  "message": "IPv6 enabled for example.com",   // renderChangelog ladder (§3.12)
  "ipv6_status": "supported"            // = new_value (projected through R1)
}
```

### 2.11 Zero-result behavior (the complete 404-vs-[] map)

**Rule.** The "empty lists return `[]` instead of JSON `null`" cleanup applies only to responses production already served as HTTP 200 with a `null` body (a serialized nil slice). It never changes a status code. Every zero-result **404** production emits is kept bug-compatibly: same status, byte-identical error JSON.

**Kept 404s on zero results** (exact production bodies; content-type application/json):

| Endpoint | Fires when | Response |
|---|---|---|
| `GET /domain/search/{q}` | 0 publicly-ranked matches | `404 {"error":"no domains found"}` |
| `GET /campaign/search/{q}` | 0 campaign-domain matches | `404 {"error":"No domains found"}` (capital N — deliberately differs from domain search) |
| `GET /campaign/{uuid}` | member-domain page empty: unknown/disabled campaign **or** `offset >=` member count (paging past the last page 404s — bug-compatible, frontend tolerates) **or** zero-member campaign | `404 {"error":"Campaign not found"}` |
| `GET /campaign/{uuid}/{domain}` | single resource not found (membership miss, unknown host, or disabled domain) | `404 {"error":"Domain not found"}` |
| `GET /changelog/{domain}` | zero changelog rows (incl. unknown/disabled/malformed host) | `404 {"error":"No changelog entries found for {domain}"}` (`{domain}` = raw path param) |
| `GET /changelog/campaign/{uuid}` | zero changelog rows for the campaign (production's third zero-rows check, `changelog.go:229-237`) | `404 {"error":"No changelog entries found for campaign {uuid}"}` where `{uuid}` is the **decoded canonical 36-char UUID** |
| `GET /changelog/campaign/{uuid}/{domain}` | zero rows | `404 {"error":"No changelog entries found for campaign {uuid} and domain {domain}"}` where `{uuid}` is the **shortuuid exactly as given in the URL** (production does not decode it for this message) and `{domain}` is the raw path param |

Consequence: because both `{"data":[...]}`-enveloped endpoints (the two searches) 404 on zero matches, `{"data":[]}` never occurs on the legacy surface.

**[]-cleanup applies (production returned 200 `null`)** — zero rows → `200 []`:
`GET /domain`, `/domain/heroes`, `/domain/topsinner`, `/domain/{domain}/log`, `/country`, `/country/{code}/sinners`, `/country/{code}/heroes`, `/changelog`, `/changelog/campaign`, `/campaign`, `/campaign/{uuid}/{domain}/log`, `/metric/asn`, and — explicitly — `GET /metric/asn/search/{q}` (production `metric.go:132-157` has no zero-rows check; it is the one search endpoint that returns `200 []` on zero matches). `GET /metric/overview` also degrades to `200 []` if `stats_global_daily` is somehow empty — never expected, because the seed migration writes day-0 rows (§3.17).

Single-resource endpoints are untouched by this rule: `GET /domain/{domain}` → `404 {"error":"domain not found"}` (lowercase, production `domain.go:156-159`) for unknown, disabled, or malformed hosts; `GET /country/{code}` → `404 {"error":"Country not found"}` for unknown codes.

New endpoints (§4): lists return `200 []` on zero rows; single resources return `404 {"error":"not_found"}` except where §4 pins a different body.

---

## 3. Legacy endpoints (endpoint by endpoint)

Golden fixtures for every endpoint in this section are captured from the named production handler and compared per 10-testing.md. "Everything else stays bug-compatible until the frontend modernization round." A versioned `/v2` API is explicitly rejected for this round.

### 3.1 `GET /` — health

Production: `whynoipv6/cmd/api/main.go:49-53`. Response: `200` `{"message":"ok"}`. **Decision:** production's raw bytes were `{"message": "ok"}` (space after colon); the new backend emits canonical JSON and the parity fixture asserts JSON equality, not byte equality, for this endpoint only.

### 3.2 `GET /ip`

Frontend calls `https://api.whynoipv6.com/ip` **hardcoded** (`whynoipv6-web/src/components/Notification.vue:38`); today it is served by nginx or lost code — the new API serves it natively. Response: `200` `{"ip":"<derived remote address>"}` where the address is the §1.2 RealIP result, serialized without port or brackets (e.g. `{"ip":"2001:db8::7"}`, `{"ip":"192.0.2.10"}`). No other keys.

### 3.3 `GET /domain?offset=&limit=` — sinner list

Production: `domain.go:73-108` (DomainList), query `db/query/domain.sql` ListDomain. Frontend: `DomainService.getDomainList`.

- Membership: `classification = 'sinner'` + publicly-ranked predicate. **Announced break (OPEN-6):** production's query was `base_domain='unsupported' OR www_domain='unsupported'`; new membership is base-unsupported only — domains with base supported but www unsupported leave this list and surface on `GET /domain/almost` as `partial`/`www_missing`. Response **shape** unchanged.
- Query:

```sql
SELECT d.*, a.name AS as_name, c.name AS country_name
FROM domain d JOIN asn a ON a.id = d.asn_id JOIN country c ON c.id = d.country_id
WHERE d.classification = 'sinner' AND d.rank IS NOT NULL AND NOT d.disabled
ORDER BY d.rank ASC LIMIT $1 OFFSET $2;
```

- Response: `200` array of §2.9 rows. Zero rows → `200 []`.

### 3.4 `GET /domain/heroes?offset=&limit=`

Production: `domain.go:111-150`. Frontend: `DomainService.getDomainHeroes`. Same as §3.3 with `classification = 'hero'`. Hero membership is now the confirmed-classification truth table (announced break vs production's `mx != 'unsupported'` query — OPEN-6). Response: `200` array of §2.9 rows; zero → `200 []`.

### 3.5 `GET /domain/topsinner`

Production: `domain.go:237-265` (TopSinner). Frontend: `DomainService.getTopShame`. Curated editorial list (`top_shame` table; writer is `v6ctl shame`, out of scope here). No pagination (production returned the whole filtered list; ≤ a dozen rows). Query pin:

```sql
SELECT d.*, a.name AS as_name, c.name AS country_name
FROM top_shame ts
JOIN domain d ON d.id = ts.domain_id
JOIN asn a ON a.id = d.asn_id
JOIN country c ON c.id = d.country_id
WHERE d.classification = 'sinner'            -- production parity: a shamed domain that
                                             --   ships IPv6 auto-hides; its row persists
  AND d.rank IS NOT NULL AND NOT d.disabled  -- publicly-ranked predicate
ORDER BY d.rank ASC;                         -- declared fix: production returned domain
                                             --   *id* as "rank" and ordered by it
```

Response: `200` array of §2.9 rows with `rank` = the real Tranco rank (the declared fix). **Decision:** `asn` and `country` are serialized normally (from the joined names). Production left both `""` because its handler simply didn't copy the fields — an artifact, not a contract; the frontend renders these rows with the same component as other domain lists, so populated values are strictly better and shape-identical. Zero rows → `200 []`.

### 3.6 `GET /domain/{domain}` — detail

Production: `domain.go:153-179` (RetrieveDomain). Frontend: `DomainService.getDomainDetails`.

- Path param through §2.5 Canonicalize; failure → `404 {"error":"domain not found"}`.
- Lookup by canonical host, any kind/rank (entity endpoint, not rank-scoped). Unknown host OR `disabled = TRUE` → `404 {"error":"domain not found"}` (production parity: ViewDomain read the disabled=FALSE view).
- Response: `200`, the §2.9 row (no `campaign_uuid`), **plus two additive keys** (new; the frozen frontend ignores unknown keys):
  - `"subdomains"`: array (max 25) of §4.2 subdomain rows — non-disabled children (`parent_id = this.id`), `ORDER BY host ASC`, cap 25. **Decision:** embedded rows use the new-model §4.2 shape (not the legacy shape) so the future frontend reads one vocabulary; cap and order pinned here.
  - `"subdomain_count"`: int — total count of non-disabled children (may exceed 25; the §4.2 endpoint paginates past the cap).
- Parity fixtures MUST strip/ignore the two additive keys when comparing against production captures (note for 10-testing.md).

### 3.7 `GET /domain/{domain}/log`

Production: `domain.go:268-294` (GetDomainLog), query `domain.sql` GetDomainLog (LIMIT 90). Frontend: `DomainService.getDomainLog`.

- Path param through Canonicalize; failure → `404 {"error":"domain not found"}`.
- Source + row shape: rule R2 (§2.8) over the entity's `scan` rows.
- **Decision:** unknown canonical host → `200 []` (production ran the query by name and returned the nil slice; only DB errors 404'd). Disabled entities: the row resolves for the lookup, but a disabled domain's detail 404s (§3.6) so the frontend never reaches this; serve `200 []` for disabled hosts as well (public-exclusion, §2.6). Zero rows → `200 []`.

### 3.8 `GET /domain/search/{q}?offset=&limit=`

Production: `domain.go:182-234` (SearchDomain), query `domain.sql` GetDomainsByName. Frontend: `DomainService.searchDomain`.

- `{q}` is a substring, NOT a hostname: no Canonicalize. Processing: lowercase, then escape `%`, `_`, and `\` for the LIKE pattern (production forgot to escape — declared fix, v2 forgot too).
- Query (trigram GIN index `idx_domain_host_trgm` serves the scan):

```sql
SELECT d.*, a.name AS as_name, c.name AS country_name
FROM domain d JOIN asn a ON a.id = d.asn_id JOIN country c ON c.id = d.country_id
WHERE d.host LIKE '%' || $1 || '%' ESCAPE '\'
  AND d.rank IS NOT NULL AND NOT d.disabled     -- production parity: searched ranked+enabled view
ORDER BY d.rank ASC LIMIT $2 OFFSET $3;
```

- Response: `200` `{"data":[ <§2.9 rows> ]}` — **envelope kept**. Zero matches → `404 {"error":"no domains found"}`.
- Campaign matches reach the Search page via `GET /campaign/search/{q}`; this endpoint stays rank-scoped.

### 3.9 `GET /country`

Production: `country.go:49-77` (CountryList), query `country.sql` ListCountry. Frontend: `CountryService.getCountryList`.

- Query: `SELECT name, code, sites, v6sites, percent FROM country ORDER BY sites DESC;` (production parity ordering). Counters are the current-state columns recomputed at the daily tick with the §2.6 scope, so figures match the lists exactly. The sentinel row (`code 'UN'`, name `'Unknown'`) appears exactly as it does today.
- Row shape (production `country.go:22-28`):

```json
{"country":"Norway","country_code":"NO","sites":8231,"v6sites":2119,"percent":25.74}
```

`percent` is the `NUMERIC(5,2)` column as a JSON number (§2.4). Zero rows → `200 []`.

### 3.10 `GET /country/{code}`, `/country/{code}/sinners`, `/country/{code}/heroes`

Production: `country.go:80-107` (CountryInfo), `:110-159` (CountrySinners), `:162-211` (CountryHeroes). Frontend: `CountryService.*`.

- `{code}` is upper-cased (`strings.ToUpper`, production parity) and matched against `country.code`. Unknown → `404 {"error":"Country not found"}` (all three routes).
- `GET /country/{code}`: `200`, single §3.9 row.
- `.../sinners?offset=&limit=`: `classification='sinner' AND country_id=$1` + publicly-ranked predicate, `ORDER BY rank ASC` (declared minor fix: production ordered by id) — same OPEN-6 membership narrowing as §3.3 (production used the same OR-predicate, `country.sql` ListDomainsByCountry). `200` array of §2.9 rows; zero → `200 []`.
- `.../heroes?offset=&limit=`: `classification='hero'`, same scoping, `ORDER BY rank ASC` (production parity). `200` array; zero → `200 []`.

### 3.11 Changelog rendering: the `renderChangelog` ladder (single implementation, API layer)

`renderChangelog(field, old, new, host) -> (message, ipv6Status)` is defined ONLY for `field IN ('base','www','ns','mx')` and `old, new IN ('supported','unsupported','no_record')`, `old` non-NULL. `ipv6_status` in the response is always `new_value`. Exact strings, verbatim from production `internal/crawler/crawl.go:416-495` (the campaign ladder in `campaign_crawl.go` is string-identical, so one function serves all five endpoints). `{h}` = entity host; for `field='www'` the rendered name is `www.{h}`:

| field | old → new | message |
|---|---|---|
| base | unsupported→supported OR no_record→supported | `IPv6 enabled for {h}` |
| base | supported→unsupported | `IPv6 lost for {h}` |
| base | no_record→unsupported | `IPv4-only for {h}` |
| base | any→no_record | `No DNS records found for {h}` |
| www | unsupported→supported OR no_record→supported | `IPv6 enabled for www.{h}` |
| www | supported→unsupported | `IPv6 lost for www.{h}` |
| www | no_record→unsupported | `IPv4-only for www.{h}` |
| www | any→no_record | `No DNS records found for www.{h}` |
| ns | unsupported→supported OR no_record→supported | `IPv6 enabled nameserver for {h}` |
| ns | supported→unsupported | `Nameservers degraded to IPv4-only for {h}` |
| ns | no_record→unsupported | `IPv4-only nameservers for {h}` |
| ns | any→no_record | `No NS records found for {h}` |
| mx | unsupported→supported OR no_record→supported | `IPv6 enabled MX records for {h}` |
| mx | supported→unsupported | `MX records degraded to IPv4-only for {h}` |
| mx | no_record→unsupported | `IPv4-only MX records for {h}` |
| mx | any→no_record | `No Mail records found for {h}` |

`field='legacy'` rows (phase-4 import escape hatch, see 05-schema.md — changelog table) **bypass the ladder**: `message = legacy_message`, `ipv6_status = legacy_status`, verbatim passthrough.

### 3.12 The five `/changelog*` endpoints

Production: `changelog.go` (ChangelogList `:51-84`, CampaignChangelogList `:87-124`, ChangelogByDomain `:127-181`, ChangelogByCampaign `:184-254`, ChangelogByCampaignDomain `:257-341`). Frontend: `ChangelogService.*` — it calls **all five** (the v2 rebuild dropped three; they are restored).

**Coverage filter (all five endpoints; implements R5):**

```sql
WHERE (   (c.field IN ('base','www','ns','mx')
           AND c.old_value IS NOT NULL
           AND c.old_value  IN ('supported','unsupported','no_record')
           AND c.new_value  IN ('supported','unsupported','no_record'))
       OR c.field = 'legacy' )
```

`conn`/`resources` rows and any transition involving `not_applicable` ARE written to the `changelog` table (they remain queryable, appear in datasets, and are available to a future v2 API) but are NOT served by the legacy endpoints — production never emitted them, the frontend is frozen, exposing them later is purely additive.

**Ordering (all feeds):** `ORDER BY c.ts DESC, c.domain_id DESC, c.field ASC`. Pagination `?offset=`/`?limit=` (default 50, max 100) on all five.

**Synthetic `id`:** the `changelog` hypertable has no identity column. `id` = **epoch milliseconds of `ts`** (int64). The frontend keys rows by array index and never dereferences `id`; collisions are harmless. Pagination stability comes from the deterministic ORDER BY.

**Per-endpoint scope and `domain_url`:**

| Endpoint | Scope (JOINs + filters, on top of the coverage filter) | `domain_url` |
|---|---|---|
| `GET /changelog` | `JOIN domain d ON d.id = c.domain_id WHERE d.rank IS NOT NULL AND NOT d.disabled` — Tranco apexes only (reproduces production's implicitly-Tranco feed; campaign-only, live-check, and subdomain entities excluded; disabled excluded per the global rule) | `"/domain/{host}"` |
| `GET /changelog/campaign` | `JOIN campaign_domain cd ON cd.domain_id = c.domain_id JOIN campaign ca ON ca.id = cd.campaign_id JOIN domain d ON d.id = c.domain_id WHERE NOT ca.disabled AND NOT d.disabled` — all campaigns, rank irrelevant. A domain in N campaigns yields N rows per change (production duplicated identically — accepted) | `"/campaign/{shortuuid(ca.uuid)}/{host}"` |
| `GET /changelog/{domain}` | entity resolved by canonical host, any kind/rank, `NOT d.disabled` (**Decision:** disabled entities are excluded per the global §2.6 rule → their feed 404s as zero-rows) | `""` (key present, empty string — production struct has no omitempty) |
| `GET /changelog/campaign/{uuid}` | shared campaign resolver (§2.7) + membership join, `NOT d.disabled` | `"/campaign/{shortuuid}/{host}"` |
| `GET /changelog/campaign/{uuid}/{domain}` | shared campaign resolver + membership check + canonical host, `NOT d.disabled` | `""` |

Row shape: §2.10. Zero-result behavior: §2.11 (feeds 1–2 → `200 []`; feeds 3–5 → the pinned 404 bodies).

### 3.13 `GET /campaign`

Production: `campaign.go:80-104` (CampaignList), query `campaign.sql` ListCampaign. Frontend: `CampaignService.getCampaignList`.

- `WHERE NOT campaign.disabled` (announced fix, §2.7), `ORDER BY campaign.id ASC` (production parity).
- Row shape (production `campaign.go:42-49`, CampaignListResponse):

```json
{"id":7,"uuid":"bHTMghm9txZFhwMKVCiBey","name":"Norwegian Banks",
 "description":"...","count":42,"v6_ready":17}
```

- `id` = `campaign.id` (int), `uuid` = shortuuid token, `count` = COUNT of `campaign_domain` members whose domain row is `NOT disabled`, `v6_ready` = R4 formula over the same member set:

```sql
SELECT ca.id, ca.uuid, ca.name, ca.description,
       count(d.id) FILTER (WHERE NOT d.disabled) AS count,
       count(d.id) FILTER (WHERE NOT d.disabled
                             AND d.base_status = 'supported'
                             AND d.ns_status   = 'supported'
                             AND d.www_status IN ('supported','not_applicable')) AS v6_ready
FROM campaign ca
LEFT JOIN campaign_domain cd ON cd.campaign_id = ca.id
LEFT JOIN domain d           ON d.id = cd.domain_id
WHERE NOT ca.disabled
GROUP BY ca.id
ORDER BY ca.id ASC;
```

- A live campaign with zero members returns `count:0, v6_ready:0` (row kept — only `campaign.disabled` removes it from the list). Zero campaigns → `200 []`.

### 3.14 `GET /campaign/{uuid}?offset=&limit=` — composite

Production: `campaign.go:107-195` (CampaignDomains). Frontend: `CampaignService.getCampaign`.

- Shared campaign resolver (§2.7): invalid token → `404 {"error":"Invalid UUID"}`; unknown or disabled → `404 {"error":"Campaign not found"}`.
- Member domains: `JOIN campaign_domain`, exclude disabled domains, paginated. **Decision:** ordering is `ORDER BY cd.added_at ASC, d.host ASC` — production ordered by `campaign_domain.id` (insertion order); the new membership table has no id column, and `added_at` (insertion time) with a unique host tiebreak is the deterministic equivalent.
- Empty page (unknown offset past the end, or zero members at all — production `campaign.go:151-155` checked `len(domains)==0`) → `404 {"error":"Campaign not found"}` (bug-compatible; §2.11).
- Response `200`:

```json
{
  "campaign": { "id":7,"uuid":"<shortuuid>","name":"...","description":"...",
                "count":42,"v6_ready":17 },
  "domains": [ { "domain":"dnb.no","base_domain":"supported","www_domain":"supported",
                 "nameserver":"supported","mx_record":"supported","v6_only":"supported",
                 "asn":"TELENOR-AS","country":"Norway",
                 "ts_aaaa":"...","ts_www":"...","ts_ns":"...","ts_mx":"...",
                 "ts_curl":"...","ts_check":"...","ts_updated":"..." } ]
}
```

`campaign` = the §3.13 row shape (same count/v6_ready formulas). `domains` rows = the campaign-domain shape (§2.9 minus `rank`/`campaign_uuid`); statuses are the **shared entity's** confirmed state now (single status truth — a domain in Tranco and a campaign shows identical status in both views).

### 3.15 `GET /campaign/{uuid}/{domain}` and `GET /campaign/{uuid}/{domain}/log`

Production: `campaign.go:198-265` (ViewCampaignDomain), `:324-368` (GetCampaignDomainLog). Frontend: `CampaignService.getCampaignDomain`, `.getCampaignDomainLog`.

- Both: shared campaign resolver (§2.7 — note: uniform 404 on invalid token; production used 400 on these two routes, superseded per §2.7), then `{domain}` through Canonicalize (failure → the route's not-found body below), then membership check (`campaign_domain` row linking the campaign and the canonical host's domain row).
- `GET /campaign/{uuid}/{domain}`: membership miss, unknown host, or **disabled domain** (**Decision:** single campaign-domain resources 404 for disabled entities, consistent with §3.6) → `404 {"error":"Domain not found"}`. Success → `200`, single campaign-domain row (§3.14 `domains[]` element shape).
- `GET /campaign/{uuid}/{domain}/log`: R2 rule (§2.8) over the entity's scan rows. Zero rows, unknown host, or membership miss → `200 []` (§2.11; production returned the nil slice). Row shape identical to §3.7.

### 3.16 `GET /campaign/search/{q}?offset=&limit=`

Production: `campaign.go:268-321` (SearchDomain), query `campaign.sql` GetCampaignDomainsByName. Frontend: `CampaignService.searchDomain`.

- `{q}`: lowercase + LIKE-escape (as §3.8), no Canonicalize.
- Scope: campaign membership, `NOT campaign.disabled AND NOT domain.disabled`; rank irrelevant (this is how unranked campaign domains reach the Search page).

```sql
SELECT d.*, a.name AS as_name, c2.name AS country_name, ca.uuid AS campaign_uuid
FROM domain d
JOIN campaign_domain cd ON cd.domain_id = d.id
JOIN campaign ca        ON ca.id = cd.campaign_id
JOIN asn a              ON a.id = d.asn_id
JOIN country c2         ON c2.id = d.country_id
WHERE d.host LIKE '%' || $1 || '%' ESCAPE '\'
  AND NOT ca.disabled AND NOT d.disabled
ORDER BY d.host ASC, ca.id ASC        -- Decision: production had NO ORDER BY (nondeterministic
LIMIT $2 OFFSET $3;                   --   paging); host+campaign id is the stable equivalent
```

- Response: `200` `{"data":[ <§2.9 rows + "campaign_uuid">"..."> ]}`. A domain in N matching campaigns yields N rows (production parity). **Decision:** `rank` serializes the shared entity's real rank (0 when NULL) — production always emitted 0 because its campaign tables had no rank column; the shared-entity model makes the real value available and the field was already present in the shape.
- Zero matches → `404 {"error":"No domains found"}` (capital N).

### 3.17 `GET /metric/overview` — fully pinned

Production: `metric.go:62-80` (Overview). Frontend: `MetricService.getTotals`; `types/Metric.ts` + `MetricCrawler.vue` read **every** key.

Response: JSON array with exactly one element, built from the latest `stats_global_daily` row (max `day`; see 05-schema.md — stats tables):

```json
[
  {
    "time": "2026-07-06T00:00:00Z",
    "data": {
      "domains":        1000000,
      "base_domain":    262144,
      "www_domain":     240000,
      "nameserver":     410000,
      "mx_record":      201000,
      "heroes":         96000,
      "top_heroes":     407,
      "top_nameserver": 561
    }
  }
]
```

- `time` = the row's `day` as an RFC 3339 timestamp at **midnight UTC** (keep it a timestamp string, not a bare date; the frontend never reads it but the type is frozen).
- Key → column mapping: `domains`←`domains`, `base_domain`←`base_supported`, `www_domain`←`www_supported`, `nameserver`←`ns_supported`, `mx_record`←`mx_supported`, `heroes`←`heroes` (the confirmed-classification count — the membership change vs production's `base+www supported` formula is the announced OPEN-6 break), `top_heroes`←`top_heroes`, `top_nameserver`←`top_nameserver`.
- All eight `data` keys are required; values are plain JSON numbers.
- First-boot guarantee: the phase-4 seed migration writes day-0 rows for all `stats_*` tables, so this endpoint always has a row (OPEN-6 "serve migrated seed values immediately"). Degenerate empty table → `200 []` (§2.11), never an error.

### 3.18 `GET /metric/asn?order=`

Production: `metric.go:94-129` (AsnMetrics), queries `asn.sql` AsnByIPv4/AsnByIPv6. Frontend: `MetricService.fetchAsnData`.

- `order` query param: default `ipv4`; allowed `ipv4|ipv6`; anything else → `400 {"error":"invalid filter parameter"}` (byte-exact production body).
- **No pagination**: production hardcoded offset 0 / limit 50 (`metric.go:108`); `offset`/`limit` params are ignored. Always at most 50 rows.
- Query (**Decision:** production excluded its hardcoded sentinel via `id != 1`; the new schema resolves sentinels by lookup, so the predicate becomes `number <> 0`. Deterministic tiebreak `number ASC` added — production had none):

```sql
SELECT id, number, name, count_total, count_v6
FROM asn
WHERE number <> 0
ORDER BY <count_total | count_v6> DESC, number ASC
LIMIT 50;
```

`order=ipv4` sorts by `count_total` (production's `count_v4` column stored the total domain count — every domain has v4), `order=ipv6` by `count_v6`.
- Row shape (production `metric.go:83-91`):

```json
{"id":314,"number":2119,"name":"TELENOR-AS","count_v4":5120,"count_v6":1444}
```

`count_v4` = `count_total - count_v6` **computed server-side** (production parity: `metric.go:98` comment "all domains have IPv4"). **Decision:** `percent_v4`/`percent_v6` are never emitted — production declared them with `omitempty` and never set them, so they never appeared on the wire; the new backend omits the fields entirely (OpenAPI schema has exactly five properties).
- Zero rows → `200 []`.

### 3.19 `GET /metric/asn/search/{q}`

Production: `metric.go:132-157` (SearchAsn) + `core/metric.go:108-126`. Frontend: `MetricService.searchAsn`.

Processing, **verbatim production semantics kept** (**Decision:** including the quirk that a name query starting with "as" loses that prefix — e.g. `Asgard` searches for `GARD`; the frontend is frozen and the quirk is harmless):
1. `q = strings.TrimPrefix(strings.ToUpper(rawParam), "AS")`.
2. If `q` parses as an integer: `SELECT ... FROM asn WHERE number = $1` (sentinel `number 0` findable — parity).
3. Else: name search `WHERE name ILIKE '%' || $1 || '%' ESCAPE '\'` with LIKE-escaping of `%`/`_`/`\` (declared fix; production didn't escape).
4. Both: `ORDER BY count_total DESC, number ASC LIMIT 100` (production ordered by its count_v4=total column; tiebreak added as §3.18).

Row shape: §3.18. Zero matches → `200 []` (the one search endpoint that never 404s — production has no zero-rows check).

---

## 4. New endpoints

New endpoints serve the real public model: statuses are the 4-value enum `supported|unsupported|no_record|not_applicable` or `null` (never confirmed); classifications are `unknown|inactive|sinner|partial|hero`; flags are `broken_v6|www_missing|ns_missing|mail_missing|resources_v4only`. No R1 projection anywhere in this section.

### 4.1 Shared new-model domain row (`DomainSummary`)

**Decision:** one row schema serves `GET /domain/almost`, the `subdomains` listings (standalone + embedded in §3.6), and it deliberately embeds the same `statuses`/`as_of` vocabulary as the §6 `confirmed` object:

```json
{
  "host": "example.com",
  "rank": 1234,                          // int or null (subdomains/campaign hosts: null)
  "kind": "apex",                        // "apex" | "subdomain"
  "classification": "partial",
  "class_flags": ["www_missing"],        // [] when none
  "gold": false,
  "statuses": {
    "base": "supported", "www": "unsupported", "ns": "supported",
    "mx": "not_applicable", "conn": "supported", "resources": null
  },
  "as_of": "2026-07-06T04:12:09.331Z",   // domain.last_checked_at; null if never scanned
  "country_code": "NO",                  // country.code via country_id ("UN" sentinel)
  "asn": { "number": 2119, "name": "TELENOR-AS" }   // sentinel {0,"Unknown"}
}
```

Source columns: `host`, `rank`, `kind`, `classification`, `class_flags`, `gold`, the six `*_status` columns, `last_checked_at`, joined `country.code`, `asn.number`/`asn.name`.

### 4.2 `GET /domain/almost?offset=&limit=` — the "almost there" tier

- Membership: `classification = 'partial'` + publicly-ranked predicate; `ORDER BY rank ASC`; pagination §2.2 (served by `idx_domain_partial`).
- Response: `200` array of §4.1 rows. Zero → `200 []`.
- This is where base-supported/www-unsupported domains land after the §3.3 sinner-membership narrowing (OPEN-6/OPEN-10: `/domain` stays sinners for the frozen frontend; the full ladder presentation is a frontend-round concern).

### 4.3 `GET /domain/{domain}/subdomains?offset=&limit=`

- Parent resolved like §3.6 (Canonicalize; unknown or disabled → `404 {"error":"domain not found"}` — **Decision:** reuse the parent detail route's body since this is a sub-resource of it).
- Children: `WHERE parent_id = $parent AND NOT disabled ORDER BY host ASC` + pagination. Any `kind='apex'` parent may have children; `kind='subdomain'` rows have none (→ `200 []`).
- Response: `200` array of §4.1 rows (children are `kind:"subdomain"`, `rank:null`, `statuses.www` always `"not_applicable"` or `null` by the kind-aware check rules). Zero → `200 []`.
- Exists for pagination past the 25-row embed cap in §3.6.

### 4.4 `GET /domain/{domain}/resources`

Per-host dependency list (issue #23; tables `resource_host` + `domain_resource`, see 05-schema.md — resources tables).

- Parent resolved as §4.3 (404 body `{"error":"domain not found"}` for unknown/disabled/malformed).
- **Decision:** no pagination — linked hosts per domain are bounded small (discovery caps per fetch); `ORDER BY rh.host ASC`.

```sql
SELECT rh.host, rh.aaaa_status, rh.last_checked_at,
       dr.source, dr.required, dr.first_seen, dr.last_seen
FROM domain_resource dr
JOIN resource_host rh ON rh.id = dr.resource_host_id
WHERE dr.domain_id = $1
ORDER BY rh.host ASC;
```

- Response `200`:

```json
[
  {
    "host": "fonts.googleapis.com",
    "status": "unsupported",             // resource_host.aaaa_status: 4-value enum or null
    "source": "discovered",              // "discovered" | "manual"
    "required": true,
    "first_seen": "2026-05-01T04:11:00Z",
    "last_seen":  "2026-07-06T04:12:09Z",
    "last_checked_at": "2026-07-06T02:00:41Z"   // null if never swept
  }
]
```

Zero links → `200 []` (also the constant answer while `crawler.resources.enabled=false`, pre-phase-5).

### 4.5 `GET /resource/{host}/dependents?offset=&limit=` — reverse dependency lookup

- `{host}` through Canonicalize; failure or no `resource_host` row → `404 {"error":"not_found"}`.
- **Decision:** composite response (mirrors the §3.14 composite precedent); `dependents` paginated per §2.2, scope = linked domains `NOT disabled` (any kind/rank — advocacy view), `ORDER BY d.rank ASC NULLS LAST, d.host ASC`.

```json
{
  "resource": {
    "host": "fonts.googleapis.com",
    "status": "unsupported",                    // aaaa_status or null
    "dependent_count": 48211,                   // resource_host.dependent_count
    "last_checked_at": "2026-07-06T02:00:41Z"
  },
  "dependents": [
    { "host": "example.com", "rank": 1234, "kind": "apex",
      "classification": "hero", "gold": false,
      "source": "discovered", "required": true }
  ]
}
```

Dependent-row keys: §4.1 subset (`host`,`rank`,`kind`,`classification`,`gold`) + link attributes (`source`,`required`). Zero dependents → `"dependents": []` (200).

### 4.6 `GET /stats/*` — time-series for graphs

Five routes, one query-parameter contract:

- `?from=YYYY-MM-DD` (inclusive), `?to=YYYY-MM-DD` (inclusive), `?interval=daily|weekly`.
- **Decision (defaults):** `to` = today UTC, `from` = `to` − 90 days, `interval` = `daily`.
- Validation: unparseable date, `from > to`, or `interval` outside the enum → `400 {"error":"invalid_parameter","message":"..."}`.
- **Decision (weekly semantics):** `interval=weekly` returns ONE row per ISO week — the row with the greatest `day` (or `ts`) inside that week within range (latest snapshot sampling, not averaging), reported under its own real `day`. SQL shape: `SELECT DISTINCT ON (date_trunc('week', day)) ... ORDER BY date_trunc('week', day), day DESC`, re-sorted ascending for output.
- Output ordering: ascending by `day`/`ts`. No pagination (bounded by the date range; a year of dailies is ≤366 rows). Zero rows in range → `200 []`.
- All values are JSON numbers; `day` is `"YYYY-MM-DD"`.

**`GET /stats/overview`** — from `stats_global_daily` (every column; scope: publicly-ranked, except `disabled` which counts ranked-but-disabled — see 05-schema.md — stats tables):

```json
[{ "day":"2026-07-06","domains":1000000,"sinners":610000,"partial":180000,
   "heroes":96000,"gold":12000,"inactive":80000,"unknown":4000,"disabled":30000,
   "base_supported":262144,"www_supported":240000,"ns_supported":410000,
   "mx_supported":201000,"conn_supported":250000,"resources_supported":90000,
   "top_heroes":407,"top_nameserver":561 }]
```

**`GET /stats/country/{code}`** — `{code}` upper-cased, matched on `country.code`; unknown → `404 {"error":"Country not found"}` (same body as §3.10 — it is the same resolver). Rows from `stats_country_daily`:

```json
[{ "day":"2026-07-06","domains":8231,"sinners":5200,"partial":900,"heroes":2100,
   "base_supported":2119,"conn_supported":2050 }]
```

**`GET /stats/campaign/{uuid}`** — shared campaign resolver (§2.7: `404 {"error":"Invalid UUID"}` / `404 {"error":"Campaign not found"}`, disabled → 404). Rows from `stats_campaign_daily` (gaps for periods when the campaign was disabled are served as-is; clients tolerate missing days):

```json
[{ "day":"2026-07-06","domains":42,"v6_ready":17,"sinners":12,"partial":9,
   "heroes":18,"base_supported":30,"www_supported":25,"ns_supported":28,
   "mx_supported":22,"conn_supported":26 }]
```

**`GET /stats/asn/{number}`** — `{number}` is the AS number (decimal integer; a leading `AS`/`as` prefix is accepted and stripped, matching §3.19's normalization). Non-numeric after stripping → `400 {"error":"invalid_parameter","message":"..."}`; unknown AS number → `404 {"error":"not_found"}`. Rows from `stats_asn_daily` (`day` is a TIMESTAMPTZ column; serialize its UTC date part as `"YYYY-MM-DD"`):

```json
[{ "day":"2026-07-06","domains":5120,"v6_domains":1444,"sinners":3300,"heroes":990 }]
```

**`GET /stats/domain/{domain}`** — per-domain history straight from `scan` (2-year retention bounds the range). Entity resolved as §3.7 (Canonicalize; **Decision:** unknown host → `200 []`, disabled → `200 []`, consistent with §3.7 — this is `/domain/{domain}/log`'s new-model sibling). The R2 definitive-rows filter applies (extended to `conn`/`resources`); **Decision:** `interval=daily` returns at most one row per UTC day (the latest qualifying scan of that day), `weekly` the latest qualifying scan per ISO week:

```sql
SELECT DISTINCT ON (date_trunc($bucket, ts))
       ts, base, www, ns, mx, conn, resources, classification
FROM scan
WHERE domain_id = $1 AND ts >= $from AND ts < $to + INTERVAL '1 day'
  AND base NOT IN ('error','inconsistent') AND www  NOT IN ('error','inconsistent')
  AND ns   NOT IN ('error','inconsistent') AND mx   NOT IN ('error','inconsistent')
  AND conn NOT IN ('error','inconsistent') AND resources NOT IN ('error','inconsistent')
ORDER BY date_trunc($bucket, ts), ts DESC;
```

```json
[{ "ts":"2026-07-06T04:12:09.331Z",
   "statuses":{"base":"supported","www":"supported","ns":"supported",
               "mx":"not_applicable","conn":"supported","resources":"supported"},
   "classification":"hero" }]
```

(Core-dimension `scan` columns never contain `partial`, so after the filter the values are exactly the 4-value public enum.)

### 4.7 `GET /datasets`

Serves the datasets manifest — full contract in §7.

---

## 5. `GET /badge/{domain}.svg` — status badge

**Route.** chi pattern `GET /badge/{domain}.svg` (chi matches a static suffix after a param within one segment; the `.svg` is part of the route, never part of the `domain` param). A `.svg`-less path (`GET /badge/example.com`) is a route miss → plain 404.

**Input handling.** Strip the `.svg` suffix, then normalize and validate with the SAME function chain as `POST /check` step 1 (§6.2): `Canonicalize()` (lowercase, IDNA Lookup ToASCII punycode, strict LDH, ≤253 octets, ≥2 labels, no IP literals) plus the reserved-TLD policy layer (reject final label ∈ {`test`, `example`, `invalid`, `localhost`, `internal`, `local`}). Failure → **400** `{"error":"invalid_host","message":"..."}` (standard JSON error, not an SVG; malformed hosts are not legitimate embeds). This is the **declared exception** to the §2.5 404-on-failure rule.

**Lookup.** `SELECT classification, gold, disabled FROM domain WHERE host = $1`. Entity endpoint — not rank-scoped: any kind/origin (Tranco apex, campaign domain, subdomain, live-check host) resolves. **Read-only, zero side effects**: never inserts a domain row, never enqueues a check_job, never touches `last_requested_at`.

**Badge selection (first match wins):**

| Condition | Message | Message color |
|---|---|---|
| no row, `disabled = TRUE` (any reason), or `classification = 'unknown'` | `unknown` | `#9f9f9f` (gray) |
| `classification = 'hero' AND gold` | `gold` | `#d4af37` |
| `classification = 'hero'` | `supported` | `#4c1` |
| `classification = 'partial'` | `partial` | `#dfb317` |
| `classification = 'sinner'` | `unsupported` | `#e05d44` |
| `classification = 'inactive'` | `inactive` | `#9f9f9f` |

Always **HTTP 200** for a valid host — a 404 renders as a broken image in READMEs. Disabled → gray `unknown` implements public exclusion for this endpoint; it deliberately differs from `GET /domain/{domain}`'s 404-on-disabled (which is production parity and does not bind this new endpoint). Badge copy is public status vocabulary, NOT ladder branding: a README badge never says "sinner"/"hero" (owners won't embed self-shaming badges). The copy/color table is ONE Go constant table — the single place to reword.

**Rendering.** shields.io flat style, label `IPv6` (label bg `#555`, white text), six precompiled variants of one template — fixed geometry + `textLength`, byte-deterministic, no font measurement, no dependencies:

```svg
<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="20" role="img" aria-label="IPv6: {MSG}"><title>IPv6: {MSG}</title><linearGradient id="s" x2="0" y2="100%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient><clipPath id="r"><rect width="{W}" height="20" rx="3" fill="#fff"/></clipPath><g clip-path="url(#r)"><rect width="37" height="20" fill="#555"/><rect x="37" width="{MW}" height="20" fill="{COLOR}"/><rect width="{W}" height="20" fill="url(#s)"/></g><g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="110" text-rendering="geometricPrecision"><text x="195" y="150" transform="scale(.1)" fill="#010101" fill-opacity=".3" textLength="270">IPv6</text><text x="195" y="140" transform="scale(.1)" textLength="270">IPv6</text><text x="{TX}" y="150" transform="scale(.1)" fill="#010101" fill-opacity=".3" textLength="{TL}">{MSG}</text><text x="{TX}" y="140" transform="scale(.1)" textLength="{TL}">{MSG}</text></g></svg>
```

Geometry table (`W = 37 + MW`, `TX = (37 + MW/2) × 10`, `TL = (MW − 10) × 10`):

| MSG | MW | W | TX | TL |
|---|---|---|---|---|
| gold | 38 | 75 | 560 | 280 |
| supported | 69 | 106 | 715 | 590 |
| partial | 53 | 90 | 635 | 430 |
| unsupported | 81 | 118 | 775 | 710 |
| inactive | 59 | 96 | 665 | 490 |
| unknown | 61 | 98 | 675 | 510 |

**Headers.** Per §1: `Content-Type: image/svg+xml`, `Cache-Control: public, max-age=3600`, global `X-Content-Type-Options: nosniff`. No ETag, no rate-limit special-casing — one indexed PK lookup + string template is cheaper than any JSON endpoint.

**Interactions.** Pre-phase-5, `domain.gold` is false for everyone (`crawler.resources.enabled=false`), so heroes render `supported` — correct, no special case. Documented usage string: `![IPv6](https://api.whynoipv6.com/badge/example.com.svg)`.

**Acceptance criteria** (fixtures in 10-testing.md): golden-file test per variant (six SVGs byte-exact); unknown host → 200 gray `unknown`; disabled host → 200 gray `unknown`; `xn--`-input and equivalent Unicode input render the same badge; `.svg`-less path → 404 (route miss); invalid host → 400 JSON.

**Scope note:** ships phase 6, priority "ship-when-cheap"; nothing else references it, so cutting it needs no spec change.

---

## 6. Live check — `POST /check` / `GET /check/{id}`

### 6.1 Rule 0 — live checks never touch confirmed state

The live-check consumer runs the engine and writes its result ONLY to `check_job.result`. It never inserts `scan` or `scan_detail` rows and never updates any `domain` column except the initial row insert for unknown hosts (§6.5 step 2). Confirmed statuses, pending counters (`*_pending`, `*_pending_count`), `*_observed`, `last_checked_at`, `next_check_at`, `classification`, changelog rows, and country/ASN counters advance exclusively via frontier scans. The POST handler's lifecycle re-entry writes (§6.6) — `last_requested_at = now()` and, for `dead`/`delisted` rows, `next_check_at = now()` / re-enable — are allowed alongside the initial row insert: they *schedule* frontier work, they never *advance* confirmed state. Rationale: the anti-flap N-consecutive-scans rule assumes daily cadence; anonymous POSTs must not be able to accelerate a confirmed transition. `check_job` rows and results are public data; sequential BIGINT ids are enumerable and that is accepted (no auth, nothing sensitive).

### 6.2 `POST /check` — processing order

Body: `{"domain": "<host>"}`.

1. **Parse + validate.** **Decision:** a body that is not valid JSON, lacks the `domain` key, or has a non-string value → `400 {"error":"invalid_host","message":"body must be {\"domain\":\"<hostname>\"}"}` (one 400 vocabulary for the endpoint). Then `Canonicalize(host)` (§2.5), then the POST /check-only policy layer on top: reject IP literals (already dead at Canonicalize) and reserved TLDs — final label ∈ {`test`, `example`, `invalid`, `localhost`, `internal`, `local`}. Failure → `400 {"error":"invalid_host","message":"..."}`. (SSRF is already handled by the engine's pinned dialer; these rejections are the API-boundary layer on top.)
2. **Rate limit.** Count `check_job` rows for this `requester_ip` (§1.2 RealIP) with `created_at > now() - interval '1 hour'`; limit `live_check.rate_ip_per_hour` (default 10). Then the global count over the same window; limit `live_check.rate_global_per_hour` (default 500). Exceeded → `429 {"error":"rate_limited","scope":"ip","retry_after_s":1042}` (or `"scope":"global"`) + `Retry-After: 1042` header. **Decision:** `retry_after_s = ceil(3600 − (now − min(created_at))` in seconds`)` over the counted window rows — the time until the oldest counted row ages out; the header carries the same integer.
3. **Lifecycle re-entry** (**Decision:** runs after rate limiting, before dedupe, on every POST whose host already has a `domain` row — including requests answered from dedupe): apply §6.6.
4. **Dedupe, domain-side.** If a `domain` row exists and `last_checked_at >= now() - interval '1 hour'` (`live_check.dedupe_window`), load its latest `scan_detail` row, run the shared result mapper (§6.4) over `details`, and return `200` with a **synthetic done envelope**: `id: null`, `host`, `status:"done"`, `cached:true`, `created_at` = the scan_detail `ts`, `completed_at` = the same `ts`, `error:null`, `result` = mapper output (`checked_at` = that `ts`, `duration_ms` = `scan_detail.duration_ms`), `confirmed` = §6.3's object from the domain row. No job row is created. (`last_checked_at` is written only by frontier commits, so live checks never count as "scanned" for this window.)
5. **Dedupe, job-side.** Else if a `check_job` for the same canonical host has `status='done' AND completed_at >= now() - interval '1 hour'`, return `200` with that job's §6.3 envelope, `cached` overridden to `true` (id = the existing job's id).
6. Else `INSERT INTO check_job (host, requester_ip) VALUES ($1, $2)` → status `pending`; return `202 {"id":123,"host":"example.com","status":"pending","created_at":"2026-07-06T10:00:00Z"}` (exactly these four keys).

### 6.3 `GET /check/{id}`

`{id}` must parse as a positive int64; non-numeric or no row → `404 {"error":"not_found"}` (**Decision:** non-numeric ids are lookup misses, same body). Success → `200`:

```json
{
  "id": 123,
  "host": "example.com",
  "status": "pending",           // pending|processing|done|failed
  "cached": false,               // false on every job row; true only in §6.2 dedupe envelopes
  "created_at": "2026-07-06T10:00:00Z",
  "completed_at": null,          // set when done|failed
  "error": null,                 // short string when failed
  "result": null,                // object below when done
  "confirmed": null              // object below when a domain row exists
}
```

`result` (produced by the shared mapper §6.4; statuses use the raw-observation vocabulary `supported|unsupported|no_record|not_applicable|error` — plus `inconsistent` for base/www when the resolver quorum split; live results are raw observations, explicitly NOT confirmed state):

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

`confirmed` (from the `domain` row; `null` if no row exists or nothing confirmed yet — i.e. all six statuses NULL):

```json
{"classification":"partial","class_flags":["mail_missing"],"gold":false,
 "statuses":{"base":"supported","www":"supported","ns":"supported",
             "mx":"unsupported","conn":"supported","resources":null},
 "as_of":"2026-07-06T04:12:09.331Z"}
```

(`as_of` = `domain.last_checked_at`.) `confirmed` is computed at **read time** on every `GET /check/{id}` and on dedupe responses.

### 6.4 Shared result mapper (one implementation, three consumers)

`MapLiveResult(sr checker.ScanResult) → result JSON`. Applies the engine→public dimension mapping exactly (keys are the PUBLIC dimension names, not engine check names):

- `base` ← `dns_aaaa_base`; `www` ← `dns_aaaa_www` (consensus-composite observations, including `inconsistent` on quorum split)
- `ns` ← `dns_ns_ipv6` (engine `partial` → `supported`); `mx` ← `dns_mx_ipv6` (`partial` → `supported`)
- `conn` ← the worker-side https/http composition table (02-observation-model.md — `conn` composition owns the table; `https_ipv6` with `http_ipv6` fallback)
- `tls`, `parity`, `dnssec`, `ptr`, `spf` ← informational, raw engine status; `smtp` ← informational with `partial` → `unsupported`
- `latency` ← `latency_ipv4`/`latency_ipv6` (`{"v4_ms":int|null,"v6_ms":int|null}`)
- `resources` is NOT engine-mapped: it is the registry roll-up, computed **read-only** over the run's `resource_discovery` host list against confirmed `resource_host.aaaa_status` (discovery `error` → `error`; `not_applicable` → `not_applicable`; hosts missing or unswept in the registry are NULL → `error` — the defer branch; while `crawler.resources.enabled=false` → `not_applicable`). No registry rows are written on this path, per Rule 0.

Because `scan_detail.details` stores the engine ScanResult serialization, the same mapper serves the domain-side dedupe path (§6.2 step 4). This mapper is ALSO the single mapping used by the frontier worker before the confirmed-status commit — one mapping, three consumers (frontier worker, live-check consumer, dedupe reader). Implementation lives with the crawler-facing mapping code; the API imports it (02-observation-model.md — the Result→observation mapper, `internal/crawler/observe.go`).

### 6.5 Consumer (contract; runs in `cmd/crawler` — placement, pool lifecycle, and shutdown in 04-lifecycle-scheduling.md)

Dedicated goroutine pool: `live_check.workers` (default 4) slots; poll every 2s when idle.

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

2. Ensure a `domain` row: `INSERT INTO domain (host, kind, parent_id, rank, created_by, last_requested_at) VALUES ($host, $kind, $parent_id, NULL, 'live_check', now()) ON CONFLICT (host) DO NOTHING`. `kind` via the campaign-import PSL helper; `parent_id` set only if the registrable parent row ALREADY exists — live checks never auto-ensure parents (a `parent_link` row would grant permanent frontier eligibility, letting abuse grow the frontier). New rows keep the column default `next_check_at = now()`, so the frontier scans the host promptly.
3. Run the full engine with a 60s context budget (`live_check.job_budget`), panic-recovered.
4. On success: `UPDATE check_job SET status='done', result=$1, completed_at=now() WHERE id=$2`. On error/timeout: `UPDATE check_job SET status='failed', error=$2, completed_at=now() WHERE id=$3`. Nothing else is written (Rule 0).

**Reaper** (same goroutine, every 60s) — guarantees every poller terminates ≤15 min:

```sql
UPDATE check_job SET status='failed', error='timed out', completed_at=now()
WHERE status IN ('pending','processing') AND created_at < now() - interval '15 minutes';
```

**Retention** (daily tick, owned by 04-lifecycle-scheduling.md): `$1` = `live_check.retention` (duration, default 720h = 30d; registry: 09-ops.md).

```sql
DELETE FROM check_job WHERE created_at < now() - $1;
```

### 6.6 Lifecycle re-entry (POST-handler writes on existing rows)

Every `POST /check` for an existing host sets `last_requested_at = now()` — this is the "live-check origin within 7 days" linkage evaluated by the daily lifecycle sweep (window `lifecycle.live_check_linkage`, default 168h), and it extends the frontier life of any rank-NULL row a user actively watches. Additionally:

- `disabled_reason = 'delisted'` → re-enable: clear `disabled`, `disabled_reason`, `disabled_at`; `orphaned_at = NULL`; `next_check_at = now()`.
- `disabled_reason = 'dead'` → leave disabled but set `next_check_at = now()`: recovery happens via the pulled-in frontier scan, which commits through the trust machine and runs its re-enable/reset step if the domain actually resolves.
- `disabled_reason IN ('service','manual')` → the live check runs and returns its result, but never re-enables.

Once `last_requested_at` ages past the window, the sweep delists the row; a later POST refreshes it and re-enables per the rules above. New hosts get `created_by='live_check'`, `rank NULL`, `last_requested_at = now()` (§6.5 step 2).

*Rejected — synchronous inline check:* a full engine run can take 60–90s (SMTP/HTTP timeouts); holding an anonymous HTTP request open that long invites trivial resource-exhaustion abuse. Job + poll matches ready.chair6.net-class tools.

### 6.7 Config keys (crawler config; registry: 09-ops.md)

`live_check.workers` (int, 4), `live_check.job_budget` (duration, 60s), `live_check.reclaim_after` (duration, 5m), `live_check.fail_after` (duration, 15m), `live_check.retention` (duration, 720h), `live_check.rate_ip_per_hour` (int, 10), `live_check.rate_global_per_hour` (int, 500), `live_check.dedupe_window` (duration, 1h).

---

## 7. Datasets

### 7.1 Hosting decision

Datasets live under the `/datasets/` path on the API origin (`api.whynoipv6.com`). No separate vhost: one cert, one DNS name, one CORS story, one nginx server block. All file references in the manifest are origin-relative absolute paths (`/datasets/...`). *Rejected — API-generated exports on demand* (1M-row × N-format generation belongs in a batch job writing static files; the API serves only the manifest) and *rejected — S3* (operator runs own hardware + nginx; a directory is the 20-line solution).

### 7.2 On-disk layout

Config key `DATASETS_DIR` (string, default `/var/lib/whynoipv6/datasets`; registry: 09-ops.md), shared by the API binary and `v6ctl export`:

```
/var/lib/whynoipv6/datasets/
├── manifest.json                      # rewritten atomically after every export
├── DICTIONARY.md                      # column + status-semantics docs
├── latest -> 2026-07-06               # symlink to newest COMPLETE snapshot
├── 2026-07-06/                        # immutable once published
│   ├── whynoipv6-top100k.csv.gz
│   ├── whynoipv6-top100k.parquet
│   ├── whynoipv6-top1m.csv.gz
│   ├── whynoipv6-top1m.parquet
│   ├── whynoipv6-full.csv.gz
│   ├── whynoipv6-full.parquet
│   └── SHA256SUMS                     # sha256sum -c compatible, all 6 files
└── 2026-07-05/ ...
```

File naming: `whynoipv6-{size_tier}.{format}`, `size_tier ∈ {top100k, top1m, full}`, `format ∈ {csv.gz, parquet}`. Public URLs: `https://api.whynoipv6.com/datasets/{YYYY-MM-DD}/whynoipv6-top1m.csv.gz`, `.../datasets/latest/whynoipv6-top1m.csv.gz` (stable URL for scripts), `.../datasets/DICTIONARY.md`. Retention: dailies 90 days, first-of-month forever.

Export content (produced by nightly `v6ctl export`, Parquet via `parquet-go/parquet-go`; job ownership 04-lifecycle-scheduling.md/09-ops.md): columns host, rank, kind, parent, classification + flags, gold, the six confirmed statuses + since-timestamps, country, asn, last_checked. `top100k`/`top1m` use the publicly-ranked predicate; `full` includes all non-disabled scannable entities (any kind/origin). `DICTIONARY.md` documents every column and the status semantics (incl. what "confirmed" means).

### 7.3 `manifest.json` schema (= the response schema of `GET /datasets`)

```json
{
  "schema_version": 1,
  "generated_at": "2026-07-06T04:30:00Z",
  "dictionary": "/datasets/DICTIONARY.md",
  "latest": {
    "date": "2026-07-06",
    "files": [
      {
        "size_tier": "top1m",
        "format": "csv.gz",
        "path": "/datasets/2026-07-06/whynoipv6-top1m.csv.gz",
        "bytes": 48211334,
        "sha256": "hex-encoded, 64 chars",
        "rows": 1000000
      }
    ]
  },
  "snapshots": [
    { "date": "2026-07-06", "files": [ /* same file object shape */ ] }
  ]
}
```

Field semantics: `schema_version` (int, starts at 1) versions the *export column schema* documented in DICTIONARY.md — bump whenever exported columns change. `generated_at` RFC 3339 UTC. `rows` = data rows excluding the CSV header (identical for the csv.gz/parquet pair of a tier). `path` is origin-relative and always points at the **dated** (immutable) path, never `latest/`, so `sha256` stays valid. `snapshots` is sorted newest-first and lists every snapshot currently retained on disk. `latest` duplicates the newest complete snapshot's entry. Every snapshot entry contains exactly 6 files (3 tiers × 2 formats).

### 7.4 Atomic publish procedure (nightly `v6ctl export`, after the stats tick)

1. Write all 6 files + `SHA256SUMS` into `$DATASETS_DIR/{date}.tmp/`; fsync files.
2. `rename({date}.tmp, {date})` — snapshot becomes visible complete-or-not-at-all.
3. Repoint latest atomically: `ln -sfn {date} $DATASETS_DIR/latest.tmp && mv -T latest.tmp latest` (rename(2), no window where `latest` is missing).
4. Prune per retention (delete expired daily dirs; keep first-of-month).
5. Regenerate `manifest.json` from the directory tree (source of truth = what is on disk), write `manifest.json.tmp`, rename over `manifest.json`.

On any failure before step 2, delete the `.tmp` dir and fire the ops webhook; the previous manifest/latest remain untouched and correct.

### 7.5 `GET /datasets` (API side)

Reads `$DATASETS_DIR/manifest.json` and returns it **verbatim** as `application/json` with `Cache-Control: public, max-age=300`. **Decision:** re-read from disk on every request (the file is a few KB; no in-process cache, no invalidation bug class). Missing or unparseable file → `503 {"error":"manifest_unavailable"}`.

### 7.6 nginx split (sibling locations in the api.whynoipv6.com server block; deployed per 09-ops.md)

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

(`root`, not `alias`, so `/datasets/...` maps directly under `/var/lib/whynoipv6/`; nginx follows the `latest` symlink by default.)

---

## 8. OpenAPI (spec-first)

- `openapi/openapi.yaml` is the committed source of truth for every route in §2.1 (~38 paths). OpenAPI 3.1. Legacy quirks are documented **explicitly**: the `{"data":[...]}` envelopes, the `{campaign, domains}` composite, shortuuid `pattern`/`example` (§2.7), singular `/metric`, the 3-string legacy status enum vs the 4-value new-model enum (two named schemas: `LegacyStatus`, `PublicStatus`), the pinned error bodies as named `Error*` response schemas, the zero-time timestamp convention, and the 404-vs-[] map as per-operation responses.
- `make generate` runs **oapi-codegen** (`github.com/oapi-codegen/oapi-codegen/v2`, latest release, chi-server **strict-server** mode → handler interfaces + typed models in `internal/api/gen/`) and **openapi-typescript** (TS types + client for `frontend/`). Handlers implement the generated strict-server interface; hand-written code never redefines wire types.
- **Generated-code gate:** CI regenerates and fails on any diff (`git diff --exit-code` after `make generate`) — the monorepo's single-commit sync promise. Generated output is committed.
- The spec doubles as the parity-fixture skeleton: every legacy operation carries `x-production-source: <file:line>` (the references in §3) so 10-testing.md fixture capture is mechanical.
- *Rejected — code-first swaggo annotations:* comment-derived specs drift and can't express the legacy envelopes precisely; with a frozen contract, the spec is where the contract lives.

---

## 9. Acceptance criteria (summary; fixtures live in 10-testing.md)

1. **Golden parity:** one recorded production fixture per §3 endpoint (source files named per endpoint), compared JSON-equal (byte-equal where pinned); `GET /domain/{domain}` comparisons strip the two additive keys (§3.6).
2. **Zero-result map:** one fixture per row of the §2.11 404 table asserting status + exact body (garbage query `zzzzqqqq` for the searches; a valid campaign with `offset=10000` for the paging case), plus `GET /metric/asn/search/zzzzqqqq` → `200 []` and each []-cleanup endpoint → `200 []`.
3. **R1/R2 synthetics:** fixtures exercising `not_applicable`→`"no_record"`, NULL→`"no_record"`, and R2's error/inconsistent row exclusion (production can't produce these).
4. **Codec:** §2.7 vectors round-trip both directions; encode output exactly 22 chars; the two negative vectors 404 with `{"error":"Invalid UUID"}`.
5. **Ladder totality:** `renderChangelog` is exercised for all 16 table rows + both `field='legacy'` passthrough branches; the §3.12 coverage filter provably excludes `conn`/`resources`/`not_applicable` rows.
6. **Membership synthetics:** an entity with confirmed `base=supported, www=unsupported` appears in `/domain/almost` and NOT in `/domain`; one with `base=unsupported` appears in `/domain` and NOT in `/domain/almost`; repeated for the country-scoped pair.
7. **Visibility:** disabled domains appear on no list, feed, stats, or search response; disabled campaigns 404 on all UUID routes and vanish from `/campaign` and `/changelog/campaign`; rank-NULL rows never appear on ranked lists but resolve on entity endpoints.
8. **Live check:** Rule-0 assertion (a completed check job leaves every `domain` state column untouched except `last_requested_at`/`next_check_at`/re-enable per §6.6); dedupe (domain-side and job-side) returns `cached:true` without new job rows; reaper flips stale jobs to `failed` ≤15 min; rate-limit fixtures per §1.8.
9. **Badge:** §5 acceptance list.
10. **Baseline:** §1.8 list; every JSON endpoint carries the NoCache header; badge and manifest carry their pinned Cache-Control values.
