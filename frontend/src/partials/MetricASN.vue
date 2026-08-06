<script setup lang="ts">
import { computed, onScopeDispose, reactive, ref, toRefs, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { LocationQuery } from 'vue-router'

import ApiError from '@/components/ApiError.vue'
import FilterInput from '@/components/FilterInput.vue'
import LeagueTable from '@/components/LeagueTable.vue'
import SampleBadge from '@/components/SampleBadge.vue'
import SegmentedTabs from '@/components/SegmentedTabs.vue'
import ChartPanel from '@/components/charts/ChartPanel.vue'
import LineChart from '@/components/charts/LineChart.vue'
import { CATEGORICAL, fmtFull, fmtPercent } from '@/components/charts/chart'

import { listASNs, listProviders } from '@/api'
import type { ASN, Provider } from '@/api'
import { ApiProblem } from '@/api/problem'
import { hostingLeague, networkAdoption, reverseDns } from '@/fixtures/metrics'

// Sort toggle keeps the old ipv4/ipv6 tab vocabulary in the URL (?sort=),
// mapped onto the API's count_total/count_v6 (§7.3). count_v4 is now served
// by the API — no client subtraction.
const props = withDefaults(defineProps<{ query?: string }>(), { query: '' })

const router = useRouter()
const route = useRoute()
const error = ref<ApiProblem | null>(null)
const state = reactive({
  asnData: [] as ASN[],
  providers: [] as Provider[],
  isLoading: true,
})
const orderBy = computed(() => {
  const sort = route.query.sort
  return sort === 'ipv6' ? 'ipv6' : 'ipv4'
})
// ?q= is the search scope; sort tabs apply when no search is active.
const routeQ = computed(() => {
  const q = route.query.q
  return typeof q === 'string' && q.length >= 2 ? q : undefined
})
const searchQuery = ref(routeQ.value ?? props.query)

const { asnData, isLoading } = toRefs(state)

// The URL is the source of truth (§9.1, like useCursorList): tab clicks and
// search submits only navigate; the route watcher is the sole fetch trigger,
// and superseded or unmounted fetches abort.
let controller: AbortController | null = null
onScopeDispose(() => controller?.abort())

async function load() {
  controller?.abort()
  const c = new AbortController()
  controller = c
  error.value = null
  state.isLoading = true
  try {
    const response = await listASNs(
      routeQ.value !== undefined
        ? { q: routeQ.value }
        : { sort: orderBy.value === 'ipv6' ? 'count_v6' : 'count_total' },
      c.signal,
    )
    if (c.signal.aborted) return
    state.asnData = response.items
  } catch (e) {
    if (c.signal.aborted) return
    error.value = ApiProblem.from(e)
  } finally {
    if (controller === c) state.isLoading = false
  }
}

// The DNS registry is a small fixed list that no filter on this page touches,
// so it is fetched once and never refetched with the ASN table.
async function loadProviders() {
  try {
    const response = await listProviders()
    state.providers = response.items
  } catch {
    // A provider outage should not blank the network league next to it.
    state.providers = []
  }
}

watch([routeQ, orderBy], ([q]) => {
  if (q !== undefined) searchQuery.value = q
  void load()
})
void load()
void loadProviders()

function setSort(order: string) {
  // Dropping ?q= returns the list to the chosen sort order.
  const query: LocationQuery = { ...route.query, sort: order }
  delete query.q
  void router.replace({ query })
}

function searchAsn(query: string) {
  if (query.length < 2) {
    return
  }
  void router.replace({ query: { ...route.query, q: query } })
}

// A league is a top table, not a browser: three more of them share this page,
// and the full list already has a home on /domains.
const NETWORK_LIMIT = 15

const networkRows = computed(() =>
  asnData.value.slice(0, NETWORK_LIMIT).map((a) => ({
    key: a.number,
    name: a.name,
    sub: `AS${a.number}`,
    total: a.count_total,
    v6: a.count_v6,
  })),
)

const truncated = computed(() => asnData.value.length > NETWORK_LIMIT)

const dnsRows = computed(() =>
  state.providers
    .slice()
    .sort((a, b) => b.count_total - a.count_total)
    .slice(0, 12)
    .map((p) => ({ key: p.id, name: p.name, total: p.count_total, v6: p.count_v6 })),
)

const hostingRows = hostingLeague
  .slice(0, 12)
  .map((h) => ({ key: h.name, name: h.name, total: h.domains, v6: h.apexV6 }))

const adoptionSeries = networkAdoption.networks.map((n, i) => ({
  key: n.name,
  label: n.name,
  color: CATEGORICAL[i % CATEGORICAL.length]!,
}))
const adoptionValues = networkAdoption.networks.map((n) => n.share)

const ptrGraded = reverseDns.withPtr + reverseDns.withoutPtr
const ptrShare = ptrGraded > 0 ? (reverseDns.withPtr / ptrGraded) * 100 : 0
</script>

<template>
  <section>
    <header class="mb-6">
      <h2 class="mb-2 text-2xl font-bold text-zinc-100">IPv6 by Network Provider</h2>
      <p class="text-lg text-gray-400">
        Every domain we crawl lives on someone's network. Here's the split per provider: hosted
        domains answering over IPv6 versus the ones still IPv4-only. One default-on change at a big
        hosting provider moves thousands of domains to dual stack overnight. The bars show who
        flipped that switch, and who's still on the fence.
      </p>
    </header>

    <ApiError v-if="error" :problem="error" />

    <div v-else class="grid gap-4">
      <!-- The search box and sort tabs scope this league only, so they live
           inside its panel — three more leagues sit below it. -->
      <section class="rounded border border-gray-700 bg-gray-800/60 p-5">
        <header class="mb-4 gap-3 sm:flex sm:items-start sm:justify-between">
          <div class="mb-3 sm:mb-0">
            <h3 class="text-base font-medium text-zinc-100">Network league</h3>
            <p class="mt-0.5 text-sm text-gray-400">
              The autonomous systems the crawled domains actually resolve to.
            </p>
          </div>
          <FilterInput
            v-model="searchQuery"
            input-id="asn-search"
            label="Search"
            placeholder="Provider name…"
            class="shrink-0 sm:w-56"
            input-class="h-9 w-full text-sm"
            button-class="text-xs font-medium"
            @submit="searchAsn(searchQuery)"
          />
        </header>

        <div class="mb-4">
          <SegmentedTabs
            :options="[
              { value: 'ipv4', label: 'IPv4' },
              { value: 'ipv6', label: 'IPv6' },
            ]"
            :model-value="orderBy"
            @update:model-value="setSort"
          />
        </div>

        <LeagueTable :rows="networkRows" />
        <p v-if="isLoading" class="text-gray-400">Loading…</p>
        <p v-else-if="truncated" class="text-xs text-gray-500">
          Showing the top {{ NETWORK_LIMIT }}. Search to find a specific network.
        </p>
      </section>

      <ChartPanel
        title="Network adoption over time"
        description="Share of each network's hosted domains answering over IPv6."
        sample
      >
        <LineChart
          :labels="networkAdoption.days"
          :series="adoptionSeries"
          :values="adoptionValues"
          :format-value="(n: number) => fmtPercent(n, 0)"
          :y-max="100"
          label="Daily IPv6 share per hosting network"
        />
      </ChartPanel>

      <section class="rounded border border-gray-700 bg-gray-800/60 p-5">
        <header class="mb-4">
          <h3 class="text-base font-medium text-zinc-100">DNS provider league</h3>
          <p class="mt-0.5 text-sm text-gray-400">
            Who runs the zone, and whether the domains in it resolve to an AAAA.
          </p>
        </header>
        <LeagueTable :rows="dnsRows" empty-text="No DNS providers to show yet." />
      </section>

      <section class="rounded border border-gray-700 bg-gray-800/60 p-5">
        <header class="mb-4 flex items-start justify-between gap-3">
          <div>
            <h3 class="text-base font-medium text-zinc-100">Hosting and CDN league</h3>
            <p class="mt-0.5 text-sm text-gray-400">
              The platform serving the site, which is usually the one that decides.
            </p>
          </div>
          <SampleBadge />
        </header>
        <LeagueTable :rows="hostingRows" />
      </section>

      <section class="rounded border border-gray-700 bg-gray-800/60 p-5">
        <header class="mb-4 flex items-start justify-between gap-3">
          <div>
            <h3 class="text-base font-medium text-zinc-100">Reverse DNS</h3>
            <p class="mt-0.5 text-sm text-gray-400">
              Of the hosts that answer over IPv6, how many resolve back to a name. Mail servers and
              logging tools care; almost nobody else has noticed.
            </p>
          </div>
          <SampleBadge />
        </header>

        <div class="mb-3 flex items-baseline gap-3">
          <span class="text-3xl font-bold tracking-tighter text-rose-600">{{
            fmtPercent(ptrShare)
          }}</span>
          <span class="text-sm text-gray-400">
            of {{ fmtFull(ptrGraded) }} IPv6 hosts resolve back to a name
          </span>
        </div>
        <div class="mb-1 flex h-3 overflow-hidden rounded">
          <div class="bg-emerald-600" :style="{ width: `${ptrShare.toFixed(2)}%` }"></div>
          <div class="bg-violet-950" :style="{ width: `${(100 - ptrShare).toFixed(2)}%` }"></div>
        </div>
        <div class="flex items-center justify-between text-xs text-gray-400">
          <span>{{ fmtFull(reverseDns.withPtr) }} have a PTR record</span>
          <span>{{ fmtFull(reverseDns.withoutPtr) }} have none</span>
        </div>
      </section>
    </div>
  </section>
</template>
