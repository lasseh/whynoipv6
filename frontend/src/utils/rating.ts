// Percent → rating badge / progress-gradient classes (§2.1 thresholds:
// Good ≥60, Medium ≥40, Bad below, Unknown when there is nothing to rate).
export interface Rating {
  rating: 'Good' | 'Medium' | 'Bad' | 'Unknown'
  colorClass: string
  gradientColor: string
}

const UNKNOWN: Rating = {
  rating: 'Unknown',
  colorClass: 'bg-gray-600/10 text-gray-600 ring-gray-600/40',
  gradientColor: 'from-gray-700 to-gray-800',
}

/**
 * `percent` is served correct by the new API (no client ÷10). Unknown when
 * percent is null (e.g. a campaign before its first stats tick) or when the
 * entity has a zero total.
 */
export function calculateRating(percent: number | null, total?: number): Rating {
  if (percent === null || total === 0) return UNKNOWN
  if (percent >= 60) {
    return {
      rating: 'Good',
      colorClass: 'bg-emerald-600/10 text-emerald-600 ring-emerald-600/40',
      gradientColor: 'from-teal-700 to-teal-800',
    }
  }
  if (percent >= 40) {
    return {
      rating: 'Medium',
      colorClass: 'bg-amber-600/10 text-amber-600 ring-amber-600/20',
      gradientColor: 'from-amber-700 to-amber-800',
    }
  }
  return {
    rating: 'Bad',
    colorClass: 'bg-rose-600/10 text-rose-600/80 ring-rose-600/20',
    gradientColor: 'from-pink-700 to-pink-800',
  }
}
