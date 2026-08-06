<script setup lang="ts">
import { computed } from 'vue'
import { fmtCompact, fmtPercent } from '@/components/charts/chart'

// The provider ranking, one row per network/DNS/hosting operator. Same split
// bar the ASN list has always used (emerald for dual-stack, violet for the
// IPv4-only remainder) so all four leagues read identically.
export interface LeagueRow {
  key: string | number
  name: string
  /** Secondary identifier: "AS13335", a domain count, whatever scopes the name. */
  sub?: string
  total: number
  v6: number
}

const props = withDefaults(defineProps<{ rows: LeagueRow[]; emptyText?: string }>(), {
  emptyText: 'No providers matched. Try a shorter name.',
})

const view = computed(() =>
  props.rows.map((r) => {
    const v4 = Math.max(0, r.total - r.v6)
    const share = r.total > 0 ? (r.v6 / r.total) * 100 : 0
    return {
      ...r,
      v4,
      share,
      v6Width: `${share.toFixed(2)}%`,
      v4Width: `${(100 - share).toFixed(2)}%`,
    }
  }),
)
</script>

<template>
  <div>
    <div v-for="row in view" :key="row.key" class="mb-3">
      <div class="mb-1 flex items-baseline justify-between gap-3">
        <span class="truncate text-base font-medium text-white">
          {{ row.name }}
          <span v-if="row.sub" class="pl-2 text-xs font-medium text-gray-500">{{ row.sub }}</span>
        </span>
        <span class="shrink-0 font-mono text-sm text-gray-300">{{ fmtPercent(row.share) }}</span>
      </div>
      <div class="mb-1 flex h-3 overflow-hidden rounded text-xs">
        <div class="bg-emerald-600" :style="{ width: row.v6Width }"></div>
        <div class="bg-violet-950" :style="{ width: row.v4Width }"></div>
      </div>
      <div class="flex items-center justify-between text-xs text-gray-400">
        <span>{{ fmtCompact(row.v6) }} dual-stack</span>
        <span>{{ fmtCompact(row.v4) }} IPv4-only</span>
      </div>
    </div>

    <p v-if="rows.length === 0" class="text-gray-400">{{ emptyText }}</p>
  </div>
</template>
