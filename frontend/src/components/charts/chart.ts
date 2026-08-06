// Geometry, palette and formatting for the hand-rolled SVG charts.
//
// Series colours are the one place a chart needs literal values: a fill is
// data-driven, so it cannot be a class. Everything structural — grid lines,
// axes, borders, labels — uses Tailwind `stroke-*`/`fill-*`/`text-*`
// utilities instead, so it resolves through style.css's `@theme` tokens and
// stays on the right side of the drift gate documented there.

export interface ChartSeries {
  key: string
  label: string
  /** Hex, because SVG fill/stroke on a data-driven series cannot be a class. */
  color: string
}

/**
 * The classification ramp, mirroring utils/rating.ts (Good → Medium → Bad).
 *
 * These five partition the tracked set exactly — hero + partial + sinner +
 * inactive + unknown = domains, verified against stats_global_daily. Saints are
 * deliberately absent: a Saint is a Hero that also serves every page resource
 * over IPv6, so stacking the two double-counts ~156k domains.
 */
export const TIER_COLOR = {
  heroes: '#10b981', // emerald-500
  partial: '#f59e0b', // amber-500
  sinners: '#e11d48', // rose-600
  inactive: '#707d86', // gray-500, the style.css token
  unknown: '#55595f', // gray-600 — dimmer than inactive, which is a real verdict
} as const

/**
 * One colour per check, in the canonical order the copy guide fixes. Six lines
 * share one axis here, so the hues are spread right around the wheel — two
 * neighbouring purples or two greens are indistinguishable at line width.
 */
export const DIMENSION_COLOR = {
  base: '#d946ef', // fuchsia-500, the brand accent leads
  www: '#818cf8', // indigo-400
  ns: '#38bdf8', // sky-400
  mx: '#2dd4bf', // teal-400
  conn: '#a3e635', // lime-400
  resources: '#f59e0b', // amber-500
} as const

export const DELTA_COLOR = { gained: '#10b981', lost: '#e11d48' } as const

/** Categorical ramp for the per-network lines — distinguishable at 375px. */
export const CATEGORICAL = [
  '#d946ef',
  '#38bdf8',
  '#10b981',
  '#f59e0b',
  '#818cf8',
  '#2dd4bf',
  '#fb7185',
  '#a3e635',
]

const compact = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 })
const full = new Intl.NumberFormat('en')

/**
 * "990K", "1.1M". The old metrics tiles divided by 1000 unconditionally and
 * rendered a million domains as "1100K"; Intl carries the unit up instead.
 */
export const fmtCompact = (n: number | null | undefined): string =>
  n == null ? '—' : compact.format(n)

export const fmtFull = (n: number | null | undefined): string => (n == null ? '—' : full.format(n))

export const fmtPercent = (n: number | null | undefined, digits = 1): string =>
  n == null ? '—' : `${n.toFixed(digits)}%`

const axisDate = new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short' })

/** "6 Aug" — short enough for an x-axis tick. */
export const fmtAxisDate = (day: string): string => axisDate.format(new Date(day))

/** Round a maximum up to 1/2/2.5/5/10 × 10ⁿ so the grid lands on read-able values. */
export function niceMax(value: number): number {
  if (!(value > 0)) return 1
  const base = 10 ** Math.floor(Math.log10(value))
  const f = value / base
  const step = f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10
  return step * base
}

/** Column-major → the per-series arrays the charts consume. */
export function pluck<T>(rows: T[], keys: readonly (keyof T)[]): number[][] {
  return keys.map((k) => rows.map((r) => Number(r[k] ?? 0)))
}
