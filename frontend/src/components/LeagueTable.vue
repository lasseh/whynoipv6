<script setup lang="ts">
import { computed } from 'vue'
import ShareBar from '@/components/ShareBar.vue'
import { fmtCompact, fmtPercent, shareColor } from '@/components/charts/chart'

// The provider ranking, one row per network/DNS/hosting operator.
//
// The bar fills against a neutral track rather than splitting into two colours:
// the IPv4-only remainder is not a second measurement, it is the part that is
// missing, and giving it its own hue made every row read as a tie. The fill
// takes its colour from the same shareColor ramp the scatter uses, so a
// provider looks the same in both.
export interface LeagueRow {
  key: string | number
  name: string
  /**
   * The identifier that actually distinguishes the row, rendered in mono so it
   * reads as an identity rather than a footnote. One organisation legitimately
   * holds many ASNs — Google runs AS15169 and AS396982, and they are 37.9% and
   * 11.3% IPv6 — so two rows sharing a name is correct data, and only the
   * number tells them apart.
   */
  sub?: string
  total: number
  v6: number
}

const props = withDefaults(defineProps<{ rows: LeagueRow[]; emptyText?: string }>(), {
  emptyText: 'No providers matched. Try a shorter name.',
})

const view = computed(() =>
  props.rows.map((r) => {
    const share = r.total > 0 ? (r.v6 / r.total) * 100 : 0
    return {
      ...r,
      share,
      color: shareColor(share),
    }
  }),
)
</script>

<template>
  <div>
    <div v-for="row in view" :key="row.key" class="mb-3.5">
      <div class="mb-1.5 flex items-baseline justify-between gap-3">
        <span class="truncate text-sm font-medium text-zinc-100">
          {{ row.name }}
          <span v-if="row.sub" class="pl-1.5 font-mono text-xs font-normal text-gray-400">{{
            row.sub
          }}</span>
        </span>
        <span class="shrink-0 font-mono text-sm" :style="{ color: row.color }">
          {{ fmtPercent(row.share) }}
        </span>
      </div>
      <ShareBar :share="row.share" :label="`${row.name} IPv6 share`" />
      <div class="mt-1 text-xs text-gray-500">
        {{ fmtCompact(row.v6) }} of {{ fmtCompact(row.total) }} domains publish an apex AAAA record
      </div>
    </div>

    <p v-if="rows.length === 0" class="text-gray-400">{{ emptyText }}</p>
  </div>
</template>
