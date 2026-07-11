<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Dimension, HistoryPoint } from '@/api'
import { statusBlockClass } from '@/utils/status'
import { formatDate } from '@/utils/date'

// The uptime timeline (§7.3): one block per day from /domains/{host}/history,
// colored by this dimension's confirmed value. Blocks render newest-right
// (flex-row-reverse, exactly the old layout); windows shorter than `days` pad
// with neutral blocks so the timeline keeps its width.
const props = withDefaults(
  defineProps<{
    points: HistoryPoint[]
    dimension: Dimension
    days?: number
    hoverEffect?: boolean
  }>(),
  { days: 90, hoverEffect: false },
)

interface Block {
  day: string | null
  colorClass: string
}

// Newest-first: index 0 renders at the right edge (first:rounded-r).
const blocks = computed<Block[]>(() => {
  const sorted = [...props.points]
    .sort((a, b) => (a.day < b.day ? -1 : a.day > b.day ? 1 : 0))
    .slice(-props.days)
    .reverse()
  const padded: Block[] = sorted.map((p) => ({
    day: p.day,
    colorClass: statusBlockClass(p[props.dimension]),
  }))
  while (padded.length < props.days) {
    padded.push({ day: null, colorClass: 'bg-gray-800' })
  }
  return padded
})

const openIndex = ref<number | null>(null)
</script>

<template>
  <div class="relative">
    <!-- Tracker Data -->
    <div class="group flex h-8 w-full items-center flex-row-reverse">
      <div
        v-for="(block, index) in blocks"
        :key="index"
        class="size-full overflow-hidden px-[0.5px] transition first:rounded-r-[4px] first:pr-0 last:rounded-l-[4px] last:pl-0 sm:px-px min-w-2 max-w-3 flex-1 opacity-80"
      >
        <!-- Block -->
        <div
          :class="[
            'size-full rounded-[1px]',
            block.colorClass,
            hoverEffect && block.day ? 'hover:opacity-50' : '',
          ]"
          @mouseenter="openIndex = block.day ? index : null"
          @mouseleave="openIndex = null"
        ></div>

        <!-- Tooltip -->
        <div
          v-if="openIndex === index && block.day"
          class="absolute z-10 w-auto rounded-md px-2 py-1 text-sm text-fuchsia-600 bg-gray-800 border-slate-200 normal-case"
          style="top: -3rem; left: 50%; transform: translateX(-50%)"
        >
          {{ formatDate(block.day) }}
        </div>
      </div>
    </div>

    <!-- Timeline Labels -->
    <div class="flex justify-between mt-2 text-xs text-gray-500">
      <span class="hidden lg:block">90 days ago</span>
      <span class="hidden sm:block lg:hidden">60 days ago</span>
      <span class="sm:hidden">30 days ago</span>
      <span>Today</span>
    </div>
  </div>
</template>
