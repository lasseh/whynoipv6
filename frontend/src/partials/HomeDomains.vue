<script setup lang="ts">
import { onMounted, ref } from 'vue'

// Partials
import DomainTable from '@/components/DomainTable.vue'

import { listSinners } from '@/api'
import type { DomainSummary } from '@/api'

const domainList = ref<DomainSummary[]>([])
const loading = ref(true)

async function getDomainList() {
  try {
    const response = await listSinners()
    domainList.value = response.items
  } catch {
    domainList.value = []
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void getDomainList()
})
</script>

<template>
  <section class="relative">
    <div class="max-w-6xl mx-auto px-4 sm:px-6">
      <div class="pt-4 pb-4 md:pt-4 md:pb-4">
        <header class="mb-6">
          <!-- Title and excerpt -->
          <div class="text-left">
            <h2 class="h3 mb-4">Wall of Shame</h2>
            <p class="text-base text-gray-400">
              As a part of this shaming-strategy, we supply all interested parties with an updated
              list over the top 1 million websites and their corresponding (lack of) IPv6 support
              clearly stated.
            </p>
            <p class="text-base text-gray-400">
              Each of the listed websites lack an AAAA-record. In addition the nameserver-support
              for IPv6 is displayed.
            </p>
          </div>
        </header>

        <!-- DomainList -->
        <div>
          <DomainTable :domains="domainList" :loading="loading" />
        </div>

        <!-- Button to Domain List-->
        <div class="mt-8">
          <div class="flex justify-center">
            <nav class="flex" role="navigation" aria-label="Navigation">
              <div class="ml-2">
                <router-link
                  to="/domains"
                  class="inline-flex items-center justify-center right-2.5 bottom-2.5 focus:ring-3 focus:outline-none font-medium rounded-sm text-sm px-4 py-2 bg-fuchsia-900 hover:bg-zinc-800 focus:ring-fuchsia-800"
                  >View More Domains</router-link
                >
              </div>
            </nav>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
