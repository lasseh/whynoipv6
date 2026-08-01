<script setup lang="ts">
import { computed, onScopeDispose, reactive, ref, toRefs, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { LocationQuery } from 'vue-router'

import ApiError from '@/components/ApiError.vue'
import FilterInput from '@/components/FilterInput.vue'
import SegmentedTabs from '@/components/SegmentedTabs.vue'

import { listASNs } from '@/api'
import type { ASN } from '@/api'
import { ApiProblem } from '@/api/problem'

// Sort toggle keeps the old ipv4/ipv6 tab vocabulary in the URL (?sort=),
// mapped onto the API's count_total/count_v6 (§7.3). count_v4 is now served
// by the API — no client subtraction.
const props = withDefaults(defineProps<{ query?: string }>(), { query: '' })

const router = useRouter()
const route = useRoute()
const error = ref<ApiProblem | null>(null)
const state = reactive({
  asnData: [] as ASN[],
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

watch([routeQ, orderBy], ([q]) => {
  if (q !== undefined) searchQuery.value = q
  void load()
})
void load()

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

const barWidths = computed(() => (asn: ASN): [string, string] => {
  const total = asn.count_v4 + asn.count_v6
  if (total === 0) return ['0%', '0%']
  return [
    `${((asn.count_v4 / total) * 100).toFixed(2)}%`,
    `${((asn.count_v6 / total) * 100).toFixed(2)}%`,
  ]
})

const formatLargeNumber = (number: number): string => {
  if (!number) return '0'
  return number >= 1000 ? `${(number / 1000).toFixed(0)}k` : number.toString()
}
</script>

<template>
  <section>
    <!-- Page header -->
    <div class="sm:flex sm:justify-between sm:items-center mb-4">
      <!-- Left: Title -->
      <div class="mb-4 sm:mb-0">
        <h2 class="text-2xl md:text-2xl text-zinc-100 font-bold">IPv6 by Network Provider</h2>
      </div>

      <!-- Search -->
      <div class="hidden md:grid grid-flow-col sm:auto-cols-max justify-start sm:justify-end gap-2">
        <FilterInput
          v-model="searchQuery"
          input-id="asn-search"
          label="Search"
          placeholder="Provider name…"
          input-class="h-10"
          button-class="text-xs font-medium"
          @submit="searchAsn(searchQuery)"
        />
      </div>
    </div>

    <!-- info content -->
    <div class="text-lg text-gray-400">
      <p class="mb-4">
        Every domain we crawl lives on someone's network. Here's the split per provider: hosted
        domains answering over IPv6 versus the ones still IPv4-only. One default-on change at a big
        hosting provider moves thousands of domains to dual stack overnight. The bars show who
        flipped that switch, and who's still on the fence.
      </p>
    </div>

    <!-- Search Mobile -->
    <div class="md:hidden grid grid-flow-col sm:auto-cols-max gap-2 mb-2">
      <FilterInput
        v-model="searchQuery"
        input-id="asn-search-mobile"
        label="Search"
        placeholder="Provider name…"
        class="w-full"
        input-class="h-10 w-full"
        button-class="text-xs font-medium"
        @submit="searchAsn(searchQuery)"
      />
    </div>

    <!-- Filter Buttons -->
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

    <!-- Error state (§6.3) -->
    <ApiError v-if="error" :problem="error" />

    <template v-else>
      <!-- Provider bars -->
      <div v-for="asn in asnData" :key="asn.number">
        <div class="flex justify-between mb-1">
          <span class="text-base font-medium text-white">
            {{ asn.name }}
            <span class="text-xs font-medium text-gray-500 pl-2">AS{{ asn.number }}</span>
          </span>
        </div>
        <div class="mb-1 flex h-3 overflow-hidden rounded text-xs">
          <div
            class="flex flex-col justify-center bg-emerald-600 text-black"
            :style="{ width: barWidths(asn)[1] }"
          ></div>
          <div
            class="flex flex-col justify-center bg-violet-950 text-black"
            :style="{ width: barWidths(asn)[0] }"
          ></div>
        </div>
        <div class="mb-3 flex items-center justify-between text-xs">
          <div class="text-gray-400">{{ formatLargeNumber(asn.count_v6) }} dual-stack</div>
          <div class="text-gray-400">{{ formatLargeNumber(asn.count_v4) }} IPv4-only</div>
        </div>
      </div>

      <p v-if="!isLoading && asnData.length === 0" class="text-gray-400">
        No providers matched. Try a shorter name.
      </p>
    </template>
  </section>
</template>
