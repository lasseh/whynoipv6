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
 * Tokyo Night, the source palette for every data colour on this page.
 *
 * Verbatim from enkia's theme so the values stay checkable against it:
 * github.com/tokyo-night/tokyo-night-vscode-theme. Chart *structure* — grid,
 * axes, borders — deliberately keeps using the site's own gray tokens through
 * Tailwind classes; only the data itself is Tokyo Night, so the panels still
 * read as part of the site rather than as an embedded terminal screenshot.
 */
export const TOKYO = {
  red: '#f7768e',
  orange: '#ff9e64',
  yellow: '#e0af68',
  green: '#9ece6a',
  teal: '#73daca',
  cyan: '#2ac3de',
  cyanBright: '#7dcfff',
  blue: '#7aa2f7',
  purple: '#bb9af7',
  comment: '#565f89',
  black: '#414868',
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
  heroes: TOKYO.green,
  partial: TOKYO.yellow,
  sinners: TOKYO.red,
  inactive: TOKYO.comment,
  unknown: TOKYO.black, // dimmer than inactive, which is at least a real verdict
} as const

/**
 * One colour per check, in the canonical order the copy guide fixes.
 *
 * Six lines share one axis, and Tokyo Night is a cool palette, so this walks
 * the widest arc it offers — magenta, blue, cyan, teal, green, orange. The
 * softer `cyanBright` is deliberately skipped: next to `blue` at line width the
 * two are the same colour.
 */
export const DIMENSION_COLOR = {
  base: TOKYO.purple,
  www: TOKYO.blue,
  ns: TOKYO.cyan,
  mx: TOKYO.teal,
  conn: TOKYO.green,
  resources: TOKYO.orange,
} as const

export const DELTA_COLOR = { gained: TOKYO.green, lost: TOKYO.red } as const

/** The accent for reference lines and annotations drawn over a plot. */
export const ANNOTATION_COLOR = TOKYO.purple

/**
 * The empty part of a bar.
 *
 * Tokyo Night's black, dimmed. At full strength it is light enough to read as
 * a bar in its own right, so a provider sitting at 0.8% looked almost full —
 * the same misreading the old two-tone split bar caused, in a new hue.
 */
export const TRACK_COLOR = 'rgba(65, 72, 104, 0.55)'

/**
 * Adoption percentage → its verdict colour, on the §2.1 thresholds that
 * utils/rating.ts already uses for badges and progress bars.
 *
 * One function rather than per-component class maps, so a provider is the same
 * colour in the scatter as in the league row beside it.
 */
export function shareColor(pct: number): string {
  if (pct >= 60) return TOKYO.green
  if (pct >= 40) return TOKYO.yellow
  if (pct >= 15) return TOKYO.orange
  return TOKYO.red
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
