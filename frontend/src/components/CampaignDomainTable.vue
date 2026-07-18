<script setup lang="ts">
import type { DomainSummary } from '@/api'
import StatusIcon from '@/components/StatusIcon.vue'

// Campaign members table: no Rank column; the "fully ready" row highlight
// consumes the server's per-row v6_ready flag — derived backend-side from
// the same predicate as adoption.v6_ready_percent (07 §4.7), never
// re-derived here. `loading` suppresses the empty state so it can't flash
// before the first page arrives.
withDefaults(defineProps<{ domains?: DomainSummary[]; uuid: string; loading?: boolean }>(), {
  domains: () => [],
  loading: false,
})
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
          v-for="domain in domains"
          :key="domain.host"
          :class="[{ 'bg-emerald-900/50': domain.v6_ready }, 'hover:bg-gray-800']"
        >
          <td class="px-5 py-3 whitespace-nowrap text-left">
            <div class="flex items-center">
              <router-link
                :to="{ name: 'CampaignDomain', params: { uuid, domain: domain.host } }"
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

    <!-- Empty state -->
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
