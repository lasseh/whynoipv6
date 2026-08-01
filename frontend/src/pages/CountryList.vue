<script setup lang="ts">
import PageShell from '@/components/PageShell.vue'
import ApiError from '@/components/ApiError.vue'
import FilterInput from '@/components/FilterInput.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'

import CountryFlag from '@/components/CountryFlag.vue'
import RatingBadge from '@/components/RatingBadge.vue'
import ProgressBar from '@/components/ProgressBar.vue'

import { listCountries } from '@/api'
import { useFilteredCollection } from '@/composables/useFilteredCollection'

// Client-side name filter over the country list (§8.7).
const {
  filtered: filteredCountryList,
  query: searchQuery,
  loading,
  error,
} = useFilteredCollection(
  (signal) => listCountries(signal),
  (c) => c.name,
)
</script>

<template>
  <PageShell>
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-20 pb-4 md:pt-24 md:pb-4">
          <div class="py-4 mx-auto">
            <!-- Page header -->
            <div class="sm:flex sm:justify-between sm:items-center mb-4">
              <!-- Left: Title -->
              <div class="mb-4 sm:mb-0">
                <h1 class="text-2xl md:text-3xl text-zinc-100 font-bold">IPv6 by Country</h1>
              </div>

              <!-- Right: Actions -->
              <div class="grid grid-flow-col sm:auto-cols-max justify-start sm:justify-end gap-2">
                <!-- Search form -->
                <FilterInput v-model="searchQuery" input-id="country-filter" />
              </div>
            </div>

            <!-- content -->
            <div class="text-lg text-gray-400">
              <p class="mb-4 md:mr-32">
                IPv6 adoption, country by country. Each domain in the Tranco list is mapped to a
                country by GeoIP, then scored on who publishes an AAAA record and who doesn't. So
                this measures a country's most-visited websites, not its networks. Some countries
                are nearly done. Some haven't started. Pick yours and meet the local Sinners.
              </p>
            </div>

            <!-- Error state (§6.3) -->
            <ApiError v-if="error" :problem="error" />
            <LoadingSpinner v-else-if="loading" />

            <div v-else class="grid grid-cols-2 xl:grid-cols-8 gap-4">
              <router-link
                v-for="country in filteredCountryList"
                :key="country.code"
                :to="{ name: 'CountryDetail', params: { id: country.code } }"
                class="col-span-full sm:col-span-6 xl:col-span-4 bg-zinc-800/50 shadow-lg rounded-sm border border-zinc-700"
              >
                <div class="flex flex-col h-full p-5">
                  <div class="grow mt-1">
                    <div class="text-zinc-100 hover:text-white mb-1">
                      <div class="flex justify-between items-center">
                        <div class="inline-flex text-zinc-100 hover:text-white mb-1">
                          <div class="flex items-center">
                            <CountryFlag :country-code="country.code" class="rounded-full" />
                            <h2 class="text-xl leading-snug font-semibold pl-2">
                              {{ country.name }}
                            </h2>
                          </div>
                        </div>
                        <div>
                          <RatingBadge :percent="country.percent" :total="country.sites" />
                        </div>
                      </div>
                    </div>
                  </div>
                  <footer class="mt-3">
                    <div class="flex justify-between mb-1">
                      <span class="text-sm font-medium text-gray-400">IPv6 ready</span>
                      <span class="text-sm font-medium text-gray-400">{{ country.percent }}%</span>
                    </div>
                    <ProgressBar :percent="country.percent" :total="country.sites" />
                  </footer>
                </div>
              </router-link>
            </div>
          </div>
        </div>
      </div>
    </section>
  </PageShell>
</template>
