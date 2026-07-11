<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'

import ChangelogTable from '@/components/ChangelogTable.vue'
import Pagination from '@/components/Pagination.vue'

import { listChangelog } from '@/api'
import { useCursorList } from '@/composables/useCursorList'

const route = useRoute()

// ?filter=tranco → GET /changelog; ?filter=campaign → GET /changelog?scope=campaign
// (07 §4.8 — a fixed recent window whose null cursors self-disable Pagination).
const anchorTop = ref<HTMLElement | null>(null)

const { items, page, next, prev, setFilter, reload } = useCursorList({
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

const changelogHeader = computed(() =>
  queryFilter.value === 'campaign' ? 'Campaign Changelogs' : 'Domain Changelogs',
)

// 30 s auto-refresh (§7.4) — refreshes hit the CDN's 300 s public cache;
// freshness comes from the API's changelog-seeded ETags.
let intervalId: number | undefined
onMounted(() => {
  intervalId = window.setInterval(reload, 30000)
})
onBeforeUnmount(() => {
  if (intervalId !== undefined) clearInterval(intervalId)
})

// Pagination
</script>

<template>
  <PageShell>
    <!-- Page sections -->
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-32 pb-4 md:pt-32 md:pb-4">
          <header ref="anchorTop" class="mb-8">
            <!-- Title and excerpt -->
            <div class="text-center md:text-left">
              <p class="text-md text-gray-400">Live changelog from the crawler</p>
            </div>
          </header>

          <div class="mb-4">
            <div class="w-full flex flex-wrap -space-x-px">
              <button
                :class="[
                  'btn grow border-zinc-700 hover:bg-zinc-800/20 rounded-none first:rounded-l last:rounded-r',
                  queryFilter === 'tranco'
                    ? 'text-fuchsia-600 bg-zinc-500/20'
                    : 'text-slate-300 bg-zinc-700/20',
                ]"
                @click="setFilter('filter', 'tranco')"
              >
                Tranco
              </button>
              <button
                :class="[
                  'btn grow border-zinc-700 hover:bg-zinc-800/20 rounded-none first:rounded-l last:rounded-r',
                  queryFilter === 'campaign'
                    ? 'text-fuchsia-600 bg-zinc-500/20'
                    : 'text-slate-300 bg-zinc-700/20',
                ]"
                @click="setFilter('filter', 'campaign')"
              >
                Campaigns
              </button>
            </div>
          </div>
        </div>
      </div>

      <ChangelogTable :changelogs="items" :header="changelogHeader" />

      <!-- Pagination -->
      <div class="mt-4">
        <Pagination :page="page" @previous="prev" @next="next" />
      </div>
    </section>
  </PageShell>
</template>
