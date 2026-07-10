# Runbook — frontier surgery

The frontier is the `domain` table itself: `next_check_at`,
`claimed_at` (30-min lease), `error_streak`, `dead_streak`,
`disabled/disabled_reason`. All surgery is plain SQL; there is no
queue service to bounce. Prefer `v6ctl disable|enable` for single
domains — direct UPDATEs are for set-level repair.

## Triggers

- Queue-depth alert (crawler_metrics `queue_depth` growing for hours).
- A crawler crash left a large claimed cohort mid-lease.
- A config/deploy error mis-scheduled a cohort (e.g. everything pushed
  into the slow lane).

## Diagnosis

```sql
-- due-now backlog
SELECT count(*) FROM domain
WHERE (NOT disabled OR disabled_reason IN ('dead','delisted'))
  AND next_check_at <= now();

-- stale leases (crash artifacts; self-heal after 30 min)
SELECT count(*) FROM domain
WHERE claimed_at IS NOT NULL AND claimed_at < now() - interval '30 minutes';

-- lane distribution
SELECT date_trunc('day', next_check_at) AS day, count(*)
FROM domain WHERE NOT disabled GROUP BY 1 ORDER BY 1 LIMIT 14;
```

## Recovery

- **Stale leases:** normally do nothing — the claim predicate reclaims
  leases older than 30 min. Force it only if a scan storm is needed
  sooner: `UPDATE domain SET claimed_at = NULL WHERE claimed_at <
  now() - interval '30 minutes';`
- **Pull a cohort in:** `UPDATE domain SET next_check_at = now()
  WHERE rank <= 1000 AND NOT disabled;` (bounded WHERE — never pull
  the whole million at once; the claim loop will absorb ~86k/day/proc).
- **Push a cohort out** (e.g. after an upstream incident produced junk
  errors): `UPDATE domain SET next_check_at = now() + interval '12
  hours', error_streak = 0 WHERE error_streak > 0 AND last_checked_at
  > now() - interval '6 hours';`
- **Aging valve:** if rank-ordered claiming starves the tail, set
  `CLAIM_ORDER=age` on one crawler process (env override) and revert
  once `min(next_check_at)` recovers.
- **Never** hand-edit confirmed-state columns (`*_status`, `*_since`,
  `classification`, `gold`) — those belong to the commit machine; a
  wrong value self-heals only after N confirmations and pollutes the
  changelog.

## Notes

- Surgery composes with the daily tick: S1–S5 sweeps run at 03:30 and
  may re-disable/re-enable rows you touched; check
  `docs/spec/04-lifecycle-scheduling.md` §8 before bulk changes to
  disabled cohorts.
