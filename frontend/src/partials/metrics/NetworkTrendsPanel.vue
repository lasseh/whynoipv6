<script setup lang="ts">
// Only networks have a daily series; DNS and hosting have no history stored
// at all, so this panel owns its own fetch and does not follow the provider
// switcher.
import { computed, onScopeDispose, shallowRef } from 'vue'

import ChartPanel from '@/components/charts/ChartPanel.vue'
import Sparkline from '@/components/charts/Sparkline.vue'
import { fmtPercent, shareColor } from '@/components/charts/chart'

import { getNetworkStats } from '@/api'
import type { NetworkTrend } from '@/api'

const networks = shallowRef<NetworkTrend[]>([])

const controller = new AbortController()
onScopeDispose(() => controller.abort())

// One request for all seven small multiples; /asns/{number}/stats would be
// seven round trips for the same panel.
async function load() {
  try {
    const response = await getNetworkStats(undefined, controller.signal)
    networks.value = response.networks
  } catch {
    // The panel going quiet must not blank the leagues beside it.
    if (!controller.signal.aborted) networks.value = []
  }
}
void load()

// Small multiples, because these seven sit between 0.8% and 86% and a shared
// axis flattens five of them onto the baseline.
// The endpoint serves counts, not a share, so the denominator stays visible:
// coverage is still growing, and a percentage alone would move when the
// denominator moved and read as deployment. Days with no total are dropped
// rather than plotted as zero.
const trends = computed(() =>
  networks.value.map((n) => {
    const share = n.points
      .filter((p) => p.count_total)
      .map((p) => ((p.count_v6 ?? 0) / (p.count_total as number)) * 100)
    const last = share.at(-1) ?? 0
    return {
      asn: n.asn,
      name: n.name,
      share,
      last,
      days: share.length,
      // Same ramp as the league bar and the scatter dot, so one network is one
      // colour everywhere on the page.
      color: shareColor(last),
      lo: share.length ? Math.min(...share) : 0,
      hi: share.length ? Math.max(...share) : 0,
    }
  }),
)
</script>

<template>
  <ChartPanel
    title="Network adoption, day by day"
    description="One box per network, each scaled to itself. Read the levels rather than the slopes: coverage is still growing, so a line can move because we reached more of a network's domains, not because it deployed anything."
  >
    <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      <div
        v-for="t in trends"
        :key="t.asn"
        class="rounded border border-gray-700/60 bg-gray-900/40 p-3"
      >
        <div class="mb-2 flex items-baseline justify-between gap-2">
          <span class="truncate text-sm text-gray-300">
            {{ t.name }}
            <span class="pl-1 text-xs text-gray-500">AS{{ t.asn }}</span>
          </span>
          <span class="shrink-0 font-mono text-sm" :style="{ color: t.color }">
            {{ fmtPercent(t.last) }}
          </span>
        </div>
        <Sparkline :values="t.share" :color="t.color" />
        <div class="mt-2 text-xs text-gray-500">
          {{ fmtPercent(t.lo) }} to {{ fmtPercent(t.hi) }} over {{ t.days }} days
        </div>
      </div>
    </div>
  </ChartPanel>
</template>
