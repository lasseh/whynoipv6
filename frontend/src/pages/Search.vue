<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import Header from '@/partials/Header.vue'
import PageIllustration from '@/partials/PageIllustration.vue'
import Footer from '@/partials/Footer.vue'

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
  filterKeys: ['q'],
})

const searchString = ref(typeof route.query.q === 'string' ? route.query.q : '')

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
            <div class="pt-4 pb-4 md:pt-4 md:pb-4">
              <form action="/search" method="get" @submit.prevent="submitSearch">
                <label for="search" class="mb-2 text-sm font-medium sr-only text-white"
                  >Search</label
                >
                <div class="relative">
                  <div class="absolute inset-y-0 left-0 flex items-center pl-3 pointer-events-none">
                    <svg
                      aria-hidden="true"
                      class="w-5 h-5 text-gray-400"
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
                  </div>
                  <input
                    id="search"
                    v-model="searchString"
                    type="search"
                    name="q"
                    class="block w-full p-4 pl-10 text-sm border rounded-sm bg-gray-800 border-gray-700 placeholder-gray-400 text-white focus:ring-fuchsia-900 focus:border-fuchsia-900"
                    placeholder="Search Domains"
                    required
                  />
                  <button
                    type="submit"
                    class="text-white absolute right-2.5 bottom-2.5 focus:ring-3 focus:outline-none font-medium rounded-sm text-sm px-4 py-2 bg-fuchsia-700 hover:bg-fuchsia-900 focus:ring-fuchsia-800"
                  >
                    Search
                  </button>
                </div>
              </form>
            </div>

            <!-- Error state -->
            <div
              v-if="error"
              class="bg-zinc-800/50 border border-zinc-700 rounded-sm shadow-lg p-5 text-center"
            >
              <div class="text-xl font-medium">{{ error.title }}</div>
            </div>

            <div v-else>
              <header class="mb-4">
                <div class="text-left">
                  <h1 class="h4 mb-4">Domains</h1>
                </div>
              </header>
              <!-- Domains -->
              <div>
                <DomainTable :domains="items" />
                <LoadingSpinner v-if="loading" />
              </div>

              <!-- Pagination -->
              <div class="mt-6">
                <Pagination :page="page" @previous="prev" @next="next" />
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
