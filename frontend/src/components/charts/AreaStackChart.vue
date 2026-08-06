<script setup lang="ts">
import ChartFrame from './ChartFrame.vue'
import { niceMax, type ChartSeries } from './chart'
import { computed } from 'vue'

// Stacked bands: every domain lands in exactly one classification, so the
// stack total is the tracked-domain count and the band heights are the split.
const props = withDefaults(
  defineProps<{
    labels: string[]
    series: ChartSeries[]
    values: number[][]
    formatValue: (n: number) => string
    label: string
    height?: number
  }>(),
  { height: 260 },
)

// Scaled off the full stack, not the visible one: toggling a series off should
// shrink the shape, not silently rescale every other band under it.
const yMax = computed(() =>
  niceMax(
    Math.max(
      ...props.labels.map((_, i) => props.values.reduce((sum, v) => sum + (v[i] ?? 0), 0)),
      0,
    ),
  ),
)

function bands(
  isVisible: (key: string) => boolean,
  xAt: (i: number) => number,
  yAt: (v: number) => number,
) {
  const n = props.labels.length
  const floor = new Array<number>(n).fill(0)
  const out: { key: string; color: string; d: string; top: string }[] = []

  for (const [si, s] of props.series.entries()) {
    if (!isVisible(s.key)) continue
    const vals = props.values[si] ?? []
    const ceiling = floor.map((base, i) => base + (vals[i] ?? 0))
    const point = (v: number, i: number) => `${xAt(i).toFixed(1)},${yAt(v).toFixed(1)}`
    const top = ceiling.map(point).join(' L')
    const bottom = floor
      .map((v, i) => ({ v, i }))
      .reverse()
      .map(({ v, i }) => point(v, i))
      .join(' L')

    out.push({ key: s.key, color: s.color, d: `M${top} L${bottom} Z`, top: `M${top}` })
    ceiling.forEach((v, i) => (floor[i] = v))
  }
  return out
}
</script>

<template>
  <ChartFrame
    :labels="labels"
    :series="series"
    :values="values"
    :y-max="yMax"
    :format-value="formatValue"
    :label="label"
    :height="height"
  >
    <template #default="{ xAt, yAt, isVisible }">
      <g v-for="band in bands(isVisible, xAt, yAt)" :key="band.key">
        <path :d="band.d" :fill="band.color" fill-opacity="0.75" />
        <!-- A crisp edge on each band; without it adjacent fills blur together. -->
        <path :d="band.top" fill="none" :stroke="band.color" stroke-width="1.5" />
      </g>
    </template>
  </ChartFrame>
</template>
