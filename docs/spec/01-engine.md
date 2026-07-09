# 01 — Checker Engine (the v6audit lift)

_Status: Round 3.0 — API redesign folded in (docs/api-design-research.md, decisions 2026-07-09): clean root API, keyset pagination, RFC 9457, no legacy compat, no history import._

**Purpose:** Defines the scanning engine of the WhyNoIPv6 crawler: the complete lift of `v6audit/internal/checker` into `internal/checker`, the exact per-check behavior of all 15 checks, the bulk DNS resolver, the SSRF-safe dialer, the consensus resolver seam, the two-phase runner, and the IPv6 self-preflight. Everything in this file is engine-side: it produces 5-valued `checker.Result`s and a `ScanResult`; turning those into per-dimension observations, quorum verdicts, and confirmed state belongs to other files.

**Deliverables:**

- `internal/checker/` — the whole package: `checker.go`, `constants.go`, `resolver.go`, `ssrf.go`, `runner.go`, `seam.go` (new — the `AAAAResolver` seam types), `preflight.go` (new — lifted from the v6audit agent), and the 15 check implementation files listed in §2 (including `resource_discovery.go`, the adapted `resource_ipv6.go`).
- The `codeberg.org/miekg/dns` go.mod pin.
- The config keys `checks.max_ns_lookups`, `checks.max_mx_lookups`, `resolver.bulk_upstreams`, `preflight.probe_host` (introduced here by name; registry: 09-ops.md).

**Decision:** There is no separate `internal/resolver` package. Per design §6 the lifted resolver (`resolver.go`) and SSRF dialer (`ssrf.go`) live inside `internal/checker`, exactly as they do in v6audit — the package is lifted as a unit. `internal/consensus` (the quorum implementation of the seam in §9) is specified in 02-observation-model.md.

**Companion files:** 00-overview.md (sizing constants), 02-observation-model.md (observation mapping, consensus quorum implementation, `conn` composition, `resources` roll-up), 03-state-machine.md (per-domain commit machine, confirmed state, the `pgx.Batch` write unit), 04-lifecycle-scheduling.md (claim loop, worker pool, scheduling/recheck backoff, preflight wiring, ops-webhook alerting), 05-schema.md (all DDL), 09-ops.md (config-key registry, Unbound deployment), 10-testing.md (fixtures, fake-DNS parity tests).

**Reference repo for code lifting:** `github.com/lasseh/v6audit` (local checkout path used throughout: `v6audit/…`). The implementer copies files from there per the inventory below; this spec is authoritative wherever it deviates from the copied source.

---

## 1. Scope and boundaries

The engine is `internal/checker`. It:

- runs 15 checks against one entity host (`apex` or `subdomain` kind) with two-phase conditional execution;
- returns a `ScanResult` whose `Results` map is keyed by check name (§5) — engine statuses are 5-valued: `supported / unsupported / partial / error / not_applicable`;
- performs all bulk DNS through its own `Resolver` (§6) pointed at two local Unbound instances, and the two classification-critical AAAA lookups (`dns_aaaa_base`, `dns_aaaa_www`) through the injected `AAAAResolver` seam (§9);
- dials only DNS-pinned, SSRF-validated literal IPs (§8);
- knows nothing about observations, quorum decisions beyond the seam types, confirmed state, the database, or scheduling.

This file does NOT own: the engine-status → observation mapping and `conn` composition (see 02-observation-model.md), the `internal/consensus` quorum implementation (see 02-observation-model.md), the per-domain commit transaction (see 03-state-machine.md), the claim loop and worker pool (see 04-lifecycle-scheduling.md), any SQL DDL (see 05-schema.md), test fixtures (see 10-testing.md).

Hard constraints restated: the engine has **no scoring and no grades** — `scoring.go` is deleted, `ScanResult` has no `Score`/`Grade` fields, and nothing in this package may reintroduce a numeric quality signal. Statuses and raw details only.

## 2. Lift inventory

Every file below is copied from the v6audit repo and then modified exactly as stated. "Verbatim" means behavior-identical: the only permitted edits are the three enumerated deviations (§3) and the global mechanical transforms (§4), which apply across the whole package. Anything marked "adapted" has additional, individually specified changes.

| v6audit source path | Target file in `internal/checker/` | Mode | Changes beyond §3/§4 |
|---|---|---|---|
| `internal/checker/checker.go` | `checker.go` | **adapted** | Drop `Category()` from the `Checker` interface; drop `ScanResult.Score`/`Grade`; delete `CheckerToDBColumn` and `DBColumnToChecker` (§5) |
| `internal/checker/constants.go` | `constants.go` | **adapted** | Delete the four scoring-category constants; keep the error/reason message constants (§5) |
| `internal/checker/resolver.go` | `resolver.go` | **adapted** | Delete the in-process TTL cache (§6); miekg/dns v2 port is §4 |
| `internal/checker/ssrf.go` | `ssrf.go` | **verbatim** | — |
| `internal/checker/runner.go` | `runner.go` | **adapted** | Remove `ComputeScore`; move `latency_ipv4` to phase 2; kind-aware execution; new constructor wiring (§10) |
| `internal/checker/dns_aaaa_base.go` | `dns_aaaa_base.go` | **adapted** | Resolves through the `AAAAResolver` seam; quorum details; NXDOMAIN branch kept as raw engine behavior (§11.1) |
| `internal/checker/dns_aaaa_www.go` | `dns_aaaa_www.go` | **adapted** | Same seam adaptation; CDN detection kept (§11.2) |
| `internal/checker/dns_ns_ipv6.go` | `dns_ns_ipv6.go` | **adapted** | Per-host cap becomes config `checks.max_ns_lookups` (§11.3) |
| `internal/checker/dns_mx_ipv6.go` | `dns_mx_ipv6.go` | **adapted** | Per-host cap becomes config `checks.max_mx_lookups`; kind-aware implicit-MX skip (§11.4) |
| `internal/checker/http_ipv6.go` | `http_ipv6.go` | **adapted** | Gains the terminal `error_type` classification — enumerated deviation 3 (§11.6) |
| `internal/checker/https_ipv6.go` | `https_ipv6.go` | **verbatim** | — (§11.5) |
| `internal/checker/tls_ipv6.go` | `tls_ipv6.go` | **verbatim** | — (§11.7) |
| `internal/checker/response_parity.go` | `response_parity.go` | **verbatim** | — (§11.8) |
| `internal/checker/resource_ipv6.go` | `resource_discovery.go` | **adapted** | Becomes discovery-only `resource_discovery`; inline AAAA checks and status derivation deleted (§11.9) |
| `internal/checker/smtp_ipv6.go` | `smtp_ipv6.go` | **verbatim** | EHLO identity string is part of deviation 1 (§11.10) |
| `internal/checker/spf_ipv6.go` | `spf_ipv6.go` | **verbatim** | — (§11.11) |
| `internal/checker/dns_ptr_ipv6.go` | `dns_ptr_ipv6.go` | **verbatim** | — (§11.12) |
| `internal/checker/dns_dnssec.go` | `dns_dnssec.go` | **verbatim** | — (§11.13) |
| `internal/checker/latency.go` | `latency.go` | **verbatim** | Phase move is a runner change, not a check change (§11.14) |
| `cmd/v6agent/main.go` lines 356–380 (`checkIPv6Connectivity`) | `preflight.go` | **adapted** | Wrapped into the `Preflight` type with a freshness clock (§12) |
| `internal/checker/scoring.go` | — | **deleted** | Never copied. No score, no grade, no `_global` merging, anywhere |

Additionally ported IN from the production repo:

| Source | Target | What |
|---|---|---|
| `whynoipv6/internal/resolver/resolver.go` lines 483–514 (`isGloballyRoutableIPv6`) | `internal/checker/routable.go` (new file) | Exported as `IsGloballyRoutableIPv6(ip net.IP) bool` — rejects IPv4/IPv4-mapped, loopback, link-local unicast/multicast, ULA (`fc00::/7`, first byte `0xfc`/`0xfd`), and the unspecified address. Consumed by `internal/consensus` when reducing per-resolver AAAA answers to symbols (`exists` requires ≥1 globally routable AAAA — see 02-observation-model.md). Nothing else from the production resolver is lifted (its naive TLD split and hardcoded upstreams are explicitly rejected by the design) |

v6audit's auth, billing, alerting, agent-dispatch, and frontend code is not lifted. Nothing outside `internal/checker` and the one `cmd/v6agent` function is copied.

## 3. The three enumerated lift deviations

These are the ONLY behavior changes permitted inside files marked "verbatim". They are deliberate and auditable; do not add a fourth.

1. **Identity constants.** The `userAgent` constant in `http_ipv6.go` changes from `V6Audit/1.0 (+https://v6audit.com/bot)` to:

   ```go
   const userAgent = "WhyNoIPv6Bot/1.0 (+https://whynoipv6.com/bot)"
   ```

   This constant is used by every HTTP(S) fetch in the package (`http_ipv6`, `https_ipv6`, `response_parity`, `resource_discovery`, `latency`). The SMTP EHLO name in `smtp_ipv6.go` changes from `EHLO v6audit.com` to `EHLO whynoipv6.com` (design §2.5 pins both identity strings together). The `/bot` page contract (purpose, opt-out contact, crawl behavior) is an API/ops deliverable, not the engine's.

2. **miekg/dns v2 port.** All lifted code imports `codeberg.org/miekg/dns` instead of `github.com/miekg/dns`. See §7 for the pin and porting rule.

3. **`http_ipv6` terminal error classification.** `http_ipv6.go` is extended with the same `error_type` classification `https_ipv6.go` already has, so the `conn` composition table (02-observation-model.md) applies identically on the http-only fallback path. Exact rule in §11.6.

## 4. Global mechanical transforms

Applied uniformly to every copied file; they change signatures and imports, never behavior:

1. **Module path.** `package checker` moves to `<module>/internal/checker`. Every `github.com/miekg/dns` import becomes `codeberg.org/miekg/dns` and the code is ported to the v2 API (§7).
2. **`Category()` removal.** The `Category()` method is deleted from the `Checker` interface and from every check implementation. The category constants in `constants.go` go with it.
3. **Kind parameter.** **Decision:** the `Checker` interface method becomes `Check(ctx context.Context, host string, kind Kind) (Result, error)`. This is the mechanism for design §2.2's kind-aware checks. Every check file gets the one-line signature change; every check EXCEPT `dns_mx_ipv6` ignores `kind` (the `www` skip for subdomains is handled by the runner, §10, not by the check). `Kind` is defined in `checker.go`:

   ```go
   type Kind string

   const (
       KindApex      Kind = "apex"
       KindSubdomain Kind = "subdomain"
   )
   ```

   The values match the `domain.kind` enum (see 05-schema.md — enums); the crawler maps the DB value onto `checker.Kind` when calling `Runner.Run`.

## 5. Core types (`checker.go`, `constants.go`)

After adaptation, `checker.go` contains exactly:

```go
package checker

// CheckStatus is a bounded set of possible check outcomes.
type CheckStatus string

const (
    StatusSupported     CheckStatus = "supported"
    StatusUnsupported   CheckStatus = "unsupported"
    StatusPartial       CheckStatus = "partial"
    StatusError         CheckStatus = "error"
    StatusNotApplicable CheckStatus = "not_applicable"
)

// Result is the outcome of a single check against a host.
type Result struct {
    Status  CheckStatus    `json:"status"`
    Details map[string]any `json:"details,omitempty"`
    Latency time.Duration  `json:"latency"`
}

// Checker is implemented by every individual check.
// Each implementation is stateless and safe for concurrent use.
type Checker interface {
    Name() string
    Check(ctx context.Context, host string, kind Kind) (Result, error)
}

// ScanResult contains the results of all checks for a single host.
type ScanResult struct {
    Domain    string            `json:"domain"`
    Results   map[string]Result `json:"results"`
    ScannedAt time.Time         `json:"scanned_at"`
    Duration  time.Duration     `json:"duration"`
}
```

Deleted relative to v6audit: `Category()` on the interface, `ScanResult.Score` and `ScanResult.Grade`, and — **Decision** — BOTH name-mapping vars `CheckerToDBColumn` and `DBColumnToChecker` (design §2.1 names only `DBColumnToChecker`; `CheckerToDBColumn` maps to the legacy v6audit JSONB column names which nothing in the new system reads — the observation mapper in 02-observation-model.md keys off engine check names directly, and `scan_detail.details` stores the `ScanResult` serialization as-is).

The serialized `ScanResult` (Go `encoding/json` of the struct above) is the exact payload the crawler writes to `scan_detail.details` (column defined in 05-schema.md — time-series hypertables; write path in 03-state-machine.md — the write unit). The engine therefore never renames a `Details` key casually: every key named in §11 is part of the stored payload contract consumed by the API detail page and the live-check mapper.

`constants.go` keeps the shared message constants (used verbatim by checks and the runner):

```go
const (
    errConnRefused     = "connection refused"
    errAddrBlocked     = "address in blocked range"
    errNoAAAARecord    = "no AAAA record"
    reasonNoAAAARecord = "no AAAA record on base domain"
    reasonNoMXWithAAAA = "no MX with AAAA record"
)
```

The `CategoryDNS/Web/Mail/Security` constants are deleted.

**Canonical check-name strings.** The check set is defined by enumeration (design §2.8 F). `Checker.Name()` returns exactly these 15 strings, which are the keys of `ScanResult.Results` and of `scan_detail.details.results`:

`dns_aaaa_base`, `dns_aaaa_www`, `dns_ns_ipv6`, `dns_mx_ipv6`, `dns_dnssec`, `http_ipv6`, `https_ipv6`, `tls_ipv6`, `http_response_parity`, `resource_discovery`, `smtp_ipv6`, `spf_ipv6`, `dns_ptr_ipv6`, `latency_ipv4`, `latency_ipv6`.

**Decision:** the adapted resource check's `Name()` is `"resource_discovery"` (the design renames the check; carrying the old `resource_ipv6` string forward would misdescribe a discovery-only check). All other names keep their v6audit strings verbatim — including `http_response_parity` and `dns_ns_ipv6`/`dns_mx_ipv6` (design §2.8 F's short forms `dns_ns`, `dns_mx`, `http`, … are prose shorthand, not wire names). Any prose count of checks is derived from this enumeration; never carry a numeric count forward independently (sizing constant "Engine checks = 15": 00-overview.md).

## 6. The bulk resolver (`resolver.go`)

`checker.Resolver` is the bulk-path DNS client used by everything except the two consensus AAAA lookups: NS chain + NS-host AAAA, MX + MX-host AAAA, A records, PTR, TXT/SPF, DNSSEC probes, the SafeDialer's DNS-pinned re-resolution, the preflight, and (in 02-observation-model.md) the conditional A lookup and the resource-host registry sweep.

### 6.1 Retained behavior (lifted 1:1)

- **Construction:** `NewResolver(upstreams []string)`. In production wiring the upstreams are the two local Unbound instances from config key `resolver.bulk_upstreams` (§13) — the crawler MUST pass them explicitly. The v6audit `defaultUpstreams` fallback (public resolvers) is kept in code as the empty-slice default but is never exercised by the wired binaries; the consensus implementation reuses `NewResolver` with single-upstream lists (see 02-observation-model.md).
- **Per-query timeout:** `dnsTimeout = 5s`, clamped to the remaining context deadline; if the deadline has already passed, return `context.DeadlineExceeded` without dialing.
- **EDNS0:** every lookup helper calls `setEDNS0(msg)` — adds an OPT record advertising `defaultUDPSize = 4096` bytes (DO bit false) unless the message already carries EDNS0.
- **Transport:** UDP first; on a truncated response, retry the SAME upstream over TCP.
- **Upstream rotation:** `nextUpstream()` round-robins across the configured upstreams under a mutex, one increment per `Query` call. Combined with the retry below this yields retry-on-next-upstream behavior.
- **Retry:** `QueryWithRetry` retries exactly once when the first attempt returns a transport error, SERVFAIL, or REFUSED. If the retry also errors: return the first attempt's error (if it had one) or the first attempt's rcode response. No further retries — "never retry a SERVFAIL'ing domain in a tight loop" is enforced structurally by the scheduler's recheck backoff (04-lifecycle-scheduling.md), not here.
- **`LookupAAAA(ctx, name) (ips []net.IP, cnameChain []string, ttl int, rcode string, err error)`** — sets RD, follows CNAME chains up to `maxCNAMEHops = 10` with a `seen` map for loop detection, accumulates the full chain, tracks the minimum answer TTL. SERVFAIL → `(nil, nil, 0, "SERVFAIL", error)`. NXDOMAIN → `(nil, nil, 0, "NXDOMAIN", nil)` (not an error). NOERROR with no AAAA → empty `ips`, `err == nil`.
- **`LookupA`**, **`LookupNS`**, **`LookupMX`** (returns records + rcode string; caller sorts by preference), **`LookupTXT`** (concatenates multi-string TXT per RFC 7208 §3.3), **`LookupPTR`** — as in the source.

### 6.2 The cache deletion (design §2.8 A — normative)

Delete from the copied `resolver.go`: the `cacheEntry` type, `dnsCacheKey`, `minTTL`, the `cache sync.Map` field on `Resolver`, and the cache load/store logic inside `QueryWithRetry` (v6audit `resolver.go` lines 28–31, 38, 50–94, 153–162, 177–182). After deletion, `QueryWithRetry` is exactly:

```go
// QueryWithRetry sends a DNS query, retrying once on transport error,
// SERVFAIL, or REFUSED. The retry lands on the next upstream via the
// round-robin in Query.
func (r *Resolver) QueryWithRetry(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
    resp, err := r.Query(ctx, msg)
    if err != nil || (resp != nil && (resp.Rcode == dns.RcodeServerFailure || resp.Rcode == dns.RcodeRefused)) {
        resp2, err2 := r.Query(ctx, msg)
        if err2 != nil {
            if err != nil {
                return nil, err // return original error
            }
            return resp, nil // return original rcode response
        }
        resp = resp2
    }
    return resp, nil
}
```

Do NOT replace the cache with an LRU or any other in-process cache. Rationale (recorded so future maintainers don't "fix" it back): the v6audit cache is unbounded, evicts only on same-key re-access, and clamps TTL to ≤300s while the frontier revisits a name every 24h — at ~12–16M mostly-unique queries/day it is a multi-GB dead map. **Unbound is the cache**; intra-scan duplicate lookups (the apex AAAA is resolved ~3–5× per domain by different checks) hit Unbound locally at sub-millisecond cost. The consensus path must never be cached at all (every observation must be fresh — 02-observation-model.md).

### 6.3 Operational requirement

`dns_dnssec` relies on a **validating** upstream (AD flag). The Unbound instances behind `resolver.bulk_upstreams` MUST have DNSSEC validation enabled (Unbound's default). Unbound deployment, tuning, and the two-instance topology are specified in 09-ops.md; the engine's only assumptions are: recursive, validating, reachable at the configured addresses, and that qname-minimisation/negative caching remain on.

## 7. DNS library pin

"miekg/dns v2" means module path **`codeberg.org/miekg/dns`**, pinned in `go.mod` at an exact version — v0.6.83 at design time; use the latest v0.6.x at implementation time. It is pre-1.0: any version bump is a reviewed change, never picked up via `go get -u` in passing. **`github.com/miekg/dnsv2` is a stale dead path — never import it.** The lifted code was written against v1 (`github.com/miekg/dns`); the port is mechanical per the official v1→v2 migration guide in the module's documentation. The ported resolver must preserve every behavior in §6.1 bit-for-bit — the fake-DNS parity tests in 10-testing.md are the gate (they run identical query scenarios against the v6audit v1 behavior spec: EDNS0 advertisement, TCP-on-truncation, retry-on-next-upstream, CNAME chase with loop detection, rcode string mapping, TXT concatenation, the DNSSEC DO/AD flag handling in §11.13).

## 8. SafeDialer / SSRF-safe dialing (`ssrf.go`)

Lifted verbatim. Normative summary of what the copied file must do:

**Blocklists** (parsed once in `NewSafeDialer`; a malformed CIDR panics at construction):

- IPv4 (`blockedIPv4`): `0.0.0.0/8`, `10.0.0.0/8`, `100.64.0.0/10` (CGNAT), `127.0.0.0/8`, `169.254.0.0/16` (link-local incl. cloud metadata), `172.16.0.0/12`, `192.0.0.0/24`, `192.0.2.0/24`, `192.88.99.0/24` (6to4 relay), `192.168.0.0/16`, `198.18.0.0/15`, `198.51.100.0/24`, `203.0.113.0/24`, `224.0.0.0/4`, `240.0.0.0/4`, `255.255.255.255/32`.
- IPv6 (`blockedIPv6`): `::1/128`, `::/128`, `::ffff:0:0/96` (IPv4-mapped), `64:ff9b::/96` (NAT64), `100::/64` (discard), `2001::/32` (Teredo), `2001:db8::/32`, `2002::/16` (6to4), `fc00::/7` (ULA), `fe80::/10`, `fec0::/10`, `ff00::/8`, `fd00:ec2::254/128` (AWS IPv6 metadata).

**Family-split matching** (`isBlocked`): an address that `To4()`s (including IPv4-mapped IPv6) is checked against the IPv4 list ONLY; native IPv6 against the IPv6 list ONLY. This split is load-bearing — it prevents `::ffff:0:0/96` from cross-matching as `0.0.0.0/0` and blocking all of IPv4, and closes the v4-mapped-address bypass.

**DNS-pinned dialing** (`DialContext(ctx, network, addr)`): split host:port; if host is already an IP literal, validate and dial it directly. Otherwise resolve via the bulk resolver (`LookupA` for `tcp4/udp4`, `LookupAAAA` for `tcp6/udp6`, both for unspecified), then for each resolved IP: skip if blocked, else dial the **literal IP** with the inner `net.Dialer{Timeout: 10s, KeepAlive: 30s}`. Resolve once, validate, dial the literal — the classic rebinding defense. Returns the first successful connection or the last error.

**`ValidateIP(ip)`** is the exported single-IP validation used by checks that pre-resolve and then dial the inner dialer directly with a pinned `ip:port` (the HTTP/TLS/SMTP/parity/latency/resource checks all do this — they never re-resolve inside the HTTP transport).

`SafeDialer` keeps its concrete bulk `*Resolver` unchanged for ALL DNS-pinned dialing: `conn`-related checks, `tls`, `parity`, `resources` re-resolve via the **bulk** resolver when dialing. Consensus answers gate classification only; they are **never** used as dial targets (design §2.8 B).

## 9. The consensus resolver seam (`seam.go`)

New file in `internal/checker`, defining the seam that `internal/consensus` implements (implementation, quorum rules, per-resolver reduction, and the conditional A lookup are specified in 02-observation-model.md — this package only defines the types and consumes them in `dns_aaaa_base`/`dns_aaaa_www`):

```go
package checker

// AAAAAnswer is the result of a (possibly quorum'd) AAAA resolution.
type AAAAAnswer struct {
    IPs        []net.IP
    CNAMEChain []string    // full chase, feeds cname_chain + CDN detection
    TTL        int         // min TTL of the returned answer set
    Rcode      string      // "NOERROR", "NXDOMAIN", ...
    Quorum     *QuorumInfo // nil when not quorum-resolved
    AOutcome   string      // "a_present" | "a_absent" | "a_error"; set only when the
                           // AAAA quorum result was NOERROR-empty — the conditional
                           // bulk-resolver A lookup (02-observation-model.md). Empty otherwise.
}

// QuorumInfo records the per-resolver breakdown of a consensus lookup.
// This type is single-sourced with 02-observation-model.md §2.1; keep them identical.
// `Rcodes` is required by 03's dead-signal computation (see 02-observation-model.md — the seam).
type QuorumInfo struct {
    PerResolver map[string]string `json:"per_resolver"` // "cloudflare"|"google"|"quad9" → per-resolver symbol:
                                                        // "exists"|"empty"|"nxdomain"|"timeout"|"error"
                                                        // (timeout/error both reduce to the quorum symbol `error`;
                                                        // kept split here for diagnostics)
    Rcodes      map[string]string `json:"rcodes"`       // same keys → raw rcode string ("NOERROR", "SERVFAIL",
                                                        // "REFUSED", ...); "" when no DNS response was received
                                                        // (transport error / timeout)
    Agreement   string            `json:"agreement"`    // "3of3", "2of3", "2of2"
    Disagreed   bool              `json:"disagreed"`    // true when an answering resolver's reduced symbol
                                                        // differed from the quorum symbol
}

// ErrQuorumInconsistent is returned when no quorum is reached.
var ErrQuorumInconsistent = errors.New("resolver quorum inconsistent")

// AAAAResolver is the seam consumed by dns_aaaa_base and dns_aaaa_www.
type AAAAResolver interface {
    LookupAAAA(ctx context.Context, name string) (AAAAAnswer, error)
}
```

Contract highlights the two consuming checks rely on (normatively restated from the design; the implementation lives in 02-observation-model.md): the returned answer is the ENTIRE answer (IPs, CNAME chain, min TTL, rcode) of the first resolver in fixed order Cloudflare → Google → Quad9 whose reduced symbol equals the quorum symbol — record sets are never merged across resolvers; `AAAAAnswer.IPs` contains only globally routable addresses (the `IsGloballyRoutableIPv6` filter of §2 is applied during per-resolver reduction, so `len(IPs) > 0` ⟺ symbol `exists`); no quorum → `AAAAAnswer{Quorum: &qi}, ErrQuorumInconsistent`; nothing on this path is ever cached.

## 10. The runner (`runner.go`)

### 10.1 Construction and wiring

**Decision:** the constructor signature (the v6audit one takes only `(dialer, logger)`; the seam split and config caps force a change):

```go
// Config carries the engine's few runtime-configurable knobs.
type Config struct {
    MaxNSLookups            int  // checks.max_ns_lookups, default 4
    MaxMXLookups            int  // checks.max_mx_lookups, default 5
    EnableResourceDiscovery bool // crawler.resources.enabled, default false (registry: 09-ops.md; write path: 02-observation-model.md)
}

// NewRunner creates a runner with all standard checks registered.
func NewRunner(cfg Config, aaaa AAAAResolver, dialer *SafeDialer, logger *slog.Logger) *Runner
```

`NewRunner` registers, in this order: `NewDNSAAAABase(aaaa)`, `NewDNSAAAAWWW(aaaa)`, `NewDNSNSIPv6(dialer, cfg.MaxNSLookups)`, `NewDNSMXIPv6(dialer, cfg.MaxMXLookups)`, `NewDNSSEC(dialer)`, `NewHTTPIPv6(dialer)`, `NewHTTPSIPv6(dialer)`, `NewTLSIPv6(dialer)`, `NewResponseParity(dialer)`, `NewResourceDiscovery(dialer)` — registered ONLY when `cfg.EnableResourceDiscovery` is true (while false, the crawler writes `resources = not_applicable` without ever invoking discovery; the mapper's `not_applicable` emission is 02-observation-model.md's, the commit-loop exclusion is 03-state-machine.md's, and the `crawler.resources.enabled` key registry is 09-ops.md's), `NewSMTPIPv6(dialer)`, `NewSPFIPv6(dialer)`, `NewDNSPTRIPv6(dialer)`, `NewLatencyIPv4(dialer)`, `NewLatencyIPv6(dialer)`. `Register(c Checker)` stays public.

The two AAAA checks take the seam, not the dialer — they only resolve, never dial, so their `SafeDialer` dependency is dropped entirely (design §2.8 B).

Engine-internal concurrency constants (compile-time, not config — distinct from the crawler-level WORKER_SLOTS constant in 00-overview.md, which counts concurrent *domains* per process):

```go
const (
    domainTimeout    = 90 * time.Second // whole-scan budget per domain
    concurrencyLimit = 6                // concurrent checks within one domain
)
```

### 10.2 `Run` — two-phase conditional execution

```go
func (r *Runner) Run(ctx context.Context, host string, kind Kind) ScanResult
```

Numbered algorithm (lifted structure; deltas marked):

1. `start := time.Now()`; derive `domainCtx` with `domainTimeout`; `results := &sync.Map{}`.
2. **Subdomain www skip (new):** if `kind == KindSubdomain`, store for `dns_aaaa_www`:
   `Result{Status: StatusNotApplicable, Details: map[string]any{"reason": "subdomain entity: www check not applicable"}}` and exclude the check from phase 1. **Decision:** exact reason string as quoted (design mandates the forced `not_applicable`; the string is new).
3. **Phase 1** — run concurrently (bounded errgroup, `concurrencyLimit`), via `runPhase`:
   `dns_aaaa_base`, `dns_aaaa_www` (unless skipped in step 2), `dns_ns_ipv6`, `dns_mx_ipv6`, `dns_dnssec`, `spf_ipv6`.
   **Delta (design §2.8 C):** `latency_ipv4` is REMOVED from the v6audit `phase1Names` set — it exists only as a v4-vs-v6 comparison, and running it in phase 1 would fire up to 3 real HTTPS GETs against ~750k v4-only sites daily (~2.5M fetches), contradicting "most domains cost only DNS".
4. Read `baseResult`, `wwwResult`, `mxResult` from the map (a missing entry reads as `Result{Status: StatusError}` via `getResult`).
5. Compute the phase-2 gate: `hasAAAA := baseResult.Status == StatusSupported || wwwResult.Status == StatusSupported`. (For subdomains, the stored `not_applicable` www result makes the gate depend on `base` alone — by construction, no special case.)
6. Partition phase-2 checkers; for each, first match wins:
   - `http_ipv6`, `https_ipv6`, `tls_ipv6`, `latency_ipv6`, `resource_discovery`, **and `latency_ipv4` (moved here)**: skip with `reasonNoAAAARecord` unless `hasAAAA`.
   - `dns_ptr_ipv6`: skip with `reasonNoAAAARecord` unless `baseResult.Status == StatusSupported` (PTR needs apex IPs specifically).
   - `http_response_parity`: skip with `reasonNoAAAARecord` unless `hasAAAA` (the A-record requirement is checked inside the check itself).
   - `smtp_ipv6`: skip with `reasonNoMXWithAAAA` unless `mxResult.Status` is `StatusSupported` or `StatusPartial`.
   - default: run.
7. Store each skipped check as `Result{Status: StatusNotApplicable, Details: map[string]any{"reason": <reason>}}` (zero latency), then run the surviving phase-2 checks via `runPhase`.
8. Collect the map into `ScanResult{Domain: host, Results: …, ScannedAt: start, Duration: time.Since(start)}`. **Delta:** the `ComputeScore` call and `Score`/`Grade` fields are gone.

Both `http_ipv6` and `https_ipv6` stay registered as independent phase-2 siblings and both run unconditionally whenever phase 2 runs — the engine has NO `conn` combiner; the `conn` composition is worker-side new code applied after `Runner.Run` (see 02-observation-model.md — conn composition).

### 10.3 `runPhase` / `runCheck` — panic recovery and error normalization

`runPhase(ctx, host, kind, checkers, results)`: `errgroup.WithContext` with `SetLimit(concurrencyLimit)`; one goroutine per check calling `runCheck`; `g.Wait()` error ignored (checks never return group errors).

`runCheck` (lifted verbatim, plus the kind pass-through):

- **Panic recovery:** a deferred `recover()` logs `check panicked` (fields: `domain`, `check`, `panic`) and stores `Result{Status: StatusError, Details: {"error": "internal error: <panic value>"}, Latency: since start}`. A panicking check NEVER kills the scan or the process.
- **Cancelled context:** if `ctx.Err() != nil` before the check runs, store `Result{Status: StatusError, Details: {"error": "scan cancelled"}}`.
- **Returned error:** logged (`check failed`) and stored as `Result{Status: StatusError, Details: {"error": err.Error()}, Latency: since start}`. (In practice every lifted check returns `(Result, nil)` and encodes failures in `Result.Status`; the error path is the safety net.)
- Success: debug-log and store.

## 11. The 15 checks

Summary table (timeouts are per-check `context.WithTimeout` inside `Check`; "bulk" = `SafeDialer.Resolver()`; "seam" = `AAAAResolver`):

| # | Name | Phase | Gate | Timeout | Resolver | Dials | Possible statuses |
|---|---|---|---|---|---|---|---|
| 1 | `dns_aaaa_base` | 1 | always | 15s | seam | never | supported/unsupported/not_applicable/error |
| 2 | `dns_aaaa_www` | 1 | always (forced n/a for subdomains) | 15s | seam | never | supported/unsupported/not_applicable/error |
| 3 | `dns_ns_ipv6` | 1 | always | 25s | bulk | never | supported/partial/unsupported/error |
| 4 | `dns_mx_ipv6` | 1 | always | 30s | bulk | never | supported/partial/unsupported/not_applicable/error |
| 5 | `dns_dnssec` | 1 | always | 20s | bulk | never | supported/unsupported/error |
| 6 | `spf_ipv6` | 1 | always | 5s | bulk | never | supported/unsupported/not_applicable/error |
| 7 | `http_ipv6` | 2 | hasAAAA | 10s | bulk | tcp6:80 | supported/unsupported/error/not_applicable |
| 8 | `https_ipv6` | 2 | hasAAAA | 10s | bulk | tcp6:443 | supported/unsupported/error/not_applicable |
| 9 | `tls_ipv6` | 2 | hasAAAA | 10s | bulk | tcp6:443 | supported/unsupported/error/not_applicable |
| 10 | `http_response_parity` | 2 | hasAAAA (+A inside) | 20s | bulk | tcp4:443 + tcp6:443 | supported/partial/unsupported/not_applicable/error |
| 11 | `resource_discovery` | 2 | hasAAAA (and `crawler.resources.enabled`) | 15s | bulk | tcp6:443 | supported("ok")/not_applicable/error |
| 12 | `smtp_ipv6` | 2 | mx ∈ {supported, partial} | 15s | bulk | tcp6:25 | supported/partial/unsupported/not_applicable/error |
| 13 | `dns_ptr_ipv6` | 2 | base = supported | 30s | bulk | never | supported/partial/unsupported/not_applicable/error |
| 14 | `latency_ipv4` | 2 (**moved**) | hasAAAA | 30s (10s/probe) | bulk | tcp4:443 | supported/not_applicable/error |
| 15 | `latency_ipv6` | 2 | hasAAAA | 30s (10s/probe) | bulk | tcp6:443 | supported/not_applicable/error |

The engine-status → per-dimension observation mapping (including which `partial`s survive to storage, the `no_record` derivation, and every table in design §2.2/§2.3.1) is 02-observation-model.md's; nothing below assigns observations.

### 11.1 `dns_aaaa_base` — apex AAAA (adapted)

Source: `v6audit/internal/checker/dns_aaaa_base.go`. Constructor becomes `NewDNSAAAABase(res AAAAResolver)`. Target name: the entity host itself (`domain` param) — for `kind=subdomain` that is the subdomain. **Decision:** the per-check timeout rises from v6audit's 5s to **15s**: the quorum fan-out worst case (2s per resolver + one retry ≈ 4s) plus the conditional bulk A lookup (5s + one retry ≈ 10s) can legitimately exceed 5s; 15s covers the worst case without slack-hunting. Body:

```go
ans, err := c.res.LookupAAAA(ctx, host)
details["rcode"] = ans.Rcode
if len(ans.CNAMEChain) > 0 { details["cname_chain"] = ans.CNAMEChain }
if ans.Quorum != nil { details["quorum"] = ans.Quorum }        // the §2.4 "disagreement annotation"
if ans.AOutcome != "" { details["a_outcome"] = ans.AOutcome }  // conditional A lookup result
if errors.Is(err, ErrQuorumInconsistent) {
    details["inconsistent"] = true
    return Result{Status: StatusError, Details: details, Latency: time.Since(start)}, nil
}
if err != nil {
    details["error"] = err.Error()
    return Result{Status: StatusError, Details: details, Latency: time.Since(start)}, nil
}
// NXDOMAIN: raw engine status stays not_applicable with the raw rcode preserved —
// the observation layer (02-observation-model.md) maps base NXDOMAIN to no_record; keeping
// the rcode in details lets dead-detection require NXDOMAIN specifically.
if ans.Rcode == "NXDOMAIN" {
    details["reason"] = "domain does not exist"
    return Result{Status: StatusNotApplicable, Details: details, Latency: time.Since(start)}, nil
}
if len(ans.IPs) == 0 {
    return Result{Status: StatusUnsupported, Details: details, Latency: time.Since(start)}, nil
}
details["addresses"] = <ans.IPs as strings>
details["ttl"] = ans.TTL
return Result{Status: StatusSupported, Details: details, Latency: time.Since(start)}, nil
```

`ans.IPs` arrives pre-filtered to globally routable addresses (§9), which is how the ported production filter reaches this check. The engine keeps v6audit's raw 5-valued statuses; `inconsistent` exists ONLY at the observation layer — the mapper rule `Status == error AND Details["inconsistent"] == true → observation inconsistent` lives in 02-observation-model.md. **Decision:** `AOutcome` is persisted under details key `"a_outcome"` (the design defines the field but not a payload key; the mapper and detail page need it in the stored scan).

### 11.2 `dns_aaaa_www` — www AAAA (adapted)

Source: `v6audit/internal/checker/dns_aaaa_www.go`. Constructor `NewDNSAAAAWWW(res AAAAResolver)`; timeout 15s (same Decision as §11.1). Queries `"www." + host`. Identical seam adaptation as §11.1, plus the lifted CDN detection kept verbatim: when a CNAME chain exists, set `details["cname_target"]` to the last element and `details["cdn_detected"] = true` if any chain element has one of the known CDN suffixes (after trimming the trailing dot): `cloudfront.net`, `cloudflare.net`, `akamaiedge.net`, `akamai.net`, `fastly.net`, `edgekey.net`, `azureedge.net`, `cdn.cloudflarenet.com`, `edgecastcdn.net`, `stackpathdns.com`, `googleapis.com`. NXDOMAIN branch keeps reason string `"www subdomain does not exist"`. For `kind=subdomain` this check never executes (runner step 2).

### 11.3 `dns_ns_ipv6` — nameserver IPv6 (adapted: config cap)

Source: `v6audit/internal/checker/dns_ns_ipv6.go`. Timeout 25s. Constructor gains the cap: `NewDNSNSIPv6(dialer *SafeDialer, maxLookups int)` (config `checks.max_ns_lookups`, default 4, replacing the `maxNSLookups` constant; a value ≤0 is a config error — see registry, 09-ops.md).

Behavior (lifted; the label walk-up is the fix for production's `co.uk` bug and needs no PSL):

1. **Label walk-up zone discovery:** query NS for the host; while the answer errors or is empty, strip one leading label (`blog.example.com` → `example.com`) and retry; stop at the TLD/bare-name boundary. For subdomains this automatically climbs to the authoritative zone — no kind-awareness needed. If the walk found a zone above the input, record `details["zone"]`.
2. No NS found anywhere → `StatusError` with `details["error"]` (`"no NS records found"` when the last answer was empty). The engine never emits `not_applicable` here; a walk-up finding no zone is `error` by design.
3. Sort nameservers alphabetically; AAAA-check the first `maxLookups` via the bulk resolver. Per-host detail map `details["nameservers"][<ns>] = {"has_ipv6": bool, "addresses": []string}`.
4. Counters: `details["total"]` (all NS found), `details["checked"]` (≤ cap), `details["ipv6_count"]` — these let the detail page render "checked 4 of 7". The scan payload contains per-host results for at most `max_ns_lookups` hosts, not all hosts.
5. Status: all checked have AAAA → `supported`; some → `partial`; none → `unsupported`. (The `partial → supported` ≥1-host public rule is the observation layer's, 02-observation-model.md.)

### 11.4 `dns_mx_ipv6` — mail IPv6 (adapted: config cap + kind-aware implicit MX)

Source: `v6audit/internal/checker/dns_mx_ipv6.go`. Timeout 30s. Constructor `NewDNSMXIPv6(dialer *SafeDialer, maxLookups int)` (config `checks.max_mx_lookups`, default 5).

1. `LookupMX(host)`; transport error → `StatusError`.
2. **No MX records:**
   - `kind == KindApex`: RFC 5321 §5.1 implicit-MX fallback — AAAA-check the host itself; has AAAA → `supported` with `details["reason"] = "implicit MX fallback (RFC 5321 §5.1)"` and the addresses; else → `not_applicable` with reason `"no MX records and no implicit AAAA fallback"`.
   - `kind == KindSubdomain`: **skip the implicit-MX fallback entirely** → `not_applicable`. **Decision:** reason string `"no explicit MX records (subdomain entity)"`. Rationale (design §2.2): "the AAAA accepts mail" is not evidence for a subdomain entity; explicit MX → evaluate normally.
3. **Null MX (RFC 7505):** exactly one record with `Mx == "."` and `Preference == 0` → `not_applicable`, reason `"null MX record"`.
4. Sort by preference ascending; cap to `maxLookups`; AAAA-check each MX host (skip hosts that are IP literals — recorded with `has_ipv6: false`, never resolved). Per-host detail `details["mx_records"][<host>] = {"preference": n, "has_ipv6": bool, "addresses": []string}`; counters `details["total"]` (capped count, as lifted) and `details["ipv6_count"]`.
5. Status: all → `supported`; some → `partial`; none → `unsupported`.

### 11.5 `dns_dnssec` — resolver-validated DNSSEC (verbatim)

Source: `v6audit/internal/checker/dns_dnssec.go`. Timeout 20s. Informational. (1) DS query for the FQDN with EDNS0 DO=true, buffer 4096: no DS → `unsupported` (`details["signed"] = false`); DS lookup error → `error`. (2) With DS present: query SOA (then A as fallback) with RD=1, CD=0, DO=false and read the AD flag from the validating upstream — AD=1 → `supported`; a definitive answer with AD=0 → `error` with `details["error"] = "DNSSEC signed but validation failed (AD=0)"` (broken DNSSEC is an error state, not "unsupported"); no queryable answer → `error`. Details: `signed`, `ds_records` (key_tag/algorithm/digest_type), `chain_complete`, `ad_flag`.

### 11.6 `http_ipv6` — HTTP over IPv6 (adapted: enumerated deviation 3)

Source: `v6audit/internal/checker/http_ipv6.go`. Timeout 10s. Behavior:

1. Bulk `LookupAAAA(host)`; error or no AAAA → `unsupported` with reason `errNoAAAARecord` (unreachable in practice — phase-2 gating already skipped the check; retained as the lifted safety branch).
2. Try up to `min(len(ips), 3)` addresses in answer order. Per attempt: `ValidateIP` (blocked → `StatusError` with `details["error"] = errAddrBlocked`, immediately); GET `http://<host>/` with the transport dial pinned to `[ip]:80` over `tcp6`, `DisableKeepAlives: true`, redirects NOT followed (`CheckRedirect` returns `http.ErrUseLastResponse` — any HTTP response, including 3xx/5xx, is success for reachability), `User-Agent: WhyNoIPv6Bot/1.0 (+https://whynoipv6.com/bot)`, body never read. Success details: `address`, `status_code`, `response_time_ms`, `server` (when present).
3. All attempts failed — **the deviation**: classify the last error exactly as `https_ipv6` does:
   - connection refused (ECONNREFUSED via `net.OpError`/`syscall.Errno` unwrap) → `Status: unsupported`, `details["error"] = errConnRefused`, **`details["error_type"] = "connection_refused"`** (new — v6audit's http check set no `error_type` on this branch);
   - timeout (`net.Error.Timeout()` or `context.DeadlineExceeded`) → `Status: error`, `details["error_type"] = "timeout"` (new);
   - everything else → `Status: error`, `details["error_type"] = "unknown"` (new).

   No `certificate_error` branch exists here (no TLS on port 80). With this, the `conn` composition table (02-observation-model.md) applies identically on the http-only fallback path.

### 11.7 `https_ipv6` — HTTPS over IPv6 (verbatim)

Source: `v6audit/internal/checker/https_ipv6.go`. Timeout 10s. This is the headline pure-tcp6 reachability check. Same structure as §11.6 with port 443 and `TLSClientConfig: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}` (system roots; certificate verification ON — never `InsecureSkipVerify`). Redirects not followed; body never read; up to 3 IPs attempted. Success details additionally include `tls_version`. Terminal classification of the last error (lifted; this is the source of truth the http check copies):

| Condition | Status | `details.error_type` |
|---|---|---|
| connection refused | `unsupported` | `"connection_refused"` |
| timeout | `error` | `"timeout"` |
| TLS error (`tls.CertificateVerificationError`, `tls.RecordHeaderError`, or `net.OpError` whose inner message starts `"tls:"`) | `unsupported` | `"certificate_error"` |
| anything else | `error` | `"unknown"` |

The `error_type` values are a wire contract: the `conn` composition and the detail page key off these exact strings (02-observation-model.md).

### 11.8 `http_response_parity` — v4-vs-v6 fetch comparison (verbatim)

Source: `v6audit/internal/checker/response_parity.go`. Timeout 20s. Informational; stored raw including `partial`. (1) `LookupA` — none → `not_applicable` ("no A record"); `LookupAAAA` — none → `not_applicable`. (2) `ValidateIP` both first addresses (blocked → `error`). (3) Fetch `https://<host>/` twice, pinned to `[v4IP]:443` over `tcp4` and `[v6IP]:443` over `tcp6`: up to `maxRedirects = 3` redirects followed but ONLY to the same host (off-host or >3 → last response), body read up to `maxBodySize = 1 MiB` for length measurement, TLS config as §11.7. v4 fetch fails → `not_applicable` (no baseline); v6 fetch fails → `unsupported`. (4) Compare: status codes equal? content type equal (base media type, params stripped, case-insensitive)? body length diff ≤ `parityTolerance = 10%` (of v4 length)? Both-3xx short-circuits to `supported` regardless of body diff. Status: status mismatch → `unsupported`; content-type mismatch → `partial`; diff > 10% → `partial`; else `supported`. Details: per-family `{address, status_code, content_type, content_length, response_time_ms}` under `ipv4`/`ipv6`, plus `status_match`, `content_type_match`, `content_length_diff_pct` (one decimal).

### 11.9 `resource_discovery` — external-host discovery (adapted from `resource_ipv6.go`)

Source: `v6audit/internal/checker/resource_ipv6.go`, renamed file and check. Timeout 15s. Discovery-only: it finds which external hosts the page references; the hosts' AAAA status lives in the `resource_host` registry and the `resources` observation comes from the registry roll-up (see 02-observation-model.md; tables in 05-schema.md; registry sweep in 06-ingest.md — resource-host registry).

**Keep (lifted verbatim):**

- IPv6-pinned page fetch: bulk `LookupAAAA(host)` → first IP, `ValidateIP`, GET `https://<host>/` with transport pinned to `[ip]:443` over `tcp6`, TLS config as §11.7, UA per §3, up to 3 redirects followed then last response (all redirect requests re-dial the same pinned address — lifted behavior, kept), body read capped at `resourceMaxBodySize = 2 MiB`.
- The streaming HTML tokenizer (`golang.org/x/net/html`): `src` of `script`/`img`/`iframe`/`source`/`video`/`audio`/`object`/`embed`, `href` of `link`, `<base href>` rebasing (first absolute base wins the running base), `data:`/`javascript:` URIs skipped, references resolved against the base, hostnames lowercased.
- External-host filter: skip the page's own host and its subdomains (`host == domain || strings.HasSuffix(host, "."+domain)`).
- Dedup (first-seen order preserved) and the `resourceMaxHosts = 50` cap.

**Delete** (v6audit `resource_ipv6.go` lines 88–147): the inline concurrent AAAA checks over the discovered hosts, the `ipv6_hosts`/`ipv4_only_hosts` tally, the 20-item `resourceMaxList` truncation, and the supported/partial/unsupported derivation.

**New result contract.** The check returns the FULL deduped host list (≤50, no truncation) and a discovery status:

| Outcome | Result.Status | Details |
|---|---|---|
| fetch succeeded (any HTTP response) | `StatusSupported` | `"hosts": []string` (may be empty), `"total_hosts": int` |
| no AAAA on the entity host (engine-internal re-check of the phase-2 gate) | `StatusNotApplicable` | `"reason": errNoAAAARecord` |
| blocked address / fetch / TLS failure | `StatusError` | `"error": "<message>"` |

**Decision:** the design's discovery status `ok` is carried as `StatusSupported` (the 5-valued `CheckStatus` has no `ok`; `supported` here means "discovery succeeded", and nothing maps this check's status to a public dimension — the crawler consumes `Details["hosts"]`, and the roll-up alone produces the `resources` observation). **Decision:** details key for the list is `"hosts"` (design pins `{total_hosts}`; the list key is new with the truncation gone). An empty list is a valid `supported` outcome (v6audit returned `not_applicable` for "no external resources"; the adapted check does not — an empty dependency set is information, and the roll-up defines its own empty-set branch).

### 11.10 `smtp_ipv6` — SMTP over IPv6 (verbatim + identity string)

Source: `v6audit/internal/checker/smtp_ipv6.go`. Timeout 15s. Informational. `LookupMX` (error → `error`; none → `not_applicable` — note: NO implicit-MX fallback here even for apexes, lifted as-is; the runner's gate means this branch is rarely reached). Sort by preference; try up to `maxSMTPAttempts = 3` MX hosts: AAAA-resolve the MX host (none → next), `ValidateIP`, dial `[ip]:25` over `tcp6`, read the banner line — not `220…` → `unsupported` ("unexpected banner"); send `EHLO whynoipv6.com\r\n` (deviation 1) — write failure → `partial`; read the multi-line EHLO response (≤100 lines, terminated by `250<space>`); record `details`: `mx_host`, `mx_preference`, `address`, `banner`, `ehlo_response`, `starttls_offered`. Success → `supported`. All attempts failed: connection-refused → `unsupported` with `errConnRefused`; otherwise `unsupported` with the last error message. (`partial → unsupported` before storage is the observation layer's rule.)

### 11.11 `spf_ipv6` — SPF IPv6 mechanics (verbatim)

Source: `v6audit/internal/checker/spf_ipv6.go`. Timeout 5s. Informational. `LookupTXT`; select records matching `v=spf1` exactly or `v=spf1 ` prefix (case-insensitive) — none → `not_applicable`; multiple → `error` (RFC 7208). Parse mechanisms with qualifiers (`+`/`-`/`~`/`?`, default `+`): pass-qualified `ip6:` → direct support; `-ip6:`/`~ip6:` → explicit reject; `include:` recursed for pass-qualified `ip6:` (DNS lookup budget `maxSPFLookups = 10`, exceeded → `error "too many DNS lookups"`); `a`/`a:`/`a/` and `mx`/`mx:`/`mx/` mechanisms AAAA-resolved for implicit support; `redirect=` evaluated only when no mechanism matched (RFC 7208 §6.1), followed permissively inside includes. Status: explicit reject without any pass path → `unsupported` (reason `"SPF explicitly rejects IPv6"`); direct or include pass → `supported`; implicit → `supported` with `details["implicit"] = true`; else `unsupported`. Details: `spf_record`, `has_ip6_mechanism`, `ip6_mechanisms`, `include_has_ip6`, `include_chain`, `lookup_count`.

### 11.12 `dns_ptr_ipv6` — PTR + FCrDNS (verbatim)

Source: `v6audit/internal/checker/dns_ptr_ipv6.go`. Timeout 30s. Informational; stored raw including `partial`. AAAA-resolve the host (gate should guarantee this); take up to `maxPTRAddresses = 3` addresses; for each: build the nibble-reversed `ip6.arpa.` name, `LookupPTR`; if a PTR exists, forward-confirm by AAAA-resolving the PTR name and checking the original address appears (FCrDNS). Status: every address has a forward-confirmed PTR → `supported`; any PTR at all otherwise → `partial`; no PTRs → `unsupported`. Details: `checks` (list of `{address, ptr_name, forward_confirmed}`), `all_confirmed`.

### 11.13 `dns_dnssec` note on transport

(Behavior in §11.5.) The DS query sets the DO bit; the AD probe explicitly does NOT set DO and relies on the validating upstream — both must survive the v2 port unchanged; the fake-DNS tests in 10-testing.md assert the flag bits on the wire.

### 11.14 `latency_ipv4` / `latency_ipv6` — TTFB comparison (verbatim; phase move only)

Source: `v6audit/internal/checker/latency.go` (one file registers both checks — this is why the 15-check enumeration counts latency twice). Check timeout 30s; per-probe timeout `latencyTimeout = 10s`. Informational. `latency_ipv4`: `LookupA` (none → `not_applicable`); `latency_ipv6`: `LookupAAAA` (none → `not_applicable`). First address, `ValidateIP`, then `latencyMeasurements = 3` sequential HTTPS GETs to `https://<host>/` pinned to `[ip]:443` (`tcp4`/`tcp6`), TTFB measured as time to `client.Do` return, body unread, redirects not followed. First probe failing with no prior success → `error`; later failures skipped. Aggregate: sort, discard the highest, average the rest (single measurement → itself). Details: `address`, `ttfb_ms`, `avg_ms`, `measurements`. Both checks are phase-2, gated on `hasAAAA` (the v4 check moved there per design §2.8 C — its per-check "no A → not_applicable" branch still handles AAAA-only hosts).

## 12. The IPv6 self-preflight (`preflight.go`)

Source: `v6audit/cmd/v6agent/main.go` lines 356–380 (`checkIPv6Connectivity`) — v6audit only ran this in the remote agent; the gap for internal workers is deliberately closed here, since a v6-dark crawler mass-producing false `unsupported` is the #1 false-negative source.

**Decision:** the lifted function is wrapped in a small stateful type (the design mandates the probe, the 5-minute freshness window, and the claim-cycle placement, but no Go shape):

```go
// PreflightFreshness is the window within which a passed preflight makes
// conn=unsupported observations definitive (02-observation-model.md, conn rows 5a/5b).
const PreflightFreshness = 5 * time.Minute

// Preflight verifies and tracks this process's IPv6 connectivity.
type Preflight struct {
    res       *Resolver     // the bulk resolver
    probeHost string        // config preflight.probe_host, "host:port"
    logger    *slog.Logger
    lastPass  atomic.Int64  // unix nanos of the last successful probe; 0 = never
}

func NewPreflight(res *Resolver, probeHost string, logger *slog.Logger) *Preflight

// Run performs one probe: AAAA-resolve the host part of probeHost via the bulk
// resolver, then tcp6-dial the first address on the port part with a 5s dialer
// timeout (net.Dialer directly — the probe target is public and fixed; SSRF
// validation is unnecessary and the SafeDialer would resolve a second time).
// On success it records time.Now() in lastPass and returns true; on any failure
// (lookup error, zero AAAA records, dial error) it logs at Error level with the
// lifted messages ("ipv6 preflight: ...") and returns false without touching
// lastPass.
func (p *Preflight) Run(ctx context.Context) bool

// PassedWithin reports whether the last successful probe is younger than d.
func (p *Preflight) PassedWithin(d time.Duration) bool
```

Contract (consumers wire it, this package defines it):

- The crawler runs `Run` before **every** claim cycle; on failure it claims nothing, alerts the ops webhook, pings the healthcheck `/fail` endpoint, and retries in 60s — that loop, the alert plumbing, and the claim integration are 04-lifecycle-scheduling.md's (heartbeat details: 09-ops.md).
- Every `conn = unsupported` observation — whether from connection-refused, TLS failure, or timeout — additionally requires `PassedWithin(PreflightFreshness)`; otherwise the observation is downgraded to `error`. The composition table applying this is 02-observation-model.md's; the engine's job is only to expose an accurate clock.
- `probeHost` comes from config `preflight.probe_host`, default `one.one.one.one:443` (§13). The host part is AAAA-resolved; the port part is dialed. A probe host with no port is a config error.

## 13. Config keys introduced by this file

Names, types, and defaults below are normative; the consolidated registry (env-var forms, precedence, validation) lives ONLY in 09-ops.md.

| Key | Type | Default | Used by |
|---|---|---|---|
| `checks.max_ns_lookups` | int | `4` | `dns_ns_ipv6` per-host AAAA detail cap (§11.3) |
| `checks.max_mx_lookups` | int | `5` | `dns_mx_ipv6` per-host AAAA detail cap (§11.4) |
| `resolver.bulk_upstreams` | []string ("host:port") | `["127.0.0.1:53", "127.0.0.1:5353"]` | bulk `Resolver` upstreams — the two local Unbound instances (§6) |
| `preflight.probe_host` | string ("host:port") | `"one.one.one.one:443"` | IPv6 self-preflight (§12) |

**Decision:** key names `resolver.bulk_upstreams` and `preflight.probe_host`, and the default Unbound listen addresses `127.0.0.1:53` / `127.0.0.1:5353` — the design pins the two-instance topology and says "their listen addresses are crawler config, not baked in" (design D.1) without naming the key or ports; two loopback listeners on distinct ports is the simplest layout consistent with that, and 09-ops.md's Unbound units must configure these same addresses (single source for the *values* is the config file, not either spec).

Everything else in the engine is a compile-time constant, deliberately not config: `domainTimeout` 90s, `concurrencyLimit` 6, `dnsTimeout` 5s, `maxCNAMEHops` 10, `defaultUDPSize` 4096, per-check timeouts (§11 table), `maxSMTPAttempts` 3, `maxPTRAddresses` 3, `maxSPFLookups` 10, `latencyMeasurements` 3, `latencyTimeout` 10s, `maxRedirects` 3, `maxBodySize` 1 MiB, `parityTolerance` 0.10, `resourceMaxBodySize` 2 MiB, `resourceMaxHosts` 50, `expirySoonDays` 30, `PreflightFreshness` 5m, the SSRF blocklists, and the identity strings (§3).

## 14. Acceptance criteria

Behavioral gates for this file's deliverables (test fixtures and the fake-DNS harness live in 10-testing.md):

1. `go vet`/build passes with `codeberg.org/miekg/dns` pinned at an exact v0.6.x and NO import of `github.com/miekg/dns` or `github.com/miekg/dnsv2` anywhere in the module.
2. Grep gates: no identifier `Score`, `Grade`, `ComputeScore`, `Category`, `CheckerToDBColumn`, `DBColumnToChecker`, `cacheEntry`, or `dnsCacheKey` in `internal/checker`; the string `v6audit` appears nowhere in shipped code.
3. `Runner.Run` on a host with no AAAA anywhere executes network I/O for phase-1 checks only; all eight phase-2 checks (nine when `crawler.resources.enabled` is true — see §10.1: `resource_discovery` is registered only then) appear in `Results` as `not_applicable` with the exact skip-reason strings of §5, and `latency_ipv4` is among them.
4. `Runner.Run(ctx, host, KindSubdomain)` yields `dns_aaaa_www` = `not_applicable` without a DNS query, and `dns_mx_ipv6` on a no-MX subdomain yields `not_applicable` without an implicit-MX AAAA lookup.
5. A check that panics yields `Result{Status: error}` for that check while every other check's result is unaffected.
6. `https_ipv6`/`http_ipv6` produce the exact `error_type` strings of §11.6/§11.7 for refused / timeout / bad-cert / other failures (fake server fixtures: 10-testing.md).
7. `resource_discovery` against a fixture page returns the full deduped external-host list (order = first seen, ≤50) with `<base href>` honored, own-host and subdomain references excluded, and `data:`/`javascript:` URIs ignored; its `Result.Status` is `supported` even when the list is empty.
8. The bulk resolver issues zero queries to the public consensus resolvers, and `internal/checker` contains no in-process DNS cache (verified by the §6.2 deletion and grep gate 2).
9. `Preflight.PassedWithin(PreflightFreshness)` flips false exactly 5 minutes after the last successful probe with no probes in between.
