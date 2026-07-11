<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import Breadcrumb from '@/components/Breadcrumb.vue'

import ChangelogTable from '@/components/ChangelogTable.vue'
import DomainStatusCard from '@/components/DomainStatusCard.vue'

import { getCampaign, getCampaignDomainChangelog, getDomain, getDomainHistory } from '@/api'
import type { ChangelogItem, DomainDetail, HistoryPoint } from '@/api'
import { ApiProblem } from '@/api/problem'

const route = useRoute()
const router = useRouter()
const uuid = route.params.uuid as string
const host = route.params.domain as string

const domain = ref<DomainDetail | null>(null)
const campaignName = ref('')
const changelogs = ref<ChangelogItem[]>([])
const history = ref<HistoryPoint[]>([])
const error = ref<ApiProblem | null>(null)

const domainRoute = (h: string) => ({ name: 'CampaignDomain', params: { uuid, domain: h } })

onMounted(async () => {
  // Breadcrumb name — non-fatal if the campaign lookup fails.
  getCampaign(uuid)
    .then((c) => (campaignName.value = c.name))
    .catch(() => (campaignName.value = ''))

  try {
    domain.value = await getDomain(host)
  } catch (e) {
    if (e instanceof ApiProblem && e.code === 'not-found') {
      void router.replace({ name: 'CampaignDomainNotFound', params: { uuid, domain: host } })
      return
    }
    error.value = ApiProblem.from(e)
    return
  }
  getCampaignDomainChangelog(uuid, host)
    .then((res) => (changelogs.value = res.items))
    .catch(() => (changelogs.value = []))
  getDomainHistory(host)
    .then((res) => (history.value = res.points))
    .catch(() => (history.value = []))
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
          <div
            v-if="error"
            class="bg-zinc-800/50 border border-zinc-700 rounded-sm shadow-lg p-5 text-center"
          >
            <div class="text-xl font-medium">{{ error.title }}</div>
            <p v-if="error.detail" class="text-gray-400 mt-2">{{ error.detail }}</p>
          </div>

          <template v-else-if="domain">
            <div class="flex justify-between items-center mb-8">
              <div class="text-left">
                <h1 class="h2">{{ domain.host }}</h1>
                <p class="text-base text-gray-400 pl-1">
                  Provider: {{ domain.asn.name }} (AS{{ domain.asn.number }})
                </p>
              </div>
            </div>

            <DomainStatusCard :domain="domain" :history="history" header-align-class="text-left" />
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
