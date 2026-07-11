<script setup lang="ts">
import { ref } from 'vue'
import { useRoute } from 'vue-router'

import Header from '@/partials/Header.vue'
import PageIllustration from '@/partials/PageIllustration.vue'
import Footer from '@/partials/Footer.vue'

import CampaignDomainTable from '@/components/CampaignDomainTable.vue'
import ChangelogTable from '@/components/ChangelogTable.vue'
import Pagination from '@/components/Pagination.vue'
import RatingBadge from '@/components/RatingBadge.vue'
import ProgressBar from '@/components/ProgressBar.vue'

import { getCampaign, getCampaignChangelog } from '@/api'
import type { CampaignDetail, ChangelogItem } from '@/api'
import { ApiProblem } from '@/api/problem'
import { useCursorList } from '@/composables/useCursorList'

const route = useRoute()
const uuid = String(route.params.uuid)

// One composite request per page (07 §4.7): the campaign header and the
// members page ride the same response — no per-campaign fan-out.
const campaign = ref<CampaignDetail | null>(null)
const notFound = ref(false)

const { items, page, meta, next, prev } = useCursorList({
  fetch: async (params, signal) => {
    try {
      const res = await getCampaign(
        uuid,
        params.cursor !== undefined ? { cursor: params.cursor } : undefined,
        signal,
      )
      campaign.value = res
      return { items: res.domains.items, page: res.domains.page, meta: res.meta }
    } catch (e) {
      if (e instanceof ApiProblem && e.code === 'not-found') notFound.value = true
      throw e
    }
  },
})

const campaignChangelog = ref<ChangelogItem[]>([])
getCampaignChangelog(uuid)
  .then((res) => {
    campaignChangelog.value = res.items
  })
  .catch(() => {
    campaignChangelog.value = []
  })

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
                        to="/campaigns"
                        class="ml-1 text-sm font-medium md:ml-2 text-gray-400 hover:text-white"
                        >Campaigns</router-link
                      >
                    </div>
                  </li>
                </ol>
              </nav>
            </div>
            <!-- End breadcrumb -->

            <!-- Campaign not found -->
            <div v-if="notFound" class="flex justify-center py-16">
              <div class="text-center">
                <div class="text-xl font-medium">Campaign not found</div>
              </div>
            </div>

            <template v-else>
              <header ref="anchorTop" class="mb-8">
                <!-- Title and excerpt -->
                <div class="text-center md:text-left">
                  <h1 class="h2 mb-4">{{ campaign?.name ?? '' }}</h1>
                  <p class="text-xl text-gray-400">{{ campaign?.description ?? '' }}</p>
                </div>
              </header>

              <div class="flex justify-between items-center">
                <div>
                  <RatingBadge
                    :percent="campaign?.adoption?.v6_ready_percent ?? null"
                    size="text-base"
                  />
                </div>
                <div>
                  <div class="text-sm font-medium text-zinc-500 mb-2">
                    {{ meta?.count ?? '—' }} Domains
                  </div>
                  <div class="text-sm font-medium text-zinc-500 mb-2">
                    {{ campaign?.adoption?.v6_ready_percent ?? 0 }}% V6 Ready
                  </div>
                </div>
              </div>
              <div class="mt-3 mb-4">
                <ProgressBar :percent="campaign?.adoption?.v6_ready_percent ?? null" height="h-4" />
              </div>

              <!-- CampaignDomains -->
              <div>
                <CampaignDomainTable :domains="items" :uuid="uuid" />
              </div>

              <!-- Pagination -->
              <div class="mt-6">
                <Pagination :page="page" @previous="goPrevious" @next="goNext" />
              </div>
            </template>
          </div>
        </div>

        <div v-if="!notFound">
          <ChangelogTable
            :changelogs="campaignChangelog"
            :domain-route="(h: string) => ({ name: 'CampaignDomain', params: { uuid, domain: h } })"
          />
        </div>
      </section>
    </main>

    <!-- Site footer -->
    <Footer />
  </div>
</template>
