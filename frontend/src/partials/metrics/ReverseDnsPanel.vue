<script setup lang="ts">
// The reverse-DNS panel reads the same daily snapshot the overview tab does.
// Fetched here rather than lifted into the page because the two tabs never
// render together, so there is no duplicate request to save.
import { computed } from 'vue'

import ShareBar from '@/components/ShareBar.vue'
import { fmtCompact, fmtFull, fmtPercent, shareColor } from '@/components/charts/chart'

import { useResource } from '@/composables/useResource'
import { getOverviewStats } from '@/api'
import type { GlobalStatsPoint } from '@/api'

// The fallback states the rule the hand-rolled catch used to: this panel
// going quiet must not blank the leagues beside it.
const { data: overview } = useResource(
  (signal) => getOverviewStats(undefined, signal).then((r) => r.points.at(-1) ?? null),
  { fallback: null as GlobalStatsPoint | null },
)

// Nullable all the way through: snapshots taken before the PTR columns existed
// carry no value, and an em dash is the honest render. Coercing to 0 would
// claim "no host has reverse DNS", which is a measurement, not a gap.
const ptrSupported = computed(() => overview.value?.ptr_supported ?? null)
const ptrGraded = computed(() => overview.value?.ptr_graded ?? null)
const ptrShare = computed(() => {
  const supported = ptrSupported.value
  const graded = ptrGraded.value
  return supported == null || !graded ? null : (supported / graded) * 100
})
const ptrWithout = computed(() => {
  const supported = ptrSupported.value
  const graded = ptrGraded.value
  return supported == null || graded == null ? null : graded - supported
})
</script>

<template>
  <section class="rounded border border-gray-700 bg-gray-800/60 p-5">
    <header class="mb-4 flex items-start justify-between gap-3">
      <div>
        <h3 class="text-base font-medium text-zinc-100">Reverse DNS</h3>
        <p class="mt-0.5 text-sm text-gray-400">
          Of the hosts that answer over IPv6, how many resolve back to a name. Mail servers and
          logging tools care; almost nobody else has noticed.
        </p>
      </div>
    </header>

    <div class="mb-3 flex items-baseline gap-3">
      <span
        class="text-3xl font-bold tracking-tighter"
        :style="{ color: shareColor(ptrShare ?? 0) }"
        >{{ fmtPercent(ptrShare) }}</span
      >
      <span class="text-sm text-gray-400">
        of {{ fmtCompact(ptrGraded) }} IPv6 hosts resolve back to a name
      </span>
    </div>
    <ShareBar class="mb-1.5" :share="ptrShare ?? 0" label="IPv6 hosts with reverse DNS" />
    <div class="flex items-center justify-between text-xs text-gray-500">
      <span>{{ fmtFull(ptrSupported) }} have a PTR record</span>
      <span>{{ fmtFull(ptrWithout) }} have none</span>
    </div>
  </section>
</template>
