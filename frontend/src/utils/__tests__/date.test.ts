import { describe, expect, it } from 'vitest'
import { formatDate, formatDateTime, formatTime } from '@/utils/date'

// Times render in the runner's local zone, so time-of-day assertions check
// the shape rather than a wall-clock value.
describe('date formatting', () => {
  it('formats date-only values in the site-wide long form', () => {
    expect(formatDate('2026-08-14')).toBe('14 August 2026')
    expect(formatDate(new Date(2026, 0, 1))).toBe('1 January 2026')
  })

  it('formats timestamps as date plus 24h time', () => {
    expect(formatDateTime('2026-08-14T12:30:00Z')).toMatch(/^\d{1,2} August 2026 \d{2}:\d{2}$/)
  })

  it('formats bare times for rows grouped under a day heading', () => {
    expect(formatTime('2026-08-14T12:30:00Z')).toMatch(/^\d{2}:\d{2}$/)
  })
})
