<script setup lang="ts">
import { computed } from 'vue'
import { calculateRating } from '@/utils/rating'

// The v6-ready percent bar. A null percent (e.g. a campaign before its first
// stats tick, §7.3) renders the empty track.
const props = withDefaults(
  defineProps<{
    percent: number | null
    total?: number | undefined
    /** Card grids use h-2.5 (default); detail headers use h-4. */
    height?: string
  }>(),
  { height: 'h-2.5' },
)

const gradient = computed(() => calculateRating(props.percent, props.total).gradientColor)
const width = computed(() => `${props.percent ?? 0}%`)
</script>

<template>
  <div class="w-full rounded-md bg-gray-700" :class="height">
    <div
      :class="`${height} rounded-md bg-gradient-to-r ${gradient}`"
      :style="{ width }"
      role="progressbar"
      :aria-valuenow="percent ?? 0"
      aria-valuemin="0"
      aria-valuemax="100"
    ></div>
  </div>
</template>
