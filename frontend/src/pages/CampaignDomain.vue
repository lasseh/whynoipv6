<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import ApiError from '@/components/ApiError.vue'
import Breadcrumb from '@/components/Breadcrumb.vue'

import ChangelogTable from '@/components/ChangelogTable.vue'
import DomainReportHeader from '@/components/DomainReportHeader.vue'
import DomainStatusCard from '@/components/DomainStatusCard.vue'
import SubdomainTable from '@/components/SubdomainTable.vue'

import { getCampaign, getCampaignDomainChangelog } from '@/api'
import { useDomainDetail } from '@/composables/useDomainDetail'
import { useEntity } from '@/composables/useEntity'
import { setPageMeta, setPageTitle } from '@/composables/usePageMeta'
import { domainPageHead } from '@/utils/domain-head'

const route = useRoute()
const uuid = computed(() => route.params.uuid as string)

const domainRoute = (h: string) => ({
  name: 'CampaignDomain',
  params: { uuid: uuid.value, domain: h },
})

const { domain, changelogs, history, subdomains, error } = useDomainDetail(
  () => route.params.domain as string,
  {
    notFoundRoute: (h) => ({
      name: 'CampaignDomainNotFound',
      params: { uuid: uuid.value, domain: h },
    }),
    fetchChangelog: (h, signal) => getCampaignDomainChangelog(uuid.value, h, undefined, signal),
  },
)

// Breadcrumb name — non-fatal if the campaign lookup fails; useEntity
// refetches on param-only navigation and aborts a superseded fetch.
const { data: campaignHeader } = useEntity(
  () => uuid.value,
  (u, signal) => getCampaign(u, undefined, signal),
)
const campaignName = computed(() => campaignHeader.value?.name ?? '')

// Data-driven head once the domain loads — the "does example.com support
// IPv6" long-tail query, plus a description naming this domain's own results.
// Built in one place so this surface and DomainDetail cannot drift. A domain with
// nothing confirmed yet keeps the route's description.
watch(domain, (d) => {
  if (!d) return
  const { title, description } = domainPageHead(d)
  if (description) setPageMeta(title, description)
  else setPageTitle(title)
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
            <DomainReportHeader :domain="domain" />

            <DomainStatusCard :domain="domain" :history="history" align="start" />

            <SubdomainTable :subdomains="subdomains" :total="domain.subdomain_count" />
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
