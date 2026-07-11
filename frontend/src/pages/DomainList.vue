<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'

import DomainTable from '@/components/DomainTable.vue'
import Pagination from '@/components/Pagination.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'

import { listHeroes, listSinners } from '@/api'
import type { DomainSummary } from '@/api'
import { useCursorList } from '@/composables/useCursorList'

const route = useRoute()

const { items, page, loading, error, next, prev, setFilter } = useCursorList<DomainSummary>({
  fetch: (params, signal) =>
    params.filter === 'heroes'
      ? listHeroes({ cursor: params.cursor }, signal)
      : listSinners({ cursor: params.cursor }, signal),
  filterKeys: ['filter'],
})

const queryFilter = computed(() => {
  const filterValue = route.query.filter
  if (filterValue === null || typeof filterValue === 'undefined') return 'sinners'
  return Array.isArray(filterValue) ? filterValue[0] || 'sinners' : filterValue
})

function applyFilter(filter: string) {
  setFilter('filter', filter)
}

// Pagination keeps the old scroll-to-anchor behavior.
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
            <div class="w-full flex flex-wrap -space-x-px">
              <button
                :class="[
                  'btn grow border-zinc-700 hover:bg-zinc-800/40 rounded-none first:rounded-l last:rounded-r',
                  queryFilter === 'sinners'
                    ? 'text-fuchsia-600 bg-zinc-600/20'
                    : 'text-slate-300 bg-zinc-800/20',
                ]"
                @click="applyFilter('sinners')"
              >
                Sinners
              </button>
              <button
                :class="[
                  'btn grow border-zinc-700 hover:bg-zinc-800/40 rounded-none first:rounded-l last:rounded-r',
                  queryFilter === 'heroes'
                    ? 'text-fuchsia-600 bg-zinc-600/20'
                    : 'text-slate-300 bg-zinc-800/20',
                ]"
                @click="applyFilter('heroes')"
              >
                Heroes
              </button>
            </div>
          </div>

          <!-- Error state -->
          <div
            v-if="error"
            class="bg-zinc-800/50 border border-zinc-700 rounded-sm shadow-lg p-5 text-center"
          >
            <div class="text-xl font-medium">{{ error.title }}</div>
          </div>

          <!-- Domains -->
          <div v-else>
            <DomainTable :domains="items" />
            <LoadingSpinner v-if="loading" />
          </div>

          <!-- Pagination -->
          <div class="mt-6">
            <Pagination :page="page" @previous="goPrevious" @next="goNext" />
          </div>
        </div>
      </div>
    </section>
  </PageShell>
</template>
