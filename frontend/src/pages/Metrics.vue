<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import SegmentedTabs from '@/components/SegmentedTabs.vue'

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
</script>

<template>
  <PageShell>
    <!-- Page sections -->
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-20 pb-4 md:pt-24 md:pb-4">
          <div class="py-4 mx-auto">
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
          <div class="mb-4">
            <SegmentedTabs
              :options="[
                { value: 'overview', label: 'Overview' },
                { value: 'asn', label: 'Network Providers' },
              ]"
              :model-value="queryFilter"
              @update:model-value="applyFilterAndUpdateRoute"
            />
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
  </PageShell>
</template>
