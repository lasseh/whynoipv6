<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import ApiError from '@/components/ApiError.vue'
import FilterInput from '@/components/FilterInput.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'
import MandateBadge from '@/components/MandateBadge.vue'
import SegmentedTabs from '@/components/SegmentedTabs.vue'

import { listCampaigns } from '@/api'
import { useFilteredCollection } from '@/composables/useFilteredCollection'

// Client-side name filter over the campaign list.
const {
  filtered: filteredCampaignList,
  query: searchQuery,
  loading,
  error,
} = useFilteredCollection(
  (signal) => listCampaigns(undefined, signal),
  (c) => c.name,
)

// Mandate tab. ?tag= is the source of truth so the mandate view is
// linkable; unknown tags fall back to the full list. The campaign set is
// bounded and already in memory, so the filter composes over the name
// filter rather than refetching with ?tag=.
const route = useRoute()
const router = useRouter()

const tab = computed(() => (route.query.tag === 'mandate' ? 'mandate' : 'all'))

function setTab(value: string): void {
  const query = { ...route.query }
  if (value === 'mandate') query.tag = 'mandate'
  else delete query.tag
  void router.push({ query })
}

const campaigns = computed(() =>
  tab.value === 'mandate'
    ? filteredCampaignList.value.filter((c) => c.tags.includes('mandate'))
    : filteredCampaignList.value,
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
                <h1 class="text-2xl md:text-3xl text-zinc-100 font-bold">Campaigns</h1>
              </div>

              <!-- Search form -->
              <div
                class="hidden md:grid grid-flow-col sm:auto-cols-max justify-start sm:justify-end gap-2"
              >
                <FilterInput v-model="searchQuery" input-id="campaign-filter" />

                <!-- Create campaign button -->
                <a
                  href="https://github.com/lasseh/whynoipv6-campaign"
                  target="_blank"
                  title="Start a campaign on GitHub"
                  class="btn bg-fuchsia-700 hover:bg-fuchsia-800 text-white"
                >
                  <svg class="w-4 h-4 fill-current opacity-50 shrink-0" viewBox="0 0 16 16">
                    <path
                      d="M15 7H9V1c0-.6-.4-1-1-1S7 .4 7 1v6H1c-.6 0-1 .4-1 1s.4 1 1 1h6v6c0 .6.4 1 1 1s1-.4 1-1V9h6c.6 0 1-.4 1-1s-.4-1-1-1z"
                    />
                  </svg>
                  <span class="hidden ml-2">Start a campaign</span>
                </a>
              </div>
            </div>

            <!-- Campaign info content -->
            <div class="text-lg text-gray-400">
              <p class="mb-4">
                Campaigns are reader-submitted lists of domains with something in common: a
                country's banks, its ISPs, its government. Each list is crawled and scored as a
                group. An entry is ready when its tracked hostname and at least one nameserver host
                publish AAAA records, and www is either supported or not applicable.
              </p>
              <p class="mb-8">
                Have a list of domains that should know better? Open an issue in the
                <a
                  href="https://github.com/lasseh/whynoipv6-campaign"
                  class="underline a-gradient"
                  target="_blank"
                  >campaign repo</a
                >
                and we'll put it on the scoreboard.
              </p>
            </div>

            <!-- Search mobile -->
            <div class="md:hidden grid grid-flow-col sm:auto-cols-max gap-2 mb-2">
              <FilterInput
                v-model="searchQuery"
                input-id="campaign-filter-mobile"
                class="w-full"
                input-class="w-full"
              />
            </div>

            <div class="mb-4 max-w-sm">
              <SegmentedTabs
                :model-value="tab"
                :options="[
                  { value: 'all', label: 'All campaigns' },
                  { value: 'mandate', label: 'Mandate' },
                ]"
                @update:model-value="setTab"
              />
            </div>

            <!-- Error state (§6.3) -->
            <ApiError v-if="error" :problem="error" />
            <LoadingSpinner v-else-if="loading" />

            <!-- Cards -->
            <div v-else class="grid grid-cols-2 xl:grid-cols-8 gap-4">
              <!-- Card -->
              <router-link
                v-for="campaign in campaigns"
                :key="campaign.uuid"
                :to="{ name: 'CampaignDetail', params: { uuid: campaign.uuid } }"
                class="col-span-full sm:col-span-6 xl:col-span-4 bg-zinc-800 shadow-lg rounded-sm border border-zinc-700"
              >
                <div class="flex flex-col h-full p-5">
                  <div class="grow mt-2">
                    <div class="inline-flex items-center gap-2 text-zinc-100 hover:text-white mb-1">
                      <h2 class="text-xl leading-snug font-semibold">{{ campaign.name }}</h2>
                      <MandateBadge v-if="campaign.tags.includes('mandate')" />
                    </div>
                    <div class="text-sm">{{ campaign.description }}</div>
                  </div>
                  <footer class="mt-5">
                    <div class="flex justify-between mb-1">
                      <span class="text-sm font-medium text-gray-400">Campaign ready</span>
                      <span class="text-sm font-medium text-gray-400"
                        >{{ campaign.adoption?.v6_ready_percent ?? '—' }}%</span
                      >
                    </div>
                    <!-- Campaign cards keep their own fuchsia bar (old look), not the rating gradient. -->
                    <div class="w-full rounded-full h-2.5 bg-gray-700">
                      <div
                        class="bg-gradient-to-r from-fuchsia-500 to-fuchsia-700 h-2.5 rounded-full"
                        :style="{ width: (campaign.adoption?.v6_ready_percent ?? 0) + '%' }"
                      ></div>
                    </div>
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
