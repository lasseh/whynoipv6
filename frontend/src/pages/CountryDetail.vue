<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import Header from '@/partials/Header.vue'
import PageIllustration from '@/partials/PageIllustration.vue'
import Footer from '@/partials/Footer.vue'

import DomainTable from '@/components/DomainTable.vue'
import Pagination from '@/components/Pagination.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'
import RatingBadge from '@/components/RatingBadge.vue'
import ProgressBar from '@/components/ProgressBar.vue'

import { getCountry, listCountryDomains } from '@/api'
import type { Country } from '@/api'
import { ApiProblem } from '@/api/problem'
import { useCursorList } from '@/composables/useCursorList'

const route = useRoute()
const code = String(route.params.id)

const country = ref<Country | null>(null)
const notFound = ref(false)

getCountry(code)
  .then((res) => {
    country.value = res
  })
  .catch((e: unknown) => {
    if (e instanceof ApiProblem && e.code === 'not-found') notFound.value = true
  })

const { items, page, loading, next, prev, setFilter } = useCursorList({
  fetch: (params, signal) =>
    listCountryDomains(
      code,
      {
        class: params.filter === 'heroes' ? 'hero' : 'sinner',
        ...(params.cursor !== undefined && { cursor: params.cursor }),
      },
      signal,
    ),
  filterKeys: ['filter'],
})

const queryFilter = ref(route.query.filter === 'heroes' ? 'heroes' : 'sinners')
watch(
  () => route.query.filter,
  (value) => {
    queryFilter.value = value === 'heroes' ? 'heroes' : 'sinners'
  },
)

// Pagination
const anchorTop = ref<HTMLElement | null>(null)
const scrollToAnchor = () => {
  anchorTop.value?.scrollIntoView({ behavior: 'auto' })
}
const goPrevious = () => {
  scrollToAnchor()
  prev()
}
const goNext = () => {
  scrollToAnchor()
  next()
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
            <!-- Breadcrumb -->
            <div class="mb-4">
              <nav class="flex" aria-label="Breadcrumb">
                <ol class="inline-flex items-center space-x-1 md:space-x-3">
                  <li class="inline-flex items-center">
                    <router-link
                      to="/"
                      class="inline-flex items-center text-sm font-medium text-gray-400 hover:text-white"
                    >
                      <svg
                        class="w-3 h-3 mr-2.5"
                        aria-hidden="true"
                        xmlns="http://www.w3.org/2000/svg"
                        fill="currentColor"
                        viewBox="0 0 20 20"
                      >
                        <path
                          d="m19.707 9.293-2-2-7-7a1 1 0 0 0-1.414 0l-7 7-2 2a1 1 0 0 0 1.414 1.414L2 10.414V18a2 2 0 0 0 2 2h3a1 1 0 0 0 1-1v-4a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v4a1 1 0 0 0 1 1h3a2 2 0 0 0 2-2v-7.586l.293.293a1 1 0 0 0 1.414-1.414Z"
                        />
                      </svg>
                      Home
                    </router-link>
                  </li>
                  <li aria-current="page">
                    <div class="flex items-center">
                      <svg
                        class="w-3 h-3 text-gray-400 mx-1"
                        aria-hidden="true"
                        xmlns="http://www.w3.org/2000/svg"
                        fill="none"
                        viewBox="0 0 6 10"
                      >
                        <path
                          stroke="currentColor"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                          stroke-width="2"
                          d="m1 9 4-4-4-4"
                        />
                      </svg>
                      <router-link
                        to="/countries"
                        class="ml-1 text-sm font-medium md:ml-2 text-gray-400 hover:text-white"
                        >Countries</router-link
                      >
                    </div>
                  </li>
                </ol>
              </nav>
            </div>

            <!-- Country not found -->
            <div v-if="notFound" class="flex justify-center py-16">
              <div class="text-center">
                <div class="text-xl font-medium">Country not found</div>
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
                    {{ country?.sites ?? '—' }} Domains
                  </div>
                  <div class="text-sm font-medium text-zinc-500 mb-2">
                    {{ country?.v6_sites ?? '—' }} Domains V6 Ready
                  </div>
                </div>
              </div>
              <div class="mt-3 mb-4">
                <div class="flex justify-between mb-1">
                  <span class="text-sm font-medium text-white">v6 Ready</span>
                  <span class="text-sm font-medium text-white">{{ country?.percent ?? 0 }}%</span>
                </div>
                <ProgressBar
                  :percent="country?.percent ?? null"
                  :total="country?.sites"
                  height="h-4"
                />
              </div>

              <div class="mb-4">
                <div class="w-full flex flex-wrap -space-x-px">
                  <button
                    :class="[
                      'btn grow border-zinc-700 hover:bg-zinc-800/20 rounded-none first:rounded-l last:rounded-r',
                      queryFilter === 'sinners'
                        ? 'text-fuchsia-600 bg-zinc-500/20'
                        : 'text-slate-300 bg-zinc-700/20',
                    ]"
                    @click="setFilter('filter', 'sinners')"
                  >
                    Sinners
                  </button>
                  <button
                    :class="[
                      'btn grow border-zinc-700 hover:bg-zinc-800/20 rounded-none first:rounded-l last:rounded-r',
                      queryFilter === 'heroes'
                        ? 'text-fuchsia-600 bg-zinc-500/20'
                        : 'text-slate-300 bg-zinc-700/20',
                    ]"
                    @click="setFilter('filter', 'heroes')"
                  >
                    Heroes
                  </button>
                </div>
              </div>

              <div class="min-h-screen">
                <DomainTable v-if="items.length > 0" :domains="items" />
                <LoadingSpinner v-if="loading" />
              </div>

              <!-- Pagination -->
              <div class="mt-6">
                <Pagination :page="page" @previous="goPrevious" @next="goNext" />
              </div>
            </template>
          </div>
        </div>
      </section>
    </main>

    <!-- Site footer -->
    <Footer />
  </div>
</template>
