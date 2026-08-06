<script setup lang="ts">
import { computed } from 'vue'

// A single series, no axes, sized to sit inside a card.
//
// Small multiples are the fix for series that share a unit but not a range:
// plotting Cloudflare at 86% and Cloudflare London at 0.8% on one axis flattens
// five of the seven networks into the baseline. One box each, scaled to itself,
// shows every level and every shape.
const props = withDefaults(
  defineProps<{
    values: number[]
    color: string
    /**
     * Smallest span the y-axis may cover, in the series' own units. Without it
     * a series that never moves gets scaled to its own noise and a 0.1pp wobble
     * draws like a mountain range.
     */
    minSpan?: number
    height?: number
  }>(),
  { minSpan: 2, height: 40 },
)

const W = 160
const PAD = 3

const path = computed(() => {
  const vs = props.values
  if (vs.length < 2) return ''
  const lo = Math.min(...vs)
  const hi = Math.max(...vs)
  const mid = (lo + hi) / 2
  const span = Math.max(hi - lo, props.minSpan)
  const top = mid + span / 2
  const inner = props.height - PAD * 2
  return (
    'M' +
    vs
      .map((v, i) => {
        const x = (i / (vs.length - 1)) * W
        const y = PAD + ((top - v) / span) * inner
        return `${x.toFixed(1)},${y.toFixed(1)}`
      })
      .join(' L')
  )
})
</script>

<template>
  <svg
    :viewBox="`0 0 ${W} ${height}`"
    class="h-auto w-full overflow-visible"
    preserveAspectRatio="none"
    aria-hidden="true"
  >
    <path
      :d="path"
      fill="none"
      :stroke="color"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      vector-effect="non-scaling-stroke"
    />
  </svg>
</template>
