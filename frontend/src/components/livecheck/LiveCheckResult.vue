<script setup lang="ts">
// The finished-scan card: core dimensions, informational checks, latency,
// tracked-status link, and the copy-link affordance. Pure presentation of a
// done envelope; the machine stays in useLiveCheck.
import { computed, ref } from 'vue'

import CheckIcon from '@/components/icons/Check.vue'
import CrossIcon from '@/components/icons/Cross.vue'
import MinusIcon from '@/components/icons/Minus.vue'

import type { CheckEnvelope } from '@/api'
import { liveStatus } from '@/utils/status'
import { formatDateTime } from '@/utils/date'

const props = withDefaults(defineProps<{ envelope: CheckEnvelope; example?: boolean }>(), {
  example: false,
})

const glyphs = { check: CheckIcon, cross: CrossIcon, minus: MinusIcon } as const

// Render order + wording for result.checks (§5.1.3 keys).
const CORE_CHECKS: [string, string][] = [
  ['base', 'Domain (AAAA)'],
  ['www', 'WWW (AAAA)'],
  ['ns', 'Nameservers'],
  ['mx', 'Mail (MX)'],
  ['conn', 'IPv6-only reachability'],
  ['resources', 'Page resources'],
]
const INFO_CHECKS: [string, string][] = [
  ['tls', 'TLS'],
  ['smtp', 'SMTP over IPv6'],
  ['parity', 'Content parity'],
  ['dnssec', 'DNSSEC'],
  ['ptr', 'Reverse DNS'],
  ['spf', 'SPF'],
]

// Shapes come from the generated schema — no hand-written re-declarations.
type CheckResult = NonNullable<CheckEnvelope['result']>

const checks = computed<NonNullable<CheckResult['checks']>>(
  () => props.envelope.result?.checks ?? {},
)
const latency = computed(() => props.envelope.result?.latency ?? null)
const durationSeconds = computed(() => {
  const ms = props.envelope.result?.duration_ms
  return ms ? (ms / 1000).toFixed(1) : null
})

// One liveStatus() call per row, not three: the row list carries the
// resolved status alongside its key and label.
const coreRows = computed(() =>
  CORE_CHECKS.map(([key, label]) => ({
    key,
    label,
    status: liveStatus(checks.value[key]?.status),
  })),
)
const infoRows = computed(() =>
  INFO_CHECKS.map(([key, label]) => ({
    key,
    label,
    status: liveStatus(checks.value[key]?.status),
  })),
)

// "Not applicable" on resources is ambiguous (mirrors DomainStatusCard):
// discovery only runs when the site loads over IPv6, so the same value means
// either "no live external host remained" or "couldn't evaluate".
const resourcesNote = computed(() => {
  if (checks.value.resources?.status !== 'not_applicable') return null
  return checks.value.conn?.status === 'supported'
    ? 'No live external resource host remained to grade.'
    : 'The site isn’t reachable over IPv6, so page resources can’t be evaluated.'
})

const copied = ref(false)
async function copyLink() {
  await navigator.clipboard.writeText(`${location.origin}/check/${props.envelope.host}`)
  copied.value = true
  setTimeout(() => (copied.value = false), 2_000)
}
</script>

<template>
  <div class="mt-8">
    <div class="flex items-center justify-between mb-3">
      <h2 class="text-xl font-bold text-pink-600 font-mono">{{ envelope.host }}</h2>
      <span v-if="!example">
        <button
          type="button"
          class="text-xs text-gray-400 hover:text-fuchsia-400 underline underline-offset-2 cursor-pointer"
          @click="copyLink"
        >
          {{ copied ? 'Copied' : 'Copy link' }}
        </button>
      </span>
    </div>

    <p v-if="envelope.cached" class="mb-4 text-sm text-gray-400">
      Showing a stored result. A fresh check runs automatically once it's older than 7 days.
    </p>

    <!-- Core dimensions -->
    <ul class="space-y-2">
      <li v-for="row in coreRows" :key="row.key" class="p-3 rounded bg-gray-800 text-sm">
        <div class="flex items-center justify-between">
          <span class="text-gray-200">{{ row.label }}</span>
          <span class="inline-flex items-center gap-2" :class="row.status.class">
            <component :is="glyphs[row.status.icon]" aria-hidden="true" />
            {{ row.status.label }}
          </span>
        </div>
        <p v-if="row.key === 'resources' && resourcesNote" class="mt-1 text-xs text-gray-500">
          {{ resourcesNote }}
        </p>
      </li>
    </ul>

    <!-- Informational checks -->
    <div class="mt-4 p-3 rounded bg-gray-800/50 border border-gray-700/50">
      <div class="text-xs font-medium text-gray-400 mb-2">Informational</div>
      <ul class="grid grid-cols-2 sm:grid-cols-3 gap-2">
        <li
          v-for="row in infoRows"
          :key="row.key"
          class="flex items-center justify-between text-xs px-2 py-1"
        >
          <span class="text-gray-400">{{ row.label }}</span>
          <span class="inline-flex items-center gap-1" :class="row.status.class">
            <component :is="glyphs[row.status.icon]" aria-hidden="true" />
            {{ row.status.label }}
          </span>
        </li>
      </ul>
      <div
        v-if="latency && (latency.v4_ms != null || latency.v6_ms != null)"
        class="mt-2 text-xs text-gray-400"
      >
        TTFB: IPv4 {{ latency.v4_ms != null ? `${latency.v4_ms} ms` : '—' }} · IPv6
        {{ latency.v6_ms != null ? `${latency.v6_ms} ms` : '—' }}
      </div>
    </div>

    <!-- Confirmed block -->
    <div
      v-if="envelope.confirmed"
      class="mt-4 p-3 rounded bg-gray-800/50 border border-gray-700/50 text-sm flex items-center justify-between"
    >
      <span class="text-gray-400">
        Tracked status:
        <span class="text-gray-200 capitalize">{{ envelope.confirmed.classification }}</span>
        <span v-if="envelope.confirmed.saint" class="text-emerald-500"> · Saint</span>
      </span>
      <RouterLink
        :to="`/domains/${envelope.host}`"
        class="text-fuchsia-500 hover:text-fuchsia-400 underline underline-offset-2"
      >
        Full history →
      </RouterLink>
    </div>

    <div v-if="example" class="mt-3 text-xs text-gray-500">
      Example data. Run a check above for a live result.
    </div>
    <div v-else class="mt-3 text-xs text-gray-500">
      Checked
      {{ envelope.result?.checked_at ? formatDateTime(envelope.result.checked_at) : 'just now' }}
      <template v-if="durationSeconds"> · scan took {{ durationSeconds }}s </template>
    </div>
  </div>
</template>
