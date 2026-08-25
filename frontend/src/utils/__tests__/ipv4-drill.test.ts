import { describe, expect, it } from 'vitest'
import { drillPhase, formatWindow, nextWindow } from '@/utils/ipv4-drill'

// Everything is pinned in UTC. The nginx gate reads $time_iso8601 on a host
// running TZ=UTC, so a banner computed in local time would disagree with the
// outage it is announcing.
const at = (iso: string) => new Date(iso)

describe('drillPhase', () => {
  it.each([
    ['well before the notice period', '2026-08-25T12:00:00Z', 'idle'],
    ['the day the notice goes up', '2026-08-30T00:00:00Z', 'upcoming'],
    ['one minute before the notice period', '2026-08-29T23:59:00Z', 'idle'],
    ['the day before the window', '2026-09-05T18:00:00Z', 'upcoming'],
    ['the moment the window opens', '2026-09-06T00:00:00Z', 'active'],
    ['mid-window', '2026-09-06T13:00:00Z', 'active'],
    ['the last second of the window', '2026-09-06T23:59:59Z', 'active'],
    ['the moment it closes', '2026-09-07T00:00:00Z', 'idle'],
  ])('is %s → %s', (_label, iso, want) => {
    expect(drillPhase(at(iso))).toBe(want)
  })
})

describe('nextWindow', () => {
  it('points at this month while the window is still ahead', () => {
    expect(nextWindow(at('2026-09-01T00:00:00Z')).toISOString()).toBe('2026-09-06T00:00:00.000Z')
  })

  it('stays on this month for the whole of the window', () => {
    expect(nextWindow(at('2026-09-06T23:00:00Z')).toISOString()).toBe('2026-09-06T00:00:00.000Z')
  })

  it('rolls to next month once the window has closed', () => {
    expect(nextWindow(at('2026-09-07T00:00:00Z')).toISOString()).toBe('2026-10-06T00:00:00.000Z')
  })

  it('rolls across the year boundary', () => {
    expect(nextWindow(at('2026-12-20T00:00:00Z')).toISOString()).toBe('2027-01-06T00:00:00.000Z')
  })

  // A short month is where a naive "add 30 days" would drift.
  it('crosses February without drifting', () => {
    expect(nextWindow(at('2027-01-31T00:00:00Z')).toISOString()).toBe('2027-02-06T00:00:00.000Z')
  })
})

describe('formatWindow', () => {
  it('renders the window date in UTC regardless of the runner zone', () => {
    expect(formatWindow(at('2026-09-06T00:00:00Z'))).toBe('6 September')
  })
})
