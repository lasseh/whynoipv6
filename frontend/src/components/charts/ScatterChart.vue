<script setup lang="ts">
import { computed, ref } from 'vue'
import { ANNOTATION_COLOR, fmtCompact, fmtPercent, shareColor, useWideViewport } from './chart'

// Size against adoption, one dot per provider.
//
// A ranked bar list answers "who is biggest" and buries the actual question,
// which is who is big *and* doing nothing. Two axes answer both at once: the
// bottom-right corner is the shame quadrant and it needs no caption.
//
// This does not build on ChartFrame. That frame indexes by column, and nearest-
// column hit testing is the wrong model here — a scatter needs nearest point in
// two dimensions, and its x axis is continuous rather than a list of days.

export interface ScatterPoint {
  key: string | number
  label: string
  /** Population size — plotted on a log axis. */
  x: number
  /** Percentage, 0-100. */
  y: number
  /** Secondary identifier shown in the tooltip: "AS13335", a slug, a region. */
  sub?: string | undefined
}

const props = withDefaults(
  defineProps<{
    /** Callers apply their own size floor — a scatter cannot know what is noise. */
    points: ScatterPoint[]
    label: string
    height?: number
  }>(),
  { height: 320 },
)

const wide = useWideViewport()
const W = computed(() => (wide.value ? 800 : 360))
const PAD = computed(() => ({ l: wide.value ? 42 : 34, r: 14, t: 14, b: 34 }))
const innerW = computed(() => W.value - PAD.value.l - PAD.value.r)
const innerH = computed(() => props.height - PAD.value.t - PAD.value.b)

// Populations span two or three decades, so a linear axis would pile every
// provider against the left edge under whichever one is biggest.
const LOG_FLOOR = 1
const logOf = (v: number) => Math.log10(Math.max(LOG_FLOOR, v))

// The low end snaps to a decade so the first gridline is a round number; the
// high end only pads past the largest point. Rounding both up to a decade left
// a quarter of the plot empty whenever the biggest provider sat just over one.
const domain = computed(() => {
  const xs = props.points.map((p) => logOf(p.x))
  const lo = Math.floor(Math.min(...xs, 3))
  const hi = Math.max(...xs, lo + 0.5) + 0.12
  return { lo, hi }
})

const xAt = (v: number) => {
  const { lo, hi } = domain.value
  return PAD.value.l + ((logOf(v) - lo) / (hi - lo)) * innerW.value
}
const yAt = (v: number) => PAD.value.t + innerH.value - (v / 100) * innerH.value

const xTicks = computed(() => {
  const { lo, hi } = domain.value
  const ticks: number[] = []
  for (let d = lo; d <= hi; d++) ticks.push(10 ** d)
  return ticks
})
const yTicks = [0, 25, 50, 75, 100]

// The median, not a domain-weighted mean: one provider holding a third of the
// list would drag a weighted average up and put nearly everyone "below
// average" for a reason that is about that one company.
const median = computed(() => {
  if (props.points.length === 0) return null
  const ys = props.points.map((p) => p.y).sort((a, b) => a - b)
  const mid = Math.floor(ys.length / 2)
  return ys.length % 2 ? ys[mid]! : (ys[mid - 1]! + ys[mid]!) / 2
})

const plotted = computed(() =>
  props.points.map((p) => ({ ...p, cx: xAt(p.x), cy: yAt(p.y), fill: shareColor(p.y) })),
)

// Only the biggest few carry a permanent label, and none do on a phone: at
// 360 viewBox units two names collide before they finish rendering. Tapping a
// dot still names it.
const NAMED = 3
const named = computed(() =>
  wide.value
    ? [...plotted.value]
        .sort((a, b) => b.x - a.x)
        .slice(0, NAMED)
        .map((p) => p.key)
    : [],
)

const hover = ref<string | number | null>(null)
const hovered = computed(() => plotted.value.find((p) => p.key === hover.value) ?? null)

function onMove(e: PointerEvent) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  if (rect.width === 0) return
  const scale = W.value / rect.width
  const x = (e.clientX - rect.left) * scale
  const y = (e.clientY - rect.top) * scale
  let best: ScatterPoint | null = null
  let bestDist = 24 * 24 // squared pick radius, in viewBox units
  for (const p of plotted.value) {
    const d = (p.cx - x) ** 2 + (p.cy - y) ** 2
    if (d < bestDist) {
      bestDist = d
      best = p
    }
  }
  hover.value = best?.key ?? null
}

const tip = computed(() => {
  const p = hovered.value
  if (!p) return null
  const pct = (p.cx / W.value) * 100
  const shift = pct < 25 ? '0%' : pct > 75 ? '-100%' : '-50%'
  return {
    point: p,
    left: `${pct}%`,
    top: `${(p.cy / props.height) * 100}%`,
    transform: `translate(${shift}, calc(-100% - 12px))`,
  }
})
</script>

<template>
  <div class="relative" @pointermove="onMove" @pointerleave="hover = null">
    <svg
      :viewBox="`0 0 ${W} ${height}`"
      class="h-auto w-full overflow-visible"
      role="img"
      :aria-label="label"
    >
      <g class="stroke-gray-700/70">
        <line
          v-for="t in yTicks"
          :key="`y${t}`"
          :x1="PAD.l"
          :x2="W - PAD.r"
          :y1="yAt(t)"
          :y2="yAt(t)"
        />
        <line
          v-for="t in xTicks"
          :key="`x${t}`"
          :x1="xAt(t)"
          :x2="xAt(t)"
          :y1="PAD.t"
          :y2="PAD.t + innerH"
        />
      </g>

      <g class="fill-gray-500 text-[11px]">
        <text v-for="t in yTicks" :key="`yl${t}`" :x="PAD.l - 7" :y="yAt(t) + 4" text-anchor="end">
          {{ t }}%
        </text>
        <text v-for="t in xTicks" :key="`xl${t}`" :x="xAt(t)" :y="height - 14" text-anchor="middle">
          {{ fmtCompact(t) }}
        </text>
        <text :x="PAD.l + innerW / 2" :y="height - 1" text-anchor="middle" class="fill-gray-600">
          domains hosted (log scale)
        </text>
      </g>

      <!-- Median line: half the providers sit below it, and on this data that
           is the headline all by itself. -->
      <template v-if="median !== null">
        <line
          :stroke="ANNOTATION_COLOR"
          stroke-opacity="0.7"
          stroke-dasharray="4 4"
          :x1="PAD.l"
          :x2="W - PAD.r"
          :y1="yAt(median)"
          :y2="yAt(median)"
        />
        <text
          :x="W - PAD.r - 4"
          :y="yAt(median) - 6"
          text-anchor="end"
          :fill="ANNOTATION_COLOR"
          class="text-[11px]"
        >
          median {{ fmtPercent(median) }}
        </text>
      </template>

      <g>
        <circle
          v-for="p in plotted"
          :key="p.key"
          :cx="p.cx"
          :cy="p.cy"
          :r="hover === p.key ? 8 : 5"
          :fill="p.fill"
          :fill-opacity="hover === null || hover === p.key ? 0.9 : 0.35"
          class="stroke-gray-900 transition-[r]"
          stroke-width="1.5"
        />
      </g>

      <g class="fill-gray-400 text-[11px]">
        <text
          v-for="p in plotted.filter((q) => named.includes(q.key))"
          :key="`n${p.key}`"
          :x="p.cx"
          :y="p.cy - 12"
          text-anchor="middle"
        >
          {{ p.label }}
        </text>
      </g>
    </svg>

    <div
      v-if="tip"
      class="pointer-events-none absolute z-10 rounded border border-gray-700 bg-gray-900/95 px-3 py-2 text-xs shadow-lg"
      :style="{ left: tip.left, top: tip.top, transform: tip.transform }"
    >
      <div class="font-medium whitespace-nowrap text-gray-200">{{ tip.point.label }}</div>
      <div v-if="tip.point.sub" class="text-gray-500">{{ tip.point.sub }}</div>
      <div class="mt-1 whitespace-nowrap text-gray-400">
        {{ fmtCompact(tip.point.x) }} domains ·
        <span :style="{ color: tip.point.fill }">{{ fmtPercent(tip.point.y) }} IPv6</span>
      </div>
    </div>
  </div>
</template>
