# 0004 — Drop rank-band cadence and the write-only telemetry

- **Status:** Accepted
- **Date:** 2026-08-01
- **Deciders:** project owner
- **Touches:** `internal/crawler` (schedule, metrics, commit, frontier), `internal/config` (registry), `internal/consensus`, `db/migrations` (000005), `db/query/metrics.sql`, 03 §2/§9, 04 §4/§15, 05 §10.2, 09 §2.10, Grafana dev dashboards (crawler, data-quality)

## Context

The pre-launch review found a cluster of "designed but never wired" surface: the
`cadence.bands` rank-band scheduling feature (registry key echoed at startup,
`ScheduleConfig.Bands` never populated, `ValidateBands` never called) and a set of
telemetry written but never read (`dead_triggered` lifecycle counter,
`Metrics.ActiveSlots`/`crawler_metrics.active_slots`, committer atomics, consensus
disagreement counts). Two sessions briefly resolved this in opposite directions — one
wired the features, one deleted them.

## Decision

**Delete, don't wire.** Simplicity first: unshipped features and unread telemetry are
carrying cost, not value. Anything genuinely needed later comes back with a consumer
attached.

1. `cadence.bands` is removed end-to-end (registry key, `Band` type, `ValidateBands`,
   the `cadence()` band branch). Cadence is uniform `cadence.default` (24h). The path
   to Tranco-full is a slower default, or reviving bands via this ADR — no schema
   change either way.
2. The write-only telemetry is removed at every layer: `dead_triggered` dim-counter
   plumbing, committer atomics, consensus disagreement tallies, and the
   `crawler_metrics.active_slots` column (migration 000005 — the table is an
   uncompressed 90-day-retention hypertable, so the drop is a plain catalog change).
3. Dashboards stop charting the removed series (crawler "Active slots" stat panel,
   data-quality `dead_triggered` series).

## Consequences

- Rank-based crawl prioritization does not exist; every domain re-checks daily. If
  scan volume outgrows that, revive bands per spec 04 §4's removed design (this ADR is
  the pointer to its shape) rather than reinventing it.
- `docs/spec/11-implementation-plan.md` P2.7 still describes the band deliverables as
  planned at the time; it is historical and intentionally not rewritten.
