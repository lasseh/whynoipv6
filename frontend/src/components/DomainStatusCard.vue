<script setup lang="ts">
import { computed, reactive } from 'vue'
import type { Dimension, DomainDetail, HistoryPoint } from '@/api'
import RatingStars from '@/components/RatingStars.vue'
import Tracker from '@/components/Tracker.vue'
import { statusBorderClass, statusLabel, statusTextClass } from '@/utils/status'
import { formatDateTime } from '@/utils/date'

// The Domain Status card shared by DomainDetail and its CampaignDomain
// variant (§8.3/§8.4): RatingStars header, the four-row §7.1 accordion with
// per-dimension Trackers, and the "Last checked" line.
const props = withDefaults(
  defineProps<{
    domain: DomainDetail
    history: HistoryPoint[]
    /** DomainDetail centers the header on mobile; CampaignDomain is left-aligned. */
    headerAlignClass?: string
  }>(),
  { headerAlignClass: 'text-center md:text-left' },
)

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
  const status = props.domain.status
  return [
    { key: 'base' as const, label: props.domain.host, value: status.base.value },
    { key: 'www' as const, label: `www.${props.domain.host}`, value: status.www.value },
    { key: 'ns' as const, label: 'Nameserver', value: status.ns.value },
    { key: 'mx' as const, label: 'E-Mail', value: status.mx.value },
  ]
})

const formattedTsCheck = computed(() =>
  props.domain.last_checked_at ? formatDateTime(props.domain.last_checked_at) : 'Not Checked Yet',
)
</script>

<template>
  <!-- Domain Status Card -->
  <div class="flex justify-between items-center">
    <div :class="headerAlignClass">
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
      <button
        type="button"
        class="w-full flex justify-between items-center p-3 text-base rounded group hover:shadow bg-gray-800 hover:bg-gray-800/30 text-white border-l-4 cursor-pointer"
        :class="statusBorderClass(row.value)"
        :aria-expanded="open[row.key]"
        :aria-controls="`tracker-${row.key}`"
        @click="open[row.key] = !open[row.key]"
      >
        <span class="flex-1 ml-3 whitespace-nowrap font-mono text-sm">{{ row.label }}</span>
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
