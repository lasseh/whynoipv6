<script setup lang="ts">
import { onMounted, onScopeDispose, ref } from 'vue'

// Partials
import DomainTable from '@/components/DomainTable.vue'

import { listSinners } from '@/api'
import type { DomainSummary } from '@/api'

const domainList = ref<DomainSummary[]>([])
const loading = ref(true)

// Abort the one-shot fetch when the visitor leaves before it lands.
const controller = new AbortController()
onScopeDispose(() => controller.abort())

async function getDomainList() {
  try {
    const response = await listSinners(undefined, controller.signal)
    domainList.value = response.items
  } catch {
    if (!controller.signal.aborted) domainList.value = []
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
              The Tranco top million, kept on the crawler's schedule: every domain's IPv6 support,
              or lack of it, on public display.
            </p>
            <p class="text-base text-gray-400">
              Every domain listed here still publishes an apex A record but no globally routable
              AAAA record. Nameserver IPv6 support is shown alongside; some manage one without the
              other.
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
            <nav class="flex" role="navigation" aria-label="Domain list">
              <div class="ml-2">
                <router-link
                  to="/domains"
                  class="inline-flex items-center justify-center right-2.5 bottom-2.5 focus:ring-3 focus:outline-none font-medium rounded-sm text-sm px-4 py-2 bg-fuchsia-900 hover:bg-zinc-800 focus:ring-fuchsia-800"
                  >View all domains</router-link
                >
              </div>
            </nav>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
