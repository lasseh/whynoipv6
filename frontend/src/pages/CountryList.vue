<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import Header from '@/partials/Header.vue'
import PageIllustration from '@/partials/PageIllustration.vue'
import Footer from '@/partials/Footer.vue'

import CountryFlag from '@/components/CountryFlag.vue'
import RatingBadge from '@/components/RatingBadge.vue'
import ProgressBar from '@/components/ProgressBar.vue'

import { listCountries } from '@/api'
import type { Country } from '@/api'
import { ApiProblem } from '@/api/problem'

const countryList = ref<Country[]>([])
const searchQuery = ref('')
const error = ref<ApiProblem | null>(null)

async function fetchCountryList() {
  try {
    const response = await listCountries()
    countryList.value = response.items
  } catch (e) {
    error.value = e instanceof ApiProblem ? e : new ApiProblem({ title: 'Request failed' }, 0)
  }
}

// Client-side name filter over the country list (§8.7).
const filteredCountryList = computed(() => {
  if (!searchQuery.value) {
    return countryList.value
  }
  return countryList.value.filter((country) =>
    country.name.toLowerCase().includes(searchQuery.value.toLowerCase()),
  )
})

onMounted(() => {
  void fetchCountryList()
})
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

      <section class="relative">
        <div class="max-w-6xl mx-auto px-4 sm:px-6">
          <div class="pt-20 pb-4 md:pt-24 md:pb-4">
            <div class="py-4 max-w-9xl mx-auto">
              <!-- Page header -->
              <div class="sm:flex sm:justify-between sm:items-center mb-4">
                <!-- Left: Title -->
                <div class="mb-4 sm:mb-0">
                  <h1 class="text-2xl md:text-3xl text-zinc-100 font-bold">Country List</h1>
                </div>

                <!-- Right: Actions -->
                <div class="grid grid-flow-col sm:auto-cols-max justify-start sm:justify-end gap-2">
                  <!-- Search form -->
                  <form class="relative">
                    <label for="action-search" class="sr-only">Filter</label>
                    <input
                      id="action-search"
                      v-model="searchQuery"
                      class="form-input pl-9 bg-zinc-800"
                      type="search"
                      placeholder="Filter…"
                    />
                    <button
                      class="absolute inset-0 right-auto group"
                      type="submit"
                      aria-label="Search"
                    >
                      <svg
                        class="w-4 h-4 shrink-0 fill-current text-zinc-500 group-hover:text-zinc-400 ml-3 mr-2"
                        viewBox="0 0 16 16"
                        xmlns="http://www.w3.org/2000/svg"
                      >
                        <path
                          d="M7 14c-3.86 0-7-3.14-7-7s3.14-7 7-7 7 3.14 7 7-3.14 7-7 7zM7 2C4.243 2 2 4.243 2 7s2.243 5 5 5 5-2.243 5-5-2.243-5-5-5z"
                        />
                        <path
                          d="M15.707 14.293L13.314 11.9a8.019 8.019 0 01-1.414 1.414l2.393 2.393a.997.997 0 001.414 0 .999.999 0 000-1.414z"
                        />
                      </svg>
                    </button>
                  </form>
                </div>
              </div>

              <!-- content -->
              <div class="text-lg text-gray-400">
                <p class="mb-4 md:mr-32">
                  This resource tracks the progress of IPv6 adoption globally by listing countries
                  and their top domains that lack IPv6 support. Aimed at network administrators,
                  policymakers, and anyone interested in the transition from IPv4 to IPv6, the data
                  aims to highlight areas that need attention to build a more robust and
                  future-proof Internet infrastructure.
                </p>
              </div>

              <!-- Error state (§6.3) -->
              <div
                v-if="error"
                class="bg-zinc-800/50 border border-zinc-700 rounded-sm shadow-lg p-5 text-center"
              >
                <div class="text-xl font-medium">{{ error.title }}</div>
                <p v-if="error.detail" class="text-gray-400 mt-2">{{ error.detail }}</p>
              </div>

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
                        <span class="text-sm font-medium text-gray-400">v6 Ready</span>
                        <span class="text-sm font-medium text-gray-400"
                          >{{ country.percent }}%</span
                        >
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
    </main>
    <!-- Site footer -->
    <Footer />
  </div>
</template>
