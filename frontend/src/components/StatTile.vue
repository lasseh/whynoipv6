<script setup lang="ts">
import SampleBadge from '@/components/SampleBadge.vue'
import { PALETTE } from '@/components/charts/chart'

// One headline number. `tone` carries the verdict so a grid of tiles reads at
// a glance instead of six identical fuchsia numbers.
withDefaults(
  defineProps<{
    value: string
    label: string
    hint?: string
    tone?: 'brand' | 'good' | 'bad' | 'muted'
    sample?: boolean
  }>(),
  { tone: 'brand', sample: false },
)

// good/bad are the same verdict the charts encode, so they take the same hues:
// a Heroes tile in one green above a Heroes band in another reads as two
// different measurements. `brand` stays the site's fuchsia — it is the wordmark
// accent, not a verdict, and the ramp keeps 67° of hue away from it so the two
// never look like the same signal — and `muted` stays a plain gray token.
const TONE = {
  brand: 'text-fuchsia-600',
  muted: 'text-gray-200',
} as const

const TONE_HEX = { good: PALETTE.teal, bad: PALETTE.red } as const

const isHex = (tone: string): tone is keyof typeof TONE_HEX => tone in TONE_HEX
</script>

<template>
  <div class="flex flex-col rounded border border-gray-700 bg-gray-800/60 p-5">
    <div class="mb-2 flex items-start justify-between gap-2">
      <div
        class="text-3xl font-bold leading-none tracking-tighter md:text-4xl"
        :class="isHex(tone) ? undefined : TONE[tone]"
        :style="isHex(tone) ? { color: TONE_HEX[tone] } : undefined"
      >
        {{ value }}
      </div>
      <SampleBadge v-if="sample" />
    </div>
    <div class="text-sm text-gray-400">{{ label }}</div>
    <div v-if="hint" class="mt-1 text-xs text-gray-500">{{ hint }}</div>
  </div>
</template>
