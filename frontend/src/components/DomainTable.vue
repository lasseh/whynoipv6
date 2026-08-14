<script setup lang="ts">
import type { DomainSummary } from '@/api'
import StatusTd from '@/components/table/StatusTd.vue'
import StatusTh from '@/components/table/StatusTh.vue'

// The one domain-table module (§4.2 list row): columns Apex/WWW/Mail (MX)/
// Nameservers/IPv6 Only map to status.base/www/mx/ns.value and ipv6_only
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
          <StatusTh
            class="px-2"
            tooltip="AAAA lookup at the zone apex: dig AAAA domain.com"
            label="Apex"
          />
          <StatusTh
            class="px-2"
            tooltip="AAAA lookup for the www host: dig AAAA www.domain.com"
            label="WWW"
          />
          <StatusTh
            class="px-2"
            tooltip="AAAA records on MX hosts for domain.com"
            label="Mail (MX)"
            short-label="MX"
          />
          <StatusTh
            class="px-2"
            tooltip="AAAA records on authoritative nameserver hosts for domain.com"
            label="Nameservers"
            short-label="NS"
          />
          <StatusTh
            class="px-5"
            tooltip="The site answers over IPv6 and passes the page-resource grade"
            label="IPv6 Only"
            short-label="V6"
          />
        </tr>
      </thead>
      <!-- Table body -->
      <tbody class="text-sm divide-y divide-slate-700 border-b border-slate-700">
        <tr
          v-for="domain in domains"
          :key="domain.host"
          :class="[
            { 'bg-emerald-900/50': campaignUuid && domain.v6_ready },
            'group hover:bg-gray-800',
          ]"
        >
          <td
            v-if="!campaignUuid"
            class="px-2 py-3 whitespace-nowrap md:table-cell hidden w-px text-center"
          >
            <div class="flex items-center">
              <div
                v-if="domain.rank !== null"
                class="inline-flex text-center font-mono text-xs text-slate-300 py-1 px-3 rounded-sm bg-zinc-700/50 group-hover:bg-fuchsia-900 transition duration-150 ease-in-out"
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
          <StatusTd class="md:w-[10%]" :value="domain.status.base.value" />
          <StatusTd class="md:w-[10%]" :value="domain.status.www.value" />
          <StatusTd class="md:w-[10%]" :value="domain.status.mx.value" />
          <StatusTd class="md:w-[10%]" :value="domain.status.ns.value" />
          <StatusTd class="md:w-[10%]" :value="domain.ipv6_only" />
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
