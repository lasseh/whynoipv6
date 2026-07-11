<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, toRefs } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { listASNs } from '@/api'
import type { ASN } from '@/api'

// Sort toggle keeps the old ipv4/ipv6 tab vocabulary in the URL (?sort=),
// mapped onto the API's count_total/count_v6 (§7.3). count_v4 is now served
// by the API — no client subtraction.
const props = withDefaults(defineProps<{ query?: string }>(), { query: '' })

const router = useRouter()
const route = useRoute()
const searchQuery = ref(props.query)
const state = reactive({
  isLoading: true,
  asnData: [] as ASN[],
})
const orderBy = computed(() => {
  const sort = route.query.sort
  return sort === 'ipv6' ? 'ipv6' : 'ipv4'
})

const { asnData } = toRefs(state)

async function getAsnData(order: string = 'ipv4') {
  try {
    state.isLoading = true
    const response = await listASNs({ sort: order === 'ipv6' ? 'count_v6' : 'count_total' })
    state.asnData = response.items
  } catch (error) {
    console.error('Failed to fetch ASN data:', error)
  } finally {
    state.isLoading = false
  }
}

async function getOrderedAsnData(order: string) {
  void getAsnData(order)
  // Update the route without adding history and without refreshing the page
  router.replace({ query: { ...route.query, sort: order } }).catch((err) => {
    console.error('Failed to update route:', err)
  })
}

// Search for ASN data
async function searchAsn(query: string) {
  if (query.length < 2) {
    console.error('Search query is too short.')
    return
  }

  state.asnData = []
  listASNs({ q: query })
    .then((response) => {
      state.asnData = response.items
      // Update the URL with the search query
      router.replace({ query: { ...route.query, q: query } }).catch((err) => {
        console.error('Failed to update route:', err)
      })
    })
    .catch((error) => {
      console.error('Failed to search ASN data:', error)
    })
}

const tabClass = (orderType: string): string[] => {
  return [
    'btn border-zinc-700 hover:bg-zinc-800/20 rounded-none first:rounded-l last:rounded-r',
    orderBy.value === orderType ? 'text-fuchsia-600 bg-zinc-500/20' : 'text-slate-300',
  ]
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

onMounted(() => {
  const urlSearchQuery = route.query.q

  if (typeof urlSearchQuery === 'string' && urlSearchQuery.length >= 2) {
    searchQuery.value = urlSearchQuery
    void searchAsn(searchQuery.value)
  } else {
    void getAsnData(orderBy.value)
  }
})

onUnmounted(() => {
  // reset the query parameter for sort
  router.push({ query: { ...router.currentRoute.value.query, sort: undefined } }).catch(() => {})
})
</script>

<template>
  <section>
    <!-- Page header -->
    <div class="sm:flex sm:justify-between sm:items-center mb-4">
      <!-- Left: Title -->
      <div class="mb-4 sm:mb-0">
        <h1 class="text-2xl md:2text-xl text-zinc-100 font-bold">Network Provider Readiness</h1>
      </div>

      <!-- Search -->
      <div class="hidden md:grid grid-flow-col sm:auto-cols-max justify-start sm:justify-end gap-2">
        <form class="relative" @submit.prevent="searchAsn(searchQuery)">
          <label for="action-search" class="sr-only">Search</label>
          <input
            id="action-search"
            v-model="searchQuery"
            class="form-input pl-9 bg-zinc-800 h-10"
            type="search"
            placeholder="Search…"
          />
          <button
            class="absolute inset-0 right-auto group text-xs font-medium"
            type="submit"
            aria-label="Search"
          >
            <svg
              class="w-4 h-4 shrink-0 fill-current text-zinc-500 group-hover:text-zinc-400 ml-3 mr-2"
              viewBox="0 0 16 16"
              xmlns="http://www.w3.org/2000/svg"
            >
              <path
                d="M7 14c-3.86 0-7-3.14-7-7s3.14-7 7-7 7 3.14 7 7-3.14 7-7 7zM7 2C4.243 2 2 4.243 2 7s2.243 5 5 5 5-2.243 5-5-2.243-5-5-5z"
              />
              <path
                d="M15.707 14.293L13.314 11.9a8.019 8.019 0 01-1.414 1.414l2.393 2.393a.997.997 0 001.414 0 .999.999 0 000-1.414z"
              />
            </svg>
          </button>
        </form>
      </div>
    </div>

    <!-- info content -->
    <div class="text-lg text-gray-400">
      <p class="mb-4">
        Unveil the stark reality of network providers' shortcomings in promoting IPv6 adoption and
        supporting their customers. Our analysis, based on the Tranco dataset, shines a spotlight on
        the persisting gaps in IPv6 readiness among these providers. It's time to hold them
        accountable for hindering progress and leaving customers behind. Explore our data and demand
        better connectivity for all.
      </p>
    </div>

    <!-- Search Mobile -->
    <div class="md:hidden grid grid-flow-col sm:auto-cols-max gap-2 mb-2">
      <form class="relative w-full" @submit.prevent="searchAsn(searchQuery)">
        <label for="action-search-mobile" class="sr-only">Search</label>
        <input
          id="action-search-mobile"
          v-model="searchQuery"
          class="form-input pl-9 bg-zinc-800 h-10 w-full"
          type="search"
          placeholder="Search…"
        />
        <button
          class="absolute inset-0 right-auto group text-xs font-medium"
          type="submit"
          aria-label="Search"
        >
          <svg
            class="w-4 h-4 shrink-0 fill-current text-zinc-500 group-hover:text-zinc-400 ml-3 mr-2"
            viewBox="0 0 16 16"
            xmlns="http://www.w3.org/2000/svg"
          >
            <path
              d="M7 14c-3.86 0-7-3.14-7-7s3.14-7 7-7 7 3.14 7 7-3.14 7-7 7zM7 2C4.243 2 2 4.243 2 7s2.243 5 5 5 5-2.243 5-5-2.243-5-5-5z"
            />
            <path
              d="M15.707 14.293L13.314 11.9a8.019 8.019 0 01-1.414 1.414l2.393 2.393a.997.997 0 001.414 0 .999.999 0 000-1.414z"
            />
          </svg>
        </button>
      </form>
    </div>

    <!-- Filter Buttons -->
    <div class="flex h-10 justify-end">
      <button :class="tabClass('ipv4')" @click="getOrderedAsnData('ipv4')">IPv4</button>
      <button :class="tabClass('ipv6')" @click="getOrderedAsnData('ipv6')">IPv6</button>
    </div>

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
        <div class="text-gray-400">{{ formatLargeNumber(asn.count_v6) }} Dual Stack</div>
        <div class="text-gray-400">{{ formatLargeNumber(asn.count_v4) }} IPv4 Only</div>
      </div>
    </div>

    <div v-if="asnData.length === 0">
      <div class="flex justify-between mb-1">
        <span class="text-base font-medium text-white">
          Not Found
          <span class="text-xs font-medium text-gray-500 pl-2">AS404</span>
        </span>
      </div>
      <div class="mb-1 flex h-3 overflow-hidden rounded text-xs">
        <div
          class="flex flex-col justify-center bg-emerald-600 text-black"
          style="width: 100%"
        ></div>
        <div class="flex flex-col justify-center bg-violet-950 text-black" style="width: 0%"></div>
      </div>
      <div class="mb-3 flex items-center justify-between text-xs">
        <div class="text-gray-400">0 IPv6 Enabled</div>
        <div class="text-gray-400">0 IPv4 Only</div>
      </div>
    </div>
  </section>
</template>
