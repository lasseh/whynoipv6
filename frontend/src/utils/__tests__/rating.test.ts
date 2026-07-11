import { describe, expect, it } from 'vitest'
import { calculateRating } from '@/utils/rating'

describe('calculateRating', () => {
  it('threshold boundaries', () => {
    expect(calculateRating(0).rating).toBe('Bad')
    expect(calculateRating(39.99).rating).toBe('Bad')
    expect(calculateRating(40).rating).toBe('Medium')
    expect(calculateRating(59.99).rating).toBe('Medium')
    expect(calculateRating(60).rating).toBe('Good')
    expect(calculateRating(100).rating).toBe('Good')
  })

  it('zero-total and null percent are Unknown', () => {
    expect(calculateRating(0, 0).rating).toBe('Unknown')
    expect(calculateRating(null).rating).toBe('Unknown')
  })

  it('carries the badge and gradient classes', () => {
    expect(calculateRating(75)).toEqual({
      rating: 'Good',
      colorClass: 'bg-emerald-600/10 text-emerald-600 ring-emerald-600/40',
      gradientColor: 'from-teal-700 to-teal-800',
    })
    expect(calculateRating(50).gradientColor).toBe('from-amber-700 to-amber-800')
    expect(calculateRating(10).gradientColor).toBe('from-pink-700 to-pink-800')
  })
})
