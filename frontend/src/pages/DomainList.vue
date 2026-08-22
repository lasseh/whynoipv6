<script setup lang="ts">
import { ref } from 'vue'

import PageShell from '@/components/PageShell.vue'
import ListState from '@/components/ListState.vue'
import SegmentedTabs from '@/components/SegmentedTabs.vue'

import DomainTable from '@/components/DomainTable.vue'
import Pagination from '@/components/Pagination.vue'

import { useCursorList } from '@/composables/useCursorList'
import { TIERS, tierBySlug } from '@/tiers'

const anchorTop = ref<HTMLElement | null>(null)

const { items, page, loading, error, filters, next, prev, setFilter } = useCursorList({
  anchor: anchorTop,
  fetch: (params, signal) => tierBySlug(params.filter).list({ cursor: params.cursor }, signal),
  filters: { filter: { values: TIERS.map((t) => t.slug), default: TIERS[0]!.slug } },
})

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
              <h1 class="h2 mb-4">The Tranco top million, judged by their AAAA records</h1>
              <p class="text-lg text-gray-400">
                We check the apex, www, nameserver and mail hosts, and whether the site actually
                answers over IPv6. Passing every required check makes a Hero. Saints pass the
                page-resource grade too. A Sinner still has an apex A record but no globally
                routable AAAA record. Active domains normally run daily, with other schedules for
                retries and backoff.
              </p>
            </div>
          </header>

          <div class="mb-4">
            <SegmentedTabs
              :options="tierTabs"
              :model-value="filters.filter"
              @update:model-value="(v) => setFilter('filter', v)"
            />
          </div>

          <!-- Domains. min-h-screen reserves the space the rows will fill:
               the table renders nothing until the first page arrives, and
               without the placeholder the footer sits inside the viewport
               and gets shoved off it when ~50 rows land (CLS 0.19 on the
               Lighthouse mobile run). A full page of rows is always taller
               than a viewport, so the reservation costs nothing once loaded. -->
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
        </div>
      </div>
    </section>
  </PageShell>
</template>
