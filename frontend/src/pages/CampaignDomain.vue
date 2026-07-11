<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import Header from '@/partials/Header.vue'
import PageIllustration from '@/partials/PageIllustration.vue'
import Footer from '@/partials/Footer.vue'

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
                  <li>
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
                        :to="`/campaigns/${uuid}`"
                        class="ml-1 text-sm font-medium md:ml-2 text-gray-400 hover:text-white truncate"
                      >
                        {{ campaignName }}
                      </router-link>
                    </div>
                  </li>
                </ol>
              </nav>
            </div>

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

              <DomainStatusCard
                :domain="domain"
                :history="history"
                header-align-class="text-left"
              />
            </template>
          </div>
        </div>

        <!-- Changelog -->
        <div v-if="!error">
          <ChangelogTable :changelogs="changelogs" :domain-route="domainRoute" />
        </div>
      </section>
    </main>

    <!-- Site footer -->
    <Footer />
  </div>
</template>
