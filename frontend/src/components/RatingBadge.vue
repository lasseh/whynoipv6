<script setup lang="ts">
import { computed } from 'vue'
import { calculateRating } from '@/utils/rating'

const props = withDefaults(
  defineProps<{
    percent: number | null
    total?: number | undefined
    prefix?: string
    /** Card grids use text-sm (default); detail headers use text-base. */
    size?: string
  }>(),
  { prefix: 'Rating: ', size: 'text-sm' },
)

const rating = computed(() => calculateRating(props.percent, props.total))
</script>

<template>
  <div
    class="inline-flex font-medium rounded-md text-center px-2.5 py-1 ring-1 ring-inset"
    :class="[size, rating.colorClass]"
  >
    {{ prefix }}{{ rating.rating }}
  </div>
</template>
