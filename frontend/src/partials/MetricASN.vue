<script setup lang="ts">
import { computed, onScopeDispose, ref, shallowRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { LocationQuery } from 'vue-router'

import ApiError from '@/components/ApiError.vue'
import FilterInput from '@/components/FilterInput.vue'
import LeagueTable from '@/components/LeagueTable.vue'
import SegmentedTabs from '@/components/SegmentedTabs.vue'
import ChartPanel from '@/components/charts/ChartPanel.vue'
import ScatterChart from '@/components/charts/ScatterChart.vue'
import { fmtFull } from '@/components/charts/chart'
import NetworkTrendsPanel from '@/partials/metrics/NetworkTrendsPanel.vue'
import ReverseDnsPanel from '@/partials/metrics/ReverseDnsPanel.vue'

import { listASNs, listHostingProviders, listProviders } from '@/api'
import type { ASN, HostingProvider, Provider } from '@/api'
import { ApiProblem } from '@/api/problem'

// Three registries answer the same question about three different entities:
// who carries a lot of domains, and how many of them answer over IPv6. They
// used to be three stacked panels of identical bars. One switcher drives the
// scatter and the league instead, so the page is a third of the height and the
// comparison is between providers rather than between panels.
const props = withDefaults(defineProps<{ query?: string }>(), { query: '' })

type Entity = 'networks' | 'dns' | 'hosting'

const ENTITIES: { value: Entity; label: string }[] = [
  { value: 'networks', label: 'Networks' },
  { value: 'dns', label: 'DNS' },
  { value: 'hosting', label: 'Hosting & CDN' },
]

const router = useRouter()
const route = useRoute()
const error = ref<ApiProblem | null>(null)
// Immutable API snapshots replaced wholesale — shallowRef, like the shared
// composables, so the payloads never get deep proxies.
const asnData = shallowRef<ASN[]>([])
const providers = shallowRef<Provider[]>([])
const hosting = shallowRef<HostingProvider[]>([])
const isLoading = ref(true)

// The URL is the source of truth (§9.1, like useCursorList), so the entity
// carries its own param and deep links survive.
const entity = computed<Entity>(() => {
  const e = route.query.e
  return ENTITIES.some((x) => x.value === e) ? (e as Entity) : 'networks'
})

// ?sort= keeps the old ipv4/ipv6 vocabulary so existing links still work; the
// labels are the clearer wording copy-review.md left as a product decision.
const orderBy = computed(() => (route.query.sort === 'ipv6' ? 'ipv6' : 'ipv4'))

// ?q= is the search scope; it only applies to the network registry, which is
// the one the API pages. Sort tabs apply when no search is active.
const routeQ = computed(() => {
  const q = route.query.q
  return typeof q === 'string' && q.length >= 2 ? q : undefined
})
const searchQuery = ref(routeQ.value ?? props.query)

let controller: AbortController | null = null
onScopeDispose(() => controller?.abort())

// The two one-shot loaders below share a scope-lifetime controller; the
// per-load `controller` above belongs to the abort-and-supersede ASN refetch.
const scopeController = new AbortController()
onScopeDispose(() => scopeController.abort())

async function load() {
  controller?.abort()
  const c = new AbortController()
  controller = c
  error.value = null
  isLoading.value = true
  try {
    const response = await listASNs(
      routeQ.value !== undefined
        ? { q: routeQ.value }
        : { sort: orderBy.value === 'ipv6' ? 'count_v6' : 'count_total' },
      c.signal,
    )
    if (c.signal.aborted) return
    asnData.value = response.items
  } catch (e) {
    if (c.signal.aborted) return
    error.value = ApiProblem.from(e)
  } finally {
    if (controller === c) isLoading.value = false
  }
}

// The DNS and hosting registries are small fixed lists that no filter here
// touches, so they are fetched once rather than alongside every ASN refetch.
async function loadProviders() {
  try {
    const response = await listProviders(undefined, scopeController.signal)
    providers.value = response.items
  } catch {
    // A provider outage should not blank the network league next to it.
    if (!scopeController.signal.aborted) providers.value = []
  }
}

async function loadHosting() {
  try {
    const response = await listHostingProviders(undefined, scopeController.signal)
    hosting.value = response.items
  } catch {
    if (!scopeController.signal.aborted) hosting.value = []
  }
}

// The sort toggle and ?q only drive the server-fetched network league; the
// DNS/hosting tabs sort their fixed lists client-side, so /asns refetches
// only while the networks tab shows it. Watching `entity` covers the return
// path: coming back to networks always refetches with the current ?sort.
watch([routeQ, orderBy, entity], ([q, , e]) => {
  if (q !== undefined) searchQuery.value = q
  if (e === 'networks') void load()
})
if (entity.value === 'networks') void load()
void loadProviders()
void loadHosting()

function setEntity(value: string) {
  // ?q= only means anything for networks, so it does not survive the switch.
  const query: LocationQuery = { ...route.query, e: value }
  delete query.q
  void router.replace({ query })
}

function setSort(order: string) {
  const query: LocationQuery = { ...route.query, sort: order }
  delete query.q
  void router.replace({ query })
}

function searchAsn(query: string) {
  if (query.length < 2) return
  void router.replace({ query: { ...route.query, q: query } })
}

interface Row {
  key: string | number
  name: string
  sub?: string
  total: number
  v6: number
}

const networkRows = computed<Row[]>(() =>
  asnData.value.map((a) => ({
    key: a.number,
    name: a.name,
    sub: `AS${a.number}`,
    total: a.count_total,
    v6: a.count_v6,
  })),
)

const dnsRows = computed<Row[]>(() =>
  providers.value.map((p) => ({
    key: p.id,
    name: p.name,
    total: p.count_total,
    v6: p.count_v6,
  })),
)

// Keyed on the slug, not the display name: the slug is what /domains?hosting=
// takes and the only stable identifier.
const hostingRows = computed<Row[]>(() =>
  hosting.value.map((h) => ({
    key: h.slug,
    name: h.name,
    total: h.count_total,
    v6: h.count_v6,
  })),
)

const ACTIVE: Record<Entity, { rows: () => Row[]; noun: string; blurb: string }> = {
  networks: {
    rows: () => networkRows.value,
    noun: 'networks',
    blurb: 'The autonomous systems the crawled domains actually resolve to.',
  },
  dns: {
    rows: () => dnsRows.value,
    noun: 'DNS providers',
    blurb:
      'Who runs each zone, and whether domains using that provider publish an apex AAAA record.',
  },
  hosting: {
    rows: () => hostingRows.value,
    noun: 'platforms',
    blurb:
      'The platform serving the site. A league among the platforms we can attribute, not a market survey.',
  },
}

const active = computed(() => ACTIVE[entity.value])

// The API pages ASNs by its own ordering; DNS and hosting arrive whole, so they
// are sorted here to keep the toggle meaning the same thing everywhere.
const sorted = computed(() => {
  const rows = active.value.rows()
  if (entity.value === 'networks') return rows
  return [...rows].sort((a, b) => (orderBy.value === 'ipv6' ? b.v6 - a.v6 : b.total - a.total))
})

const LEAGUE_LIMIT = 15
const leagueRows = computed(() => sorted.value.slice(0, LEAGUE_LIMIT))
const truncated = computed(() => sorted.value.length > LEAGUE_LIMIT)

// /asns applies no size floor, so without one a three-domain reseller at 100%
// lands top-left and reads as a star. The dropped count is shown, not hidden.
const SCATTER_FLOOR = 500
const scatterPoints = computed(() =>
  sorted.value
    .filter((r) => r.total >= SCATTER_FLOOR)
    .map((r) => ({
      key: r.key,
      label: r.name,
      sub: r.sub,
      x: r.total,
      y: r.total > 0 ? (r.v6 / r.total) * 100 : 0,
    })),
)
const belowFloor = computed(() => sorted.value.length - scatterPoints.value.length)

const laggards = computed(() => {
  const pts = scatterPoints.value
  if (pts.length === 0) return null
  return { under: pts.filter((p) => p.y < 5).length, total: pts.length }
})
</script>

<template>
  <section>
    <header class="mb-6">
      <h2 class="mb-2 text-2xl font-bold text-zinc-100">IPv6 by provider</h2>
      <p class="text-lg text-gray-400">
        The tabs group active ranked domains by attributed network, DNS provider, and hosting or CDN
        platform. Some attribution is unknown, and currently empty entries can appear. A default-on
        IPv6 change at a large provider can affect thousands of domains at once.
      </p>
    </header>

    <div class="mb-4">
      <SegmentedTabs :options="ENTITIES" :model-value="entity" @update:model-value="setEntity" />
    </div>

    <ApiError v-if="error" :problem="error" />

    <div v-else class="grid gap-4">
      <ChartPanel
        title="Size against adoption"
        :description="`Each ${active.noun.replace(/s$/, '')} is placed by attributed domain count and apex IPv6 share. The bottom-right corner is the interesting one: plenty of domains, little IPv6.`"
      >
        <ScatterChart
          :points="scatterPoints"
          :label="`IPv6 adoption against domains attributed, per ${active.noun.replace(/s$/, '')}`"
        />
        <p class="mt-3 text-xs text-gray-500">
          <template v-if="laggards">
            {{ laggards.under }} of the {{ laggards.total }} plotted are under 5%.
          </template>
          Anything under {{ fmtFull(SCATTER_FLOOR) }} domains is left off
          <template v-if="belowFloor > 0">({{ belowFloor }} here)</template>
          — at that size a single dual-stack customer swings the percentage.
        </p>
      </ChartPanel>

      <section class="rounded border border-gray-700 bg-gray-800/60 p-5">
        <header class="mb-4 gap-3 sm:flex sm:items-start sm:justify-between">
          <div class="mb-3 sm:mb-0">
            <h3 class="text-base font-medium text-zinc-100">The league</h3>
            <p class="mt-0.5 text-sm text-gray-400">{{ active.blurb }}</p>
          </div>
          <div class="flex shrink-0 items-center gap-2">
            <FilterInput
              v-if="entity === 'networks'"
              v-model="searchQuery"
              input-id="asn-search"
              label="Search"
              placeholder="Provider name…"
              class="w-full sm:w-52"
              input-class="h-9 w-full text-sm"
              button-class="text-xs font-medium"
              @submit="searchAsn(searchQuery)"
            />
          </div>
        </header>

        <div class="mb-4">
          <SegmentedTabs
            :options="[
              { value: 'ipv4', label: 'Most domains' },
              { value: 'ipv6', label: 'Most IPv6' },
            ]"
            :model-value="orderBy"
            @update:model-value="setSort"
          />
        </div>

        <LeagueTable :rows="leagueRows" />
        <p v-if="isLoading && entity === 'networks'" class="text-gray-400">Loading…</p>
        <p v-else-if="truncated" class="text-xs text-gray-500">
          Showing the top {{ LEAGUE_LIMIT }} of {{ sorted.length }}.
          <template v-if="entity === 'networks'">Search to find a specific network.</template>
        </p>
      </section>

      <NetworkTrendsPanel />

      <ReverseDnsPanel />
    </div>
  </section>
</template>
