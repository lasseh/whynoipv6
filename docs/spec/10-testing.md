# 10 — Test Plan

**Purpose:** This file is the single source for every test vector, golden fixture, and
integration scenario the implementation must satisfy. Where other spec files state
*acceptance criteria* (properties the code must have), this file owns the concrete
*fixture tables* that prove them — the exhaustive quorum/mapping/classification vectors,
the parity-fixture capture-and-replay plan, the testcontainers integration scenarios,
and the coverage bar per package. An implementer builds the test suite from this file
alone, citing the companion files only for the shapes under test.

**Deliverables:**

- `internal/domain/host_test.go` — `Canonicalize` vector table (§2).
- `internal/consensus/*_test.go` — quorum permutation table + breaker sequences (§3).
- `internal/crawler/observe_test.go` — base/www, conn, ns/mx, resources mapper vectors (§4).
- `internal/crawler/commit_test.go` — anti-flap / commit-machine sequences (§5).
- `internal/domain/classify_test.go` — classification truth-table + flags + gold vectors (§6).
- `internal/api/*_test.go` — `legacyStatus`, `renderChangelog`, shortuuid codec, badge renderer vectors (§7); golden parity + synthetic fixtures (§8).
- `internal/api/testdata/parity/**`, `internal/api/testdata/badge/**` — recorded golden JSON and byte-exact SVG files.
- `internal/postgres/*_integration_test.go`, `internal/crawler/*_integration_test.go` — testcontainers integration suite (§9, §10), all behind the `integration` build tag.
- `testdata/parity/capture.go` (build tag `capture`) — the production recording harness (§8.1).
- Make targets `test`, `test-integration`, `capture-fixtures`, `lint` (§11); per-package coverage bar (§12).

**Companion files:** 05-schema.md (all DDL under test — this file quotes SELECT/UPDATE/INSERT but never CREATE/ALTER), 02-observation-model.md (mapper/quorum semantics the §3–§4 vectors verify), 03-state-machine.md (commit algorithm the §5 sequences verify), 04-lifecycle-scheduling.md (claim query, sweep, lease fence the §9–§10 integration tests exercise), 06-ingest.md (Canonicalize, Tranco/campaign/resource/attribution acceptance the §2/§10 tables prove), 07-api.md (**authoritative** for every endpoint shape, error body, and route the §7–§8 fixtures assert against — this file never restates a response shape, it cites `07-api.md — §N`), 09-ops.md (CI wiring and the consolidated config-key registry — this file names config keys but does not define them), 00-overview.md (canonical sizing constants cited by name).

---

## 1. Strategy and house conventions

### 1.1 Test taxonomy

Three tiers, distinguished by Go build tags so `go test ./...` runs only the first:

| Tier | Build tag | Needs | Runs in |
|---|---|---|---|
| **Unit** (vector tables, pure functions) | none | nothing (no DB, no network) | `make test`, every push |
| **Integration** (real Postgres/TimescaleDB) | `//go:build integration` | Docker (testcontainers) | `make test-integration`, CI + pre-merge |
| **Capture** (record production goldens) | `//go:build capture` | network to `api.whynoipv6.com` | `make capture-fixtures`, manual/one-shot |

Golden replay (comparing the new API against already-recorded `testdata/parity/*.json`)
is a **unit** test — it reads committed files, needs no network, and gates every push.
Only *recording* the goldens needs the network, and that is the `capture`-tagged harness
(§8.1). Fixtures are committed to the repo; the production API is the frozen reference,
so a golden is re-captured only when a maintainer deliberately re-baselines.

### 1.2 House style (all tiers)

- **Table-driven, always.** Each vector table below becomes one `[]struct{...}` with a
  `name` field, iterated under `t.Run(tc.name, ...)`. The `name` is copied verbatim from
  the fixture table's leftmost identifier so a failure names the exact row.
- Standard library `testing` only. No assertion framework, no mocking framework; use
  `google/go-cmp/cmp` for struct/JSON diffs (already a transitive dep) and hand-written
  fakes for seams (the loopback DNS harness in §1.3, the clock in §5.1).
- Deterministic clocks: any code reading wall time takes an injected `now func() time.Time`
  or `time.Time` argument (the mapper and commit already do — 02/03). Tests pass a fixed
  base instant `T0 = 2026-01-01T00:00:00Z` and advance it explicitly; **no test sleeps**.
- `-race` is mandatory on every tier (`make test` sets it); the claim-contention and
  lease-fence tests (§10) are meaningless without it.
- No global state between cases; each `t.Run` builds its own inputs. Integration tests
  get a fresh schema per test via a template-database clone (§9.1), never a shared mutable DB.
- Every fixture file is UTF-8, LF line endings, and ends in a trailing newline; byte-exact
  goldens (badge SVGs §7.4, pinned error bodies §8.3) are compared with `bytes.Equal`, not
  string normalization.

### 1.3 Consensus/mapper test seam (in-process loopback DNS)

`internal/consensus` and the mapper are tested against in-process DNS servers on loopback,
not the live network. `checker.Resolver` is a **concrete struct**, not an interface
(01-engine.md — §6: `NewResolver(upstreams []string)`, `func (r *Resolver) QueryWithRetry(...)`),
so a test drives it by pointing a real `checker.Resolver` at a server the test controls —
there is nothing to "implement". The harness (`internal/consensus/dnstest`, reused by the
mapper tests):

1. Start scripted authoritative DNS servers on `127.0.0.1:0` (ephemeral port) using
   `dns.Server` from `codeberg.org/miekg/dns` with a `dns.Handler` that, per query
   name+type, returns a scripted `(rcode, []dns.RR)` answer or — to simulate a transport
   timeout — never responds (the resolver's per-query deadline then fires). Three servers,
   labelled `cloudflare`, `google`, `quad9`; a fourth backs the bulk resolver used for the
   conditional A lookup (§3.2).
2. Build one `checker.Resolver` per server via `NewResolver([]string{"127.0.0.1:<port-N>"})`.
3. **Decision:** inject the three provider resolvers plus the bulk resolver through a
   test-only unexported constructor in `internal/consensus`,
   `newWithResolvers(cfg Config, providers []*checker.Resolver, bulk *checker.Resolver,
   alert func(context.Context, string), logger *slog.Logger) *Resolver`. The public
   `New` (02-observation-model.md — §2) builds the three per-provider resolvers from the
   pinned `providers` upstreams (02 §2.2) and delegates to `newWithResolvers`; the test calls
   `newWithResolvers` directly with loopback-pointed substitutes. This seam is **required**,
   not optional: the provider upstreams (`1.1.1.1:53`, `8.8.8.8:53`, `9.9.9.9:53`, …) are
   compile-time constants, not config, so a loopback server cannot be reached by address
   substitution through `New`.

These DNS servers are the only I/O seam any consensus or mapper unit test touches.

---

## 2. `Canonicalize(host)` vectors (normative)

Tests `internal/domain/host.go` `Canonicalize` (06-ingest.md — §1; algorithm restated
there). Two tables: accept (output must equal exactly) and reject (must return a non-nil
error wrapping `ErrInvalidHost`; `errors.Is(err, ErrInvalidHost)` must hold). Grep gate
(06 acceptance #1): no other file in the module lowercases a hostname — assert by a
`go vet`-style source scan test that `strings.ToLower` on host input appears only in
`host.go`.

**Accept table** (`raw` → `want`):

| name | raw | want |
|---|---|---|
| trailing_dot | `dnb.no.` | `dnb.no` |
| upper_and_dot | `DNB.no.` | `dnb.no` |
| idn_unicode | `møre.no` | `xn--mre-qla.no` |
| idn_already_punycode | `XN--MRE-QLA.no` | `xn--mre-qla.no` |
| leading_trailing_space | `  dnb.no  ` | `dnb.no` |
| mixed_case | `Example.COM` | `example.com` |
| three_labels | `api.dnb.no` | `api.dnb.no` |
| max_label_63 | `<63×a>.no` | `<63×a>.no` |
| total_len_253 | `<host totalling exactly 253 octets>` | (same, lowercased/punycode) |
| uts46_fold | `ß.example` (sharp-s) | `xn--zca.example` |

**Reject table** (`raw` → `ErrInvalidHost`):

| name | raw | failing step |
|---|---|---|
| empty | `` | 2 (empty after trim) |
| whitespace_only | `   ` | 2 |
| underscore_wildcard | `_wildcard_.ph` | 4 (LDH rejects `_`) |
| empty_label | `a..b` | 4 |
| double_trailing_dot | `dnb.no..` | 1 strips one dot → `dnb.no.` → 4 empty label |
| ipv4_literal | `1.2.3.4` | 5 (`net.ParseIP != nil`) |
| ipv6_bracketed | `[::1]` | 2 (`[` `]` chars) |
| single_label | `localhost` | 5 (≥2 labels) |
| tld_only | `no` | 5 (≥2 labels) |
| over_253 | `<254-octet input>` | 5 (length) |
| label_over_63 | `<64×a>.no` | 5 (label length) |
| has_scheme | `http://x.no` | 2 (`/` and `:`) |
| has_path | `x.no/foo` | 2 (`/`) |
| has_port | `x.no:443` | 2 (`:`) |
| has_at | `a@b.no` | 2 (`@`) |
| has_query | `x.no?a=1` | 2 (`?`) |
| internal_space | `foo bar.no` | 2 (whitespace) |

The 253- and 254-octet inputs are generated in code (`strings.Repeat("a."×k) + "no"`), not
literal, so the length boundary is exact. `xn--`-prefixed A-label input round-trips
unchanged through `ToASCII` (idempotence) — the `idn_already_punycode` and `uts46_fold`
rows together prove both directions.

---

## 3. Consensus quorum vectors (normative)

Tests `internal/consensus` (02-observation-model.md — §2). Quorum is taken over the four
reduced per-resolver symbols `{exists, empty, nxdomain, error}` (timeout and network error
both reduce to `error`; SERVFAIL and REFUSED both reduce to `error`, never to `empty`). The
outcome is one of: a quorum symbol + `QuorumInfo`, or `ErrQuorumInconsistent`, or a plain
error (≤1 valid answer). Rules (02 §2.6): ≥2 valid answers agree → that symbol; ≥2 valid
answers with no two agreeing → `ErrQuorumInconsistent` (observation `inconsistent`); ≤1
valid answer → plain error (observation `error`).

### 3.1 Three-resolver permutation table

Notation: each row scripts the three fakes `(CF, GO, Q9)`. `N` = non-answer (script as
`timeout` unless the row says otherwise). `Agreement` = `"<#answering-agreeing>of<#queried>"`;
`Disagreed` = true iff an **answering** resolver's symbol differs from the quorum symbol.

| name | CF | GO | Q9 | result | Agreement | Disagreed |
|---|---|---|---|---|---|---|
| 3of3_exists | exists | exists | exists | quorum `exists` | 3of3 | false |
| 3of3_empty | empty | empty | empty | quorum `empty` | 3of3 | false |
| 3of3_nxdomain | nxdomain | nxdomain | nxdomain | quorum `nxdomain` | 3of3 | false |
| 2of3_exists_over_empty | exists | exists | empty | quorum `exists` | 2of3 | true |
| 2of3_exists_over_nx | exists | exists | nxdomain | quorum `exists` | 2of3 | true |
| 2of3_empty_over_exists | empty | empty | exists | quorum `empty` | 2of3 | true |
| 2of3_empty_over_nx | empty | empty | nxdomain | quorum `empty` | 2of3 | true |
| 2of3_nx_over_exists | nxdomain | nxdomain | exists | quorum `nxdomain` | 2of3 | true |
| 2of3_nx_over_empty | nxdomain | nxdomain | empty | quorum `nxdomain` | 2of3 | true |
| 2plus1nonanswer_exists | exists | exists | N | quorum `exists` | 2of3 | false |
| 2plus1nonanswer_empty | empty | empty | N | quorum `empty` | 2of3 | false |
| 2plus1nonanswer_nx | nxdomain | nxdomain | N | quorum `nxdomain` | 2of3 | false |
| servfail_is_nonanswer | exists | exists | SERVFAIL | quorum `exists` | 2of3 | false |
| refused_is_nonanswer | empty | empty | REFUSED | quorum `empty` | 2of3 | false |
| noquorum_1_1_1 | exists | empty | nxdomain | `ErrQuorumInconsistent` | — | — |
| noquorum_two_disagree | exists | empty | N | `ErrQuorumInconsistent` | — | — |
| noquorum_exists_nx_N | exists | nxdomain | N | `ErrQuorumInconsistent` | — | — |
| noquorum_empty_nx_N | empty | nxdomain | N | `ErrQuorumInconsistent` | — | — |
| error_1valid_exists | exists | N | N | plain error (obs `error`) | — | — |
| error_1valid_empty | empty | N | N | plain error | — | — |
| error_1valid_nx | nxdomain | N | N | plain error | — | — |
| error_0valid | N | N | N | plain error | — | — |

Assertions per row:
- The reduced symbols land in `QuorumInfo.PerResolver["cloudflare"|"google"|"quad9"]`, with
  `timeout` and `error` kept **split** there for diagnostics even though both reduce to a
  non-answer (02 §2 acceptance #2). `servfail_is_nonanswer`/`refused_is_nonanswer` assert
  the raw rcode string (`"SERVFAIL"`/`"REFUSED"`) is observable in `PerResolver`.
- On a quorum, the returned `AAAAAnswer` (IPs, CNAMEChain, TTL, Rcode) is **byte-identical**
  to the first agreeing resolver in fixed order CF→GO→Q9 — no record-set merging (02 §2
  acceptance #3). Script CF and GO with *different* AAAA record sets that both reduce to
  `exists`; assert the answer equals CF's set exactly.
- Non-globally-routable AAAA (loopback `::1`, link-local `fe80::`, ULA `fc00::/7`) are
  filtered before the symbol is computed: a resolver returning only `::1` reduces to
  `empty`, not `exists`. Add a `nonroutable_reduces_to_empty` row `(only-::1, empty, empty)`
  → quorum `empty`.

### 3.2 Conditional A lookup vectors

The bulk-resolver A query runs **iff** the AAAA quorum symbol is `empty`, and never
otherwise (02 §4 / §2.7). Fake the bulk resolver's A answer independently.

| name | AAAA quorum | scripted A answer | AOutcome | A query issued? |
|---|---|---|---|---|
| a_present | empty | NOERROR, ≥1 A | `a_present` | yes |
| a_absent_noerror | empty | NOERROR, no A | `a_absent` | yes |
| a_absent_nxdomain | empty | NXDOMAIN | `a_absent` (contradiction resolved in domain's favor) | yes |
| a_error_servfail | empty | SERVFAIL | `a_error` | yes |
| a_error_timeout | empty | timeout | `a_error` | yes |
| no_a_on_exists | exists | (unused) | `""` | **no** |
| no_a_on_nxdomain | nxdomain | (unused) | `""` | **no** |
| no_a_on_error | error (0 valid) | (unused) | `""` | **no** |
| no_a_on_inconsistent | no quorum | (unused) | `""` | **no** |

"A query issued?" is asserted by a call-counter on the bulk fake: exactly one A call on the
`empty` rows, zero on all others.

### 3.3 Degraded (2-of-2) and breaker sequences

Behavioral, not single-shot — driven by a scripted sequence of lookups with a fake clock.

- **Provider breaker (02 §2 acceptance #5).** Feed one provider >`failure_rate` (default
  0.50) non-answers over ≥`min_samples` (default 200) within the `window`; assert the
  provider is dropped, `QuorumInfo.Agreement` becomes `2of2` on subsequent lookups, and a
  degraded 2-fanout scores `(exists,exists)→exists 2of2`, `(exists,empty)→ErrQuorumInconsistent`,
  `(exists,N)→plain error`. A canary succeeding `recovery_probes` (default 3) consecutive
  times restores the 3-fanout. Assert a **second** provider is never dropped while one is
  already out (quorum can't degrade below 2-of-2).
- **Fast-lane breaker (02 §2 acceptance #5).** Over the `window`, drive
  `(error+inconsistent)/total > nondefinitive_rate` (0.05) with ≥`min_samples` (500); assert
  `FastLaneSuppressed()` flips true and stays true until the rate holds below `recover_below`
  (0.02) for one full window. This gates 04's 2h/6h pull-ins; the sequence asserts only the
  flag, the scheduling consequence is §5.4.
- **Per-provider rate limit.** Assert the token bucket blocks (does not error) and that
  `consensus.per_provider_qps` acquisitions are serialized; use a fake clock and assert the
  Nth acquire within a second waits rather than exceeding the bucket. Config keys
  `consensus.per_provider_qps`, `consensus.fastlane_breaker.*`, `consensus.provider_breaker.*`
  (registry: 09-ops.md).

Breaker thresholds are read from config in the test (not hardcoded) so a config change
re-tunes without a test edit; the sequence asserts the *behavior at the configured value*.

---

## 4. Mapper vectors (normative)

Tests `internal/crawler/observe.go` `MapObservations` (02-observation-model.md — §7). Every
row of every mapping table below is one case. The mapper is pure; inputs are a synthetic
`checker.ScanResult`, `kind`, `preflightPassedAt`, `now`, `links`, `resourcesEnabled`.

### 4.1 `base` composite vectors (every row of the §2.3.1 base table)

| name | AAAA quorum | A outcome | base observation |
|---|---|---|---|
| base_exists | exists | (not run) | `supported` |
| base_empty_apresent | empty | a_present | `unsupported` |
| base_empty_aabsent | empty | a_absent | `no_record` |
| base_empty_aerror | empty | a_error | `error` |
| base_nxdomain | nxdomain | (not run) | `no_record` |
| base_error | error | (not run) | `error` |
| base_inconsistent | no quorum | (not run) | `inconsistent` |
| base_subdomain_self | exists (on subdomain host) | (not run) | `supported` (kind=subdomain resolves the host itself) |

### 4.2 `www` composite vectors (every row of the §2.3.1 www table)

| name | AAAA quorum | A outcome | www observation |
|---|---|---|---|
| www_exists | exists | (not run) | `supported` |
| www_empty_apresent | empty | a_present | `unsupported` |
| www_empty_aabsent | empty | a_absent | `not_applicable` |
| www_empty_aerror | empty | a_error | `error` |
| www_nxdomain | nxdomain | (not run) | `not_applicable` |
| www_error | error | (not run) | `error` |
| www_inconsistent | no quorum | (not run) | `inconsistent` |
| www_subdomain_forced_na | (any) | (any) | `not_applicable` (kind=subdomain skips www entirely) |

Assert **`www` never yields `no_record`** by construction: no input combination in the table
produces it, and a property test over the full `{exists,empty,nxdomain,error}×{a_present,a_absent,a_error}`
cross-product confirms `no_record ∉ www outputs`.

### 4.3 `conn` composition vectors (every decision-table row, incl. preflight guard)

Tests the conn composer (02-observation-model.md — §5). Inputs: `https_ipv6` result `H`
(status + `error_type`), `http_ipv6` result `P`, `preflightPassedAt` relative to `now`.
`preflightFreshness = 5m` (constant).

| name | H status | H.error_type | P status | preflight | conn obs | source/http_only |
|---|---|---|---|---|---|---|
| conn_https_ok | supported | — | (any) | fresh | `supported` | https / false |
| conn_http_fallback | unsupported | connection_refused | supported | fresh | `supported` | http / **true** |
| conn_cert_error | unsupported | certificate_error | (any) | fresh | `unsupported` | — |
| conn_refused_no_http | unsupported | connection_refused | unsupported | fresh | `unsupported` | — |
| conn_no_aaaa_on_host | unsupported | (no-AAAA) | (any) | fresh | `unsupported` | — |
| conn_timeout_preflight_fresh | error | timeout | (any) | fresh (<5m) | `unsupported` (row 5a) | — |
| conn_timeout_preflight_stale | error | timeout | (any) | stale (>5m) | `error` (row 5b) | — |
| conn_error_other | error | unknown | (any) | fresh | `error` (row 5c) | — |
| conn_phase2_skipped | not_applicable | — | not_applicable | (any) | `not_applicable` | — |
| guard_cert_stale_downgrade | unsupported | certificate_error | (any) | **stale** | `error` (guard downgrades any conn=unsupported when preflight stale) | — |
| guard_refused_stale_downgrade | unsupported | connection_refused | unsupported | **stale** | `error` (guard) | — |

Assertions: the two `guard_*` rows prove the belt-and-suspenders final guard (02 §5) — a
conn=`unsupported` produced by rows 3/4 is downgraded to `error` if preflight is stale.
Row 5a is already preflight-gated inside the table. `error` outcomes (5b/5c) are **never**
overridden by `P=supported`. On `supported` outcomes, assert the `scan_detail.details.conn`
payload carries `source` (`"https"` row 1, `"http"` row 2) and `http_only` (true only row 2);
`http_only` is payload-only and does not become a flag or alter `v6_only`.

### 4.4 `ns` / `mx` / informational vectors (every row of the remaining-dimension table)

| name | dimension | engine status | observation |
|---|---|---|---|
| ns_supported | ns | supported | `supported` |
| ns_partial_to_supported | ns | partial | `supported` (≥1-host rule) |
| ns_unsupported | ns | unsupported | `unsupported` |
| ns_error | ns | error | `error` |
| ns_no_zone_defensive | ns | error (walk-up found no zone) | `error` (never `not_applicable`) |
| mx_supported | mx | supported | `supported` |
| mx_partial_to_supported | mx | partial | `supported` |
| mx_unsupported | mx | unsupported | `unsupported` |
| mx_nullmx | mx | not_applicable | `not_applicable` |
| mx_error | mx | error | `error` |
| ptr_partial_verbatim | ptr | partial | `partial` (raw status stored verbatim) |
| parity_partial_verbatim | parity | partial | `partial` (raw status stored verbatim) |
| smtp_partial_to_unsupported | smtp | partial | `unsupported` (mapped before storage) |
| dnssec_verbatim | dnssec | (raw) | (raw status, no mapping table) |
| spf_verbatim | spf | (raw) | (raw status) |

Assert `partial` appears in mapper output **only** for `ptr`/`parity` (02 §8 acceptance #6);
`ns`, `mx`, `conn`, `resources` never emit `no_record`.

### 4.5 `resources` roll-up vectors (every branch of the §4.6 algorithm)

Inputs: this scan's `conn` observation and the `links []LinkedResource` (each carries a
`*IPv6Status`, nil = host unswept), plus `resourcesEnabled`.

| name | conn obs | links (required host statuses) | resourcesEnabled | resources obs |
|---|---|---|---|---|
| res_conn_error_defer | error | (any) | true | `error` |
| res_conn_inconsistent_defer | inconsistent | (any) | true | `error` |
| res_conn_unsupported_na | unsupported | (any) | true | `not_applicable` |
| res_conn_notapplicable_na | not_applicable | (any) | true | `not_applicable` |
| res_null_host_defer | supported | [supported, NULL] | true | `error` (unswept host defers) |
| res_empty_after_prune_na | supported | [no_record, not_applicable] | true | `not_applicable` (dead refs pruned → empty) |
| res_no_links_na | supported | [] | true | `not_applicable` |
| res_any_unsupported | supported | [supported, unsupported] | true | `unsupported` |
| res_all_supported | supported | [supported, supported] | true | `supported` |
| res_dead_ref_excluded | supported | [supported, no_record] | true | `supported` (no_record excluded, remaining all supported) |
| res_disabled_gate | supported | [unsupported] | **false** | `not_applicable` (phase gate; `ResourcesExcluded=true`) |

When `resourcesEnabled=false`, assert the mapper sets `ResourcesExcluded=true` so the commit
excludes the dimension (03 mechanism), and `resources=not_applicable` on the scan row. `error`
never advances pending (proven in §5.5 sequences). Links with `required=FALSE` are excluded
from the roll-up input by the §6 query (`WHERE dr.required`) — an integration concern (§10.2).

---

## 5. Anti-flap / commit-machine sequences (normative)

Tests `internal/crawler` commit unit (03-state-machine.md — §5) as a *pure state transition*:
given a claimed snapshot `S`, an `Observations` set `O`, timestamp `T`, and lease `L`, the
commit computes the next state + changelog rows without touching the DB (the DB write is an
integration concern, §10). Each sequence is a list of `(Δt, observation)` steps applied to a
starting state; after each step assert `d_status`, `d_pending`, `d_pending_count`, `d_since`,
whether a changelog row was emitted, and `last_counted_at`. `min_confirm_spacing = 12h`
(config `anti_flap.min_confirm_spacing`, registry: 09-ops.md); `N(base/www/ns/mx)=2`,
`N(conn/resources)=3`.

Convention: `T0 = 2026-01-01T00:00:00Z`; "spaced" = +12h or more since `last_counted_at`;
"close" = <12h. Steps list the dimension under test; other dimensions hold a stable
definitive value so classification is well-defined.

### 5.1 Bootstrap (first definitive commit)

| step | Δt | base obs | counting? | d_status after | d_pending | count | changelog? |
|---|---|---|---|---|---|---|---|
| 1 | T0 | supported | yes (last_counted_at NULL) | supported | NULL | 0 | **no** (first-confirmation rule) |

`d_since` = T0; `last_counted_at` = T0. A NULL→value transition **never** writes a changelog
row (03 §11; 07-api R1 downstream). Repeat once each for a `www`, `ns`, `mx` bootstrap and a
`conn` bootstrap (conn also commits immediately on first definitive value despite N=3).

### 5.2 N=2 flip (base), correctly spaced

Start: `base=supported` confirmed at T0, `last_counted_at=T0`.

| step | Δt | obs | counting? | d_status | d_pending | count | changelog? |
|---|---|---|---|---|---|---|---|
| 1 | +24h | unsupported | yes | supported | unsupported | 1 | no |
| 2 | +48h | unsupported | yes (spaced) | **unsupported** | NULL | 0 | **yes** old=supported new=unsupported |

Changelog row `ts=+48h`, `field=base`, `old=supported`, `new=unsupported`; `d_since=+48h`.

### 5.3 Counting-gate spacing (close rechecks do not count)

Start: `base=supported` at T0. Simulates 2h fast-lane rechecks producing many scans inside a
12h window — none may advance the confirmation.

| step | Δt | obs | counting? (T−last_counted ≥12h) | d_pending | count | changelog? |
|---|---|---|---|---|---|---|
| 1 | +24h | unsupported | yes | unsupported | 1 | no |
| 2 | +26h | unsupported | **no** (2h since last_counted) | unsupported | 1 (unchanged) | no |
| 3 | +28h | unsupported | **no** (4h) | unsupported | 1 | no |
| 4 | +37h | unsupported | yes (≥12h since +24h) | — → flip | 0 | **yes** |

Proves the confirmed flip cannot happen faster than `(N−1)×12h` even under 2h rechecks (03
§17 acceptance #1). The non-counting scans **still write** their scan + scan_detail rows and
update `d_observed` (§5.6).

### 5.4 N=3 flip (conn / resources)

Start: `conn=supported` at T0.

| step | Δt | obs | counting? | d_pending | count | changelog? |
|---|---|---|---|---|---|---|
| 1 | +24h | unsupported | yes | unsupported | 1 | no |
| 2 | +48h | unsupported | yes | unsupported | 2 | no |
| 3 | +72h | unsupported | yes | — → **flip** | 0 | **yes** old=supported new=unsupported |

Repeat the identical sequence for `resources`. A third close (non-spaced) scan between steps
must not advance the count (compose with §5.3's gate).

### 5.5 Error / inconsistent interleaving (non-definitive touches nothing)

Start: `base=supported` at T0.

| step | Δt | obs | effect |
|---|---|---|---|
| 1 | +24h | unsupported | pending=unsupported, count=1 |
| 2 | +48h | error | **nothing changes**: pending=unsupported count=1, no changelog, `d_status` intact, `d_since` intact; `d_observed=error` still updated; scan/scan_detail still written |
| 3 | +72h | inconsistent | same — pending survives |
| 4 | +96h | unsupported | count=2 → **flip**, changelog old=supported new=unsupported |

Proves `error`/`inconsistent` never modify status/pending/since/classification/flags/gold/
attribution but the scan rows are still written and `d_observed` updates (03 §17 acceptance #3).
For `conn`, add a companion showing an `error` conn observation does not trigger a `recheck_error`
pull-in (only base/www drive pull-ins — 04). For `resources`, an `error` roll-up (unswept host
or conn deferral) never advances `resources_pending` (02 §6).

### 5.6 Different-value reset (pending replaced, not incremented)

Start: `base=supported` at T0.

| step | Δt | obs | pending | count |
|---|---|---|---|---|
| 1 | +24h | unsupported | unsupported | 1 |
| 2 | +48h | no_record | no_record (replaced) | 1 (reset) |
| 3 | +72h | no_record | — → flip to no_record | 0, changelog old=supported new=no_record |

A new value that differs from both `d_status` and `d_pending` resets `d_pending=O, count=1`
(03 §5 else-branch).

### 5.7 Step-R dead recovery

Start: domain `disabled=true, disabled_reason='dead'`, all core `d_status` still holding their
pre-death confirmed values, `dead_streak=0`.

| step | Δt | base obs | effect |
|---|---|---|---|
| 1 | +30d | supported (definitive) | **Step R fires before applying**: clear disabled/reason/at, dead_streak=0, every core `d_status/d_observed/d_pending/d_pending_count/d_since` → NULL, informational cols → NULL, classification=`unknown`, class_flags=`{}`, gold=false — **no changelog for the reset**. Then this scan's `base=supported` flows through the normal loop against NULL → commits immediately, `base_status=supported`, **no changelog** (first-confirmation). |

Assert: a domain returning from the dead reappears with a fresh status and a *clean* changelog
(no reset row, no bootstrap row) (03 §17 acceptance #2, #7). Recovery only fires when the base
observation is **definitive** and `disabled_reason='dead'`; an `error` base on a dead row leaves
it disabled.

### 5.8 Dead-streak trigger

Start: enabled domain. Feed `dead_streak` unresolvable scans (03 §4 dead-signal: either (a)
apex A+AAAA both NXDOMAIN and NS walk finds no zone, or (b) all 3 consensus resolvers returned
explicit SERVFAIL/REFUSED for apex AAAA after retry — timeouts do **not** count).

| step | scans 1–6 | scan 7 |
|---|---|---|
| unresolvable each | dead_streak increments 1..6, not disabled | dead_streak reaches `lifecycle.dead_streak` (7) → `disabled=true, disabled_reason='dead'`, both streaks reset to 0 |

Assert **seven and never fewer** (03 §17 acceptance #7); one resolvable scan anywhere in the
run resets `dead_streak=0`. Add a `timeout_not_dead` case: three apex-AAAA timeouts (not
SERVFAIL) never increment `dead_streak`.

### 5.9 Lease fence (pure-side assertion)

The commit's fenced UPDATE `WHERE id = $id AND claimed_at = $L` (03 §12) matching 0 rows means
the whole transaction is discarded. At the pure layer, assert the commit builder produces the
UPDATE with the lease predicate and that a `RowsAffected==0` result maps to "write NOTHING +
increment `lease_lost`". The real two-worker race is the integration test §10.4.

---

## 6. Classification truth-table vectors (normative)

Tests `internal/domain/classify.go` `Classify` (03-state-machine.md — §10). Pure function over
confirmed values; each ∈ `{supported, unsupported, no_record, not_applicable, NULL}`. First
match wins. Two outputs: `classification` and, independently, the five flags + `gold`.

### 6.1 Classification (every truth-table row + edges)

| name | base | www | ns | conn | mx | classification |
|---|---|---|---|---|---|---|
| unknown_base_null | NULL | (any) | (any) | (any) | (any) | `unknown` |
| inactive_norecord | no_record | (any) | (any) | (any) | (any) | `inactive` |
| sinner_unsupported | unsupported | (any) | (any) | (any) | (any) | `sinner` |
| hero_all_supported | supported | supported | supported | supported | supported | `hero` |
| hero_www_na | supported | not_applicable | supported | supported | supported | `hero` |
| hero_www_norecord | supported | no_record | supported | supported | supported | `hero` |
| hero_mx_na | supported | supported | supported | supported | not_applicable | `hero` |
| partial_conn_null | supported | supported | supported | NULL | supported | `partial` |
| partial_conn_na | supported | supported | supported | not_applicable | supported | `partial` |
| partial_ns_null | supported | supported | NULL | supported | supported | `partial` |
| partial_www_unsupported | supported | unsupported | supported | supported | supported | `partial` |
| partial_conn_unsupported | supported | supported | supported | unsupported | supported | `partial` |
| partial_mx_unsupported | supported | supported | supported | supported | unsupported | `partial` |

Edges to assert explicitly (03 §17 acceptance #8): `base=no_record` → `inactive` regardless
of all other dimensions (even if every other dim is `supported`); `www=no_record` counts
toward hero (row `hero_www_norecord`); `conn=NULL` → `partial` with **no flag**; `not_applicable`
and NULL never *shame* (never trigger `sinner`) and never satisfy hero unless the rule lists
them (www/mx list `not_applicable`; ns/conn do not).

### 6.2 Flags (independent of classification)

| name | conn | www | ns | mx | resources | flags set |
|---|---|---|---|---|---|---|
| flag_none_all_supported | supported | supported | supported | supported | supported | (none) |
| flag_broken_v6 | unsupported | supported | supported | supported | supported | `broken_v6` |
| flag_www_missing | supported | unsupported | supported | supported | supported | `www_missing` |
| flag_ns_missing | supported | supported | unsupported | supported | supported | `ns_missing` |
| flag_mail_missing | supported | supported | supported | unsupported | supported | `mail_missing` |
| flag_resources_v4only | supported | supported | supported | supported | unsupported | `resources_v4only` |
| flag_null_no_flag | NULL | NULL | NULL | NULL | NULL | (none) |
| flag_na_no_flag | not_applicable | not_applicable | not_applicable | not_applicable | not_applicable | (none) |
| flag_norecord_no_flag | supported | no_record | supported | no_record | no_record | (none) |
| flag_all_five | unsupported | unsupported | unsupported | unsupported | unsupported | all 5 |

A flag is set **only** when the named dimension is confirmed `unsupported`; NULL,
`not_applicable`, and `no_record` set no flag. Flags are computed for every domain regardless
of classification — assert a `sinner` (base=unsupported) with `conn=unsupported` carries
`broken_v6`.

### 6.3 Gold

| name | classification | resources | gold |
|---|---|---|---|
| gold_hero_res_supported | hero | supported | **true** |
| gold_hero_res_na | hero | not_applicable | **true** |
| gold_hero_res_unsupported | hero | unsupported | false |
| gold_hero_res_null | hero | NULL | false |
| gold_partial_res_supported | partial | supported | false |

`gold = classification==hero AND resources ∈ {supported, not_applicable}`; NULL resources →
not gold (03 §17 acceptance #8). Compose with the phase gate: while `crawler.resources.enabled=false`
`resources_status` is NULL for everyone, so **no domain is gold** (03 §17 acceptance #9,
integration-verified in §10.2).

---

## 7. API serialization vectors (normative)

Tests the pure serialization helpers in `internal/api`. Shapes are authoritative in
07-api.md — these tables fix the *values*.

### 7.1 `legacyStatus` projection (07-api.md — §2.8 R1)

| name | input `*ipv6_status` | output string |
|---|---|---|
| nil_never_confirmed | nil | `no_record` |
| not_applicable | `not_applicable` | `no_record` |
| supported | `supported` | `supported` |
| unsupported | `unsupported` | `unsupported` |
| no_record | `no_record` | `no_record` |

Negative invariant (property test over the enum + nil): `legacyStatus` output is always one of
exactly `{"supported","unsupported","no_record"}` and **never** `not_applicable`, `error`,
`inconsistent`, `unknown`, or empty string. Any future enum value must map or the test fails.

### 7.2 `renderChangelog` ladder (07-api.md — §3.11; 16 rows)

`renderChangelog(field, old, new, host) → (message, ipv6_status)`, defined only for
`field ∈ {base,www,ns,mx}`, `old,new ∈ {supported,unsupported,no_record}`, `old ≠ nil`.
`host = "dnb.no"`; for `field=www` the rendered name is `www.dnb.no`. `ipv6_status` is always
`new`.

| name | field | old→new | message |
|---|---|---|---|
| base_enabled_from_unsup | base | unsupported→supported | `IPv6 enabled for dnb.no` |
| base_enabled_from_norec | base | no_record→supported | `IPv6 enabled for dnb.no` |
| base_lost | base | supported→unsupported | `IPv6 lost for dnb.no` |
| base_v4only | base | no_record→unsupported | `IPv4-only for dnb.no` |
| base_no_dns | base | supported→no_record | `No DNS records found for dnb.no` |
| www_enabled | www | unsupported→supported | `IPv6 enabled for www.dnb.no` |
| www_lost | www | supported→unsupported | `IPv6 lost for www.dnb.no` |
| www_v4only | www | no_record→unsupported | `IPv4-only for www.dnb.no` |
| www_no_dns | www | supported→no_record | `No DNS records found for www.dnb.no` |
| ns_enabled | ns | unsupported→supported | `IPv6 enabled nameserver for dnb.no` |
| ns_degraded | ns | supported→unsupported | `Nameservers degraded to IPv4-only for dnb.no` |
| ns_v4only | ns | no_record→unsupported | `IPv4-only nameservers for dnb.no` |
| ns_no_ns | ns | supported→no_record | `No NS records found for dnb.no` |
| mx_enabled | mx | unsupported→supported | `IPv6 enabled MX records for dnb.no` |
| mx_degraded | mx | supported→unsupported | `MX records degraded to IPv4-only for dnb.no` |
| mx_v4only | mx | no_record→unsupported | `IPv4-only MX records for dnb.no` |
| mx_no_mx | mx | supported→no_record | `No Mail records found for dnb.no` |

That is the four "enabled" rows (each covering both `unsupported→supported` and
`no_record→supported`) plus 12 others = the 16 distinct messages. Assert the two enabled
variants per field render the identical string (the ambiguous-old collapse the phase-4 importer
relies on, §8.5). Plus two passthrough rows: `field='legacy'` returns `(legacy_message,
legacy_status)` verbatim, bypassing the ladder.

**Coverage-filter proof (07-api.md — §3.12 / R5):** a property test feeds the ladder's input
domain the full cross-product of `field ∈ {base,www,ns,mx,conn,resources}` × transitions incl.
`not_applicable`, then asserts the SQL filter (quoted from 07) `WHERE old_value IS NOT NULL AND
field IN ('base','www','ns','mx') AND old_value IN (...) AND new_value IN (...)` admits exactly
the 16-row domain (plus `field='legacy'`), and that a row whose `(old,new)` collapse to the same
string under R1 (e.g. `not_applicable`→`no_record` where the other side is already `no_record`)
is omitted.

### 7.3 shortuuid codec (07-api.md — §2.7)

Round-trip both directions; encode output exactly 22 chars.

| name | canonical UUID | token |
|---|---|---|
| live_campaign_1 | `baff94c3-c4b2-4f19-be66-3247250f7868` | `bHTMghm9txZFhwMKVCiBey` |
| live_campaign_2 | `9b587e73-7694-46f7-b3dc-96f6a1c15317` | `VeT2mCvhzny4kAiQ9oLe2r` |
| zero_uuid | `00000000-0000-0000-0000-000000000000` | `2222222222222222222222` |

For each: `encodeUUID(uuid) == token` (len 22) **and** `decodeUUID(token) == uuid`. Negative
vectors (both → `404 {"error":"Invalid UUID"}` via the path-param handler, exact body):

| name | path param | reason |
|---|---|---|
| garbage_token | `not-a-token` | `-` outside base57 alphabet |
| raw_uuid_in_path | `baff94c3-c4b2-4f19-be66-3247250f7868` | raw UUID not accepted (`-` outside alphabet) |

Pin the version: `github.com/lithammer/shortuuid/v4` (v4.x); a v3 import would produce
variable-length tokens and fail the len-22 assertion — the test *is* the version guard.

### 7.4 Badge renderer golden SVGs (07-api.md — §5)

Six byte-exact golden files `internal/api/testdata/badge/{gold,supported,partial,unsupported,inactive,unknown}.svg`,
each rendered from the geometry table (07-api.md — §5; `W,MW,TX,TL` per message) with the
message color from the selection table. The renderer is a pure `(classification, gold, disabled)
→ []byte`; assert `bytes.Equal` against the golden. Classification→badge mapping vectors:

| name | row (classification, gold, disabled) | message | color |
|---|---|---|---|
| badge_gold | hero, gold=true, false | `gold` | `#d4af37` |
| badge_supported | hero, gold=false, false | `supported` | `#4c1` |
| badge_partial | partial, —, false | `partial` | `#dfb317` |
| badge_unsupported | sinner, —, false | `unsupported` | `#e05d44` |
| badge_inactive | inactive, —, false | `inactive` | `#9f9f9f` |
| badge_unknown_class | unknown, —, false | `unknown` | `#9f9f9f` |
| badge_unknown_norow | (no row) | `unknown` | `#9f9f9f` |
| badge_unknown_disabled | (any), —, **disabled=true** | `unknown` | `#9f9f9f` |

The three `unknown`-message rows (no row, unknown classification, disabled) all render the
**same** `unknown.svg` byte-for-byte. Handler-level badge tests (input handling, 200-always,
`.svg`-less 404, invalid-host 400) are in §10.6.

---

## 8. Parity-fixture plan

Golden fixtures prove the new API is byte/JSON-compatible with the frozen production API for
every legacy endpoint, plus synthetic fixtures for the branches production can never emit.
Endpoint **shapes** are authoritative in 07-api.md — this section owns the fixture files and the
capture/replay mechanism, never a response shape.

### 8.1 Capture method (named)

Production `https://api.whynoipv6.com` is the live frozen reference. The recorder is a
`//go:build capture` test, `testdata/parity/capture.go`, run by `make capture-fixtures`:

1. For each legacy endpoint in 07-api.md — §2.1 route inventory (classes marked **L**), issue
   an HTTP `GET` against `$WHYNOIPV6_PROD_BASE` (default `https://api.whynoipv6.com`) using a
   fixed, curated set of **stable** path params (below) and record the raw response body +
   status + `Content-Type` prefix into `testdata/parity/<slug>.golden.json`, where `<slug>`
   encodes method+path+params. Each golden file carries a header comment with the production
   handler `file:line` (the `x-production-source` from 07-api.md — §8), so a capture is
   mechanically re-runnable and auditable.
2. Curated stable params (chosen because they resolve on the production DB and are unlikely to
   churn): domain `dnb.no` (a known hero/partial), a known sinner from the `top_shame` list
   (`github.com` per 07-api.md history-import list), country `NO`, a live campaign UUID
   `bHTMghm9txZFhwMKVCiBey` (from §7.3), an ASN with v6 domains, and the garbage query
   `zzzzqqqq` for zero-result rows.
3. Goldens are committed. The production API is not called again unless a maintainer re-baselines
   (`make capture-fixtures`); the diff on re-baseline is reviewed like any code change.

Replay is a plain unit test (`internal/api/parity_test.go`, no build tag): spin the new API
handler over a **seeded** integration DB (§9) loaded with fixture rows chosen to reproduce the
captured entities, issue the same request, and compare.

### 8.2 Comparison modes (per 07-api.md pins)

- **JSON-equal** (default): decode both bodies to `any`, `cmp.Diff` — key order and whitespace
  ignored. Used for all list/detail endpoints.
- **byte-equal**: exact bytes. Used only where 07 pins it (badge SVGs §7.4; the pinned 404/error
  bodies §8.3). `GET /` health is the one documented JSON-equal-not-byte-equal exception (07-api.md
  — §3.1: production emitted `{"message": "ok"}` with a space; the new backend emits canonical JSON).
- **Content-Type**: assert the media-type prefix only (`application/json`), never the `charset`
  parameter (07-api.md — §1 decision).
- **Additive-key strip**: `GET /domain/{domain}` comparisons strip the two additive keys
  (`subdomains`, `subdomain_count`) before diffing against the production capture (07-api.md — §3.6).

One golden replay case per **L** endpoint in the route inventory (07-api.md — §2.1), source
file named per endpoint in 07-api.md — §3.

### 8.3 Zero-result 404 / `[]`-cleanup fixtures (07-api.md — §2.11)

One fixture per row of the kept-404 table (07-api.md — §2.11), asserting status **and** exact
body (byte-equal), because production's capitalization differs per endpoint:

| endpoint (garbage input) | status | exact body |
|---|---|---|
| `GET /domain/search/zzzzqqqq` | 404 | `{"error":"no domains found"}` |
| `GET /campaign/search/zzzzqqqq` | 404 | `{"error":"No domains found"}` (capital N) |
| `GET /campaign/{valid}?offset=10000` | 404 | `{"error":"Campaign not found"}` |
| `GET /campaign/{valid}/nosuchhost.example` | 404 | `{"error":"Domain not found"}` |
| `GET /changelog/nosuchhost.example` | 404 | `{"error":"No changelog entries found for nosuchhost.example"}` |
| `GET /changelog/campaign/{valid-uuid-no-rows}` | 404 | `{"error":"No changelog entries found for campaign <decoded-36-char-uuid>"}` |
| `GET /changelog/campaign/{uuid}/{domain-no-rows}` | 404 | `{"error":"No changelog entries found for campaign <shortuuid-as-given> and domain <raw-domain>"}` |

Plus the `[]`-cleanup rows returning `200 []` on zero rows (each endpoint in the 07-api.md — §2.11
list), and explicitly `GET /metric/asn/search/zzzzqqqq → 200 []`. The 404-message endpoints assert
which uuid form appears: `/changelog/campaign/{uuid}` echoes the **decoded** canonical 36-char
UUID; `/changelog/campaign/{uuid}/{domain}` echoes the **shortuuid as given**; the echoed domain
is the **raw** path param, not the canonical form (07-api.md — §2.5).

### 8.4 Synthetic fixtures — branches production never produces

Production only ever emitted `supported|unsupported|no_record` and never the 4-value enum's
`not_applicable`/`error`/`inconsistent`, so these are hand-built DB states + expected responses,
keyed to the 07-api.md rule they prove:

- **R1 projection (07-api.md — §2.8):** a `domain` row with `www_status='not_applicable'`,
  `mx_status=NULL` → `GET /domain/{domain}` serializes both as `"no_record"`; a campaign
  composite row and a changelog `ipv6_status` likewise. Assert no legacy body ever contains the
  strings `not_applicable`, `error`, `inconsistent`, `unknown`.
- **R2 log filter (07-api.md — §2.8):** seed `scan` rows for one domain mixing definitive and
  `error`/`inconsistent` per-dimension values; `GET /domain/{domain}/log` returns **only** the
  rows where none of base/www/ns/mx is `error`/`inconsistent` (quote the R2 SELECT from 07),
  each surviving field passed through R1, `id = extract(epoch from ts)::bigint`, capped at 90.
  Same for `GET /campaign/{uuid}/{domain}/log`.
- **R3 timestamp mapping (07-api.md — §2.8):** a domain with `base_since=NULL` serializes
  `ts_aaaa: "0001-01-01T00:00:00Z"` (Go zero time), no fallback substitution; each `ts_*` key
  maps to its source column per the R3 table.
- **R4 `v6_ready` (07-api.md — §2.8):** a subdomain-heavy campaign member with
  `www_status='not_applicable'` counts as `v6_ready` when `base='supported' AND ns='supported'`;
  a member with `www_status=NULL` does **not**. Assert `GET /campaign` `count`/`v6_ready` and the
  composite `{campaign}` object.
- **Membership synthetics (07-api.md — §9 #6):** an entity confirmed `base=supported,
  www=unsupported` appears in `GET /domain/almost` (with `www_missing` in `class_flags`) and
  **not** in `GET /domain`; one confirmed `base=unsupported` appears in `GET /domain` and not in
  `/domain/almost`. Repeat for the `/country/{code}/sinners` vs `/country/{code}/heroes` pair.
- **Visibility synthetics (07-api.md — §9 #7):** a `disabled=true` domain appears on **no** list,
  feed, stats, or search response and 404s on `GET /domain/{domain}`; a `disabled=true` campaign
  404s on every UUID route and vanishes from `GET /campaign` and `GET /changelog/campaign`; a
  `rank IS NULL` entity never appears on a ranked list but resolves on `GET /domain/{domain}`.
- **RealIP / CORS / rate-bucket trio (07-api.md — §1.8):**
  1. `GET /ip` with header `X-Real-IP: 2001:db8::7` → body `{"ip":"2001:db8::7"}` (not `::1`) —
     guards the frontend IPv4-banner check and the §5.3 per-IP bucket.
  2. `OPTIONS /check` with `Origin: https://whynoipv6.com` and
     `Access-Control-Request-Method: POST` → 2xx with `Access-Control-Allow-Origin` set and
     `POST` in `Access-Control-Allow-Methods`.
  3. Two `POST /check` with different `X-Real-IP` values consume **different** rate-limit
     buckets (one exhausting its bucket does not 429 the other).
- **`GET /metric/overview` (07-api.md — §3.17):** built from the latest `stats_global_daily` row,
  array-of-one, all eight `data` keys present, `time` = the row's `day` as midnight-UTC RFC3339;
  a fixture proving the seed day-0 row makes the endpoint non-empty on first boot.

### 8.5 Changelog byte-equality import gate (phase-4)

The single most important parity gate (08-migration-cutover.md — §6.6 / §11 Gate G1 defines it
and delegates the fixture mechanics here; the forward render is 07-api.md — §3.11
`renderChangelog`). Fixture: a set of production `changelog`/`campaign_changelog` rows
`(message, ipv6_status)` covering every production message string (the 16 forward strings +
their ambiguous-old collapses + at least one unmappable row). Run the importer's reverse-map +
cross-check, then for **every** imported row assert:

```
renderChangelog(field, old_value, new_value, host)  ==  (original message, original ipv6_status)   // byte-equal
```

or, for rows routed to the escape hatch, the `field='legacy'` passthrough
`(legacy_message, legacy_status)` byte-equals the original. Specific cases:

- Each of the 16 forward messages reverse-maps to the pattern's `(field, old, new)` and re-renders
  identically; the two `IPv6 enabled for …` and the `No … records found` collapses both re-render
  their single string regardless of which `old` the importer canonically recorded (`old='unsupported'`
  by rule) — the ambiguous-old render-safety proof.
- A row whose stored `ipv6_status` disagrees with the derived `new_value`, a row matching no
  pattern, and a row with `ipv6_status` outside the three legacy strings each route to
  `field='legacy'` and render verbatim.
- A PK collision `(domain_id, ts, field)` between the two source tables: value-identical → one kept;
  differing → `ts` bumped +1µs until unique.

This gate is what makes phase-4's "old entries render identically" verification unconditional.

---

## 9. Integration harness (testcontainers + TimescaleDB)

All integration tests are `//go:build integration` and run under `make test-integration`. CI
wiring for this target is 09-ops.md's concern (this file does not own it).

### 9.1 Container and schema lifecycle

- Image: `timescale/timescaledb:latest-pg18` (rolling tag, never pinned — global rule; matches
  05-schema.md — §2). One container per test binary via `testcontainers-go`, started in
  `TestMain`, torn down after.
- Schema per test: `TestMain` applies migrations `000001→000003` once to a **template**
  database (05-schema.md — §3–§5); each test `t` clones it with `CREATE DATABASE t_<name>
  TEMPLATE <template>` and connects a fresh pool, so tests never share mutable state and run in
  parallel. (This `CREATE DATABASE` is a test-harness clone of an existing template, not schema
  DDL — the schema DDL lives only in 05-schema.md.)
- Every DDL assertion cites 05-schema.md; this file quotes only SELECT/UPDATE/INSERT.

### 9.2 Migration up/down (05-schema.md — §13 acceptance #1, #2)

1. `v6ctl migrate up` on a fresh container applies `000001→000003` green; `migrate version`
   reports 3, not dirty; a second `up` is a no-op.
2. `timescaledb_information.hypertables` lists exactly **6** hypertables;
   `SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name IN
   ('policy_compression','policy_retention','policy_refresh_continuous_aggregate')` = **10**
   = **5** columnstore (`scan`, `scan_detail`, `changelog`, `stats_asn_daily`, and the
   `scan_daily_adoption` materialization hypertable — columnstore jobs keep the legacy
   `proc_name='policy_compression'`) + 4 retention + 1 cagg-refresh (the `proc_name` filter
   excludes the built-in TimescaleDB telemetry job; 05-schema.md — §4);
   `timescaledb_information.continuous_aggregates` lists `scan_daily_adoption`.
3. `SELECT count(*) FROM country` = 251; sentinel rows `asn.number = 0` and `country.code = 'UN'`
   return exactly one each; `SELECT count(*) FROM stats_global_daily` = 1 (seed day-0 row).
4. `migrate down` to 0 then `up` again is green (down migrations are reversible).
5. CHECK-constraint negatives (05-schema.md — §13 #6): inserting a native `changelog` row with
   `old_value NULL`, or a `field='legacy'` row with `legacy_message NULL`, fails.
6. `sqlc generate` runs clean against the three up-files and the full `db/query/` set; generated
   code compiles (a `go build ./...` gate, not a runtime test).

### 9.3 Claim-plan gate (05-schema.md — §13 acceptance #5; 04 — §3)

On a seeded fixture of ≥100k `domain` rows with a realistic `next_check_at` spread, `EXPLAIN`
the claim query (quoted from 04-lifecycle-scheduling.md — §3) and assert the plan is an **index
scan on `idx_domain_due`** bounded by `next_check_at <= now()` followed by a top-N heapsort on
`(rank NULLS LAST, next_check_at)` — never a sequential scan, never an inner
`ORDER BY next_check_at LIMIT k` pre-filter (04 warns this silently flips fall-behind policy).
Assert conversely that no ranked-list endpoint plan uses `idx_domain_due`.

---

## 10. Integration scenarios (behavioral, against real DDL)

### 10.1 Commit machine against real DDL (03 — §5; 05 — scan/scan_detail/changelog/domain)

Drive the §5 pure sequences through the **real** `pgx.Batch`-in-`pgx.Tx` write unit against a
live schema and assert the persisted rows:

- After a bootstrap commit: one `scan` row, one `scan_detail` row, `domain.base_status` updated,
  `domain.updated_at` bumped, **zero** `changelog` rows.
- After an N=2 flip: exactly one `changelog` row `(domain_id, ts, field, old, new)`, `ts` equal to
  the scan `ts`, and `changelog.ts` joins a `scan` row with the same `(domain_id, ts)` (03 §17 #5).
- `changelog.old_value`/`new_value` never NULL and never equal on native rows (03 §17 #5).
- **Idempotent replay** (03 §17 #6): replaying an identical commit unit with the same `T` after a
  simulated mid-flight retry produces no duplicate `scan`/`scan_detail` (the `ON CONFLICT
  (domain_id, ts) DO NOTHING`), and cannot double-write changelog (the fence forbids a second
  successful domain UPDATE for the same lease).
- The commit UPDATE bumps `domain.updated_at`; the claim stamp does **not** (05 §13 #7 / §9 rule).

### 10.2 Resource roll-up + link maintenance (02 §6; 04 sweep; 06 §9 acceptance #8, #9)

- **`dependent_count` invariant (property test):** apply arbitrary interleavings of discovery
  link-insert / `last_seen`-refresh / 30-day prune (discovery statements A–C from 06-ingest.md —
  §5) and assert after each that `resource_host.dependent_count` equals
  `SELECT count(*) FROM domain_resource WHERE resource_host_id = X` (03 §17 #10). `+1` only on a
  genuine insert, `-1` only on a prune delete.
- Manual links (`source='manual'`) survive prune; a discovered link on a manual pair upgrades to
  `manual`, never downgrades; `required=FALSE` links are excluded from the roll-up query input.
- **Sweep confirmation machine (06 §9 #9):** NULL→first-definitive `aaaa_status` commits
  immediately; a status change requires exactly **2** consecutive definitive sweeps; a
  non-definitive sweep (timeout/SERVFAIL) changes nothing except bumping `next_check_at = now()+2h`;
  a definitive sweep sets `next_check_at = now()+24h`; hosts never write changelog rows.
- **Phase gate (03 §17 #9):** with `crawler.resources.enabled=false`, every `scan.resources` row
  is `not_applicable`, all `domain.resources_*` columns stay NULL, and no domain is `gold`; flip
  to true and one clean scan after the first sweep confirms resources via the first-observation rule.

### 10.3 Lifecycle sweep (04 — §8; design §2.6 step 1)

Seed rows and run the daily lifecycle-sweep transaction; assert the set-based outcome:

| name | starting row | after sweep |
|---|---|---|
| linked_campaign_clears_orphan | rank NULL, member of an **enabled** campaign, `orphaned_at` set | `orphaned_at = NULL` (linked) |
| disabled_campaign_no_linkage | rank NULL, member of a **disabled** campaign only | treated as unlinked → `orphaned_at` set/kept |
| child_clears_orphan | rank NULL, has a child (`parent_id` points at it) | `orphaned_at = NULL` |
| recent_livecheck_clears | rank NULL, `last_requested_at` within `live_check_linkage` (168h) | `orphaned_at = NULL` |
| livecheck_expired_delist | rank NULL, `created_by='live_check'`, unlinked, `last_requested_at` aged out | disabled immediately `reason='delisted'`, `next_check_at=now()+slow_lane_every` (no 30-day grace) |
| other_unlinked_grace | rank NULL, unlinked, not live_check, `orphaned_at` just now | `orphaned_at=now()`, still scanned normally |
| other_unlinked_disable | rank NULL, unlinked, `orphaned_at < now()-delist_grace` (720h) | disabled `reason='delisted'` |
| ranked_never_orphaned | `rank IS NOT NULL` | `orphaned_at=NULL`, untouched |

Config keys `lifecycle.live_check_linkage`, `lifecycle.delist_grace`, `lifecycle.slow_lane_every`,
`lifecycle.dead_streak` (registry: 09-ops.md). Assert the sweep is the **single owner** of
`orphaned_at` — Tranco import and campaign sync never set it (06 acceptance).

Re-entry (06 §9 #5): a `delisted` row appearing in today's Tranco list re-enables with
`next_check_at=now()`; a `dead` row stays disabled with `next_check_at=now()`; `service`/`manual`
rows only get rank updates and never auto-re-enable.

### 10.4 Claim contention + lease-fence chaos (04 §3; 03 §12)

Both need `-race` and two concurrent workers against one live DB.

- **Claim contention:** two goroutines each run the claim UPDATE (quoted from 04-lifecycle-scheduling.md
  — §3) in a tight loop against a seeded due-set; assert every claimed `domain.id` is claimed by
  **exactly one** worker (disjoint id sets, no row claimed twice) — the `FOR UPDATE SKIP LOCKED`
  proof — and that the union eventually covers the whole due-set.
- **Lease-fence chaos:** worker A claims a batch (lease `L_A`). Before A commits, force a reclaim:
  advance the row's `claimed_at` past the 30-minute lease and let worker B claim it (lease `L_B`),
  scan it, and commit. Now A attempts its commit with the stale `L_A`: the fenced UPDATE
  `WHERE id=$id AND claimed_at=$L_A` matches **0 rows**, the whole transaction rolls back — no
  `scan`, no `scan_detail`, no `changelog`, no `domain` state, no resource links — and the
  `lease_lost` counter increments (03 §17 #4). Assert B's write is the only one persisted and no
  double changelog exists for the domain. Run the interleaving both orders (A-then-B reclaim and
  B-commits-first) to prove two lease values can never both win.

### 10.5 Live-check lifecycle (07-api.md — §6; design §5.3)

Endpoint shapes/bodies are authoritative in 07-api.md — §6; this scenario drives the full
job lifecycle against a live DB + a fake engine (returns a scripted `ScanResult`):

- **POST validation:** invalid host → 400 `{"error":"invalid_host",...}`; reserved TLD
  (`.internal`, `.local`, RFC 2606) → 400; IP literal → 400.
- **Rate limit:** the 11th `POST /check` from one `X-Real-IP` within an hour → 429
  `{"error":"rate_limited","scope":"ip",...}` + `Retry-After`; the 501st global → `scope:"global"`.
- **Dedupe domain-side:** an existing `domain` row with `last_checked_at` within `dedupe_window`
  (1h) returns `200` with `cached:true` and `id:null` from the latest `scan_detail`, creating **no**
  `check_job` row.
- **Dedupe job-side:** a prior `check_job status='done'` within the window returns `200
  cached:true` for the same host, no new row.
- **Insert path:** otherwise `202 {"id",...,"status":"pending"}`; the consumer claims it (the claim
  UPDATE quoted from design §5.3), ensures a `domain` row (`created_by='live_check'`, `rank NULL`,
  `parent_id` only if the parent already exists), runs the fake engine, and writes `status='done',
  result=...`.
- **Rule 0 (07-api.md — §9 #8):** after a completed job, assert **every** `domain` state column is
  untouched **except** `last_requested_at`/`next_check_at`/re-enable — no `scan`/`scan_detail` row
  written by the live path, no `*_status`/`*_pending`/`classification`/changelog change.
- **Lifecycle re-entry:** a POST for a `delisted` host re-enables it (`next_check_at=now()`); for a
  `dead` host leaves it disabled but sets `next_check_at=now()`; for `service`/`manual` runs the
  check but never re-enables. Each POST refreshes `last_requested_at=now()`.
- **Reaper:** a `pending`/`processing` job older than `fail_after` (15m) is flipped to
  `failed, error='timed out'` within one reaper tick; every poller terminates ≤15 min.
- **Retention:** the daily-tick purge deletes `check_job` rows older than `retention` (30d).

Config keys `live_check.workers`, `job_budget`, `reclaim_after`, `fail_after`, `retention`,
`rate_ip_per_hour`, `rate_global_per_hour`, `dedupe_window` (registry: 09-ops.md).

### 10.6 Badge handler (07-api.md — §5)

Against a live DB with seeded rows: a hero-gold host → `200` `image/svg+xml` byte-equal to
`gold.svg`; unknown host → `200` gray `unknown`; disabled host → `200` gray `unknown` (differs
from `GET /domain/{domain}`'s 404-on-disabled); `xn--`-input and the equivalent Unicode input
render the **same** badge (Canonicalize folds them); `.svg`-less path (`/badge/dnb.no`) → `404`
(route miss); invalid host → `400` `{"error":"invalid_host",...}` (JSON, not SVG); response
carries `Cache-Control: public, max-age=3600`.

---

## 11. `make test` / `make lint`

Per the house Make-is-the-interface rule (targets defined in 09-ops.md; this file states what
each must *cover*, not the recipe):

- **`make test`** — `go test -race ./...`: runs every **unit** tier test (all §2–§8 vector tables,
  golden replay §8, serialization/badge renderers), no Docker, no network. This is the
  every-push gate and must stay green offline. Coverage report emitted (`-coverprofile`).
- **`make test-integration`** — `go test -race -tags=integration ./...`: the §9–§10 testcontainers
  suite. Requires Docker; runs pre-merge and in CI.
- **`make capture-fixtures`** — `go test -tags=capture ./testdata/parity/...`: the §8.1 recorder;
  manual/one-shot, needs network to `api.whynoipv6.com`. Never runs in the normal test path.
- **`make lint`** — `golangci-lint run` over the whole module (config in the repo `.golangci.yml`).
  Includes the two grep-style source gates enforced as failing lint/vet checks: (a) no
  `strings.ToLower` on hostnames outside `internal/domain/host.go` (06 acceptance #1); (b) no
  `not_applicable`/`error`/`inconsistent`/`unknown` string literal emitted from any legacy
  serializer (§7.1 invariant).

The default `make test && make lint` after every change (global workflow rule) exercises the
unit tier + lint; the integration tier is a separate gate so a machine without Docker can still
develop and run the fast suite.

---

## 12. Coverage expectations per package

"Exhaustive" = every branch and every vector-table row is covered; a new enum value or table row
that lacks a case fails the build (enforced by the property/totality tests noted). "Smoke" = the
happy path plus the one or two error branches that matter; no line-coverage target.

| Package | Bar | Rationale |
|---|---|---|
| `internal/domain` (Canonicalize, classify, legacyStatus, IPv6Status/Observation types) | **Exhaustive** | The trust core's pure logic; §2, §6, §7.1 tables are total over their input domains. |
| `internal/consensus` (quorum, breakers, A lookup) | **Exhaustive** | §3 covers the full 3-symbol permutation + degraded mode + both breakers; classification correctness depends on it. |
| `internal/crawler` — `observe.go` (mapper) | **Exhaustive** | §4 covers every mapping-table row + the resources branches; the property tests forbid unmapped combinations. |
| `internal/crawler` — commit unit | **Exhaustive** | §5 covers every anti-flap branch, the counting gate, step R, dead trigger, lease fence; §10.1/§10.4 prove the DB write + fence. |
| `internal/api` — `renderChangelog`, shortuuid codec, `legacyStatus`, badge renderer | **Exhaustive** | Wire-frozen contract; §7 tables are byte-pinned. |
| `internal/api` — handlers (routing, parity) | **High** — golden replay per legacy endpoint (§8) + every synthetic-branch fixture (§8.4) + every 404-body row (§8.3) | Parity with a frozen frontend is the whole point; each endpoint has at least one golden and each pinned quirk a fixture. |
| `internal/postgres` / sqlc queries | **Integration-covered** | Exercised by §9–§10 against real DDL; the claim-plan gate (§9.3) and commit/contention/fence (§10.1/§10.4) are the meaningful assertions, not line coverage of generated code. |
| `internal/ingest` (Tranco, campaign sync, resource discovery, attribution) | **High** — 06 acceptance #2–#10 each become a fixture/integration case | Correctness of ranks/membership/dedup/attribution is load-bearing but exercised at the behavioral level. |
| `internal/lock` (advisory locks) | **Integration smoke** — one two-process `TryRun` contention case (one wins, one gets `ErrHeld`) + one `Run` wait-and-run case | Postgres does the hard part; the test proves the key encoding and skip/wait behavior. |
| `cmd/api`, `cmd/crawler`, `cmd/v6ctl` wiring; config load; graceful shutdown; dataset export; `crawler_metrics` streaming | **Smoke** | Orchestration/glue; a start-serve-shutdown test and a config-defaults test suffice — the logic they wire is covered in the packages above. |

The exhaustive packages carry a **totality guard** where the input is a closed enum: a `switch`
with no `default` (or a `default: t.Fatalf`) over `IPv6Status`/`Observation`/`Classification`
so adding an enum value without a mapping breaks the test at compile or first run. This is the
mechanism that keeps the vector tables above authoritative as the code evolves.
