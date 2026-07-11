<script setup lang="ts">
import { computed } from 'vue'
import type { StatusBlock } from '@/api'

// The 4-star detail rating (§7.3, resolved OPEN-F1): one star per rated
// dimension (base/www/ns/mx). supported → filled emerald; not_applicable →
// muted zinc (neither earned nor missing — a no-MX domain is never
// penalized); unsupported/no_record/null → empty gray. Stars render
// filled-first to keep the old left-filled look.
const props = defineProps<{ status: StatusBlock }>()

type StarKind = 'filled' | 'muted' | 'empty'

const STAR_ORDER: Record<StarKind, number> = { filled: 0, muted: 1, empty: 2 }
const STAR_CLASS: Record<StarKind, string> = {
  filled: 'text-emerald-600',
  muted: 'text-zinc-600',
  empty: 'text-gray-600',
}

const stars = computed<StarKind[]>(() =>
  (['base', 'www', 'ns', 'mx'] as const)
    .map((dim): StarKind => {
      const value = props.status[dim].value
      if (value === 'supported') return 'filled'
      if (value === 'not_applicable') return 'muted'
      return 'empty'
    })
    .sort((a, b) => STAR_ORDER[a] - STAR_ORDER[b]),
)
</script>

<template>
  <div class="flex items-center space-x-1">
    <div
      v-for="(kind, index) in stars"
      :key="index"
      :class="kind === 'muted' ? 'has-tooltip relative inline-block' : 'inline-block'"
    >
      <span
        v-if="kind === 'muted'"
        class="tooltip absolute rounded border border-slate-700 shadow-lg p-1 bg-gray-800 text-fuchsia-600 normal-case transform -translate-x-1/2 -translate-y-full"
        >Not applicable</span
      >
      <svg
        class="w-4 h-4"
        :class="STAR_CLASS[kind]"
        aria-hidden="true"
        xmlns="http://www.w3.org/2000/svg"
        fill="currentColor"
        viewBox="0 0 22 20"
      >
        <path
          d="M20.924 7.625a1.523 1.523 0 0 0-1.238-1.044l-5.051-.734-2.259-4.577a1.534 1.534 0 0 0-2.752 0L7.365 5.847l-5.051.734A1.535 1.535 0 0 0 1.463 9.2l3.656 3.563-.863 5.031a1.532 1.532 0 0 0 2.226 1.616L11 17.033l4.518 2.375a1.534 1.534 0 0 0 2.226-1.617l-.863-5.03L20.537 9.2a1.523 1.523 0 0 0 .387-1.575Z"
        />
      </svg>
    </div>
  </div>
</template>

<style scoped>
.tooltip {
  visibility: hidden;
  left: 50%;
  white-space: nowrap;
}

.has-tooltip:hover .tooltip {
  visibility: visible;
  z-index: 50;
}
</style>
