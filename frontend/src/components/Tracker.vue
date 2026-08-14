<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Dimension, HistoryPoint, StatusValue } from '@/api'
import { statusBlockClass, statusLabel, statusTextClass } from '@/utils/status'
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
  status: StatusValue
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
    status: p[props.dimension],
    colorClass: statusBlockClass(p[props.dimension]),
  }))
  while (padded.length < props.days) {
    padded.push({ day: null, status: null, colorClass: 'bg-gray-800' })
  }
  return padded
})

const openIndex = ref<number | null>(null)

const openBlock = computed(() =>
  openIndex.value !== null ? (blocks.value[openIndex.value] ?? null) : null,
)

// Hover is mouse-only (pointerenter with pointerType); touch goes through
// click-to-toggle so a synthesized mouseenter can't leave the bubble stuck.
function hoverBlock(e: PointerEvent, index: number, day: string | null): void {
  if (e.pointerType === 'mouse') openIndex.value = day ? index : null
}

function leaveBlock(e: PointerEvent): void {
  if (e.pointerType === 'mouse') openIndex.value = null
}

function tapBlock(index: number, day: string | null): void {
  if (!day) return
  openIndex.value = openIndex.value === index ? null : index
}

// Keyboard path (WCAG 2.1.1): the timeline is one focus stop; arrows step
// the same openIndex the pointer drives. Index 0 renders rightmost (newest),
// so ArrowLeft walks older and ArrowRight newer; padded no-data blocks sit
// past the last real day and are skipped by the clamp.
const realCount = computed(() => blocks.value.filter((b) => b.day !== null).length)

function stepDay(older: boolean): void {
  if (realCount.value === 0) return
  const delta = older ? 1 : -1
  const next = openIndex.value === null ? 0 : openIndex.value + delta
  openIndex.value = Math.min(realCount.value - 1, Math.max(0, next))
}

// The bubble anchors over the hovered block (blocks are equal-width, so the
// center is pure arithmetic — index 0 renders rightmost), clamped so ~230px
// of tooltip never escapes the tracker at the outer blocks.
const tooltipStyle = computed(() => {
  if (openIndex.value === null) return {}
  const n = blocks.value.length
  const centerPct = ((n - 1 - openIndex.value + 0.5) / n) * 100
  return {
    top: '-3rem',
    left: `clamp(115px, ${centerPct}%, calc(100% - 115px))`,
    transform: 'translateX(-50%)',
  }
})

function blockAria(block: Block): string | undefined {
  return block.day ? `${formatDate(block.day)} — ${statusLabel(block.status)}` : undefined
}
</script>

<template>
  <div class="relative">
    <!-- Tracker Data -->
    <div
      class="group flex h-8 w-full items-center flex-row-reverse"
      :tabindex="hoverEffect ? 0 : undefined"
      :role="hoverEffect ? 'application' : undefined"
      :aria-label="
        hoverEffect
          ? 'Daily status timeline. Use the left and right arrow keys to read each day.'
          : undefined
      "
      @keydown.left.prevent="hoverEffect && stepDay(true)"
      @keydown.right.prevent="hoverEffect && stepDay(false)"
      @keydown.esc="openIndex = null"
      @blur="openIndex = null"
    >
      <div
        v-for="(block, index) in blocks"
        :key="index"
        class="size-full overflow-hidden px-[0.5px] transition first:rounded-r-[4px] first:pr-0 last:rounded-l-[4px] last:pl-0 sm:px-px min-w-2 max-w-3 flex-1 opacity-80"
        @pointerenter="hoverEffect && hoverBlock($event, index, block.day)"
        @pointerleave="hoverEffect && leaveBlock($event)"
        @click="hoverEffect && tapBlock(index, block.day)"
      >
        <!-- Block -->
        <div
          role="img"
          :aria-label="blockAria(block)"
          :class="[
            'size-full rounded-[1px]',
            block.colorClass,
            hoverEffect && block.day ? 'hover:opacity-50' : '',
          ]"
        ></div>
      </div>
    </div>

    <!-- Day tooltip: anchored over the hovered block, clamped to the card -->
    <div
      v-if="openBlock?.day"
      aria-live="polite"
      class="absolute z-10 w-auto whitespace-nowrap rounded-md px-2 py-1 text-sm bg-gray-900 border border-gray-700 shadow-lg normal-case"
      :style="tooltipStyle"
    >
      <span class="inline-block size-2 rounded-full mr-1.5" :class="openBlock.colorClass"></span>
      <span class="text-gray-200">{{ formatDate(openBlock.day) }}</span>
      <span class="text-gray-500"> — </span>
      <span :class="statusTextClass(openBlock.status)">{{ statusLabel(openBlock.status) }}</span>
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
