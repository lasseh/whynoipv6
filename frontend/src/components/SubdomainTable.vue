<script setup lang="ts">
import type { DomainSummary } from '@/api'
import StatusIcon from '@/components/StatusIcon.vue'
import Tooltip from '@/components/Tooltip.vue'

// The children tracked under one apex (06 §3.7): curated lists, campaign
// entries and live-checked hosts alike. Informational by design, so the card
// carries no rating: what is listed varies by how much attention a domain has
// had, and the parent's score must not follow that.
// Columns drop DomainTable's Rank and WWW: a child is always rank-NULL, and
// the engine records www as not applicable for kind='subdomain'.
withDefaults(
  defineProps<{
    subdomains?: DomainSummary[]
    /** Children the API reports, which can exceed one page. */
    total?: number
  }>(),
  { subdomains: () => [], total: 0 },
)
</script>

<template>
  <div v-if="subdomains.length > 0" class="mt-10">
    <div class="font-bold text-xl text-pink-600">Subdomains</div>
    <p class="mt-1 text-sm text-gray-400">
      Other hosts tracked under this domain, checked the same way. They do not affect its rating.
    </p>

    <div class="overflow-x-auto">
      <table class="table-fixed min-w-full text-slate-300 mt-4">
        <thead
          class="text-xs font-semibold uppercase text-fuchsia-600 border-t border-b border-slate-700"
        >
          <tr>
            <th class="md:px-2 px-5 py-3 whitespace-nowrap text-left">
              <div class="font-semibold text-left">Host</div>
            </th>
            <th class="px-2 py-3 whitespace-nowrap">
              <Tooltip text="AAAA lookup for this host: dig AAAA host">IPv6</Tooltip>
            </th>
            <th class="px-2 py-3 whitespace-nowrap">
              <div class="font-semibold text-center md:block hidden">
                <Tooltip
                  text="Authoritative nameservers for this subdomain, each checked for an AAAA record"
                  >Nameservers</Tooltip
                >
              </div>
              <div class="font-semibold text-center md:hidden">NS</div>
            </th>
            <th class="px-2 py-3 whitespace-nowrap">
              <div class="font-semibold text-center md:block hidden">
                <Tooltip text="MX hosts for this subdomain, each checked for an AAAA record"
                  >Mail (MX)</Tooltip
                >
              </div>
              <div class="font-semibold text-center md:hidden">MX</div>
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
        <tbody class="text-sm divide-y divide-slate-700 border-b border-slate-700">
          <tr v-for="sub in subdomains" :key="sub.host" class="hover:bg-gray-800">
            <td class="px-5 md:px-2 py-3 whitespace-nowrap text-left">
              <router-link
                :to="{ name: 'DomainDetail', params: { domain: sub.host } }"
                class="font-medium text-slate-100"
              >
                {{ sub.host }}
              </router-link>
            </td>
            <td class="px-2 py-3 whitespace-nowrap w-px md:w-[12%] text-center">
              <div class="inline-flex px-2.5 py-1">
                <StatusIcon :value="sub.status.base.value" />
              </div>
            </td>
            <td class="px-2 py-3 whitespace-nowrap w-px md:w-[12%] text-center">
              <div class="inline-flex px-2.5 py-1">
                <StatusIcon :value="sub.status.ns.value" />
              </div>
            </td>
            <td class="px-2 py-3 whitespace-nowrap w-px md:w-[12%] text-center">
              <div class="inline-flex px-2.5 py-1">
                <StatusIcon :value="sub.status.mx.value" />
              </div>
            </td>
            <td class="px-2 py-3 whitespace-nowrap w-px md:w-[12%] text-center">
              <div class="inline-flex px-2.5 py-1">
                <StatusIcon :value="sub.ipv6_only" />
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="mt-3 flex items-center justify-between gap-3 text-xs text-gray-400">
      <span>
        <template v-if="total > subdomains.length">
          Showing the first {{ subdomains.length }} of {{ total }}.
        </template>
      </span>
      <a
        href="https://github.com/lasseh/whynoipv6-campaign"
        target="_blank"
        rel="noopener noreferrer"
        class="text-gray-400 hover:text-fuchsia-500 underline underline-offset-2 whitespace-nowrap"
      >
        Suggest a subdomain →
      </a>
    </div>
  </div>
</template>
