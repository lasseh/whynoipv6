<script setup lang="ts">
import { ref } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import Breadcrumb from '@/components/Breadcrumb.vue'

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

const { items, page, meta, error, next, prev } = useCursorList({
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
  <PageShell>
    <!-- Page sections -->
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-20 pb-4 md:pt-24 md:pb-4">
          <Breadcrumb :trail="[{ label: 'Campaigns', to: '/campaigns' }]" />

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
              <!-- Error state (§6.3) -->
              <div
                v-if="error"
                class="bg-zinc-800/50 border border-zinc-700 rounded-sm shadow-lg p-5 text-center"
              >
                <div class="text-xl font-medium">{{ error.title }}</div>
                <p v-if="error.detail" class="text-gray-400 mt-2">{{ error.detail }}</p>
              </div>
              <CampaignDomainTable v-else :domains="items" :uuid="uuid" />
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
  </PageShell>
</template>
