# 0002 — The `ipv6_only` fold, shadow-row suppression, and resources always on

- **Status:** Accepted
- **Date:** 2026-07-12
- **Deciders:** project owner
- **Touches:** `internal/domain` (`IPv6Only`), `internal/crawler` (commit machine), `internal/api` (payloads, feed wording), `openapi.yaml`, frontend DomainTable + `utils/changelog.ts`, 03 §10/§11, 07 §4.2, 09 §2.2, 12 §7.1/§7.4

## Context

The domain list table showed four DNS-record dimensions (Apex/WWW/E-Mail/Nameserver) and hid the two derived dimensions, `conn` (does the site's own server answer over IPv6 — real HTTP/HTTPS dials to the apex, pinned to its AAAA addresses) and `resources` (do the site's required third-party page resources have IPv6). Yet those two together answer the question visitors actually care about: **does this site present the same over an IPv6-only connection?**

Two related problems surfaced while reviewing the changelog UX:

1. The generic changelog template rendered nonsense for derived dimensions ("gstatic.com no longer uses connectivity"), and the server feed serializer had already drifted from the frontend wordings despite the spec pinning them together.
2. `conn → not_applicable` changelog rows are pure noise: `conn` only reaches `not_applicable` when base/www lose their AAAA, which writes its own row in the same confirmation window. Same for `resources → not_applicable` (shadow of `conn` leaving `supported`).

This is a trust-critical surface: the fold's logic must be exact or users will not believe the data.

## Decision

1. **One pure fold, backend-owned.** `domain.IPv6Only(conn, resources) *IPv6Status` lives next to `Classify` (03 §10) and is the only definition:
   - `supported` iff `conn = supported` AND `resources ∈ {supported, not_applicable}` (the vacuous pass the gold rule uses);
   - `unsupported` iff `conn = unsupported` (broken_v6 wins, first match) or `resources = unsupported`;
   - `not_applicable` iff `conn = not_applicable` (no AAAA — the base/www columns tell that story);
   - `NULL` otherwise — **strict**: `conn = supported` with unconfirmed `resources` claims nothing, and the impossible `no_record` inputs claim nothing.
   Serialized as `ipv6_only` on the §4.2 summary and §4.3 detail, derived at render time from the same confirmed sextet as `status` — the frontend never re-derives it. Unlike `gold`, the fold is **ungated by classification**: a non-hero domain can be fully usable IPv6-only.
2. **Frontend column.** The domain table becomes Rank / Domain / Apex / WWW / E-Mail / Nameserver / **IPv6 Only**, rendered via the standard `StatusIcon` vocabulary (`null` → "Not yet checked"). Strict-blank at launch was chosen over a conn-only interim meaning: the column never claims more than what was measured.
3. **Shadow-row suppression at write time (03 §11).** Confirmed `conn/resources → not_applicable` flips never write changelog rows — same mechanism as the bootstrap rule; the flip itself still commits and still reports a telemetry Transition. Transitions *out of* `not_applicable` keep their rows. DB, API, feeds, and frontend agree by construction.

   _Erratum 2026-09-02 (amends items 2 and 3, review issue 02):_ item 2's premise for `resources` is wrong. 02 §6's roll-up returns `not_applicable` whenever the effective link set is empty — a dependency pruned at 30 days or swept to `no_record` — with `conn` still `supported`, so that flip is not a shadow of anything: it clears `resources_v4only`, sets `saint`, turns `ipv6_only` `supported`, and now writes its row. Suppression for `resources` is keyed on the cause: only when `conn`'s post-step-2 status is non-nil and not `supported`. `conn → not_applicable` is unchanged. The rest of the decision — write-time suppression, transitions out of `not_applicable` keeping their rows, one predicate for changelog and telemetry (now `Transition.Shadow`) — stands. The "Negative / accepted" bullet about invisible shadow flips narrows accordingly.
4. **Bespoke wording for derived dimensions (12 §7.4).** `conn`: "is now reachable over IPv6" / "is no longer reachable over IPv6" / "published IPv6 addresses — but connections fail". `resources`: "now loads all page resources over IPv6" / "loads some page resources without IPv6". Goldens on both sides (frontend `changelog.test.ts`, backend `feed_wording_test.go`) pin the shared table; the feed serializer's drift ("the apex", "mail", old_value ignored) is fixed to the same table.
5. **`crawler.resources.enabled` defaults to `true`** (09 §2.2). The owner never intends to disable the resources crawl; the flag remains only as an emergency ops brake. This makes the strict-NULL launch window a bootstrap transient (N=3 confirmations), not a permanent empty column.

## Consequences

**Positive**

- The flagship "IPv6 Only" verdict has exactly one implementation, exhaustively table-tested (5×5 cross-product with trust invariants: `supported` requires positive evidence on both dimensions; `unsupported` requires a definitive negative).
- The changelog carries only meaningful rows; feeds, API, and UI cannot disagree about which rows exist or how they read.
- "Almost There" rows can show all-green DNS columns with a red IPv6 Only — the broken-v6 story the site exists to tell.

**Negative / accepted**

- The column is blank until the resources dimension confirms across the fleet (~N scans after first deploy) — chosen deliberately over interim semantics.
- Suppressed shadow flips are invisible in the changelog table itself (they remain derivable from the kept base/www/conn rows and the `*_since` columns).
- `gold` and `ipv6_only` overlap conceptually but differ by design (hero-gated tier vs. per-domain fold); both are documented in 03 §10.
