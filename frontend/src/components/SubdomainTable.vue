<script setup lang="ts">
import { computed } from 'vue'
import type { DomainSummary } from '@/api'
import StatusTd from '@/components/table/StatusTd.vue'
import StatusTh from '@/components/table/StatusTh.vue'

// The children tracked under one apex (06 §3.7): curated lists, campaign
// entries and live-checked hosts alike. Informational by design, so the card
// carries no rating: what is listed varies by how much attention a domain has
// had, and the parent's score must not follow that.
// Columns drop DomainTable's Rank and WWW: a child is always rank-NULL, and
// the engine records www as not applicable for kind='subdomain'.
const props = withDefaults(
  defineProps<{
    subdomains?: DomainSummary[]
    /** Children the API reports, which can exceed one page. */
    total?: number
  }>(),
  { subdomains: () => [], total: 0 },
)

// Most subdomains carry no MX of their own, and the engine gives them no
// implicit-MX fallback (01-engine.md §11.4), so the column is usually a full
// height of dashes. Show it only when some row has something to say — plenty
// of subdomains do run mail (graph.facebook.com, m.youtube.com).
const showMx = computed(() =>
  props.subdomains.some((s) => s.status.mx.value && s.status.mx.value !== 'not_applicable'),
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
            <StatusTh
              class="px-2"
              tooltip="AAAA lookup for this host: dig AAAA host"
              label="IPv6"
            />
            <StatusTh
              class="px-2"
              tooltip="AAAA records on authoritative nameserver hosts for this subdomain"
              label="Nameservers"
              short-label="NS"
            />
            <StatusTh
              v-if="showMx"
              class="px-2"
              tooltip="AAAA records on MX hosts for this subdomain"
              label="Mail (MX)"
              short-label="MX"
            />
            <StatusTh
              class="px-5"
              tooltip="The site answers over IPv6 and passes the page-resource grade"
              label="IPv6 Only"
              short-label="V6"
            />
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
            <StatusTd class="md:w-[12%]" :value="sub.status.base.value" />
            <StatusTd class="md:w-[12%]" :value="sub.status.ns.value" />
            <StatusTd v-if="showMx" class="md:w-[12%]" :value="sub.status.mx.value" />
            <StatusTd class="md:w-[12%]" :value="sub.ipv6_only" />
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
