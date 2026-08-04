<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import ApiError from '@/components/ApiError.vue'
import Breadcrumb from '@/components/Breadcrumb.vue'

import ChangelogTable from '@/components/ChangelogTable.vue'
import DomainStatusCard from '@/components/DomainStatusCard.vue'
import MandateBadge from '@/components/MandateBadge.vue'
import SubdomainTable from '@/components/SubdomainTable.vue'

import { getDomainChangelog } from '@/api'
import { useDomainDetail } from '@/composables/useDomainDetail'
import { setPageTitle } from '@/composables/usePageMeta'

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

// Data-driven title once the domain loads — the "does example.com support
// IPv6" long-tail query, mirrored by CampaignDomain.
watch(domain, (d) => {
  if (d) setPageTitle(`Does ${d.host} support IPv6?`)
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
                <p v-if="domain.parent" class="text-base text-gray-400 pl-1">
                  Subdomain of
                  <router-link
                    :to="{ name: 'DomainDetail', params: { domain: domain.parent } }"
                    class="text-fuchsia-500 hover:text-fuchsia-400 underline underline-offset-2"
                  >
                    {{ domain.parent }}
                  </router-link>
                </p>
              </div>
              <div v-if="domain.rank !== null" class="text-center">
                <div
                  class="inline-flex text-center font-mono py-1 px-3 rounded-sm bg-fuchsia-900 hover:bg-fuchsia-900 transition duration-150 ease-in-out"
                >
                  Rank: {{ domain.rank }}
                </div>
              </div>
            </div>

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
