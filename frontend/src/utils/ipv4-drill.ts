// The coordinated IPv4 outage window from draft-martin-retry-over-ipv6: the
// 6th of each month, 00:00-24:00 UTC. deploy/nginx/whynoipv6.com.conf gates
// the same day off $time_iso8601 — these two definitions must agree, and the
// only reason this client-side copy exists is the draft's requirement to show
// a notice for at least seven days before a window opens.
//
// Everything here is UTC, deliberately: a visitor west of Greenwich would
// otherwise see the banner clear a day early. All arithmetic goes through
// Date.UTC, never the local-time getters.

/** Day of the month the window falls on, UTC. */
export const DRILL_DAY = 6

/** How many days ahead the notice goes up, per the draft. */
export const DRILL_NOTICE_DAYS = 7

const DAY_MS = 86_400_000

export type DrillPhase =
  /** No window near enough to mention. */
  | 'idle'
  /** Within the notice period before the next window. */
  | 'upcoming'
  /** The window is open right now. */
  | 'active'

/** Midnight UTC on the drill day of `now`'s month. */
function windowIn(now: Date): Date {
  return new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), DRILL_DAY))
}

/**
 * Start of the window that is open now, or the next one to open. Rolls into
 * the following month once the current month's window has closed, which is
 * also how it handles December.
 */
export function nextWindow(now: Date): Date {
  const thisMonth = windowIn(now)
  if (now.getTime() < thisMonth.getTime() + DAY_MS) return thisMonth
  return new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + 1, DRILL_DAY))
}

/** Where `now` sits relative to the drill schedule. */
export function drillPhase(now: Date): DrillPhase {
  const start = nextWindow(now).getTime()
  const at = now.getTime()
  if (at >= start) return 'active'
  return at >= start - DRILL_NOTICE_DAYS * DAY_MS ? 'upcoming' : 'idle'
}

/** "6 September" — the window's date, for banner copy. */
export function formatWindow(start: Date): string {
  return new Intl.DateTimeFormat('en-GB', {
    day: 'numeric',
    month: 'long',
    timeZone: 'UTC',
  }).format(start)
}
