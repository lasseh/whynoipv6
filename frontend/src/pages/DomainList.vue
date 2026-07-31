<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import ApiError from '@/components/ApiError.vue'
import SegmentedTabs from '@/components/SegmentedTabs.vue'

import DomainTable from '@/components/DomainTable.vue'
import Pagination from '@/components/Pagination.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'

import type { DomainSummary } from '@/api'
import { useCursorList } from '@/composables/useCursorList'
import { TIERS, tierBySlug } from '@/tiers'

const route = useRoute()

const anchorTop = ref<HTMLElement | null>(null)

const { items, page, loading, error, next, prev, setFilter } = useCursorList<DomainSummary>({
  anchor: anchorTop,
  fetch: (params, signal) => tierBySlug(params.filter).list({ cursor: params.cursor }, signal),
  filterKeys: ['filter'],
})

const queryFilter = computed(() => tierBySlug([route.query.filter].flat()[0] ?? undefined).slug)

const tierTabs = TIERS.filter((t) => !t.hidden).map((t) => ({ value: t.slug, label: t.label }))
</script>

<template>
  <PageShell>
    <!-- Page sections -->
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-24 pb-12 md:pt-32 md:pb-20">
          <header ref="anchorTop" class="mb-8">
            <div class="text-center md:text-left">
              <h1 class="h2 mb-4">Unmasking the Top 1M Websites of the World</h1>
              <p class="text-lg text-gray-400">
                Within the elite realm of the internet's top 1 million websites lies a distinct
                divide: the forward-thinking Heroes who've embraced IPv6, propelling us toward a
                brighter digital future, and the Sinners, who despite their influence, hold us back
                by neglecting this advancement. Dive in as we spotlight these websites, celebrating
                the innovators and calling out those resisting progress.
              </p>
            </div>
          </header>

          <div class="mb-4">
            <SegmentedTabs
              :options="tierTabs"
              :model-value="queryFilter"
              @update:model-value="(v) => setFilter('filter', v)"
            />
          </div>

          <!-- Error state -->
          <ApiError v-if="error" :problem="error" />

          <!-- Domains -->
          <div v-else>
            <DomainTable :domains="items" :loading="loading" />
            <LoadingSpinner v-if="loading" />
          </div>

          <!-- Pagination -->
          <div class="mt-6">
            <Pagination :page="page" @previous="prev" @next="next" />
          </div>
        </div>
      </div>
    </section>
  </PageShell>
</template>
