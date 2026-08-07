// Geometry, palette and formatting for the hand-rolled SVG charts.
//
// Series colours are the one place a chart needs literal values: a fill is
// data-driven, so it cannot be a class. Everything structural — grid lines,
// axes, borders, labels — uses Tailwind `stroke-*`/`fill-*`/`text-*`
// utilities instead, so it resolves through style.css's `@theme` tokens and
// stays on the right side of the drift gate documented there.

import { onUnmounted, ref, type Ref } from 'vue'

export interface ChartSeries {
  key: string
  label: string
  /** Hex, because SVG fill/stroke on a data-driven series cannot be a class. */
  color: string
}

/**
 * True on viewports wide enough for a wide viewBox.
 *
 * A chart's viewBox width is really its text-size control: the SVG stretches to
 * its container, so a viewBox of 800 on a 340px phone scales every axis label
 * down by 2.4x and the chart becomes unreadable. Every chart narrows its
 * viewBox below the `sm` breakpoint, so the query lives here rather than being
 * re-derived per chart type.
 */
export function useWideViewport(): Ref<boolean> {
  const wide = ref(true)
  // jsdom has no matchMedia, and the tests never assert the narrow layout.
  if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
    const mq = window.matchMedia('(min-width: 640px)')
    wide.value = mq.matches
    const sync = (e: MediaQueryListEvent) => (wide.value = e.matches)
    mq.addEventListener('change', sync)
    onUnmounted(() => mq.removeEventListener('change', sync))
  }
  return wide
}

/**
 * The data palette, tuned to sit at the same depth as the site's own accents.
 *
 * The constraint that decides these values is not hue, it is lightness. The
 * site's accents (fuchsia-600 #c026d3, fuchsia-500 #d946ef, purple-600
 * #5d5dff) average 85% saturation at 59% lightness against a #1f2124 card:
 * vivid, and sitting *in* the page. A pastel set averaging ~70% lightness
 * floats above it and reads as a widget someone embedded — which is exactly
 * how the Tokyo Night palette this replaces looked here, despite Tokyo Night
 * being a fine theme on its own navy background. This set measures 87% / 60%.
 *
 * Hues then follow the site rather than an external theme. `rating.ts` already
 * codifies the verdict ramp as teal → amber → pink, so good stays teal, and
 * the warm end runs yellow → orange → red. Red rather than pink at the bottom
 * keeps 67° of hue between "Sinners" and the fuchsia brand accent, which sit
 * in adjacent tiles; pink would have left 37° and read as the brand.
 *
 * Chart *structure* — grid, axes, borders, labels — is deliberately absent
 * here. It stays on Tailwind classes so it resolves through style.css's
 * `@theme` tokens; only data-driven fills need literals.
 */
export const PALETTE = {
  teal: '#2dd4bf', // teal-400  · 8.7:1
  green: '#4ade80', // green-400 · 9.5:1
  lime: '#a3e635', // lime-400  · 10.7:1
  yellow: '#facc15', // yellow-400 · 10.5:1
  amber: '#fbbf24', // amber-400 · 9.7:1
  orange: '#f97316', // orange-500 · 5.8:1
  red: '#f87171', // red-400   · 5.8:1
  cyan: '#22d3ee', // cyan-400  · 8.9:1
  violet: '#8d8dff', // the site's own --color-purple-500 · 5.7:1
  fuchsia: '#d946ef', // the brand accent, for annotations over a plot
  gray: '#707d86', // --color-gray-500
  grayDim: '#55595f', // --color-gray-600
} as const

/**
 * The classification ramp, mirroring utils/rating.ts (Good → Medium → Bad).
 *
 * These five partition the tracked set exactly — hero + partial + sinner +
 * inactive + unknown = domains, verified against stats_global_daily. Saints are
 * deliberately absent: a Saint is a Hero that also serves every page resource
 * over IPv6, so stacking the two double-counts ~156k domains.
 */
export const TIER_COLOR = {
  heroes: PALETTE.teal,
  partial: PALETTE.yellow,
  sinners: PALETTE.red,
  inactive: PALETTE.gray,
  unknown: PALETTE.grayDim, // dimmer than inactive, which is at least a real verdict
} as const

/**
 * One colour per check, in the canonical order the copy guide fixes.
 *
 * Six lines share one axis, so these walk the wheel monotonically and the
 * legend reads as a spectrum. Every adjacent pair is at least 46° apart —
 * an earlier draft put cyan next to teal at 15° and they were one colour at
 * stroke width.
 */
export const DIMENSION_COLOR = {
  base: PALETTE.fuchsia, // 292° — the brand accent leads on the headline check
  www: PALETTE.violet, // 240°
  ns: PALETTE.cyan, // 188°
  mx: PALETTE.green, // 142°
  conn: PALETTE.lime, // 83°
  resources: PALETTE.orange, // 25°
} as const

export const DELTA_COLOR = { gained: PALETTE.teal, lost: PALETTE.red } as const

/**
 * The accent for reference lines and annotations drawn over a plot.
 *
 * The brand fuchsia: an annotation is the chart talking about itself rather
 * than another measurement, so it should not borrow a colour the data uses.
 */
export const ANNOTATION_COLOR = PALETTE.fuchsia

/**
 * The empty part of a bar — the site's own `--color-gray-700`, its border
 * token, at 1.3:1 against the card.
 *
 * A track has one job: disappear. Two previous attempts gave it a hue with
 * enough presence to read as a bar in its own right, which made a provider
 * sitting at 0.8% look almost full.
 */
export const TRACK_COLOR = '#33363a'

/**
 * Adoption percentage → its verdict colour, on the §2.1 thresholds that
 * utils/rating.ts already uses for badges and progress bars.
 *
 * One function rather than per-component class maps, so a provider is the same
 * colour in the scatter as in the league row beside it.
 */
export function shareColor(pct: number): string {
  if (pct >= 60) return PALETTE.teal
  if (pct >= 40) return PALETTE.yellow
  if (pct >= 15) return PALETTE.orange
  return PALETTE.red
}

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
