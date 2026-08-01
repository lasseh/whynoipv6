<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import ApiError from '@/components/ApiError.vue'

import DomainSearchForm from '@/components/DomainSearchForm.vue'
import DomainTable from '@/components/DomainTable.vue'
import Pagination from '@/components/Pagination.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'

import { searchDomains } from '@/api'
import type { DomainSummary, Page } from '@/api'
import { useCursorList } from '@/composables/useCursorList'

const route = useRoute()

const emptyPage: Page = { next_cursor: null, prev_cursor: null, has_more: false }

// One result list (§8.5): ?q= spans rank-NULL rows server-side, so the old
// second "Campaign Domains" section folds into this table.
const { items, page, loading, error, next, prev, setFilter } = useCursorList<DomainSummary>({
  fetch: (params, signal) =>
    params.q
      ? searchDomains({ q: params.q, cursor: params.cursor }, signal)
      : Promise.resolve({ items: [], page: emptyPage }),
  filters: { q: { default: '' } },
})

const searchString = ref(typeof route.query.q === 'string' ? route.query.q : '')

// The submitted query (URL-synced by useCursorList) — the input model alone
// must not flip the page between its prompt and results states while typing.
const activeQuery = computed(() => (typeof route.query.q === 'string' ? route.query.q : ''))

watch(
  () => route.query.q,
  (q) => {
    if (typeof q === 'string') searchString.value = q
  },
)

function submitSearch() {
  setFilter('q', searchString.value || undefined)
}
</script>

<template>
  <PageShell>
    <!-- Page sections -->
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-20 pb-4 md:pt-24 md:pb-4">
          <div class="pt-4 pb-4 md:pt-4 md:pb-4">
            <DomainSearchForm v-model="searchString" @submit="submitSearch" />
          </div>

          <!-- Error state -->
          <ApiError v-if="error" :problem="error" />

          <!-- Prompt state: nothing searched yet -->
          <div v-else-if="!activeQuery" class="py-16 text-center">
            <svg
              aria-hidden="true"
              class="w-10 h-10 mx-auto mb-4 text-gray-600"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
              xmlns="http://www.w3.org/2000/svg"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
              ></path>
            </svg>
            <div class="text-xl font-medium">Search the domain index</div>
            <p class="mt-2 text-gray-400">
              Look up any tracked domain: Saint, Sinner, or something in between. Try
              <router-link to="/search?q=google" class="text-fuchsia-500 hover:underline"
                >google</router-link
              >.
            </p>
          </div>

          <div v-else>
            <header class="mb-4">
              <div class="text-left">
                <h1 class="h4 mb-4">Results for &ldquo;{{ activeQuery }}&rdquo;</h1>
              </div>
            </header>
            <!-- Domains -->
            <div>
              <DomainTable :domains="items" :loading="loading" />
              <LoadingSpinner v-if="loading" />
            </div>

            <!-- Zero results: offer the live check as the escape hatch -->
            <p v-if="!loading && items.length === 0" class="mt-4 text-center text-gray-400">
              Nothing in the index by that name.
              <router-link :to="`/check/${activeQuery}`" class="text-fuchsia-500 hover:underline"
                >Run a live check on {{ activeQuery }}</router-link
              >.
            </p>

            <!-- Pagination -->
            <div class="mt-6">
              <Pagination :page="page" @previous="prev" @next="next" />
            </div>
          </div>
        </div>
      </div>
    </section>
  </PageShell>
</template>
