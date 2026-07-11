<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import Header from '@/partials/Header.vue'
import PageIllustration from '@/partials/PageIllustration.vue'
import Footer from '@/partials/Footer.vue'

import ChangelogTable from '@/components/ChangelogTable.vue'
import RatingStars from '@/components/RatingStars.vue'
import Tracker from '@/components/Tracker.vue'

import { getDomain, getDomainChangelog, getDomainHistory } from '@/api'
import type { ChangelogItem, Dimension, DomainDetail, HistoryPoint } from '@/api'
import { ApiProblem } from '@/api/problem'
import { statusBorderClass, statusLabel, statusTextClass } from '@/utils/status'
import { formatDateTime } from '@/utils/date'

const route = useRoute()
const router = useRouter()
const host = route.params.domain as string

const domain = ref<DomainDetail | null>(null)
const changelogs = ref<ChangelogItem[]>([])
const history = ref<HistoryPoint[]>([])
const error = ref<ApiProblem | null>(null)

// Accordion state per dimension row.
const open = reactive<Record<Dimension, boolean>>({
  base: false,
  www: false,
  ns: false,
  mx: false,
  conn: false,
  resources: false,
})

// The four phase-1 rows (§7.1): Apex / WWW / Nameserver / E-Mail.
const rows = computed(() => {
  if (!domain.value) return []
  const status = domain.value.status
  return [
    { key: 'base' as const, label: domain.value.host, value: status.base.value },
    { key: 'www' as const, label: `www.${domain.value.host}`, value: status.www.value },
    { key: 'ns' as const, label: 'Nameserver', value: status.ns.value },
    { key: 'mx' as const, label: 'E-Mail', value: status.mx.value },
  ]
})

const formattedTsCheck = computed(() => {
  if (domain.value?.last_checked_at) {
    return formatDateTime(domain.value.last_checked_at)
  }
  return 'Not Checked Yet'
})

onMounted(async () => {
  try {
    domain.value = await getDomain(host)
  } catch (e) {
    if (e instanceof ApiProblem && e.code === 'not-found') {
      void router.replace({ name: 'DomainNotFound', params: { domain: host } })
      return
    }
    error.value = e instanceof ApiProblem ? e : new ApiProblem({ title: 'Request failed' }, 0)
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
                        to="/domains"
                        class="ml-1 text-sm font-medium md:ml-2 text-gray-400 hover:text-white"
                        >Domains</router-link
                      >
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
                <div v-if="domain.rank !== null" class="text-center">
                  <div
                    class="inline-flex text-center font-mono py-1 px-3 rounded-sm bg-fuchsia-900 hover:bg-fuchsia-900 transition duration-150 ease-in-out"
                  >
                    Rank: {{ domain.rank }}
                  </div>
                </div>
              </div>

              <!-- Domain Status Card -->
              <div class="flex justify-between items-center">
                <div class="text-center md:text-left">
                  <div class="font-bold text-xl text-pink-600">Domain Status</div>
                </div>
                <!-- Rating Stars -->
                <div class="text-center">
                  <RatingStars :status="domain.status" />
                </div>
              </div>

              <!-- Domain Status With dropdown -->
              <ul class="my-4 space-y-3">
                <li v-for="row in rows" :key="row.key">
                  <div
                    class="flex justify-between items-center p-3 text-base rounded group hover:shadow bg-gray-800 hover:bg-gray-800/30 text-white border-l-4 cursor-pointer"
                    :class="statusBorderClass(row.value)"
                    @click="open[row.key] = !open[row.key]"
                  >
                    <span class="flex-1 ml-3 whitespace-nowrap font-mono text-sm">{{
                      row.label
                    }}</span>
                    <span
                      :class="statusTextClass(row.value)"
                      class="inline-flex items-center justify-center px-2 py-0.5 ml-3"
                    >
                      {{ statusLabel(row.value) }}
                    </span>
                    <!-- Up/Down Icon -->
                    <span v-show="history.length > 0" class="ml-2">
                      <svg
                        v-if="!open[row.key]"
                        xmlns="http://www.w3.org/2000/svg"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke-width="1.5"
                        stroke="currentColor"
                        class="w-4 h-4"
                      >
                        <path
                          stroke-linecap="round"
                          stroke-linejoin="round"
                          d="m19.5 8.25-7.5 7.5-7.5-7.5"
                        />
                      </svg>

                      <svg
                        v-else
                        xmlns="http://www.w3.org/2000/svg"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke-width="1.5"
                        stroke="currentColor"
                        class="w-4 h-4"
                      >
                        <path
                          stroke-linecap="round"
                          stroke-linejoin="round"
                          d="m4.5 15.75 7.5-7.5 7.5 7.5"
                        />
                      </svg>
                    </span>
                  </div>
                  <div
                    v-show="open[row.key] && history.length > 0"
                    class="mt-2 p-2 mr-1 ml-1 bg-gray-800/30 rounded-md"
                  >
                    <Tracker
                      :points="history"
                      :dimension="row.key"
                      :days="90"
                      :hover-effect="true"
                      class="mt-3 hidden lg:block"
                    />
                    <Tracker
                      :points="history"
                      :dimension="row.key"
                      :days="60"
                      :hover-effect="true"
                      class="mt-3 hidden sm:block lg:hidden"
                    />
                    <Tracker
                      :points="history"
                      :dimension="row.key"
                      :days="30"
                      :hover-effect="true"
                      class="mt-3 block sm:hidden"
                    />
                  </div>
                </li>
              </ul>

              <div class="inline-flex items-center text-xs font-normal text-gray-400">
                Last checked: {{ formattedTsCheck }}
              </div>
              <!-- End Domain Status Card -->
            </template>
          </div>
        </div>

        <!-- Changelog -->
        <div v-if="!error">
          <ChangelogTable :changelogs="changelogs" />
        </div>
      </section>
    </main>

    <!-- Site footer -->
    <Footer />
  </div>
</template>
