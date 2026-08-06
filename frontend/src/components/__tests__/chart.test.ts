import { describe, expect, it } from 'vitest'
import { fmtCompact, fmtFull, fmtPercent, niceMax, pluck } from '@/components/charts/chart'

describe('fmtCompact', () => {
  it('carries the unit up past a thousand thousands', () => {
    // The tiles used to divide by 1000 unconditionally and render the crawler's
    // daily sweep as "1100K". Anything at or over a million must read in M.
    expect(fmtCompact(1_095_290)).toBe('1.1M')
    expect(fmtCompact(989_973)).toBe('990K')
    expect(fmtCompact(241_738)).toBe('241.7K')
    expect(fmtCompact(871)).toBe('871')
  })

  it('renders an absent value as a dash rather than zero', () => {
    expect(fmtCompact(null)).toBe('—')
    expect(fmtCompact(undefined)).toBe('—')
    expect(fmtCompact(0)).toBe('0')
  })
})

describe('fmtFull and fmtPercent', () => {
  it('groups thousands and fixes percent precision', () => {
    expect(fmtFull(989_973)).toBe('989,973')
    expect(fmtPercent(34.6421)).toBe('34.6%')
    expect(fmtPercent(34.6421, 0)).toBe('35%')
    expect(fmtPercent(null)).toBe('—')
  })
})

describe('niceMax', () => {
  it('rounds an axis ceiling up to a readable step', () => {
    expect(niceMax(989_973)).toBe(1_000_000)
    expect(niceMax(3_007)).toBe(5_000)
    expect(niceMax(1_800)).toBe(2_000)
    expect(niceMax(2_400)).toBe(2_500)
  })

  it('never returns a zero span for an empty series', () => {
    expect(niceMax(0)).toBe(1)
    expect(niceMax(-5)).toBe(1)
  })
})

describe('pluck', () => {
  it('turns row objects into one array per series', () => {
    const rows = [
      { heroes: 1, sinners: 10 },
      { heroes: 2, sinners: 20 },
    ]
    expect(pluck(rows, ['heroes', 'sinners'])).toEqual([
      [1, 2],
      [10, 20],
    ])
  })

  it('reads a null column as zero so the stack still closes', () => {
    expect(pluck([{ heroes: null }], ['heroes'])).toEqual([[0]])
  })
})
