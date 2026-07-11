<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import Header from '@/partials/Header.vue'
import PageIllustration from '@/partials/PageIllustration.vue'
import Footer from '@/partials/Footer.vue'

import MetricCrawler from '@/partials/MetricCrawler.vue'
import MetricASN from '@/partials/MetricASN.vue'

const router = useRouter()
const route = useRoute()

const queryFilter = computed(() =>
  typeof route.query.t === 'string' && route.query.t ? route.query.t : 'overview',
)

const applyFilterAndUpdateRoute = (filterType: string) => {
  // Preserve the rest of the query (§5 lists sort/q as retained state).
  void router.push({ query: { ...route.query, t: filterType } })
}

const tabClass = (filterType: string): string[] => {
  return [
    'btn grow border-zinc-700 hover:bg-zinc-800/20 rounded-none first:rounded-l last:rounded-r',
    queryFilter.value === filterType ? 'text-fuchsia-600 bg-zinc-500/20' : 'text-slate-300',
  ]
}
</script>

<template>
  <div class="flex flex-col min-h-screen overflow-hidden">
    <!-- Site header -->
    <Header />

    <!-- Page content -->
    <main class="grow">
      <!-- Page illustration -->
      <div class="relative max-w-6xl mx-auto h-0 pointer-events-none" aria-hidden="true">
        <PageIllustration />
      </div>

      <!-- Page sections -->
      <section class="relative">
        <div class="max-w-6xl mx-auto px-4 sm:px-6">
          <div class="pt-20 pb-4 md:pt-24 md:pb-4">
            <div class="py-4 max-w-9xl mx-auto">
              <!-- Page header -->
              <div class="sm:flex sm:justify-between sm:items-center mb-4">
                <!-- Left: Title -->
                <div class="mb-4 sm:mb-0">
                  <h1 class="text-2xl md:text-3xl text-zinc-100 font-bold">Metrics</h1>
                  <p class="text-lg text-gray-400">
                    Insights into IPv6 Deployment Statistics within the Tranco Dataset.
                  </p>
                </div>
              </div>
            </div>

            <!-- Tab Buttons -->
            <div class="flex mb-4">
              <button :class="tabClass('overview')" @click="applyFilterAndUpdateRoute('overview')">
                Overview
              </button>
              <button :class="tabClass('asn')" @click="applyFilterAndUpdateRoute('asn')">
                Network Providers
              </button>
            </div>
            <!-- Tab Content -->
            <div>
              <div v-if="queryFilter === 'overview'">
                <MetricCrawler />
              </div>
              <div v-if="queryFilter === 'asn'">
                <MetricASN />
              </div>
            </div>
          </div>
        </div>
      </section>
    </main>

    <!-- Site footer -->
    <Footer />
  </div>
</template>
