# WhyNoIPv6 — Domain Context

The canonical project glossary lives in `docs/spec/00-overview.md` §6 (entity/kind,
dimension, observation, confirmed status, classification, gold, frontier, lease,
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

- **Keyset spec.** The per-endpoint description of a keyset-paginated list —
  `api.KeysetSpec{Sort, Positioned, Fetch, Key}` — consumed by `api.KeysetPage`,
  which owns the whole cursor pipeline (fingerprint → decode → seek → N+1 fetch →
  trim → cursor minting). Endpoints supply only what varies; the backward/positioned
  window conventions live in one place. The `around_rank` centered window shares
  only the minting half (`MintPage`). _Avoid:_ hand-rolling the decode→trim→mint
  sequence in a handler.
