# WhyNoIPv6 — Domain Context

The canonical project glossary lives in `docs/spec/00-overview.md` §6 (entity/kind,
dimension, observation, confirmed status, classification, saint, frontier, lease,
quorum, changelog, campaign membership, preflight, keyset cursor, tier collections).
Terms defined there are not redefined here — this file only records vocabulary that
crystallized *after* the spec was frozen.

## Terms

- **Check detail.** The typed per-check payload on an engine `checker.Result` — one
  struct per check (e.g. `AAAABaseDetail`), all embedding `CommonDetail{Error, Reason}`
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

- **Shadow transition.** A confirmed `conn/resources → not_applicable` flip — a
  deterministic consequence of a base/www/conn row from the same confirmation
  window. It commits (status/`*_since`/telemetry Transition) but never writes a
  changelog row (03 §11, write-time suppression, same mechanism as bootstrap).
  _Avoid:_ read-time filtering of changelog rows per consumer.

- **Keyset spec.** The per-endpoint description of a keyset-paginated list —
  `api.KeysetSpec{Sort, Positioned, Fetch, Key}` — consumed by `api.KeysetPage`,
  which owns the whole cursor pipeline (fingerprint → decode → seek → N+1 fetch →
  trim → cursor minting). Endpoints supply only what varies; the backward/positioned
  window conventions live in one place. The `around_rank` centered window shares
  only the minting half (`MintPage`). _Avoid:_ hand-rolling the decode→trim→mint
  sequence in a handler.
