<script setup lang="ts">
import { ref } from 'vue'
import type { DomainSummary } from '@/api'
import StatusIcon from '@/components/StatusIcon.vue'

// The §4.2 list row: columns Apex/WWW/E-Mail/Nameserver map to
// status.base/www/mx/ns.value (§7.1). Rank badge hides on null — never
// renders 0 (§7.3). `loading` suppresses the empty state so it can't
// flash before the first page arrives.
withDefaults(defineProps<{ domains?: DomainSummary[]; loading?: boolean }>(), {
  domains: () => [],
  loading: false,
})

const hoverIndex = ref<number | null>(null)
</script>

<template>
  <!-- Table -->
  <div class="overflow-x-auto">
    <table v-if="domains.length > 0" class="table-fixed min-w-full text-slate-300">
      <!-- Table header -->
      <thead
        class="text-xs font-semibold uppercase text-fuchsia-600 border-t border-b border-slate-700"
      >
        <tr>
          <th class="px-5 py-3 whitespace-nowrap text-left md:table-cell hidden">
            <div class="font-semibold text-left">Rank</div>
          </th>
          <th class="md:px-2 px-5 py-3 whitespace-nowrap text-left">
            <div class="font-semibold text-left">Domain</div>
          </th>
          <th class="px-2 py-3 whitespace-nowrap">
            <div class="has-tooltip">
              <span
                class="tooltip rounded border border-slate-700 shadow-lg p-1 bg-gray-800 text-fuchsia-600 -mt-8 normal-case"
                >Top-level domain query: dig AAAA domain.com</span
              >
              Apex
            </div>
          </th>
          <th class="px-2 py-3 whitespace-nowrap">
            <div class="has-tooltip">
              <span
                class="tooltip rounded border border-slate-700 shadow-lg p-1 bg-gray-800 text-fuchsia-600 -mt-8 normal-case"
                >Query AAAA record for www.domain.com</span
              >
              WWW
            </div>
          </th>
          <th class="px-2 py-3 whitespace-nowrap">
            <div class="font-semibold text-center md:block hidden">
              <div class="has-tooltip">
                <span
                  class="tooltip rounded border border-slate-700 shadow-lg p-1 bg-gray-800 text-fuchsia-600 -mt-8 normal-case"
                  >Query MX record for domain.com</span
                >
                E-Mail
              </div>
            </div>
            <div class="font-semibold text-center md:hidden">MX</div>
          </th>
          <th class="px-5 py-3 whitespace-nowrap">
            <div class="font-semibold text-center md:block hidden">
              <div class="has-tooltip">
                <span
                  class="tooltip rounded border border-slate-700 shadow-lg p-1 bg-gray-800 text-fuchsia-600 -mt-8 normal-case"
                  >Query NS record for domain.com</span
                >
                Nameserver
              </div>
            </div>
            <div class="font-semibold text-center md:hidden">NS</div>
          </th>
        </tr>
      </thead>
      <!-- Table body -->
      <tbody class="text-sm divide-y divide-slate-700 border-b border-slate-700">
        <tr
          v-for="(domain, index) in domains"
          :key="domain.host"
          class="hover:bg-gray-800"
          @mouseover="hoverIndex = index"
          @mouseout="hoverIndex = null"
        >
          <td class="px-2 py-3 whitespace-nowrap md:table-cell hidden w-px text-center">
            <div class="flex items-center">
              <div
                v-if="domain.rank !== null"
                :class="hoverIndex === index ? 'bg-fuchsia-900' : 'bg-zinc-700/50'"
                class="inline-flex text-center font-mono text-xs text-slate-300 py-1 px-3 rounded-sm hover:bg-fuchsia-900 transition duration-150 ease-in-out"
              >
                {{ domain.rank }}
              </div>
            </div>
          </td>
          <td class="px-5 md:px-2 py-3 whitespace-nowrap text-left">
            <div class="flex items-center">
              <router-link
                :to="{ name: 'DomainDetail', params: { domain: domain.host } }"
                class="font-medium text-slate-100"
              >
                {{ domain.host }}
              </router-link>
            </div>
          </td>
          <td class="px-2 py-3 whitespace-nowrap w-px md:w-[10%] text-center">
            <div class="inline-flex px-2.5 py-1">
              <StatusIcon :value="domain.status.base.value" />
            </div>
          </td>
          <td class="px-2 py-3 whitespace-nowrap w-px md:w-[10%] text-center">
            <div class="inline-flex px-2.5 py-1">
              <StatusIcon :value="domain.status.www.value" />
            </div>
          </td>
          <td class="px-2 py-3 whitespace-nowrap w-px md:w-[10%] text-center">
            <div class="inline-flex px-2.5 py-1">
              <StatusIcon :value="domain.status.mx.value" />
            </div>
          </td>
          <td class="px-2 py-3 whitespace-nowrap w-px md:w-[10%] text-center">
            <div class="inline-flex px-2.5 py-1">
              <StatusIcon :value="domain.status.ns.value" />
            </div>
          </td>
        </tr>
      </tbody>
    </table>

    <!-- No Data Available State -->
    <div v-else-if="!loading" class="flex justify-center">
      <div class="text-center">
        <div class="text-xl font-medium">No domains found</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tooltip {
  visibility: hidden;
  position: absolute;
}

.has-tooltip:hover .tooltip {
  visibility: visible;
  z-index: 50;
}
</style>
