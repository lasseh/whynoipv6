<script setup lang="ts">
// The waiting state (Nielsen H1/H2): narrate the scan's real phases with an
// elapsed counter and an asymptotic progress bar instead of a static line.
// Stage timings approximate the engine's two-phase run; the sequencing is
// cosmetic but the phases are what the scan actually does. The page renders
// this only while a scan runs, so the interval's lifetime is the component's
// own — no watch-and-clear bookkeeping.
import { computed, onMounted, onUnmounted, ref } from 'vue'

const props = defineProps<{ host: string; pending: boolean }>()
defineEmits<{ cancel: [] }>()

const SCAN_STAGES: [number, string][] = [
  [0, 'Resolving DNS records — AAAA, nameservers, mail…'],
  [4, 'Cross-checking three public resolvers; two must agree…'],
  [9, 'Connecting to the site over IPv6 only…'],
  [16, 'Checking mail servers and TLS over IPv6…'],
  [24, 'Fetching the page and discovering its resources…'],
  [45, 'Still working — slow targets can take up to 90 seconds…'],
]

const elapsed = ref(0)
let timer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  timer = setInterval(() => elapsed.value++, 1_000)
})
onUnmounted(() => {
  if (timer !== null) clearInterval(timer)
})

const waitMessage = computed(() => {
  if (props.pending) return 'Waiting in queue…'
  let msg = SCAN_STAGES[0]![1]
  for (const [at, m] of SCAN_STAGES) {
    if (elapsed.value >= at) msg = m
  }
  return msg
})

// Asymptotic fill toward a 95 % cap: fast early motion, never falsely done.
const progress = computed(() => Math.min(95, Math.round(100 * (1 - Math.exp(-elapsed.value / 18)))))
</script>

<template>
  <div class="mt-8">
    <div class="flex items-center justify-between mb-2 text-sm">
      <span class="font-mono text-gray-200">{{ host }}</span>
      <span class="text-gray-500 tabular-nums">{{ elapsed }}s</span>
    </div>
    <div class="w-full h-2 rounded-md bg-gray-800 overflow-hidden">
      <div
        class="h-2 rounded-md bg-gradient-to-r from-fuchsia-700 to-fuchsia-500 transition-all duration-1000 ease-linear"
        :style="{ width: `${progress}%` }"
        role="progressbar"
        :aria-valuenow="progress"
        aria-valuemin="0"
        aria-valuemax="100"
      ></div>
    </div>
    <div class="mt-3 flex items-center justify-between gap-4">
      <p class="text-sm text-gray-400" aria-live="polite">{{ waitMessage }}</p>
      <button
        type="button"
        class="text-xs text-gray-400 hover:text-pink-500 underline underline-offset-2 cursor-pointer shrink-0"
        @click="$emit('cancel')"
      >
        Cancel
      </button>
    </div>
  </div>
</template>
