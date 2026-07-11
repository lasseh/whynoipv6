<script setup lang="ts">
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import ApiError from '@/components/ApiError.vue'
import Breadcrumb from '@/components/Breadcrumb.vue'

import ChangelogTable from '@/components/ChangelogTable.vue'
import DomainStatusCard from '@/components/DomainStatusCard.vue'

import { getDomainChangelog } from '@/api'
import { useDomainDetail } from '@/composables/useDomainDetail'

const route = useRoute()
const host = route.params.domain as string

const { domain, changelogs, history, error } = useDomainDetail(host, {
  notFoundRoute: { name: 'DomainNotFound', params: { domain: host } },
  fetchChangelog: () => getDomainChangelog(host),
})
</script>

<template>
  <PageShell>
    <!-- Page sections -->
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-20 pb-4 md:pt-24 md:pb-4">
          <Breadcrumb :trail="[{ label: 'Domains', to: '/domains' }]" />

          <!-- Error state -->
          <ApiError v-if="error" :problem="error" />

          <template v-else-if="domain">
            <div class="flex justify-between items-center mb-8">
              <div class="text-left">
                <h1 class="h2">{{ domain.host }}</h1>
                <p class="text-base text-gray-400 pl-1">
                  Provider: {{ domain.asn.name }} (AS{{ domain.asn.number }})
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
