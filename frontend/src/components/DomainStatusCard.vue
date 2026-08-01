<script setup lang="ts">
import { computed, reactive } from 'vue'
import type { Dimension, DomainDetail, HistoryPoint } from '@/api'
import InformationalCard from '@/components/InformationalCard.vue'
import RatingStars from '@/components/RatingStars.vue'
import Tracker from '@/components/Tracker.vue'
import { statusCardBorderClass, statusCardTextClass, statusLabel } from '@/utils/status'
import { formatDateTime } from '@/utils/date'

// The Domain Status card shared by DomainDetail and its CampaignDomain
// variant (§8.3/§8.4): RatingStars header, the four-row §7.1 accordion with
// per-dimension Trackers, and the "Last checked" line.
const props = withDefaults(
  defineProps<{
    domain: DomainDetail
    history: HistoryPoint[]
    /** DomainDetail centers the header on mobile; CampaignDomain is left-aligned. */
    align?: 'center' | 'start'
  }>(),
  { align: 'center' },
)

const headerAlignClass = computed(() =>
  props.align === 'start' ? 'text-left' : 'text-center md:text-left',
)

// Accordion state per row (the four §7.1 dimensions + the derived fold).
const open = reactive<Record<Dimension | 'ipv6_only', boolean>>({
  base: false,
  www: false,
  ns: false,
  mx: false,
  conn: false,
  resources: false,
  ipv6_only: false,
})

interface Row {
  key: Dimension | 'ipv6_only'
  label: string
  value: DomainDetail['ipv6_only']
  /** Tracker dimensions shown when expanded; labeled when more than one. */
  dims: { dim: Dimension; label?: string; desc: string }[]
}

// "Not applicable" on resources means two different things: a vacuous pass
// (page loads over IPv6, no external dependencies) or "couldn't evaluate"
// (site unreachable over IPv6, so discovery never ran) — disambiguate by
// the connection check, since discovery only runs when the site loads.
function resourcesDesc(status: DomainDetail['status']): string {
  if (status.resources.value !== 'not_applicable') {
    return 'Scripts, fonts, and images load from IPv6-capable hosts.'
  }
  return status.conn.value === 'supported'
    ? 'Not applicable: the page pulls no resources from external hosts.'
    : 'Not applicable: the site isn’t reachable over IPv6, so page resources can’t be evaluated.'
}

// The four §7.1 rows (Apex / WWW / Nameserver / Mail (MX)) plus the derived
// IPv6 Only fold (ADR 0002), which expands to its two source trackers.
const rows = computed<Row[]>(() => {
  const status = props.domain.status
  return [
    {
      key: 'base',
      label: props.domain.host,
      value: status.base.value,
      dims: [
        {
          dim: 'base',
          desc: 'The apex domain publishes an AAAA record, cross-checked against three independent resolvers.',
        },
      ],
    },
    {
      key: 'www',
      label: `www.${props.domain.host}`,
      value: status.www.value,
      dims: [{ dim: 'www', desc: 'The www hostname publishes an AAAA record.' }],
    },
    {
      key: 'ns',
      label: 'Nameservers',
      value: status.ns.value,
      dims: [
        { dim: 'ns', desc: 'The domain’s DNS is served by at least one IPv6-capable nameserver.' },
      ],
    },
    {
      key: 'mx',
      label: 'Mail (MX)',
      value: status.mx.value,
      dims: [
        {
          dim: 'mx',
          desc: 'Mail servers (MX) are reachable over IPv6 — or no mail is configured.',
        },
      ],
    },
    {
      key: 'ipv6_only',
      label: 'IPv6-only',
      value: props.domain.ipv6_only,
      dims: [
        {
          dim: 'conn',
          label: 'Reachability',
          desc: 'The site answers a real HTTP request over an IPv6-only connection.',
        },
        {
          dim: 'resources',
          label: 'Page resources',
          desc: resourcesDesc(status),
        },
      ],
    },
  ]
})

const formattedTsCheck = computed(() =>
  props.domain.last_checked_at ? formatDateTime(props.domain.last_checked_at) : 'never',
)
</script>

<template>
  <!-- Domain Status Card -->
  <div class="flex justify-between items-center">
    <div :class="headerAlignClass">
      <div class="font-bold text-xl text-pink-600">IPv6 status</div>
    </div>
    <!-- Rating Stars -->
    <div class="text-center">
      <RatingStars :status="domain.status" />
    </div>
  </div>

  <!-- Domain Status With dropdown -->
  <ul class="my-4 space-y-3">
    <li v-for="row in rows" :key="row.key">
      <button
        type="button"
        class="w-full flex justify-between items-center p-3 text-base text-left rounded group hover:shadow bg-gray-800 hover:bg-gray-800/30 text-white border-l-4 cursor-pointer"
        :class="statusCardBorderClass(row.value)"
        :aria-expanded="open[row.key]"
        :aria-controls="`tracker-${row.key}`"
        @click="open[row.key] = !open[row.key]"
      >
        <span class="flex-1 ml-3 whitespace-nowrap font-mono text-sm">{{ row.label }}</span>
        <span
          :class="statusCardTextClass(row.value)"
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
            <path stroke-linecap="round" stroke-linejoin="round" d="m19.5 8.25-7.5 7.5-7.5-7.5" />
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
            <path stroke-linecap="round" stroke-linejoin="round" d="m4.5 15.75 7.5-7.5 7.5 7.5" />
          </svg>
        </span>
      </button>
      <div
        v-show="open[row.key] && history.length > 0"
        :id="`tracker-${row.key}`"
        class="mt-2 p-2 mr-1 ml-1 bg-gray-800/30 rounded-md"
      >
        <div v-for="d in row.dims" :key="d.dim">
          <div v-if="d.label" class="mt-2 text-xs font-medium text-gray-400">{{ d.label }}</div>
          <div class="mt-1 text-xs text-gray-500">{{ d.desc }}</div>
          <Tracker
            :points="history"
            :dimension="d.dim"
            :days="90"
            :hover-effect="true"
            class="mt-3 hidden lg:block"
          />
          <Tracker
            :points="history"
            :dimension="d.dim"
            :days="60"
            :hover-effect="true"
            class="mt-3 hidden sm:block lg:hidden"
          />
          <Tracker
            :points="history"
            :dimension="d.dim"
            :days="30"
            :hover-effect="true"
            class="mt-3 block sm:hidden"
          />
        </div>
      </div>
    </li>
  </ul>

  <InformationalCard :informational="domain.informational" />

  <div class="mt-4 flex items-center justify-between text-xs font-normal text-gray-400">
    <span>Last checked: {{ formattedTsCheck }}</span>
    <RouterLink
      to="/faq?page=2"
      class="text-gray-400 hover:text-fuchsia-500 underline underline-offset-2"
    >
      How these checks work →
    </RouterLink>
  </div>
  <!-- End Domain Status Card -->
</template>
