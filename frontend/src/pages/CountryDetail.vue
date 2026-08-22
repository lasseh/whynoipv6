<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import SegmentedTabs from '@/components/SegmentedTabs.vue'
import ListState from '@/components/ListState.vue'
import Breadcrumb from '@/components/Breadcrumb.vue'

import DomainTable from '@/components/DomainTable.vue'
import Pagination from '@/components/Pagination.vue'
import RatingBadge from '@/components/RatingBadge.vue'
import ProgressBar from '@/components/ProgressBar.vue'

import { getCountry, listCountryDomains } from '@/api'
import { useCursorList } from '@/composables/useCursorList'
import { useEntity } from '@/composables/useEntity'
import { setPageTitle } from '@/composables/usePageMeta'

const route = useRoute()
const code = computed(() => String(route.params.id ?? ''))

// Header fetch — abort/supersede/param-renavigation via useEntity.
const { data: country, notFound } = useEntity(
  () => code.value,
  (c, signal) => getCountry(c, signal),
)

const anchorTop = ref<HTMLElement | null>(null)

const { items, page, loading, error, next, prev, setFilter, filters } = useCursorList({
  anchor: anchorTop,
  key: () => code.value,
  fetch: (params, signal) =>
    listCountryDomains(
      code.value,
      {
        class: params.filter === 'heroes' ? 'hero' : 'sinner',
        ...(params.cursor !== undefined && { cursor: params.cursor }),
      },
      signal,
    ),
  filters: { filter: { values: ['sinners', 'heroes'], default: 'sinners' } },
})

// Data-driven title once the country loads.
watch(country, (c) => {
  if (c) setPageTitle(`IPv6 Adoption in ${c.name}`)
})

// Pagination
</script>

<template>
  <PageShell>
    <!-- Page sections -->
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-20 pb-4 md:pt-24 md:pb-4">
          <Breadcrumb :trail="[{ label: 'Countries', to: '/countries' }]" />

          <!-- Country not found -->
          <div v-if="notFound" class="flex justify-center py-16">
            <div class="text-center">
              <div class="text-xl font-medium">
                Country not found. We go by ISO 3166 codes; check yours.
              </div>
            </div>
          </div>

          <template v-else>
            <header ref="anchorTop" class="mb-8">
              <div class="text-left">
                <h1 class="h2 mb-4">{{ country?.name ?? '' }}</h1>
              </div>
            </header>

            <div class="flex justify-between items-center">
              <div>
                <RatingBadge
                  :percent="country?.percent ?? null"
                  :total="country?.sites"
                  size="text-base"
                />
              </div>
              <div>
                <div class="text-sm font-medium text-zinc-500 mb-2">
                  {{ country?.sites ?? '—' }} domains tracked
                </div>
                <div class="text-sm font-medium text-zinc-500 mb-2">
                  {{ country?.v6_sites ?? '—' }} domains with apex IPv6
                </div>
              </div>
            </div>
            <div class="mt-3 mb-4">
              <div class="flex justify-between mb-1">
                <span class="text-sm font-medium text-white">Apex IPv6</span>
                <span class="text-sm font-medium text-white">{{ country?.percent ?? 0 }}%</span>
              </div>
              <ProgressBar
                :percent="country?.percent ?? null"
                :total="country?.sites"
                height="h-4"
              />
            </div>

            <div class="mb-4">
              <SegmentedTabs
                :options="[
                  { value: 'sinners', label: 'Sinners' },
                  { value: 'heroes', label: 'Heroes' },
                ]"
                :model-value="filters.filter"
                @update:model-value="(v) => setFilter('filter', v)"
              />
            </div>

            <div class="min-h-screen">
              <ListState
                :loading="loading"
                :error="error"
                :count="items.length"
                empty-text="No domains found"
              >
                <DomainTable :domains="items" />
              </ListState>
            </div>

            <!-- Pagination -->
            <div class="mt-6">
              <Pagination :page="page" @previous="prev" @next="next" />
            </div>
          </template>
        </div>
      </div>
    </section>
  </PageShell>
</template>
