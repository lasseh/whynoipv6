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

import { getDomainChangelog } from '@/api'
import { useDomainDetail } from '@/composables/useDomainDetail'
import { setPageMeta, setPageTitle } from '@/composables/usePageMeta'
import { domainPageHead } from '@/utils/domain-head'

const route = useRoute()

const { domain, changelogs, history, subdomains, error } = useDomainDetail(
  () => route.params.domain as string,
  {
    notFoundRoute: (h) => ({ name: 'DomainNotFound', params: { domain: h } }),
    fetchChangelog: (h, signal) => getDomainChangelog(h, undefined, signal),
  },
)

// A subdomain gets its parent as a crumb, so the hierarchy is visible from
// the top of the page rather than implied by the hostname.
const trail = computed(() => {
  const crumbs = [{ label: 'Domains', to: '/domains' }]
  const parent = domain.value?.parent
  if (parent) {
    crumbs.push({ label: parent, to: `/domains/${parent}` })
  }
  return crumbs
})

// Data-driven head once the domain loads — the "does example.com support
// IPv6" long-tail query, plus a description naming this domain's own results.
// Built in one place so this surface and CampaignDomain cannot drift. A domain with
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
          <Breadcrumb :trail="trail" />

          <!-- Error state -->
          <ApiError v-if="error" :problem="error" />

          <template v-else-if="domain">
            <DomainReportHeader :domain="domain" show-rank />

            <DomainStatusCard :domain="domain" :history="history" />

            <SubdomainTable :subdomains="subdomains" :total="domain.subdomain_count" />
          </template>
        </div>
      </div>

      <!-- Changelog -->
      <div v-if="!error">
        <ChangelogTable :changelogs="changelogs" />
      </div>
    </section>
  </PageShell>
</template>
