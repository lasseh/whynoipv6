<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import ApiError from '@/components/ApiError.vue'

import { getOverviewStats } from '@/api'
import type { GlobalStatsPoint } from '@/api'
import { ApiProblem } from '@/api/problem'

// Latest point of GET /stats/overview; old field mapping per §7.3:
// base_domain→base_supported, nameserver→ns_supported, mx_record→mx_supported.
const latest = ref<GlobalStatsPoint | null>(null)
const isLoading = ref(true)
const error = ref<ApiProblem | null>(null)

async function getTotals() {
  error.value = null
  try {
    const response = await getOverviewStats()
    const points = [...response.points].sort((a, b) => (a.day < b.day ? -1 : 1))
    latest.value = points.at(-1) ?? null
  } catch (e) {
    latest.value = null
    error.value = ApiProblem.from(e)
  } finally {
    isLoading.value = false
  }
}

const percentages = computed(() => {
  const domains = latest.value?.domains
  const heroes = latest.value?.heroes
  if (domains == null || heroes == null) return '—'
  const total = domains + heroes
  if (total === 0) return '0%'
  return `${((heroes / total) * 100).toFixed(1)}%`
})

onMounted(() => {
  void getTotals()
})

const formatLargeNumber = (number: number | null | undefined): string => {
  if (number == null) return '—'
  if (number >= 1000) {
    return (number / 1000).toFixed(0) + 'K'
  }
  return number.toString()
}
</script>

<template>
  <!-- Error state (§6.3) -->
  <ApiError v-if="error" :problem="error" />

  <section v-else-if="!isLoading && latest">
    <header class="mb-8">
      <div class="text-left">
        <h3 class="h4 mb-1">Overview</h3>
        <p class="text-lg text-gray-400 mb-2">
          Of the
          <span class="text-fuchsia-600">{{ latest.domains ?? '—' }}</span>
          most-visited websites on the internet, only
          <span class="text-fuchsia-600">{{ percentages }}</span>
          are fully IPv6-ready. IPv6 has been a standard since 1998. In the Tranco top 1000 the
          picture is no prettier:
          <span class="text-fuchsia-600">{{ latest.top_heroes ?? '—' }}</span>
          have IPv6 enabled, and
          <span class="text-fuchsia-600">{{ latest.top_nameserver ?? '—' }}</span>
          sit behind nameservers reachable over IPv6.
        </p>
        <p class="text-lg text-gray-400">
          For context: IPv6 became a standard in 1998, and again in 2017, in case anyone missed it
          the first time. The numbers above are what nearly three decades of "we'll get to it" looks
          like. Every one of them moves the day someone publishes an AAAA record.
        </p>
      </div>
    </header>

    <div
      class="grid md:grid-cols-4 bg-gray-800 divide-y md:divide-y-0 md:divide-x divide-gray-700 px-6 md:px-0 md:py-4 text-center mb-8"
    >
      <!-- 1st item -->
      <div class="py-6 md:py-0 md:px-8">
        <div class="text-4xl font-bold leading-tight tracking-tighter text-fuchsia-700 mb-2">
          {{ formatLargeNumber(latest.domains) }}
        </div>
        <div class="text-lg text-gray-400">Total valid domains</div>
      </div>
      <!-- 3rd item -->
      <div class="py-6 md:py-0 md:px-8">
        <div class="text-4xl font-bold leading-tight tracking-tighter text-fuchsia-700 mb-2">
          {{ formatLargeNumber(latest.ns_supported) }}
        </div>
        <div class="text-lg text-gray-400">Domains with IPv6 nameservers</div>
      </div>
      <!-- 2nd item -->
      <div class="py-6 md:py-0 md:px-8">
        <div class="text-4xl font-bold leading-tight tracking-tighter text-fuchsia-700 mb-2">
          {{ formatLargeNumber(latest.base_supported) }}
        </div>
        <div class="text-lg text-gray-400">IPv6-enabled domains</div>
      </div>
      <!-- 4rd item -->
      <div class="py-6 md:py-0 md:px-8">
        <div class="text-4xl font-bold leading-tight tracking-tighter text-fuchsia-700 mb-2">
          {{ formatLargeNumber(latest.heroes) }}
        </div>
        <div class="text-lg text-gray-400">Fully IPv6-ready domains</div>
      </div>
    </div>

    <!-- Top 1k -->
    <header class="mb-8">
      <div class="text-left">
        <h3 class="h4 mb-1">Top 1000</h3>
        <p class="text-base text-gray-400">
          The sites with the biggest engineering budgets and the fewest excuses:
        </p>
      </div>
    </header>
    <div
      class="grid md:grid-cols-2 bg-gray-800 divide-y md:divide-y-0 md:divide-x divide-gray-700 px-6 md:px-0 md:py-4 text-center"
    >
      <!-- 1st item -->
      <div class="py-6 md:py-0 md:px-8">
        <div class="text-4xl font-bold leading-tight tracking-tighter text-fuchsia-700 mb-2">
          {{ formatLargeNumber(latest.top_heroes) }}
        </div>
        <div class="text-lg text-gray-400">IPv6-enabled domains</div>
      </div>
      <!-- 2rd item -->
      <div class="py-6 md:py-0 md:px-8">
        <div class="text-4xl font-bold leading-tight tracking-tighter text-fuchsia-700 mb-2">
          {{ formatLargeNumber(latest.top_nameserver) }}
        </div>
        <div class="text-lg text-gray-400">Domains with IPv6 nameservers</div>
      </div>
    </div>
  </section>
</template>
