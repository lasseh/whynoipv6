<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import Breadcrumb from '@/components/Breadcrumb.vue'

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

const { items, page, loading, error, next, prev, setFilter } = useCursorList({
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
  <PageShell>
    <!-- Page sections -->
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-20 pb-4 md:pt-24 md:pb-4">
          <Breadcrumb :trail="[{ label: 'Countries', to: '/countries' }]" />

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
              <!-- Error state (§6.3) -->
              <div
                v-if="error"
                class="bg-zinc-800/50 border border-zinc-700 rounded-sm shadow-lg p-5 text-center"
              >
                <div class="text-xl font-medium">{{ error.title }}</div>
                <p v-if="error.detail" class="text-gray-400 mt-2">{{ error.detail }}</p>
              </div>
              <DomainTable v-else-if="items.length > 0" :domains="items" />
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
  </PageShell>
</template>
