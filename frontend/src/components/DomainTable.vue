<script setup lang="ts">
import { ref } from 'vue'
import type { DomainSummary } from '@/api'
import StatusIcon from '@/components/StatusIcon.vue'
import Tooltip from '@/components/Tooltip.vue'

// The one domain-table module (§4.2 list row): columns Apex/WWW/E-Mail/
// Nameserver/IPv6 Only map to status.base/www/mx/ns.value and ipv6_only
// (§7.1). Two surfaces share it — leaderboards (rank badge, hidden on null
// and never 0 (§7.3), DomainDetail links) and campaign members
// (`campaignUuid` set: member links, the server's v6_ready row highlight
// (07 §4.7 — never re-derived here), no Rank column).
// `loading` suppresses the empty state so it can't flash before the first
// page arrives.
withDefaults(
  defineProps<{ domains?: DomainSummary[]; loading?: boolean; campaignUuid?: string }>(),
  {
    domains: () => [],
    loading: false,
  },
)

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
          <th
            v-if="!campaignUuid"
            class="px-5 py-3 whitespace-nowrap text-left md:table-cell hidden"
          >
            <div class="font-semibold text-left">Rank</div>
          </th>
          <th class="md:px-2 px-5 py-3 whitespace-nowrap text-left">
            <div class="font-semibold text-left">Domain</div>
          </th>
          <th class="px-2 py-3 whitespace-nowrap">
            <Tooltip text="Top-level domain query: dig AAAA domain.com">Apex</Tooltip>
          </th>
          <th class="px-2 py-3 whitespace-nowrap">
            <Tooltip text="Query AAAA record for www.domain.com">WWW</Tooltip>
          </th>
          <th class="px-2 py-3 whitespace-nowrap">
            <div class="font-semibold text-center md:block hidden">
              <Tooltip text="Query MX record for domain.com">Mail (MX)</Tooltip>
            </div>
            <div class="font-semibold text-center md:hidden">MX</div>
          </th>
          <th class="px-2 py-3 whitespace-nowrap">
            <div class="font-semibold text-center md:block hidden">
              <Tooltip text="Query NS record for domain.com">Nameserver</Tooltip>
            </div>
            <div class="font-semibold text-center md:hidden">NS</div>
          </th>
          <th class="px-5 py-3 whitespace-nowrap">
            <div class="font-semibold text-center md:block hidden">
              <Tooltip text="Loads fully over an IPv6-only connection (site + page resources)"
                >IPv6 Only</Tooltip
              >
            </div>
            <div class="font-semibold text-center md:hidden">V6</div>
          </th>
        </tr>
      </thead>
      <!-- Table body -->
      <tbody class="text-sm divide-y divide-slate-700 border-b border-slate-700">
        <tr
          v-for="(domain, index) in domains"
          :key="domain.host"
          :class="[{ 'bg-emerald-900/50': campaignUuid && domain.v6_ready }, 'hover:bg-gray-800']"
          @mouseover="hoverIndex = index"
          @mouseout="hoverIndex = null"
        >
          <td
            v-if="!campaignUuid"
            class="px-2 py-3 whitespace-nowrap md:table-cell hidden w-px text-center"
          >
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
                :to="
                  campaignUuid
                    ? {
                        name: 'CampaignDomain',
                        params: { uuid: campaignUuid, domain: domain.host },
                      }
                    : { name: 'DomainDetail', params: { domain: domain.host } }
                "
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
          <td class="px-2 py-3 whitespace-nowrap w-px md:w-[10%] text-center">
            <div class="inline-flex px-2.5 py-1">
              <StatusIcon :value="domain.ipv6_only" />
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
