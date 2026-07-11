<script setup lang="ts">
import { computed } from 'vue'
import { calculateRating } from '@/utils/rating'

const props = withDefaults(
  defineProps<{
    percent: number | null
    total?: number
    prefix?: string
  }>(),
  { prefix: 'Rating: ' },
)

const rating = computed(() => calculateRating(props.percent, props.total))
</script>

<template>
  <div
    class="text-sm inline-flex font-medium rounded-md text-center px-2.5 py-1 ring-1 ring-inset"
    :class="rating.colorClass"
  >
    {{ prefix }}{{ rating.rating }}
  </div>
</template>
