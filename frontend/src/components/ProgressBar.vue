<script setup lang="ts">
import { computed } from 'vue'
import { calculateRating } from '@/utils/rating'

// The v6-ready percent bar. A null percent (e.g. a campaign before its first
// stats tick, §7.3) renders the empty track.
const props = defineProps<{
  percent: number | null
  total?: number
}>()

const gradient = computed(() => calculateRating(props.percent, props.total).gradientColor)
const width = computed(() => `${props.percent ?? 0}%`)
</script>

<template>
  <div class="w-full rounded-md h-2.5 bg-gray-700">
    <div
      :class="`h-2.5 rounded-md bg-gradient-to-r ${gradient}`"
      :style="{ width }"
      role="progressbar"
      :aria-valuenow="percent ?? 0"
      aria-valuemin="0"
      aria-valuemax="100"
    ></div>
  </div>
</template>
