<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref } from 'vue'

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

import { getChangeStats, getCrawlerStats, getOverviewStats } from '@/api'
import type { ChangePoint, CrawlerStats, GlobalStatsPoint } from '@/api'
import { ApiProblem } from '@/api/problem'

// GET /stats/overview is fetched once and used twice: the last point drives the
// tiles, the whole series drives the two charts. The old version threw the
// series away after reading `.at(-1)`, which is why this page had no history on
// it despite already holding a fortnight of daily snapshots.
const points = ref<GlobalStatsPoint[]>([])
const crawler = ref<CrawlerStats | null>(null)
const changes = ref<ChangePoint[]>([])
const isLoading = ref(true)
const error = ref<ApiProblem | null>(null)

// One scope-lifetime controller for the three one-shot fetches: leaving the
// tab aborts them instead of letting them resolve into a dead component.
const controller = new AbortController()
onScopeDispose(() => controller.abort())

// The endpoint defaults to `to − 90d`, but a chart's x-axis should be stated
// by whoever draws it, not inherited from a server default that can move.
const WINDOW_DAYS = 90

function windowStart(): string {
  const start = new Date()
  start.setUTCDate(start.getUTCDate() - WINDOW_DAYS)
  return start.toISOString().slice(0, 10)
}

async function load() {
  error.value = null
  try {
    const response = await getOverviewStats({ from: windowStart() }, controller.signal)
    // A snapshot with no domains in it is the stats job having run before the
    // crawler's first pass finished, not a day on which the list was empty.
    // Charting it draws a cliff out of nothing.
    points.value = response.points
      .filter((p) => (p.domains ?? 0) > 0)
      .sort((a, b) => (a.day < b.day ? -1 : 1))
  } catch (e) {
    if (controller.signal.aborted) return
    points.value = []
    error.value = ApiProblem.from(e)
  } finally {
    isLoading.value = false
  }
}

// Throughput is a separate, continuously-moving resource on its own cache
// class. Its failure must not blank the adoption tiles beside it, so it is
// fetched independently rather than folded into load().
async function loadCrawler() {
  try {
    crawler.value = await getCrawlerStats(controller.signal)
  } catch {
    if (!controller.signal.aborted) crawler.value = null
  }
}

// Churn sits on the changelog cache class rather than the daily generation,
// so it is its own request.
async function loadChanges() {
  try {
    const response = await getChangeStats({ from: windowStart() }, controller.signal)
    changes.value = response.points
  } catch {
    if (!controller.signal.aborted) changes.value = []
  }
}

onMounted(() => {
  void load()
  void loadCrawler()
  void loadChanges()
})

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

// BarDeltaChart mirrors the second series itself, so both are passed as raw
// positive counts in gained-then-lost order.
const deltaValues = computed(() => [
  changes.value.map((d) => d.gained),
  changes.value.map((d) => d.lost),
])

// The idle loop checkpoints every five minutes even with nothing to do, so a
// timestamp hours old means a dead process rather than a quiet one. Worth
// saying on the tile: it is the only data-age signal the page has.
const STALE_AFTER_HOURS = 3
const crawlerHint = computed(() => {
  const base = 'Includes retries and failures, so one host can count more than once.'
  const latest = crawler.value?.latest
  if (!latest) return base
  const hours = Math.floor((Date.now() - new Date(latest).getTime()) / 3_600_000)
  return hours >= STALE_AFTER_HOURS ? `${base} Last checkpoint ${hours}h ago.` : base
})

// The advisory checks come off the same /stats/overview point as everything
// else. They are null on snapshots taken before the columns existed, and pct()
// propagates that to an em dash rather than a confident 0%.
const ptrShare = computed(() => pct(latest.value?.ptr_supported, latest.value?.ptr_graded))
const smtpShare = computed(() => pct(latest.value?.smtp_supported, latest.value?.smtp_graded))

// "Looks ready" is the gap between a gradeable MX and one that answered.
const smtpPaperOnly = computed(() => {
  const graded = latest.value?.smtp_graded
  const answering = latest.value?.smtp_supported
  return graded == null || answering == null ? null : graded - answering
})
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
        ranked domains, only
        <span class="text-fuchsia-600">{{ fmtPercent(heroShare) }}</span>
        are Heroes. In the Tranco top 1000,
        <span class="text-fuchsia-600">{{ latest.top_heroes ?? '—' }}</span>
        have apex IPv6 and no confirmed www failure;
        <span class="text-fuchsia-600">{{ latest.top_nameserver ?? '—' }}</span>
        use nameserver hosts with AAAA records.
      </p>
      <p class="text-lg text-gray-400">
        IPv6 became a standard in 1998, and again in 2017 in case anyone missed it the first time.
        The numbers below show how "we'll get to it" is going.
      </p>
    </header>

    <div class="mb-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <!-- The whole tracked set, so campaign and curated entries are in it.
           The tiers below are scored against the ranked list, which is why the
           hint spells the split out rather than leaving two numbers to clash. -->
      <StatTile
        :value="fmtCompact(latest.tracked_total)"
        label="Domains tracked"
        :hint="`${fmtCompact(latest.domains)} are in the current Tranco ranking. The rest are campaign entries, curated lists, and domains that fell off it.`"
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
        :hint="`${latest.top_nameserver ?? '—'} use at least one nameserver host with an AAAA record.`"
      />
      <StatTile
        :value="fmtCompact(latest.heroes)"
        label="Heroes"
        :hint="`${fmtCompact(latest.saints)} are Saints: they pass the page-resource grade too.`"
        tone="good"
      />
      <StatTile
        :value="fmtCompact(latest.sinners)"
        label="Sinners"
        hint="An apex A record, but no globally routable AAAA. That's the whole classification."
        tone="bad"
      />
      <StatTile
        :value="fmtCompact(crawler?.checked_24h)"
        label="Checks attempted in 24 hours"
        :hint="crawlerHint"
        tone="muted"
      />
    </div>

    <div class="mb-8 grid gap-4">
      <ChartPanel
        title="Where ranked domains sit, day by day"
        description="Every active ranked domain lands in exactly one class, so the bands add up to the list."
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
        description="Apex records that flipped to supported, against the ones that flipped back. Churn, not net movement: a domain can appear in both bars on the same day."
      >
        <BarDeltaChart
          :labels="changes.map((d) => d.day)"
          :series="DELTA_SERIES.map((s) => ({ ...s }))"
          :values="deltaValues"
          :format-value="fmtCompact"
          label="Daily count of domains gaining and losing apex IPv6 support"
        />
      </ChartPanel>
    </div>

    <header class="mb-4">
      <h3 class="h4 mb-1">Beyond a AAAA record</h3>
      <p class="text-lg text-gray-400">
        AAAA is only the DNS part. These advisory checks show what works beyond it; they never
        change a rating.
      </p>
    </header>

    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <StatTile
        :value="fmtPercent(ptrShare)"
        label="Reverse DNS on IPv6 hosts"
        :hint="`${fmtFull(latest.ptr_supported)} of ${fmtFull(latest.ptr_graded)} graded hosts answer a PTR lookup.`"
        tone="bad"
      />
      <StatTile
        :value="fmtPercent(smtpShare)"
        label="Mail that answers over IPv6"
        :hint="`${fmtFull(latest.smtp_supported)} of ${fmtFull(latest.smtp_graded)} graded mail servers presented a banner.`"
        tone="good"
      />
      <StatTile
        :value="fmtCompact(smtpPaperOnly)"
        label="Mail that only looks ready"
        hint="The MX has an AAAA record, but the SMTP check failed."
        tone="bad"
      />
    </div>
  </section>
</template>
