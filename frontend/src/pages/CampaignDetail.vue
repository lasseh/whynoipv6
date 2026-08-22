<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import ApiError from '@/components/ApiError.vue'
import Breadcrumb from '@/components/Breadcrumb.vue'

import DomainTable from '@/components/DomainTable.vue'
import ChangelogTable from '@/components/ChangelogTable.vue'
import Pagination from '@/components/Pagination.vue'
import RatingBadge from '@/components/RatingBadge.vue'
import MandateBadge from '@/components/MandateBadge.vue'
import ProgressBar from '@/components/ProgressBar.vue'

import { getCampaign, getCampaignChangelog } from '@/api'
import type { CampaignDetail } from '@/api'
import { ApiProblem } from '@/api/problem'
import { useCursorList } from '@/composables/useCursorList'
import { useEntity } from '@/composables/useEntity'
import { setPageTitle } from '@/composables/usePageMeta'

const route = useRoute()
const uuid = computed(() => String(route.params.uuid ?? ''))

// One composite request per page (07 §4.7): the campaign header and the
// members page ride the same response — no per-campaign fan-out.
const campaign = ref<CampaignDetail | null>(null)
const notFound = ref(false)

const anchorTop = ref<HTMLElement | null>(null)

const { items, page, meta, loading, error, next, prev } = useCursorList({
  anchor: anchorTop,
  key: () => uuid.value,
  fetch: async (params, signal) => {
    try {
      const res = await getCampaign(
        uuid.value,
        params.cursor !== undefined ? { cursor: params.cursor } : undefined,
        signal,
      )
      // Guard the side-writes like useCursorList guards its own: a superseded
      // response must not install a stale header or title.
      if (!signal.aborted) campaign.value = res
      return { items: res.domains.items, page: res.domains.page, meta: res.meta }
    } catch (e) {
      if (!signal.aborted && e instanceof ApiProblem && e.code === 'not-found')
        notFound.value = true
      throw e
    }
  },
})

// Changelog side surface — abort/supersede/param-renavigation via
// useEntity; a failure just leaves the list empty (non-fatal).
const { data: changelogRes } = useEntity(
  () => uuid.value,
  (u, signal) => getCampaignChangelog(u, undefined, signal),
)
const campaignChangelog = computed(() => changelogRes.value?.items ?? [])

// The list engine owns the refetch via `key`; these two side surfaces are
// this page's own state, so they still reset on a new campaign.
watch(uuid, (u) => {
  if (!u) return
  campaign.value = null
  notFound.value = false
})

// Data-driven title once the campaign loads.
watch(campaign, (c) => {
  if (c) setPageTitle(`${c.name} IPv6 Campaign`)
})

// Pagination
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
              <div class="text-xl font-medium">
                Campaign not found. Wrong UUID or a stale link; nothing to shame here.
              </div>
            </div>
          </div>

          <template v-else>
            <header ref="anchorTop" class="mb-8">
              <!-- Title and excerpt -->
              <div class="text-center md:text-left">
                <div
                  class="mb-4 flex flex-col items-center gap-2 md:flex-row md:items-center md:gap-3"
                >
                  <h1 class="h2">{{ campaign?.name ?? '' }}</h1>
                  <MandateBadge v-if="campaign?.tags.includes('mandate')" />
                </div>
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
                  {{ meta?.count ?? '—' }} domains
                </div>
                <div class="text-sm font-medium text-zinc-500 mb-2">
                  campaign readiness {{ campaign?.adoption?.v6_ready_percent ?? 0 }}%
                </div>
              </div>
            </div>
            <div class="mt-3 mb-4">
              <ProgressBar :percent="campaign?.adoption?.v6_ready_percent ?? null" height="h-4" />
            </div>

            <!-- CampaignDomains -->
            <div>
              <!-- Error state (§6.3) -->
              <ApiError v-if="error" :problem="error" />
              <DomainTable v-else :domains="items" :campaign-uuid="uuid" :loading="loading" />
            </div>

            <!-- Pagination -->
            <div class="mt-6">
              <Pagination :page="page" @previous="prev" @next="next" />
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
