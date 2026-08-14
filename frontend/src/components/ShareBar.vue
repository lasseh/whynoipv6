<script setup lang="ts">
// The one share bar: a shareColor fill against the neutral track, used by the
// provider league rows and the reverse-DNS panel. Track is Tokyo Night's own
// neutral rather than the site's gray-700: it sits inside the bar, so it
// belongs to the data, and against the card border a blue-leaning track reads
// as an empty measurement instead of a second piece of chrome.
import { computed } from 'vue'
import { TRACK_COLOR, shareColor } from '@/components/charts/chart'

const props = defineProps<{ share: number; label: string }>()

const width = computed(() => `${props.share.toFixed(2)}%`)
const color = computed(() => shareColor(props.share))
</script>

<template>
  <div class="flex h-1.5 overflow-hidden rounded-full" :style="{ backgroundColor: TRACK_COLOR }">
    <div
      class="rounded-full"
      :style="{ width, backgroundColor: color }"
      role="progressbar"
      :aria-valuenow="Math.round(share)"
      aria-valuemin="0"
      aria-valuemax="100"
      :aria-label="label"
    ></div>
  </div>
</template>
