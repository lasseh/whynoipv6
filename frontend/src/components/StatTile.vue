<script setup lang="ts">
import SampleBadge from '@/components/SampleBadge.vue'

// One headline number. `tone` carries the verdict so a grid of tiles reads at
// a glance instead of six identical fuchsia numbers: the ramp is the same one
// utils/rating.ts uses for badges and progress bars.
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

const TONE = {
  brand: 'text-fuchsia-600',
  good: 'text-emerald-500',
  bad: 'text-rose-600',
  muted: 'text-gray-200',
} as const
</script>

<template>
  <div class="flex flex-col rounded border border-gray-700 bg-gray-800/60 p-5">
    <div class="mb-2 flex items-start justify-between gap-2">
      <div class="text-3xl font-bold leading-none tracking-tighter md:text-4xl" :class="TONE[tone]">
        {{ value }}
      </div>
      <SampleBadge v-if="sample" />
    </div>
    <div class="text-sm text-gray-400">{{ label }}</div>
    <div v-if="hint" class="mt-1 text-xs text-gray-500">{{ hint }}</div>
  </div>
</template>
