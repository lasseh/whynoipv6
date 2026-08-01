<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import ApiError from '@/components/ApiError.vue'
import Breadcrumb from '@/components/Breadcrumb.vue'

import ChangelogTable from '@/components/ChangelogTable.vue'
import DomainStatusCard from '@/components/DomainStatusCard.vue'
import MandateBadge from '@/components/MandateBadge.vue'

import { getCampaign, getCampaignDomainChangelog } from '@/api'
import { useDomainDetail } from '@/composables/useDomainDetail'

const route = useRoute()
const uuid = route.params.uuid as string
const host = route.params.domain as string

const campaignName = ref('')

const domainRoute = (h: string) => ({ name: 'CampaignDomain', params: { uuid, domain: h } })

const { domain, changelogs, history, error } = useDomainDetail(host, {
  notFoundRoute: { name: 'CampaignDomainNotFound', params: { uuid, domain: host } },
  fetchChangelog: () => getCampaignDomainChangelog(uuid, host),
})

onMounted(() => {
  // Breadcrumb name — non-fatal if the campaign lookup fails.
  getCampaign(uuid)
    .then((c) => (campaignName.value = c.name))
    .catch(() => (campaignName.value = ''))
})
</script>

<template>
  <PageShell>
    <!-- Page sections -->
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-20 pb-4 md:pt-24 md:pb-4">
          <Breadcrumb
            :trail="[
              { label: 'Campaigns', to: '/campaigns' },
              { label: campaignName, to: `/campaigns/${uuid}` },
            ]"
          />

          <!-- Error state -->
          <ApiError v-if="error" :problem="error" />

          <template v-else-if="domain">
            <div class="flex justify-between items-center mb-8">
              <div class="text-left">
                <div class="flex items-center gap-3">
                  <h1 class="h2">{{ domain.host }}</h1>
                  <MandateBadge
                    v-if="domain.mandates.length > 0"
                    :names="domain.mandates.map((m) => m.name)"
                  />
                </div>
                <p class="text-base text-gray-400 pl-1">
                  Provider: {{ domain.asn.name }} (AS{{ domain.asn.number }})
                </p>
              </div>
            </div>

            <DomainStatusCard :domain="domain" :history="history" align="start" />
          </template>
        </div>
      </div>

      <!-- Changelog -->
      <div v-if="!error">
        <ChangelogTable :changelogs="changelogs" :domain-route="domainRoute" />
      </div>
    </section>
  </PageShell>
</template>
