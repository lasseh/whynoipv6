<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { fmtAxisDate, type ChartSeries } from './chart'

// The one chart chassis: viewBox, grid, axes, legend, hover crosshair and
// tooltip. Chart bodies render into the default slot and receive the scales,
// so pointer handling exists once instead of once per chart type — that
// duplication is where hand-rolled charts usually rot.
//
// Sizing is pure viewBox: the SVG is width:100%, height:auto, so it reflows
// with the container and needs no ResizeObserver.

const props = withDefaults(
  defineProps<{
    /** ISO day per column, in ascending order. */
    labels: string[]
    series: ChartSeries[]
    /** [seriesIndex][labelIndex] — the frame reads these for the tooltip only. */
    values: number[][]
    yMax: number
    yMin?: number
    formatValue: (n: number) => string
    height?: number
    /** Bars sit on band centres; lines and areas sit on the edges. */
    band?: boolean
    /** Screen-reader description of what the chart shows. */
    label: string
  }>(),
  { yMin: 0, height: 260, band: false },
)

// The viewBox width is really the text-size control. The SVG stretches to its
// container, so a viewBox of 800 on a 340px phone scales every label down by
// 2.4× and the axis becomes illegible. Narrowing the viewBox on small screens
// keeps labels near their nominal size and, because the height is fixed, also
// makes the plot taller relative to its width — which is what a phone wants.
const wide = ref(true)
if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
  const mq = window.matchMedia('(min-width: 640px)')
  wide.value = mq.matches
  const sync = (e: MediaQueryListEvent) => (wide.value = e.matches)
  mq.addEventListener('change', sync)
  onUnmounted(() => mq.removeEventListener('change', sync))
}

const W = computed(() => (wide.value ? 800 : 360))
const PAD = computed(() => ({ l: wide.value ? 46 : 38, r: 10, t: 10, b: 26 }))
const GRID_LINES = 4

const innerW = computed(() => W.value - PAD.value.l - PAD.value.r)
const innerH = computed(() => props.height - PAD.value.t - PAD.value.b)
const count = computed(() => props.labels.length)

const bandWidth = computed(() => (count.value > 0 ? innerW.value / count.value : innerW.value))

const xAt = (i: number): number =>
  props.band
    ? PAD.value.l + bandWidth.value * (i + 0.5)
    : PAD.value.l + (count.value > 1 ? (i / (count.value - 1)) * innerW.value : innerW.value / 2)

const yAt = (v: number): number => {
  const span = props.yMax - props.yMin || 1
  return PAD.value.t + innerH.value - ((v - props.yMin) / span) * innerH.value
}

const hidden = ref(new Set<string>())
const isVisible = (key: string): boolean => !hidden.value.has(key)

function toggle(key: string) {
  const next = new Set(hidden.value)
  // Never let the last series be hidden — an empty plot reads as a bug.
  if (next.has(key)) next.delete(key)
  else if (next.size < props.series.length - 1) next.add(key)
  hidden.value = next
}

const ticks = computed(() =>
  Array.from({ length: GRID_LINES + 1 }, (_, i) => {
    const v = props.yMin + ((props.yMax - props.yMin) * i) / GRID_LINES
    return { v, y: yAt(v) }
  }),
)

// Anchored on the last column so "today" always carries a label; four fit on a
// phone before "25 Jul" starts colliding with its neighbour.
const xTicks = computed(() => {
  const stride = Math.max(1, Math.ceil(count.value / (wide.value ? 6 : 4)))
  return props.labels
    .map((day, i) => ({ day, i }))
    .filter(({ i }) => (count.value - 1 - i) % stride === 0)
})

const hoverIndex = ref<number | null>(null)

function onMove(e: PointerEvent) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  if (rect.width === 0 || count.value === 0) return
  const svgX = ((e.clientX - rect.left) / rect.width) * W.value
  const t = (svgX - PAD.value.l) / innerW.value
  const raw = props.band ? Math.floor(t * count.value) : Math.round(t * (count.value - 1))
  hoverIndex.value = Math.min(count.value - 1, Math.max(0, raw))
}

const tooltip = computed(() => {
  const i = hoverIndex.value
  if (i === null) return null
  const rows = props.series
    .map((s, si) => ({ series: s, value: props.values[si]?.[i] ?? 0 }))
    .filter((r) => isVisible(r.series.key))
    .sort((a, b) => Math.abs(b.value) - Math.abs(a.value))
  const pct = (xAt(i) / W.value) * 100
  // Flip the anchor near the edges so the card never clips out of the panel.
  const shift = pct < 22 ? '0%' : pct > 78 ? '-100%' : '-50%'
  return { i, rows, day: props.labels[i]!, left: `${pct}%`, transform: `translateX(${shift})` }
})
</script>

<template>
  <div class="relative" @pointermove="onMove" @pointerleave="hoverIndex = null">
    <svg
      :viewBox="`0 0 ${W} ${height}`"
      class="w-full h-auto overflow-visible"
      role="img"
      :aria-label="label"
    >
      <!-- Horizontal grid and the value axis. -->
      <g class="stroke-gray-700/70">
        <line v-for="t in ticks" :key="t.v" :x1="PAD.l" :x2="W - PAD.r" :y1="t.y" :y2="t.y" />
      </g>
      <g class="fill-gray-500 text-[11px]">
        <text v-for="t in ticks" :key="t.v" :x="PAD.l - 8" :y="t.y + 4" text-anchor="end">
          {{ formatValue(t.v) }}
        </text>
      </g>

      <!-- A diverging chart needs its baseline to read louder than the grid. -->
      <line
        v-if="yMin < 0"
        class="stroke-gray-500"
        :x1="PAD.l"
        :x2="W - PAD.r"
        :y1="yAt(0)"
        :y2="yAt(0)"
      />

      <slot
        :x-at="xAt"
        :y-at="yAt"
        :inner-h="innerH"
        :band-width="bandWidth"
        :is-visible="isVisible"
        :hover-index="hoverIndex"
        :zero-y="yAt(Math.max(yMin, 0))"
      />

      <!-- Crosshair last so it sits above the series. -->
      <line
        v-if="hoverIndex !== null"
        class="stroke-gray-400/60"
        stroke-dasharray="3 3"
        :x1="xAt(hoverIndex)"
        :x2="xAt(hoverIndex)"
        :y1="PAD.t"
        :y2="PAD.t + innerH"
      />

      <g class="fill-gray-500 text-[11px]">
        <text v-for="t in xTicks" :key="t.i" :x="xAt(t.i)" :y="height - 8" text-anchor="middle">
          {{ fmtAxisDate(t.day) }}
        </text>
      </g>
    </svg>

    <!-- HTML tooltip rather than SVG text: Tailwind styles it, and wrapping
         works without measuring glyphs by hand. -->
    <div
      v-if="tooltip"
      class="pointer-events-none absolute top-0 z-10 max-w-[70vw] min-w-40 rounded border border-gray-700 bg-gray-900/95 px-3 py-2 text-xs shadow-lg"
      :style="{ left: tooltip.left, transform: tooltip.transform }"
    >
      <div class="mb-1.5 font-medium text-gray-300">{{ fmtAxisDate(tooltip.day) }}</div>
      <div v-for="row in tooltip.rows" :key="row.series.key" class="flex items-center gap-2 py-0.5">
        <span
          class="size-2 shrink-0 rounded-full"
          :style="{ backgroundColor: row.series.color }"
        ></span>
        <span class="grow whitespace-nowrap text-gray-400">{{ row.series.label }}</span>
        <span class="font-mono text-gray-200">{{ formatValue(row.value) }}</span>
      </div>
    </div>

    <!-- Legend doubles as the series toggle; six lines at 375px are unreadable
         until you can switch some off. -->
    <div class="mt-3 flex flex-wrap gap-x-4 gap-y-1.5">
      <button
        v-for="s in series"
        :key="s.key"
        type="button"
        class="flex items-center gap-1.5 text-xs transition hover:text-gray-200"
        :class="isVisible(s.key) ? 'text-gray-400' : 'text-gray-600'"
        :aria-pressed="isVisible(s.key)"
        @click="toggle(s.key)"
      >
        <span
          class="size-2 shrink-0 rounded-full transition"
          :style="{ backgroundColor: s.color, opacity: isVisible(s.key) ? 1 : 0.25 }"
        ></span>
        {{ s.label }}
      </button>
    </div>
  </div>
</template>
