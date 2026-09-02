# WhyNoIPv6 — Domain Context

The canonical project glossary lives in `docs/spec/00-overview.md` §6 (entity/kind,
dimension, observation, confirmed status, classification, saint, frontier, lease,
quorum, changelog, campaign membership, preflight, keyset cursor, tier collections).
Terms defined there are not redefined here — this file only records vocabulary that
crystallized *after* the spec was frozen.

## Terms

- **Check detail.** The typed per-check payload on an engine `checker.Result` — one
  struct per check *family* (12 detail structs over the 15 check names: `AAAADetail`
  is shared by `dns_aaaa_base`/`dns_aaaa_www`, `HTTPDetail` by `http_ipv6`/`https_ipv6`,
  `LatencyDetail` by `latency_ipv4`/`latency_ipv6`), keyed by the `newDetail` dispatch
  in `checker/detail.go`, all embedding `CommonDetail{Error, Reason}`
  and satisfying the unexported-marker `Detail` interface. It is the single
  compiler-checked contract for what a check reports beyond its status; the
  `scan_detail` JSON stored per scan (03 §14.2) is its serialization plus the
  `conn`/`consensus` hoists. Consumers reach it only through the per-check accessors
  on `checker.ScanResult` (never by map key, never by re-asserting types).
  _Avoid:_ "details map", `map[string]any` payloads.

- **Confirmed sextet.** The six per-dimension confirmed (status, since) column
  pairs in canonical dimension order (base, www, ns, mx, conn, resources),
  carried by `db.ConfirmedSextet` / `postgres.DomainRow.Confirmed()`. Every
  public `StatusBlock` is built from it by the one constructor pair in `api`
  (`statusBlockOf`/`statusBlockTyped`); each row type lists its
  column→dimension pairing exactly once, in its `Confirmed` method.
  _Avoid:_ hand-mapping the six `*_status`/`*_since` columns in a handler.

- **LinkSet.** The normalized `[]observe.LinkedResource` input to the
  resources roll-up, built only by the two constructors in `observe` —
  `PersistedLinks` (commit path) and `LiveLinks` (live-check path) — which
  share one convention: a missing or NULL registry status stays `nil` and
  defers the dimension (`rollupResources` → error). _Avoid:_ building
  `LinkedResource` slices in callers.

- **IPv6-only fold.** The derived `ipv6_only` status (ADR 0002): `domain.IPv6Only(conn,
  resources)` — "does the site present the same over an IPv6-only connection".
  Ungated by classification (unlike saint), strict on NULL inputs, serialized on
  domain payloads at render time, rendered as the table's "IPv6 Only" column.
  _Avoid:_ re-deriving conn+resources verdicts in the frontend or handlers.

- **Shadow transition.** A confirmed `→ not_applicable` flip that another row
  in the same confirmation window already explains. It commits
  (status/`*_since`/telemetry Transition) but writes no changelog row (03 §11,
  write-time suppression, same mechanism as bootstrap). Keyed on the **cause**,
  not the target value: `conn → not_applicable` always qualifies (base/www lost
  their AAAA and wrote their own row), while `resources → not_applicable`
  qualifies only when `conn` actually left `supported` in this commit — the
  roll-up also returns `not_applicable` for an empty link set, and a domain
  dropping its last v4-only dependency is news worth a row. `ComputeCommit`
  decides once and carries the verdict as `Transition.Shadow`, so metrics
  counts what the changelog counts.
  _Avoid:_ read-time filtering of changelog rows per consumer, and
  re-deriving the predicate from `(dim, new_value)` alone.

- **Commit unit.** The typed per-domain write unit `postgres.CommitUnit`
  (03 §12's batch: fenced `CommitDomainParams` UPDATE + changelog/scan/detail
  rows + resource links), executed only by `postgres.FlushCommit` — one
  pgx.Batch, one tx, lease fence. Statement args bind through the sqlc params
  structs (field order = placeholder order, pinned by
  `TestCommitStatementBinding`); the crawler's `Committer` reaches the flush
  only through its seam, so tests substitute a fake. _Avoid:_ hand-listing
  commit columns positionally in a caller.

- **Keyset spec.** The per-endpoint description of a keyset-paginated list —
  `api.KeysetSpec{Sort, Preceded, Fetch, Key}` — consumed by `api.KeysetPage`,
  which owns the whole cursor pipeline (fingerprint → decode → seek → N+1 fetch →
  trim → cursor minting). Endpoints supply only what varies; the backward/positioned
  window conventions live in one place. The `around_rank` centered window shares
  only the minting half (`MintPage`). _Avoid:_ hand-rolling the decode→trim→mint
  sequence in a handler.

- **List spec.** The per-endpoint description of a list endpoint's rim —
  `api.ListSpec` (keyset lists: Sorts/Sort, Scope, Live, Fetch, Key, Item,
  CSV, Count) and `api.WholeSpec` (bounded sets served whole), consumed by
  `ServeList`/`ServeWhole`, which own everything around the keyset spec:
  sort resolution, `?format=` negotiation, the CSV cap raise, freshness via
  the `metaSource` seam (`pgMeta` in production, a fake in unit tests), the
  ETag/304 gate before any window fetch, cursor error mapping, the row→item
  map, and the envelope+count. Composite envelopes (dependents, the campaign
  detail, `/domains`' cursor branch) reuse the response-free half
  `api.ListPage`, which writes its own 400/500. Deliberate non-adopters:
  `serveDomainList`'s rim (one pinned copy) and `writeRecentWindow`.
  _Avoid:_ hand-writing the sort→format→limit→generation→304 sequence in a
  handler.

- **Changelog validator.** The live-surface ETag seed (07 §6.1), which comes
  in two deliberate shapes. `CacheChangelogWindow` serves every
  **fixed-window** surface — the feeds and `writeRecentWindow`'s scoped
  lists — and seeds from the window it already holds: newest `ts` plus the
  row count. The count is load-bearing, not belt-and-braces: `changelog.ts`
  is the worker-fixed scan start, so a slow scan can insert a row *older*
  than the window's max and leave a max-only validator unchanged.
  `CacheChangelog` serves the **paginated** lists, which gate on the ETag
  before they know their window, so they seed from the table-wide
  `ChangelogMaxTS` — over-invalidating a quiet scope, never stale.
  _Avoid:_ seeding a fixed-window surface from the global mark; it never
  304s, because some domain somewhere transitions every few minutes.

- **Series spec.** The `{points}` counterpart to the list spec —
  `api.SeriesSpec{Live, Window, Fetch, Day, Point}` consumed by
  `api.ServeSeries`, which owns the §4.10 rim: window parsing, the optional
  window floor, generation/maxTS through the same `metaSource` seam, the
  ETag/304 gate before any fetch, the row→point map, weekly sampling, and
  the `{points,meta}` envelope. Five adopters (overview, country, campaign,
  changes, asn); `Live` is `/stats/changes` alone and `Window` is its
  `capHistoryWindow` floor. The lockstep `sampleWeekly` documents — points
  and days the same length, or the weekly sample indexes out of range — is
  structural here: the rim derives both slices from one row index, so no
  adopter can desynchronize them. Each adopter keeps its own day formatting,
  because `pgtype.Date` rows carry no zone and `Timestamptz` rows do.
  Deliberate non-adopters: `getNetworkStats` (grouped envelope; reuses only
  `sampleWeekly` and `enterCache`) and `getCrawlerStats` (telemetry, not
  confirmed state — labelling it `confirmed_state` is the conflation §4.10
  forbids). _Avoid:_ building parallel points/days slices in a handler.
