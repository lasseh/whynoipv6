<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import Breadcrumb from '@/components/Breadcrumb.vue'

import ChangelogTable from '@/components/ChangelogTable.vue'
import DomainStatusCard from '@/components/DomainStatusCard.vue'

import { getDomain, getDomainChangelog, getDomainHistory } from '@/api'
import type { ChangelogItem, DomainDetail, HistoryPoint } from '@/api'
import { ApiProblem } from '@/api/problem'

const route = useRoute()
const router = useRouter()
const host = route.params.domain as string

const domain = ref<DomainDetail | null>(null)
const changelogs = ref<ChangelogItem[]>([])
const history = ref<HistoryPoint[]>([])
const error = ref<ApiProblem | null>(null)

onMounted(async () => {
  try {
    domain.value = await getDomain(host)
  } catch (e) {
    if (e instanceof ApiProblem && e.code === 'not-found') {
      void router.replace({ name: 'DomainNotFound', params: { domain: host } })
      return
    }
    error.value = ApiProblem.from(e)
    return
  }
  // Non-fatal side surfaces — an error just leaves them empty.
  getDomainChangelog(host)
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
          <Breadcrumb :trail="[{ label: 'Domains', to: '/domains' }]" />

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
