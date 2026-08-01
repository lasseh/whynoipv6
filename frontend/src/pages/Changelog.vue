<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import SegmentedTabs from '@/components/SegmentedTabs.vue'

import ApiError from '@/components/ApiError.vue'
import ChangelogTable from '@/components/ChangelogTable.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'
import Pagination from '@/components/Pagination.vue'

import { listChangelog } from '@/api'
import { useCursorList } from '@/composables/useCursorList'

const route = useRoute()

// ?filter=tranco → GET /changelog; ?filter=campaign → GET /changelog?scope=campaign
// (07 §4.8 — a fixed recent window whose null cursors self-disable Pagination).
const anchorTop = ref<HTMLElement | null>(null)

const { items, page, loading, error, setFilter, next, prev, reload } = useCursorList({
  anchor: anchorTop,
  fetch: (params, signal) =>
    listChangelog(
      {
        ...(params.filter === 'campaign' && { scope: 'campaign' as const }),
        ...(params.cursor !== undefined && { cursor: params.cursor }),
      },
      signal,
    ),
  filterKeys: ['filter'],
})

const queryFilter = computed(() => (route.query.filter === 'campaign' ? 'campaign' : 'tranco'))

// 30 s auto-refresh (§7.4) — refreshes hit the CDN's 300 s public cache;
// freshness comes from the API's changelog-seeded ETags. Only the live head
// of the feed refreshes: paginated pages and hidden tabs are left alone.
const isLive = computed(() => !route.query.cursor)

let refreshId: number | undefined
onMounted(() => {
  refreshId = window.setInterval(() => {
    if (document.hidden || !isLive.value) return
    reload()
  }, 30000)
})
onBeforeUnmount(() => {
  if (refreshId !== undefined) clearInterval(refreshId)
})
</script>

<template>
  <PageShell>
    <!-- Page sections -->
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-32 pb-4 md:pt-32 md:pb-4">
          <header ref="anchorTop" class="mb-6">
            <div class="text-center sm:text-left">
              <h1 class="h3">Changelog</h1>
              <p class="text-base text-gray-400 mt-1">
                Confirmed IPv6 changes as the crawler observes them
              </p>
            </div>
          </header>

          <div class="mb-4 max-w-sm">
            <SegmentedTabs
              :options="[
                { value: 'tranco', label: 'Tranco Top 1M' },
                { value: 'campaign', label: 'Campaigns' },
              ]"
              :model-value="queryFilter"
              @update:model-value="(v) => setFilter('filter', v)"
            />
          </div>
        </div>
      </div>

      <div v-if="error" class="max-w-6xl mx-auto px-4 sm:px-6">
        <ApiError :problem="error" />
      </div>
      <template v-else>
        <ChangelogTable v-if="!loading || items.length > 0" :changelogs="items" header="" />
        <LoadingSpinner v-if="loading" />

        <!-- Pagination -->
        <div class="mt-4">
          <Pagination :page="page" @previous="prev" @next="next" />
        </div>
      </template>
    </section>
  </PageShell>
</template>
