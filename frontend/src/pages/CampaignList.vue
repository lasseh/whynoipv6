<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import PageShell from '@/components/PageShell.vue'
import ApiError from '@/components/ApiError.vue'
import FilterInput from '@/components/FilterInput.vue'
import MandateBadge from '@/components/MandateBadge.vue'

import { listCampaigns } from '@/api'
import type { CampaignListItem } from '@/api'
import { ApiProblem } from '@/api/problem'

const campaignList = ref<CampaignListItem[]>([])
const searchQuery = ref('')
const error = ref<ApiProblem | null>(null)

async function fetchCampaignList() {
  try {
    const response = await listCampaigns()
    campaignList.value = response.items
  } catch (e) {
    error.value = ApiProblem.from(e)
  }
}

// A computed property to get the filtered campaign list based on the search query
const filteredCampaignList = computed(() => {
  if (!searchQuery.value) {
    return campaignList.value
  }
  return campaignList.value.filter((campaign) =>
    campaign.name.toLowerCase().includes(searchQuery.value.toLowerCase()),
  )
})

onMounted(() => {
  void fetchCampaignList()
})
</script>

<template>
  <PageShell>
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-20 pb-4 md:pt-24 md:pb-4">
          <div class="py-4 max-w-9xl mx-auto">
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
                  title="Add Campaign on Github"
                  class="btn bg-fuchsia-700 hover:bg-fuchsia-800 text-white"
                >
                  <svg class="w-4 h-4 fill-current opacity-50 shrink-0" viewBox="0 0 16 16">
                    <path
                      d="M15 7H9V1c0-.6-.4-1-1-1S7 .4 7 1v6H1c-.6 0-1 .4-1 1s.4 1 1 1h6v6c0 .6.4 1 1 1s1-.4 1-1V9h6c.6 0 1-.4 1-1s-.4-1-1-1z"
                    />
                  </svg>
                  <span class="hidden xs:block ml-2">Create Campaign</span>
                </a>
              </div>
            </div>

            <!-- Campaign info content -->
            <div class="text-lg text-gray-400">
              <p class="mb-4">
                Our Campaigns page serves as a rallying point for users like you who recognize the
                importance of IPv6. Here, we highlight user-submitted lists of domains that are
                still operating in the IPv4 realm. This page is more than just a compilation of
                domains; it's a call to action for businesses, website owners, and service providers
                to step up their game and move towards an IPv6-supported future.
              </p>
              <p class="mb-8">
                Have you discovered a domain that hasn't embraced the IPv6 technology yet? We invite
                you to take an active role in our initiative. By submitting a issue to our
                <a
                  href="https://github.com/lasseh/whynoipv6-campaign"
                  class="underline a-gradient"
                  target="_blank"
                  >GitHub Repository</a
                >
                , we can collectively advocate for the adoption of IPv6. Act today and help us
                promote the adoption of IPv6, one shame campaign at a time.
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

            <!-- Error state (§6.3) -->
            <ApiError v-if="error" :problem="error" />

            <!-- Cards -->
            <div v-else class="grid grid-cols-2 xl:grid-cols-8 gap-4">
              <!-- Card -->
              <router-link
                v-for="campaign in filteredCampaignList"
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
                      <span class="text-sm font-medium text-gray-400">v6 Ready</span>
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
