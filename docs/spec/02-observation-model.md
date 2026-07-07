# 02 — Observation Model

**Purpose:** Defines how raw engine results become per-dimension observations: the 7-value internal observation vocabulary and its usage rules, the consensus (quorum) resolver package with its rate control and breakers, the normative mapping tables from engine outcomes to the six core dimensions (`base`, `www`, `ns`, `mx`, `conn`, `resources`), and the worker-side Result→observation mapper. Everything downstream of an observation (confirm/pending state machine, changelog, classification) is out of scope here.

**Deliverables:**

- `internal/consensus` — the multi-resolver quorum wrapper implementing the `checker.AAAAResolver` seam: provider fan-out, symbol reduction, quorum, conditional A lookup, per-provider token buckets, fast-lane breaker, provider breaker + canary.
- `internal/checker` seam types consumed by `internal/consensus`: `AAAAAnswer`, `QuorumInfo`, `ErrQuorumInconsistent`, `AAAAResolver` (the types live in package `checker`; their implementation lives in `consensus`).
- `internal/crawler/observe.go` — the Result→observation mapper (`MapObservations`), including the `conn` composition function and the `resources` roll-up.
- `internal/domain/observation.go` — the Go `Observation` enum and its helper predicates (pure, zero deps).

**Companion files:** 01-engine.md (engine lift, adapted check internals, self-preflight, `resource_discovery`), 03-state-machine.md (what happens to an observation: confirm/pending commit, dead signal, classification, scan/scan_detail assembly), 04-lifecycle-scheduling.md (recheck pull-ins consuming `inconsistent`/`error` and the fast-lane breaker state), 05-schema.md (all DDL: `observation`/`ipv6_status` enums, `domain`, `scan`, `resource_host`, `domain_resource`), 09-ops.md (config-key registry), 00-overview.md (sizing constants), 10-testing.md (fixtures and acceptance tests).

---

## 1. Observation vocabulary

### 1.1 The two enums

Two SQL enum types exist (DDL in 05-schema.md — Enums; restated here for meaning only, never redefine them elsewhere):

- **`ipv6_status`** — 4-valued: `supported | unsupported | no_record | not_applicable`. This is the *public* status model. Classification, the API, badges, and all confirmed `domain.*_status` columns use only these four values.
- **`observation`** — 7-valued: `supported | partial | unsupported | no_record | not_applicable | error | inconsistent`. This is the *internal* raw-observation model. `partial`, `error`, and `inconsistent` never reach public output: classification and the API read only the confirmed 4-valued columns.

Go mirror, in `internal/domain/observation.go` (pure package, zero deps):

```go
package domain

// Observation is the 7-valued raw observation outcome (SQL enum `observation`).
type Observation string

const (
    ObsSupported     Observation = "supported"
    ObsPartial       Observation = "partial"
    ObsUnsupported   Observation = "unsupported"
    ObsNoRecord      Observation = "no_record"
    ObsNotApplicable Observation = "not_applicable"
    ObsError         Observation = "error"
    ObsInconsistent  Observation = "inconsistent"
)

// Definitive reports whether o can advance confirmed state.
// error and inconsistent are non-definitive; the empty string is "not recorded".
func (o Observation) Definitive() bool {
    switch o {
    case ObsSupported, ObsPartial, ObsUnsupported, ObsNoRecord, ObsNotApplicable:
        return true
    }
    return false
}

// IPv6Status is the 4-valued public status (SQL enum `ipv6_status`).
type IPv6Status string

const (
    StatusSupported     IPv6Status = "supported"
    StatusUnsupported   IPv6Status = "unsupported"
    StatusNoRecord      IPv6Status = "no_record"
    StatusNotApplicable IPv6Status = "not_applicable"
)
```

(Note: `checker.CheckStatus` — the lifted engine's 5-valued `supported/unsupported/partial/error/not_applicable` — is a third, distinct vocabulary. Engine statuses exist only inside `internal/checker` results; this file's mapper is the only code that converts them to `domain.Observation`.)

### 1.2 Usage rules (normative)

1. **Non-definitive values.** `error` and `inconsistent` never advance confirmed state, never write changelog rows, and never appear in public output. They are recorded in the `scan` row and in `domain.*_observed`, and they drive recheck scheduling (see 04-lifecycle-scheduling.md): `inconsistent` → 2h lane, `error` → 6h lane, for the `base`/`www` dimensions only.
2. **`inconsistent` is consensus-only.** Only the two consensus dimensions (`base`, `www`) can ever be `inconsistent` — it means the resolver quorum failed (§2.6). No other dimension may produce it.
3. **`no_record` is base-only.** Only the `base` dimension ever produces (and confirms) `no_record`. `www`, `ns`, `mx`, `conn`, and `resources` never produce `no_record` (the www composite maps the equivalent cases to `not_applicable`; see §4).
4. **`partial` storage rule.** `partial` is a legal *stored* value ONLY for the two informational dimensions whose mapping is "kept as-is": `ptr` and `parity` (columns `domain.ptr_observed`, `domain.parity_observed`, `scan.ptr`, `scan.parity`). Every other partial-capable engine check is mapped to a non-partial observation BEFORE any DB write: `ns` partial → `supported`, `mx` partial → `supported` (the ≥1-host rule: one v6-capable NS makes the zone resolvable over v6, one v6 MX host accepts mail over v6), `smtp` partial → `unsupported`. `resources` has no engine `partial` on its path — the roll-up (§6) is defined directly in 4-valued terms. The core-dimension `*_observed`/`*_pending` columns and the commit algorithm (03-state-machine.md) therefore never see `partial`; the raw engine verdict is always preserved in `scan_detail.details`.
5. **Core vs informational.** Core dimensions (`base`, `www`, `ns`, `mx`, `conn`, `resources`) pass through the confirm/pending machinery and can appear in the changelog. Informational dimensions (`dnssec`, `ptr`, `smtp`, `parity`, plus payload-only `tls`/`spf`/`latency`) store the latest observation only, with no confirmation machinery and no changelog.
6. **Every scan produces all six core observations.** The `scan` table's six core observation columns are NOT NULL (05-schema.md — Time-series hypertables); the mapper (§7) always emits a value for each, using `not_applicable` for skipped paths and `error` for defensive fallbacks.

---

## 2. The consensus resolver — `internal/consensus`

Quorum applies **only** to the two classification-critical lookups: apex AAAA and www AAAA. Everything else (NS/MX chains and host AAAA, A records, PTR, TXT/SPF, DNSSEC, resource-host sweep) goes through the **bulk resolver** — the lifted, cache-free `checker.Resolver` pointed at the two local Unbound instances (see 01-engine.md). Consensus answers gate classification only; they are **never used as dial targets** — `conn`/`tls`/`parity`/`resources` re-resolve via the bulk resolver when dialing (SafeDialer keeps its concrete bulk `*checker.Resolver` unchanged).

### 2.1 The seam (types in package `checker`)

These types live in `internal/checker` so the adapted `dns_aaaa_base.go`/`dns_aaaa_www.go` can consume them without importing `consensus`. Copy exactly:

```go
// AAAAAnswer is the result of a (possibly quorum'd) AAAA resolution.
type AAAAAnswer struct {
    IPs        []net.IP
    CNAMEChain []string    // full chase, feeds cname_chain + CDN detection
    TTL        int         // min TTL of the returned answer set
    Rcode      string      // "NOERROR", "NXDOMAIN", ...
    Quorum     *QuorumInfo // nil when not quorum-resolved
    AOutcome   string      // "a_present" | "a_absent" | "a_error"; set only when the
                           // AAAA quorum result was NOERROR-empty — the conditional
                           // bulk-resolver A lookup (§2.7). Empty otherwise.
}

// QuorumInfo records the per-resolver breakdown of a consensus lookup.
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

**Decision:** `QuorumInfo` carries one field beyond the design's listing — `Rcodes` (with JSON tags on all fields, snake_case). It is required by two already-decided consumers: the recorded per-resolver tuple *{resolver, rcode, reduced symbol, answered}* in `scan_detail.details.consensus`, and the dead-signal branch "all 3 consensus resolvers returned an explicit SERVFAIL or REFUSED rcode for apex AAAA after retry" (03-state-machine.md — dead-signal computation), which cannot be computed from symbols alone (the `error` symbol also covers NOTIMP/FORMERR/transport errors). **Decision:** `answered` is derived from `Rcodes`, NOT from the reduced symbol: `answered := QuorumInfo.Rcodes[provider] != ""` (a DNS response carrying any rcode was received; the empty string means transport error/timeout, i.e. not answered). Deriving it from the symbol would be wrong — a provider that returned an explicit SERVFAIL or REFUSED rcode reduces to symbol `error` (§2.4) yet *did* answer, and 03-state-machine.md §4 dead-signal branch (b) requires exactly those answered-with-rcode-∈-{SERVFAIL,REFUSED} providers; `answered := symbol NOT IN ('timeout','error')` would mark every SERVFAIL/REFUSED provider `answered=false` and make branch (b) permanently unsatisfiable.

### 2.2 Providers, order, and construction

Pinned constants in `internal/consensus` (not config — the three anycast networks are a locked design decision):

```go
// Fixed order. Answer selection (§2.6) and QuorumInfo keys use these names.
var providers = []provider{
    {name: "cloudflare", upstreams: []string{"1.1.1.1:53", "[2606:4700:4700::1111]:53"}},
    {name: "google",     upstreams: []string{"8.8.8.8:53", "[2001:4860:4860::8888]:53"}},
    {name: "quad9",      upstreams: []string{"9.9.9.9:53", "[2620:fe::fe]:53"}},
}
```

Each provider gets its own `checker.Resolver` instance constructed with exactly its two upstream addresses (v4 + v6). The lifted resolver's round-robin + retry-on-next behavior then means a retry within one provider rotates to the provider's other address family — deliberate. The in-process TTL cache does not exist (it is deleted in the lift, 01-engine.md); **no caching anywhere on the consensus path** — every observation must be fresh.

Package API:

```go
package consensus

// Config mirrors the consensus.* config keys (types and defaults: registry in 09-ops.md).
type Config struct {
    PerProviderQPS int              // consensus.per_provider_qps, default 15 (per process)
    FastLane       FastLaneConfig   // consensus.fastlane_breaker.*
    Provider       ProviderConfig   // consensus.provider_breaker.*
}
type FastLaneConfig struct {
    NondefinitiveRate float64       // default 0.05
    Window            time.Duration // default 15m
    MinSamples        int           // default 500
    RecoverBelow      float64       // default 0.02
}
type ProviderConfig struct {
    FailureRate    float64          // default 0.50
    Window         time.Duration    // default 15m
    MinSamples     int              // default 200
    RecoveryProbes int              // default 3
}

// New builds the consensus resolver. bulk is the shared bulk checker.Resolver
// (Unbound upstreams) used ONLY for the conditional A lookup (§2.7). alert
// posts a one-line message to the ops webhook (internal/notify).
func New(cfg Config, bulk *checker.Resolver, alert func(ctx context.Context, msg string), logger *slog.Logger) *Resolver

// Resolver implements checker.AAAAResolver.
func (r *Resolver) LookupAAAA(ctx context.Context, name string) (checker.AAAAAnswer, error)

// FastLaneSuppressed reports whether the fast-lane breaker is open (§2.9).
// Consumed by scheduling (04-lifecycle-scheduling.md) at commit time.
func (r *Resolver) FastLaneSuppressed() bool

// DroppedProvider returns the name of the currently dropped provider, or "" (§2.10).
func (r *Resolver) DroppedProvider() string

// Close stops the canary and window-maintenance goroutines.
func (r *Resolver) Close()
```

Both crawler processes run their own `consensus.Resolver`; all breaker/bucket state is per-process (the config default `per_provider_qps: 15` is sized for 2 processes → 30 qps/provider total ceiling vs the sized ~24 qps/provider demand — sizing constants: 00-overview.md).

### 2.3 Per-resolver query discipline (timeout + retry)

Per consensus lookup, the three (or two, when a provider is dropped) providers are queried **concurrently**. Per provider:

1. Acquire that provider's token (§2.8). If `ctx` expires while waiting, the provider's outcome is a non-answer (`error` symbol, rcode `""`).
2. Derive a per-provider context: `pctx, cancel := context.WithTimeout(ctx, 4*time.Second)`.
3. Call the provider resolver's lifted `LookupAAAA(pctx, name)` (EDNS0, UDP→TCP on truncation, CNAME chase ≤10 hops — all unchanged from the lift).
4. Reduce the result to a per-resolver symbol (§2.4).

**Decision:** the design's "2s per-resolver timeout, one retry" is implemented as a **4-second per-provider context budget** (2s × (1 attempt + 1 retry)), with `checker.Resolver` left completely untouched: the lifted `Query` already clamps its `dns.Client` timeout to the context remainder, and the lifted `QueryWithRetry` already provides exactly one retry (on transport error, SERVFAIL, or REFUSED — rotating to the provider's other address). A provider that blackholes the first attempt can consume the whole 4s and lose its retry; that is acceptable because a blackholing provider is a non-answer either way, while the retry's real value is on fast-failing responses (SERVFAIL/REFUSED/ICMP-refused return in milliseconds and leave nearly the full budget). The 4s budget also bounds the CNAME chase. The adapted checks' own 5s check timeout (01-engine.md) remains the outer bound.

There is no `consensus.per_resolver_timeout` config key; the 2s/4s figures are package constants:

```go
const (
    perAttemptTimeout = 2 * time.Second // documentation constant; enforced via perProviderBudget
    perProviderBudget = 4 * time.Second // context deadline per provider per lookup
)
```

### 2.4 Per-resolver symbol reduction (valid answers vs non-answers)

Each provider's reply reduces to one of five per-resolver symbols. The first three are **valid answers** (they vote); `timeout`/`error` are **non-answers** (they never vote). Classification happens **after** the single retry:

```go
// reduce classifies one provider's LookupAAAA result.
// ips is the answer's AAAA set AFTER the globally-routable filter (§2.5).
func reduce(ans checker.AAAAAnswer, err error) (symbol string) {
    switch {
    case err != nil && isTimeoutErr(err):     // context deadline / net.Error timeout
        return "timeout"                       // non-answer
    case err != nil:                           // transport error, or SERVFAIL (the lifted
        return "error"                         //   LookupAAAA converts SERVFAIL to an error) — non-answer
    case ans.Rcode == "NXDOMAIN":
        return "nxdomain"                      // valid answer
    case ans.Rcode == "NOERROR" && len(routableOnly(ans.IPs)) > 0:
        return "exists"                        // valid answer: ≥1 globally-routable AAAA
    case ans.Rcode == "NOERROR":
        return "empty"                         // valid answer: NOERROR, no (routable) AAAA after CNAME chase
    default:
        return "error"                         // any other rcode: REFUSED, NOTIMP, FORMERR, ... — non-answer
    }
}
```

Two traps this table exists to close (both verified against the lifted `resolver.go`):

- **SERVFAIL** surfaces from the lifted `LookupAAAA` as a Go error (`"SERVFAIL for %s"`), not as an rcode-bearing answer → non-answer. SERVFAIL is "the resolver could not determine an answer" (e.g. broken DNSSEC seen by all three validating resolvers); it is **never a vote**.
- **REFUSED** (and every other non-NOERROR/non-NXDOMAIN rcode) passes through the lifted `LookupAAAA` as a normal return with zero answer records. It MUST classify as the `error` symbol, never as `empty` — the reduction keys on rcode, not on `len(ips)`.

The raw rcode string is captured per provider into `QuorumInfo.Rcodes` (empty string when no DNS response was received at all).

### 2.5 Globally-routable AAAA filter

Ported from production `whynoipv6/internal/resolver/resolver.go:486-514` and applied on the consensus path in two places: (a) inside `reduce` — the `exists`/`empty` decision counts only routable addresses; (b) on the returned `AAAAAnswer.IPs` — non-routable addresses are removed before return (if all are filtered, the answer is `empty`). An IPv6 address is **not** globally routable when any of:

```go
func isGloballyRoutableIPv6(ip net.IP) bool {
    if ip.To4() != nil        { return false } // IPv4 or IPv4-mapped
    if ip.IsLoopback()        { return false } // ::1
    if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() { return false } // fe80::/10, ff02::/16
    if len(ip) >= 1 && (ip[0] == 0xfc || ip[0] == 0xfd)     { return false } // ULA fc00::/7
    if ip.IsUnspecified()     { return false } // ::
    return true
}
```

The same function is exported from `internal/checker` and reused by the bulk-path checks that count AAAA answers (`dns_ns_ipv6`, `dns_mx_ipv6`, the resource-host sweep — see 01-engine.md and §6).

### 2.6 Quorum rules

The quorum is taken **over reduced symbols, not record sets** (GeoDNS legitimately returns different AAAA contents per region; what must agree is *whether v6 exists*), and only **over valid answers**:

| Valid answers | Rule | Outcome |
|---|---|---|
| ≥2 agree on symbol S | 3-0, 2-1, or 2-0-with-one-non-answer | quorum result = S |
| ≥2, but no two agree | e.g. exists/empty/nxdomain split, or 1-1 with one non-answer | **no quorum** → observation `inconsistent` |
| ≤1 | ≥2 providers non-answering | **quorum unavailable** → observation `error` |

With a dropped provider (§2.10) the fan-out is 2 and quorum degrades to 2-of-2: both agree → that symbol; both answer but disagree → `inconsistent`; ≤1 valid answer → `error`.

**Return contract** of `Resolver.LookupAAAA`:

- **Quorum reached:** return the **entire answer** (filtered IPs, CNAME chain, min TTL, rcode) of the *first provider in fixed order cloudflare → google → quad9 whose reduced symbol equals the quorum symbol*, with `Quorum` filled (`Disagreed=true` on 2-1 splits). Never merge record sets across resolvers. If the quorum symbol is `empty`, additionally run the conditional A lookup (§2.7) and set `AOutcome`. Return `err = nil`.
- **No quorum:** return `checker.AAAAAnswer{Quorum: &qi}, checker.ErrQuorumInconsistent`.
- **Quorum unavailable (≤1 valid answer):** return `checker.AAAAAnswer{Quorum: &qi}, fmt.Errorf("aaaa consensus for %s: %d valid answers from %d providers", name, nValid, nActive)` — a plain error, deliberately NOT `ErrQuorumInconsistent`, so the adapted checks' existing error branch fires and the mapper produces `error`, not `inconsistent`.

`QuorumInfo` filling: `PerResolver` and `Rcodes` get one entry per *queried* provider (2 or 3 keys); `Agreement = fmt.Sprintf("%dof%d", nMatching, nActive)` where `nMatching` is the count of valid answers equal to the quorum symbol and `nActive` the fan-out size (yields exactly `"3of3"`, `"2of3"`, `"2of2"`); on no-quorum/unavailable outcomes set `Agreement = fmt.Sprintf("0of%d", nActive)` **Decision:** (the design enumerates only the reached-quorum strings; `0ofN` is the natural extension for the failure records). `Disagreed = true` iff any valid answer's symbol differs from the quorum symbol (always false when no quorum exists — the field is only meaningful on success).

### 2.7 Conditional A lookup (`AOutcome`)

When **and only when** the AAAA quorum result is `empty` (NOERROR, no routable AAAA), `internal/consensus` issues **ONE** A query for the same name through the **bulk resolver** (no quorum; these are the two "A×2" queries budgeted in 00-overview.md — sizing constants). Outcomes:

- `a_present` — ≥1 A record.
- `a_absent` — NOERROR with no A records; an A-NXDOMAIN contradicting the AAAA NOERROR is **also** treated as `a_absent` (resolve contradictions in the domain's favor).
- `a_error` — any other rcode (SERVFAIL, REFUSED, …) or transport error.

**Decision:** the lifted `checker.Resolver.LookupA` cannot express this classification (it collapses every non-NOERROR rcode — including SERVFAIL — into an empty, error-free result, which would misclassify `a_error` as `a_absent`). `internal/consensus` therefore implements its own A classification directly on the bulk resolver's `QueryWithRetry`:

```go
func (r *Resolver) classifyA(ctx context.Context, name string) string {
    msg := new(dns.Msg)
    msg.SetQuestion(dns.Fqdn(name), dns.TypeA)
    msg.RecursionDesired = true
    // EDNS0 per the lifted setEDNS0
    resp, err := r.bulk.QueryWithRetry(ctx, msg) // Unbound; retry-once semantics unchanged
    switch {
    case err != nil:
        return "a_error"
    case resp.Rcode == dns.RcodeSuccess:
        if countA(resp.Answer) > 0 { return "a_present" }
        return "a_absent"
    case resp.Rcode == dns.RcodeNameError:
        return "a_absent" // NXDOMAIN contradicting the AAAA NOERROR → domain's favor
    default:
        return "a_error"
    }
}
```

No CNAME chase is needed: the upstreams are recursive (Unbound), so any chain's terminal A records already appear in the answer section. No token bucket applies (bulk path, local Unbound). The A answer is not quorumed: any single wrong answer still has to survive the N=2 confirmation gate (03-state-machine.md) before it can change confirmed state.

### 2.8 Rate control: per-provider token buckets

One `golang.org/x/time/rate.Limiter` per provider: rate = `consensus.per_provider_qps` tokens/s sustained, burst = `consensus.per_provider_qps` (one second of burst). Acquisition is **blocking** (`limiter.Wait(ctx)`) — worker slots absorb the wait; this is the "smooth the rate" mechanism.

**Decision:** one token is acquired per provider **per consensus lookup** (before the provider's first query attempt); retries and CNAME-chase hops ride the same token. The provider limits being protected are three orders of magnitude above our sustained rate (Google documents 1500 qps/IP, Quad9 a 500 qps contact threshold; we run ~24 qps/provider total), so the bounded per-lookup multiplier (retry + chase) is absorbed by that margin; per-attempt accounting would add locking complexity for no protective value. Canary probes (§2.10) also acquire a token.

### 2.9 Fast-lane breaker

Caps the resolver-degradation amplification (worst case otherwise ≈ 12× the sized 24 qps/provider, via the 2h/6h recheck pull-ins re-querying a degraded resolver set).

- **Sample** = one `LookupAAAA` call outcome, recorded at return: *definitive* (quorum reached on `exists`/`empty`/`nxdomain`) or *non-definitive* (no quorum, or ≤1 valid answer). **Decision:** samples are consensus-lookup outcomes only; `a_error` from the bulk A lookup (§2.7) is not a sample — the breaker measures public-resolver health, and the A path is local Unbound.
- **Window** = rolling `consensus.fastlane_breaker.window` (default 15m), implemented as a ring of 15 one-minute buckets of `(total, nondefinitive)` counters, advanced by a 1-minute ticker.
- **Open (suppress):** when `total ≥ min_samples` (500) AND `nondefinitive/total > nondefinitive_rate` (0.05) over the trailing window → set suppressed, alert the ops webhook (`"consensus fast-lane breaker OPEN: nondefinitive rate X over 15m (n=N)"`), log `level=warn msg="fastlane breaker open"`.
- **Effect while open:** `FastLaneSuppressed()` returns true; the scheduler stops applying the 2h/6h pull-ins — non-definitive scans schedule at `cadence(rank)` instead (consumed at commit-time scheduling; see 04-lifecycle-scheduling.md). Nothing else changes: lookups still run, quorum still applies, observations are still recorded.
- **Close (recover):** **Decision:** evaluated once per minute while open; the breaker closes when `nondefinitive/total < recover_below` (0.02) over the trailing full window, with `total == 0` counting as recovered (an idle window is a healthy window — the design's "stays below for one full window" is exactly the trailing-window rate, since the window is the full lookback). On close: alert + `level=info msg="fastlane breaker closed"`.

### 2.10 Provider breaker + canary

Per-provider health, independent of the fast-lane breaker:

- **Sample** = one provider's reduced symbol per consensus lookup; *failure* = non-answer (`timeout` or `error` symbol, i.e. after the retry). Same 1-minute-bucket ring per provider, window `consensus.provider_breaker.window` (15m).
- **Drop:** when a provider's `samples ≥ min_samples` (200) AND `failures/samples > failure_rate` (0.50) over the window → remove it from the fan-out, alert (`"consensus provider breaker: dropped <name> (failure rate X over 15m)"`), log `level=warn msg="provider dropped" provider=<name>`. Quorum degrades to 2-of-2 (§2.6).
- **Decision — at most one provider may be dropped at a time.** If a second provider crosses the threshold while one is already dropped, it is NOT dropped: alert loudly (`"consensus: second provider <name> over failure threshold while <other> is dropped — NOT dropping; investigate"`) and continue with the 2-provider fan-out. Rationale: a 1-provider "quorum" is meaningless, and two simultaneously sick anycast networks almost certainly indicate our own vantage is broken — the growing non-answer rate then surfaces as `error` observations and the fast-lane breaker, which is the correct failure mode.
- **Canary:** while a provider is dropped, a dedicated goroutine probes it every 5 minutes (`canaryInterval = 5 * time.Minute`, package constant): one AAAA lookup for the canary name via that provider (token acquired; per-provider budget §2.3 applies). **Decision:** the canary name is the package constant `canaryName = "one.one.one.one"` — the same stable anycast name the crawler self-preflight dials (01-engine.md), guaranteed to exist with AAAA on all three networks. A probe **succeeds** iff it reduces to a valid answer (`exists`/`empty`/`nxdomain` — in practice `exists`). After `recovery_probes` (3) consecutive successes: restore the provider to the fan-out, clear its window counters (**Decision:** counters reset on restore so pre-outage failures cannot instantly re-trip the breaker), alert + `level=info msg="provider restored" provider=<name>`.
- Canary results do not enter the fast-lane breaker or the provider's regular window.

### 2.11 Config keys

Introduced by this file, all under the crawler config (types/defaults above; **registry: 09-ops.md**):

`consensus.per_provider_qps`, `consensus.fastlane_breaker.nondefinitive_rate`, `consensus.fastlane_breaker.window`, `consensus.fastlane_breaker.min_samples`, `consensus.fastlane_breaker.recover_below`, `consensus.provider_breaker.failure_rate`, `consensus.provider_breaker.window`, `consensus.provider_breaker.min_samples`, `consensus.provider_breaker.recovery_probes`, and (in §6) `crawler.resources.enabled`.

Not config (package constants): provider names/addresses, fixed provider order, `perAttemptTimeout` 2s / `perProviderBudget` 4s, `canaryInterval` 5m, `canaryName`, and the mapper's `preflightFreshness` 5m (§5).

---

## 3. Adapted `dns_aaaa_base` / `dns_aaaa_www` — the Result contract the mapper reads

Check internals (constructor changes, CDN detection, detail assembly) are owned by 01-engine.md; this section pins the exact `checker.Result` surface the mapper (§7) depends on. Constructors become `NewDNSAAAABase(res checker.AAAAResolver)` / `NewDNSAAAAWWW(res checker.AAAAResolver)` — these two checks only resolve, never dial (SafeDialer dependency dropped). Core adapted logic:

```go
ans, err := c.res.LookupAAAA(ctx, name)
if ans.Quorum != nil {
    details["quorum"] = ans.Quorum          // persists into scan_detail (the disagreement annotation)
}
if errors.Is(err, checker.ErrQuorumInconsistent) {
    details["inconsistent"] = true
    return checker.Result{Status: checker.StatusError, Details: details, Latency: time.Since(start)}, nil
}
if ans.AOutcome != "" {
    details["a_outcome"] = ans.AOutcome
}
// ...existing err / NXDOMAIN / no-ips / supported branches, using ans.IPs, ans.CNAMEChain,
// ans.TTL, ans.Rcode (details["rcode"], details["addresses"], details["ttl"],
// details["cname_chain"], details["cname_target"], details["cdn_detected"] as in the lift)
```

Resulting engine-status ↔ details contract (exhaustive; engine statuses stay 5-valued — `inconsistent` exists only at the observation layer):

| Engine `Result.Status` | Cause | Details keys guaranteed present |
|---|---|---|
| `supported` | quorum `exists` | `rcode`, `addresses`, `ttl`, `quorum` |
| `unsupported` | quorum `empty` (NOERROR, no routable AAAA) | `rcode`, `quorum`, `a_outcome` ∈ {`a_present`,`a_absent`,`a_error`} |
| `not_applicable` | quorum `nxdomain` | `rcode` = `"NXDOMAIN"`, `quorum`, `reason` |
| `error` + `details["inconsistent"] == true` | no quorum | `quorum`, `inconsistent` |
| `error` (no `inconsistent` key) | ≤1 valid answer, or any other lookup error | `error`, `quorum` (when the fan-out ran) |

The raw rcode is kept in `scan_detail` so the dead-signal computation can require NXDOMAIN specifically (03-state-machine.md). For `kind = subdomain` entities the runner forces the `www` check to `not_applicable` without running it (01-engine.md — kind-aware checks).

---

## 4. Observation mapping tables (normative)

Each per-resolver AAAA answer (apex and www, consensus resolver) reduces to one of four quorum symbols — `exists` (≥1 globally-routable AAAA; loopback/link-local/ULA rejected), `empty` (NOERROR, no AAAA after CNAME chase), `nxdomain`, `error` (timeout / SERVFAIL / network error) — and quorum (§2.6) is taken over these symbols. No-quorum → observation `inconsistent`; quorum unavailable (`error`) → observation `error` (both non-definitive: never advance confirmed state, never write changelog; scheduling per 04-lifecycle-scheduling.md).

**Conditional A lookup:** when and only when the AAAA quorum result is `empty`, ONE un-quorumed A query goes through the bulk resolver (§2.7): `a_present` / `a_absent` / `a_error`.

**`base` observation (apex; for `kind=subdomain`, the host itself):**

| AAAA quorum | A lookup | base observation |
|---|---|---|
| `exists` | not run | `supported` |
| `empty` | `a_present` | `unsupported` (sinner-eligible: A exists, AAAA definitively absent) |
| `empty` | `a_absent` | `no_record` (empty/parked zone → inactive) |
| `empty` | `a_error` | `error` |
| `nxdomain` | not run | `no_record` (domain doesn't exist → inactive; raw rcode kept in `scan_detail` so dead-detection can require NXDOMAIN specifically) |
| `error` | not run | `error` |
| no quorum | not run | `inconsistent` |

**`www` observation** (skipped entirely — forced `not_applicable` — for `kind=subdomain`):

| AAAA quorum | A lookup | www observation |
|---|---|---|
| `exists` | not run | `supported` |
| `empty` | `a_present` | `unsupported` (www is v4-only → blocks hero, `www_missing` flag) |
| `empty` | `a_absent` | `not_applicable` (www node serves nothing → site doesn't use www) |
| `empty` | `a_error` | `error` |
| `nxdomain` | not run | `not_applicable` (site doesn't use www) |
| `error` | not run | `error` |
| no quorum | not run | `inconsistent` |

**`www` never produces `no_record`.** `no_record` can only ever be observed (and confirmed) for `base`.

**Remaining core dimensions** (engine status → observation; engine statuses per the lifted `checker.go`):

| Dimension | `supported` | `partial` | `unsupported` | `not_applicable` | `error` |
|---|---|---|---|---|---|
| `ns` | `supported` | `supported` (≥1-host rule) | `unsupported` | (never emitted; walk-up finding no zone yields engine `error` — defensive mapping: `error`, §7.3) | `error` |
| `mx` | `supported` | `supported` (≥1-host rule) | `unsupported` | `not_applicable` (null-MX; and for subdomains, no explicit MX) | `error` |
| `conn` | `supported` | n/a | `unsupported` (requires preflight pass ≤5 min; full composition + timeout rule in §5) | `not_applicable` (phase-2 skip: no AAAA) | `error` (except preflight-guarded persistent timeouts → `unsupported`, §5 row 5a) |

`resources` is not engine-status-mapped: its observation is produced by the registry roll-up (§6; the adapted `resource_discovery` check only feeds link discovery). `ns`, `mx`, `conn`, and `resources` never produce `no_record`. Informational dimensions (`tls`/`smtp`/`parity`/`dnssec`/`ptr`/`spf`/`latency`) have no mapping table — `ptr` and `parity` store the raw engine status verbatim (including `partial`), `smtp` maps `partial` → `unsupported` before storage, and the rest keep their raw status in the scan payload (§7.4).

---

## 5. `conn` composition (derived dimension)

`conn` is a **derived dimension with no single engine source** — v6audit has no combiner (`https_ipv6` and `http_ipv6` are independent phase-2 siblings). The composition function is new code in `internal/crawler`, applied after `Runner.Run` and before the commit.

**Inputs:** the same scan's `https_ipv6` result `H` (status + `details.error_type` + `details.reason`) and `http_ipv6` result `P` (status), plus the freshness of the process self-preflight. Both checks stay registered as independent phase-2 checks and both run unconditionally whenever phase 2 runs (gate unchanged from the lifted runner: base OR www AAAA supported). When phase 2 is skipped, both are `not_applicable`. `http_ipv6` carries the same terminal `error_type` classification as `https_ipv6` (enumerated lift deviation; 01-engine.md), so this table applies identically on the http-only fallback path.

**Decision table (first match wins):**

| # | Condition | conn observation | Notes |
|---|---|---|---|
| 1 | H = `supported` | `supported` | source=`https`, http_only=false |
| 2 | H = `unsupported` AND H.error_type = `connection_refused` AND P = `supported` | `supported` | source=`http`, **http_only=true** — the only fallback case |
| 3 | H = `unsupported` AND H.error_type = `certificate_error` | `unsupported` | never rescued by http: an invalid cert over v6 is broken v6 |
| 4 | H = `unsupported` (any other case: connection_refused with P ≠ supported; or no-AAAA-on-host) | `unsupported` | ⇒ `broken_v6` flag once confirmed (03-state-machine.md) |
| 5a | H = `error` with H.error_type = `timeout` AND the process preflight passed within the last 5 minutes | `unsupported` | a persistent connect/response timeout against a published AAAA **is** the canonical broken-v6 failure and must be definitive; the raw `error_type = "timeout"` stays in the scan payload |
| 5b | H = `error` with H.error_type = `timeout` but preflight stale/failed | `error` | non-definitive; worker should not be claiming anyway |
| 5c | H = `error`, any other error_type (`unknown`, blocked address, internal) | `error` | non-definitive: touches nothing, `recheck_error` does not apply (rule 2 in 04-lifecycle-scheduling.md: only base/www drive pull-ins) |
| 6 | H = `not_applicable` (phase 2 skipped: no AAAA on base or www) | `not_applicable` | |

**Final preflight guard (belt-and-suspenders, applied after the table):** **every** `conn = unsupported` observation — whether from connection-refused (row 4), TLS failure (row 3), or timeout (row 5a) — requires the preflight to have passed within the last 5 minutes (`preflightFreshness = 5 * time.Minute`, constant); otherwise the observation is downgraded to `error`. The preflight timestamp is process state maintained by the claim loop (01-engine.md — self-preflight); the mapper receives it as an input.

Rules and consequences:

- `error` outcomes (rows 5b/5c) are **never overridden by P, even P = `supported`** — non-definitive per the commit's "touch nothing" branch; recorded in the scan log only.
- The N=3 anti-flap for `conn` (03-state-machine.md) is unchanged and is the flap guard: a single slow (>10s) response never demotes a hero; only three consecutive daily timeouts confirm `unsupported`, write the changelog entry, and raise `broken_v6`.
- "HTTP-only site" is **operationally defined** as: port 443 actively refuses (ECONNREFUSED) while port 80 serves over IPv6. A 443 that blackholes (firewall DROP → timeout) takes rows 5a/5b.
- Rows 2/3 cannot conflict, and "no-AAAA + P=supported" cannot occur: both checks issue the identical AAAA lookup on the same host through the same bulk resolver (Unbound-cached).
- **Target host:** `conn` always dials **the entity's own host** — the apex for `kind=apex`, the subdomain itself for `kind=subdomain`. There is **no www fallback for conn**. A www-only domain (AAAA on `www` but not the apex) gets `conn = unsupported` (row 4, no-AAAA-on-host); this can never change hero membership because hero already requires `base = supported`.
- **Accepted skew (stated, not fixed):** the https/http checks re-resolve AAAA via the **bulk** resolver, not the consensus verdict used for `base`/`www`. A persistent disagreement (e.g. region-scoped GeoDNS AAAA visible to the 3 public anycast networks but not to our local Unbound) can confirm `conn = unsupported` on a `base = supported` domain after N=3 consecutive scans. That outcome is accepted and semantically honest — "publishes AAAA, unreachable over v6 from our vantage" is exactly what `broken_v6` means — and transient skew is absorbed by the N=3 anti-flap.

**Payload:** the mapper hoists a derived object into `scan_detail.details`:

```json
"conn": {"status": "<final conn observation>", "source": "https" | "http", "http_only": false, "error_type": "timeout"}
```

`source` is present only on `supported` outcomes (row 1 → `"https"`, row 2 → `"http"`); `http_only` is always present (true only on row 2); `error_type` is copied from the https result when present and omitted on success. `http_only` is payload-only for the detail page — it is **not** a `class_flag` and does not alter the legacy `v6_only` field, which serves the confirmed `conn` status unchanged (see the API compat file).

---

## 6. `resources` roll-up (registry-based)

The `resources` observation is produced by rolling up **confirmed** per-host statuses from the globally deduped `resource_host` registry over this domain's links in `domain_resource` (DDL: 05-schema.md — resources tables). Link discovery (the adapted `resource_discovery` check + `v6ctl resource add`) and the daily host sweep are specified elsewhere (01-engine.md for the check; 03-state-machine.md for the link upsert/prune inside the commit transaction; the sweep worker in 04-lifecycle-scheduling.md); this section owns only the roll-up algorithm.

**Inputs:** this scan's `conn` observation (§5), and the confirmed statuses of this domain's effective required-host set. The roll-up is computed **read-only, before the commit batch is built** — never after the link upsert/prune. This is forced by the commit architecture: `MapObservations` runs before `Commit` (04-lifecycle-scheduling.md — per-domain slot sequence), and the commit is a single `pgx.Batch` whose statement 1 (the fenced domain `UPDATE`) already carries `resources_pending` computed from `O[resources]`, while the link upsert/prune are later statements in that same batch (03-state-machine.md §12.3). There is therefore no post-upsert moment at which to run the roll-up; it reads the link set as it stands at claim time, folded with this scan's discovery output. This matches the design's canonical definition of the roll-up — "computed read-only over the run's `resource_discovery` host list against confirmed `resource_host.aaaa_status`", the single mapping used on both the live-check and frontier-commit paths.

The effective required-host set is:

```sql
-- (1) this domain's PERSISTED required links, read at claim/pre-upsert time
--     (includes manual links, which never appear in discovery output D):
SELECT rh.aaaa_status
FROM domain_resource dr
JOIN resource_host rh ON rh.id = dr.resource_host_id
WHERE dr.domain_id = $1 AND dr.required;
```

**Decision — fold in this scan's discovery output D:** the caller adds to the set, read-only, every required host in this scan's `resource_discovery` output D that is not already among the persisted links above. Because such a host has not yet been linked to this domain (and, on first-ever discovery anywhere, has no `resource_host` row at all — it will be `INSERT`ed with `aaaa_status NULL` by this commit's upsert), each folded host contributes a **NULL** status entry. Folding all not-yet-persisted D hosts as NULL is the conservative, simplest form: it can only *defer* (never falsely advance), it needs no extra per-host status lookup, and a host already known to the global registry but newly linked here simply defers one scan until its persisted link carries the real status next scan — the same one-scan cost as a brand-new host. This fold reproduces the post-upsert link set minus the prune (a link being pruned this scan is 30-day-stale and remains in the pre-upsert persisted set for this one roll-up; the discrepancy is a single scan, absorbed by the N=3 domain hysteresis below).

**Algorithm (all branches normative):**

```
if conn_observation in {error, inconsistent}:  resources_obs = error          # defer with conn
elif conn_observation != supported:            resources_obs = not_applicable # v6-unreachable site: deps moot
else:
  hosts = confirmed aaaa_status of the effective required-host set (persisted required
          links ∪ folded D hosts, per Inputs above; folded-in D hosts are NULL)
  hosts = hosts minus those with status in {no_record, not_applicable}        # dead references are not
                                                                              #   evidence of v4-only dependence
  if any remaining status IS NULL:             resources_obs = error          # host not yet swept: defer,
                                                                              #   never advances pending
  elif hosts is empty:                         resources_obs = not_applicable # no (live) external deps
  elif any status = unsupported:               resources_obs = unsupported
  else:                                        resources_obs = supported
```

The observation then enters the commit machinery unchanged (N=3; 03-state-machine.md). **Deliberate double hysteresis:** host-level N=2 (in the sweep) stacked under domain-level N=3 gives a worst-case ~5 days for a gold transition; this is intentional — the domain level also absorbs link-set churn (rotating ad/CDN hosts across fetches), which host-level confirmation cannot. `error` never advances `resources_pending`, so the NULL window on discovery day costs one deferred day, not a false transition: a host newly discovered this scan enters the roll-up via the D-fold as a NULL-status entry (Inputs above), yielding `error` (defer) even though its link is not persisted until this same commit's upsert — the read-only fold is precisely what preserves this defer without depending on the upsert having run.

**Phase gate — config key `crawler.resources.enabled`** (bool, default `false`; flipped to `true` at phase-5 deploy; **registry: 09-ops.md**). While `false`, the crawler: skips `resource_discovery` entirely, the mapper emits `resources = not_applicable` on every scan (satisfying the NOT NULL `scan.resources` column), and the commit **excludes the resources dimension from the confirm/pending loop** — the domain's `resources_status/observed/pending/pending_count/since` columns stay NULL (mechanism in 03-state-machine.md; the mapper signals this with `ResourcesExcluded = true`, §7.2). Consequently `gold = hero AND resources ∈ {supported, not_applicable}` evaluates false for all domains until phase 5 — correct: no gold badges before the feature ships.

---

## 7. The Result→observation mapper — `internal/crawler/observe.go`

The mapper is the only code that converts `checker.ScanResult` (engine vocabulary) into `domain.Observation` values (observation vocabulary). It is a pure function of its inputs; it performs no I/O except that its `resources` input set is produced by the read-only, pre-commit roll-up query (§6) executed by the caller before the commit batch is built.

### 7.1 Signature and inputs

```go
// LinkedResource is one required host's confirmed status in the effective
// required-host set (persisted pre-upsert links ∪ folded discovery output D; §6).
type LinkedResource struct {
    AAAAStatus *domain.IPv6Status // nil = host never swept (aaaa_status IS NULL),
                                  // OR a D-folded host not yet persisted for this domain
}

// MapObservations converts one engine scan into per-dimension observations.
//   kind              — the entity kind (apex | subdomain)
//   sr                — Runner.Run output (all 15 checks present; skipped checks
//                       are recorded not_applicable by the runner)
//   preflightPassedAt — last successful self-preflight of this process
//   now               — the commit timestamp T (fixed once per domain)
//   links             — the effective required-host set's confirmed statuses:
//                       persisted pre-upsert links folded with this scan's discovery
//                       output D (§6 Inputs), computed read-only before the commit
//                       batch; ignored when resourcesEnabled is false
//   resourcesEnabled  — crawler.resources.enabled
func MapObservations(
    kind domain.Kind,
    sr checker.ScanResult,
    preflightPassedAt time.Time,
    now time.Time,
    links []LinkedResource,
    resourcesEnabled bool,
) Observations
```

### 7.2 Output

```go
// Observations is the mapper output consumed by the commit (03-state-machine.md).
type Observations struct {
    // Core dimensions — always set (never "").
    Base, WWW, NS, MX, Conn, Resources domain.Observation

    // Informational dimensions — always set (never ""); no confirmation machinery.
    DNSSEC, PTR, SMTP, Parity domain.Observation

    // TTFB averages; nil unless the respective latency check returned supported.
    LatencyV4Ms, LatencyV6Ms *int32

    // ConnDetail is the derived payload object hoisted into scan_detail.details["conn"] (§5).
    ConnDetail map[string]any

    // ResourcesExcluded is true when crawler.resources.enabled=false: the commit
    // must skip the resources dimension in the confirm/pending loop (§6).
    ResourcesExcluded bool
}
```

Column targets (names only; DDL in 05-schema.md): the six core values populate `scan.base/www/ns/mx/conn/resources` and `domain.*_observed`; the informational values populate `scan.dnssec/ptr/smtp/parity` and `domain.dnssec_observed/ptr_observed/smtp_observed/parity_observed`; latency populates `scan.latency_v4_ms/latency_v6_ms` and `domain.latency_v4_ms/latency_v6_ms`.

### 7.3 Core-dimension algorithm (numbered, exhaustive)

Check-name keys into `sr.Results`: `dns_aaaa_base`, `dns_aaaa_www`, `dns_ns_ipv6`, `dns_mx_ipv6`, `https_ipv6`, `http_ipv6` (the lifted `Name()` strings are kept unchanged; the adapted resource check renames to `resource_discovery` — 01-engine.md).

1. **`base`** — from `r := sr.Results["dns_aaaa_base"]`, implementing §4's base table over the §3 Result contract:
   1. `r.Status == error` AND `r.Details["inconsistent"] == true` → `inconsistent` (**the inconsistent rule**: this is the only source of `inconsistent`, for `base` and `www` alike).
   2. `r.Status == error` (no `inconsistent` key) → `error`.
   3. `r.Status == supported` → `supported`.
   4. `r.Status == not_applicable` (quorum `nxdomain`) → `no_record`.
   5. `r.Status == unsupported` (quorum `empty`) → by `r.Details["a_outcome"]`: `a_present` → `unsupported`; `a_absent` → `no_record`; `a_error` → `error`; key missing → `error` + `level=warn msg="a_outcome missing"` (defensive; the consensus wrapper always sets it on the empty path).
2. **`www`** — if `kind == subdomain` → `not_applicable` unconditionally. Otherwise identical to step 1 over `sr.Results["dns_aaaa_www"]` with the two www-table substitutions: `not_applicable` engine status (nxdomain) → `not_applicable` (not `no_record`), and `a_absent` → `not_applicable` (not `no_record`).
3. **`ns`** — from `sr.Results["dns_ns_ipv6"].Status`: `supported` → `supported`; `partial` → `supported`; `unsupported` → `unsupported`; `error` → `error`; `not_applicable` → `error` + `level=warn msg="unexpected ns not_applicable"` (**Decision:** defensive mapping — the walk-up always reaches an authoritative zone, so engine `not_applicable` is unreachable by construction; mapping it non-definitive guarantees it can never confirm and never touch state).
4. **`mx`** — from `sr.Results["dns_mx_ipv6"].Status`: `supported` → `supported`; `partial` → `supported`; `unsupported` → `unsupported`; `not_applicable` → `not_applicable` (null-MX per RFC 7505; and for `kind=subdomain`, no explicit MX — the implicit-MX fallback is disabled for subdomains in the adapted check, 01-engine.md); `error` → `error`.
5. **`conn`** — the §5 decision table over `H = sr.Results["https_ipv6"]`, `P = sr.Results["http_ipv6"]`, with `preflightFresh := now.Sub(preflightPassedAt) <= preflightFreshness`; then the final preflight guard (any `unsupported` with `!preflightFresh` → `error`). Also builds `ConnDetail` (§5 payload).
6. **`resources`** — if `!resourcesEnabled` → `not_applicable`, `ResourcesExcluded = true`. Else the §6 algorithm over the `conn` observation from step 5 and `links`.
7. **Defensive fallback:** any expected check name absent from `sr.Results` → that dimension is `error` + `level=error msg="check result missing"` (cannot happen with the lifted runner, which records every registered check including skips; the fallback exists so a future runner bug degrades to non-definitive observations instead of false state).

### 7.4 Informational dimensions

- **`dnssec`** ← `sr.Results["dns_dnssec"].Status` stored raw (the check emits only `supported/unsupported/not_applicable/error`; it never emits `partial` — verified against the lifted source. Defensively, an illegal `partial` here maps to `error` + warn log, preserving the §1.2 partial storage rule).
- **`ptr`** ← `sr.Results["dns_ptr_ipv6"].Status` stored **verbatim, including `partial`** (partial FCrDNS).
- **`smtp`** ← `sr.Results["smtp_ipv6"].Status` with `partial` → `unsupported` (an EHLO that half-works does not accept mail over v6); all other statuses raw.
- **`parity`** ← `sr.Results["http_response_parity"].Status` stored **verbatim, including `partial`**.
- **`latency`** ← `sr.Results["latency_ipv4"]` / `sr.Results["latency_ipv6"]`: when `Status == supported`, `Latency*Ms` = the check's `Details["avg_ms"]` value (int32); otherwise nil → SQL NULL.
- **`tls` and `spf`** produce **no observation and no typed column anywhere** — informational-only, they live exclusively in `scan_detail.details` JSONB (accepted design, not an omission).

### 7.5 Payload hoists

Beyond per-check `Details` (payload assembly owned by 03-state-machine.md), the mapper contributes two hoisted objects to `scan_detail.details`:

1. `details["conn"]` — the §5 derived object.
2. `details["consensus"]` — `{"base": <QuorumInfo of dns_aaaa_base>, "www": <QuorumInfo of dns_aaaa_www>}`, copied from the checks' `Details["quorum"]` values (present whenever the fan-out ran; the JSON shape is §2.1's tagged struct, from which the recorded per-resolver tuple {resolver, rcode, reduced symbol, answered} is fully derivable). Omit a key when the corresponding check has no quorum info (e.g. the forced-`not_applicable` www on subdomains).

---

## 8. Acceptance criteria

Test fixtures and tables live in 10-testing.md (fake-DNS server driving `internal/consensus` and the seam; table-driven mapper tests). An implementation of this file is done when:

1. Quorum truth table: every combination of 3 per-resolver symbols from {exists, empty, nxdomain, timeout, error} yields the §2.6 outcome, including all 2-1 splits (`Disagreed=true`), 2-0-with-non-answer, no-quorum → `ErrQuorumInconsistent`, and ≤1-valid → plain error; same for the 2-provider degraded mode.
2. REFUSED and SERVFAIL both classify as non-answers (never as `empty`); REFUSED is observable in `QuorumInfo.Rcodes`.
3. The answer returned on quorum is byte-identical to the first in-order agreeing provider's answer (no record-set merging), with non-routable AAAA filtered.
4. The conditional A lookup runs exactly when the quorum symbol is `empty`, classifies `a_present`/`a_absent`/`a_error` per §2.7 (including A-NXDOMAIN → `a_absent` and A-SERVFAIL → `a_error`), and never runs otherwise.
5. Breakers: fast-lane opens/closes at the configured thresholds and flips `FastLaneSuppressed()`; provider breaker drops at >50% non-answers over ≥200 samples, degrades quorum to 2-of-2, restores after 3 canary successes, and never drops a second provider.
6. Mapper: every row of the §4 base/www/ns/mx tables, every row of the §5 conn table (including both preflight guard downgrades), and every branch of the §6 roll-up (conn-error defer, conn-unsupported → not_applicable, dead-reference exclusion, NULL-host defer, empty → not_applicable, any-unsupported, all-supported, and the `resources.enabled=false` gate) is covered by a table-driven case, and `partial` appears in mapper output only for `ptr`/`parity`.
