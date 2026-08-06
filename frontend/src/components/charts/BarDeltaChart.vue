<script setup lang="ts">
import { computed } from 'vue'
import ChartFrame from './ChartFrame.vue'
import { niceMax, type ChartSeries } from './chart'

// Diverging bars: gains grow up from the baseline, losses down. Both are
// counts of the same event type, so a shared axis is the honest framing —
// the day's net is whichever side of zero has more ink.
const props = withDefaults(
  defineProps<{
    labels: string[]
    /** Two series: the upward one first, then the one drawn below the baseline. */
    series: ChartSeries[]
    /** Raw positive counts; the mirroring happens here, not in the callers. */
    values: number[][]
    formatValue: (n: number) => string
    label: string
    height?: number
  }>(),
  { height: 260 },
)

const signed = computed<number[][]>(() =>
  props.values.map((row, i) => (i === 0 ? row : row.map((v) => -v))),
)

const bound = computed(() => niceMax(Math.max(...props.values.flat(), 0)))
</script>

<template>
  <ChartFrame
    :labels="labels"
    :series="series"
    :values="signed"
    :y-max="bound"
    :y-min="-bound"
    :format-value="(n: number) => formatValue(Math.abs(n))"
    :label="label"
    :height="height"
    band
  >
    <template #default="{ xAt, yAt, bandWidth, isVisible, zeroY }">
      <template v-for="(s, si) in series" :key="s.key">
        <rect
          v-for="(v, i) in signed[si] ?? []"
          v-show="isVisible(s.key)"
          :key="i"
          :x="xAt(i) - bandWidth * 0.3"
          :y="v >= 0 ? yAt(v) : zeroY"
          :width="bandWidth * 0.6"
          :height="Math.max(1, Math.abs(yAt(v) - zeroY))"
          :fill="s.color"
          fill-opacity="0.85"
          rx="1"
        />
      </template>
    </template>
  </ChartFrame>
</template>
