<script setup lang="ts">
import { computed } from 'vue'
import ChartFrame from './ChartFrame.vue'
import { niceMax, type ChartSeries } from './chart'

// Independent lines on a shared axis: the series here measure different things
// (checks, providers) and are meant to be compared, not summed.
const props = withDefaults(
  defineProps<{
    labels: string[]
    series: ChartSeries[]
    values: number[][]
    formatValue: (n: number) => string
    label: string
    height?: number
    /** Percent charts read better pinned to 100 than to the tallest line. */
    yMax?: number
  }>(),
  { height: 260 },
)

const scale = computed(
  () => props.yMax ?? niceMax(Math.max(...props.values.flat().filter(Number.isFinite), 0)),
)

const path = (vals: number[], xAt: (i: number) => number, yAt: (v: number) => number): string =>
  'M' + vals.map((v, i) => `${xAt(i).toFixed(1)},${yAt(v).toFixed(1)}`).join(' L')
</script>

<template>
  <ChartFrame
    :labels="labels"
    :series="series"
    :values="values"
    :y-max="scale"
    :format-value="formatValue"
    :label="label"
    :height="height"
  >
    <template #default="{ xAt, yAt, isVisible, hoverIndex }">
      <template v-for="(s, si) in series" :key="s.key">
        <path
          v-if="isVisible(s.key)"
          :d="path(values[si] ?? [], xAt, yAt)"
          fill="none"
          :stroke="s.color"
          stroke-width="2"
          stroke-linejoin="round"
          stroke-linecap="round"
        />
        <circle
          v-if="isVisible(s.key) && hoverIndex !== null"
          :cx="xAt(hoverIndex)"
          :cy="yAt(values[si]?.[hoverIndex] ?? 0)"
          r="3.5"
          :fill="s.color"
          class="stroke-gray-900"
          stroke-width="2"
        />
      </template>
    </template>
  </ChartFrame>
</template>
