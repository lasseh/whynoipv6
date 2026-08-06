<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import ApiError from '@/components/ApiError.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'
import StatTile from '@/components/StatTile.vue'
import AreaStackChart from '@/components/charts/AreaStackChart.vue'
import BarDeltaChart from '@/components/charts/BarDeltaChart.vue'
import ChartPanel from '@/components/charts/ChartPanel.vue'
import LineChart from '@/components/charts/LineChart.vue'
import {
  DELTA_COLOR,
  DIMENSION_COLOR,
  TIER_COLOR,
  fmtCompact,
  fmtFull,
  fmtPercent,
  pluck,
} from '@/components/charts/chart'

import { getOverviewStats } from '@/api'
import type { GlobalStatsPoint } from '@/api'
import { ApiProblem } from '@/api/problem'
import { adoptionDelta, crawlerToday, mail, reverseDns } from '@/fixtures/metrics'

// GET /stats/overview is fetched once and used twice: the last point drives the
// tiles, the whole series drives the two charts. The old version threw the
// series away after reading `.at(-1)`, which is why this page had no history on
// it despite already holding a fortnight of daily snapshots.
const points = ref<GlobalStatsPoint[]>([])
const isLoading = ref(true)
const error = ref<ApiProblem | null>(null)

async function load() {
  error.value = null
  try {
    const response = await getOverviewStats()
    // A snapshot with no domains in it is the stats job having run before the
    // crawler's first pass finished, not a day on which the list was empty.
    // Charting it draws a cliff out of nothing.
    points.value = response.points
      .filter((p) => (p.domains ?? 0) > 0)
      .sort((a, b) => (a.day < b.day ? -1 : 1))
  } catch (e) {
    points.value = []
    error.value = ApiProblem.from(e)
  } finally {
    isLoading.value = false
  }
}

onMounted(() => void load())

const latest = computed(() => points.value.at(-1) ?? null)
const days = computed(() => points.value.map((p) => p.day))

const pct = (part: number | null | undefined, whole: number | null | undefined): number | null =>
  part == null || !whole ? null : (part / whole) * 100

// Heroes over the tracked set. The previous computed divided by
// `domains + heroes`, but heroes are a subset of domains, so the denominator
// counted every hero twice and the headline number came out low.
const heroShare = computed(() => pct(latest.value?.heroes, latest.value?.domains))
const apexShare = computed(() => pct(latest.value?.base_supported, latest.value?.domains))

const TIERS = [
  { key: 'heroes', label: 'Heroes', color: TIER_COLOR.heroes },
  { key: 'partial', label: 'Partial', color: TIER_COLOR.partial },
  { key: 'sinners', label: 'Sinners', color: TIER_COLOR.sinners },
  { key: 'inactive', label: 'Inactive', color: TIER_COLOR.inactive },
  { key: 'unknown', label: 'Unknown', color: TIER_COLOR.unknown },
] as const

const DIMENSIONS = [
  { key: 'base_supported', label: 'Apex (AAAA)', color: DIMENSION_COLOR.base },
  { key: 'www_supported', label: 'WWW (AAAA)', color: DIMENSION_COLOR.www },
  { key: 'ns_supported', label: 'Nameservers', color: DIMENSION_COLOR.ns },
  { key: 'mx_supported', label: 'Mail (MX)', color: DIMENSION_COLOR.mx },
  { key: 'conn_supported', label: 'IPv6-only reachability', color: DIMENSION_COLOR.conn },
  { key: 'resources_supported', label: 'Page resources', color: DIMENSION_COLOR.resources },
] as const

const tierValues = computed(() =>
  pluck(
    points.value,
    TIERS.map((t) => t.key),
  ),
)
const dimensionValues = computed(() =>
  pluck(
    points.value,
    DIMENSIONS.map((d) => d.key),
  ),
)

const DELTA_SERIES = [
  { key: 'gained', label: 'Gained IPv6', color: DELTA_COLOR.gained },
  { key: 'lost', label: 'Lost IPv6', color: DELTA_COLOR.lost },
] as const

const deltaValues = [adoptionDelta.map((d) => d.gained), adoptionDelta.map((d) => d.lost)]

const ptrShare = computed(() => pct(reverseDns.withPtr, reverseDns.withPtr + reverseDns.withoutPtr))
const smtpGraded = mail.answering + mail.paperOnly
const smtpShare = computed(() => pct(mail.answering, smtpGraded))
</script>

<template>
  <ApiError v-if="error" :problem="error" />

  <div v-else-if="isLoading" class="py-16 text-center"><LoadingSpinner /></div>

  <section v-else-if="latest">
    <header class="mb-8">
      <h3 class="h4 mb-1">IPv6 adoption, live from Why No IPv6</h3>
      <p class="mb-2 text-lg text-gray-400">
        Of the
        <span class="text-fuchsia-600">{{ fmtFull(latest.domains) }}</span>
        most-visited websites on the internet, only
        <span class="text-fuchsia-600">{{ fmtPercent(heroShare) }}</span>
        are fully IPv6-ready. IPv6 has been a standard since 1998. In the Tranco top 1000 the
        picture is no prettier:
        <span class="text-fuchsia-600">{{ latest.top_heroes ?? '—' }}</span>
        have IPv6 enabled, and
        <span class="text-fuchsia-600">{{ latest.top_nameserver ?? '—' }}</span>
        sit behind nameservers reachable over IPv6.
      </p>
      <p class="text-lg text-gray-400">
        For context: IPv6 became a standard in 1998, and again in 2017, in case anyone missed it the
        first time. The numbers below are what nearly three decades of "we'll get to it" looks like.
        Every one of them moves the day someone publishes an AAAA record.
      </p>
    </header>

    <div class="mb-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <StatTile
        :value="fmtCompact(latest.domains)"
        label="Domains tracked"
        hint="Ranked by Tranco, minus the ones that stopped resolving."
        tone="muted"
      />
      <StatTile
        :value="fmtPercent(apexShare)"
        label="Apex IPv6 adoption"
        :hint="`${fmtFull(latest.base_supported)} domains publish an AAAA record.`"
      />
      <StatTile
        :value="fmtPercent((latest.top_heroes ?? 0) / 10)"
        label="Top 1000 with IPv6"
        :hint="`${latest.top_nameserver ?? '—'} of them have IPv6 nameservers.`"
      />
      <StatTile
        :value="fmtCompact(latest.heroes)"
        label="Heroes"
        :hint="`${fmtCompact(latest.saints)} are Saints: page resources over IPv6 too.`"
        tone="good"
      />
      <StatTile
        :value="fmtCompact(latest.sinners)"
        label="Sinners"
        hint="No AAAA anywhere. One DNS change from the exit."
        tone="bad"
      />
      <StatTile
        :value="fmtCompact(crawlerToday.checked)"
        label="Domains checked today"
        hint="The crawler sweeps the whole list every 24 hours."
        tone="muted"
        sample
      />
    </div>

    <div class="mb-8 grid gap-4">
      <ChartPanel
        title="Where domains sit, day by day"
        description="Every tracked domain lands in exactly one class, so the bands add up to the list."
      >
        <AreaStackChart
          :labels="days"
          :series="TIERS.map((t) => ({ ...t }))"
          :values="tierValues"
          :format-value="fmtCompact"
          label="Stacked daily count of domains per classification"
        />
      </ChartPanel>

      <ChartPanel
        title="IPv6 support by dimension"
        description="The six checks, counted separately. Nameservers lead; the pages they point at do not."
      >
        <LineChart
          :labels="days"
          :series="DIMENSIONS.map((d) => ({ ...d, key: d.key }))"
          :values="dimensionValues"
          :format-value="fmtCompact"
          label="Daily count of domains passing each IPv6 check"
        />
      </ChartPanel>

      <ChartPanel
        title="IPv6 gained and lost per day"
        description="Checks that flipped to supported, against the ones that flipped back."
        sample
      >
        <BarDeltaChart
          :labels="adoptionDelta.map((d) => d.day)"
          :series="DELTA_SERIES.map((s) => ({ ...s }))"
          :values="deltaValues"
          :format-value="fmtCompact"
          label="Daily count of checks gaining and losing IPv6 support"
        />
      </ChartPanel>
    </div>

    <header class="mb-4">
      <h3 class="h4 mb-1">Beyond a AAAA record</h3>
      <p class="text-lg text-gray-400">
        An AAAA record is the entry fee, not the finish line. These three checks are advisory: they
        never change a rating, they just show how much of the deployment was finished.
      </p>
    </header>

    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <StatTile
        :value="fmtPercent(ptrShare)"
        label="Reverse DNS on IPv6 hosts"
        :hint="`${fmtFull(reverseDns.withPtr)} of ${fmtFull(reverseDns.withPtr + reverseDns.withoutPtr)} graded hosts answer a PTR lookup.`"
        tone="bad"
        sample
      />
      <StatTile
        :value="fmtPercent(smtpShare)"
        label="Mail that answers over IPv6"
        :hint="`${fmtFull(mail.answering)} of ${fmtFull(smtpGraded)} graded mail servers presented a banner.`"
        tone="good"
        sample
      />
      <StatTile
        :value="fmtCompact(mail.paperOnly)"
        label="Mail that only looks ready"
        hint="The MX has an AAAA record. Nothing answers on it."
        tone="bad"
        sample
      />
    </div>
  </section>
</template>
