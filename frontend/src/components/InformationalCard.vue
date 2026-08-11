<script setup lang="ts">
import { computed } from 'vue'
import type { DomainDetail } from '@/api'
import { infoStatus } from '@/utils/status'
import CheckIcon from '@/components/icons/Check.vue'
import CrossIcon from '@/components/icons/Cross.vue'
import MinusIcon from '@/components/icons/Minus.vue'

// The §4.3 informational block as the quiet secondary card of 12-frontend §10.3.
// These four dimensions are advisory — they store the latest observation only,
// never pass the confirm/pending machinery, and never gate classification
// (02 §5). `tls` and `spf` run too but have no typed column anywhere; they
// live only in scan_detail evidence, so they cannot appear here.
const props = defineProps<{ informational: DomainDetail['informational'] }>()

const glyphs = { check: CheckIcon, cross: CrossIcon, minus: MinusIcon } as const

const DIMENSIONS: { key: 'dnssec' | 'ptr' | 'smtp' | 'parity'; label: string; desc: string }[] = [
  {
    key: 'dnssec',
    label: 'DNSSEC',
    desc: 'The zone is signed and its chain of trust validates from the root.',
  },
  {
    key: 'ptr',
    label: 'Reverse DNS',
    desc: 'The domain’s IPv6 addresses resolve back to a hostname (PTR).',
  },
  {
    key: 'smtp',
    label: 'SMTP over IPv6',
    desc: 'A mail server presents its SMTP banner over an IPv6 connection.',
  },
  {
    key: 'parity',
    label: 'Content parity',
    desc: 'The IPv4 and IPv6 responses return the same status and content type; non-redirect bodies are similar in length.',
  },
]

const rows = computed(() =>
  DIMENSIONS.map((d) => ({ ...d, view: infoStatus(props.informational[d.key]) })),
)

const v4 = computed(() => props.informational.latency_v4_ms)
const v6 = computed(() => props.informational.latency_v6_ms)
const hasLatency = computed(() => v4.value !== null || v6.value !== null)

// Both legs measured: the interesting number is the gap, not the pair. Ignore
// differences inside measurement noise — the engine averages 3 TTFB samples,
// so small deltas say nothing.
const latencyVerdict = computed(() => {
  const a = v4.value
  const b = v6.value
  if (a === null || b === null) return null
  const delta = a - b
  if (Math.abs(delta) < 5 || Math.abs(delta) < a * 0.1) {
    return { text: 'IPv6 is on par with IPv4', class: 'text-gray-400' }
  }
  return delta > 0
    ? { text: `IPv6 is ${delta} ms faster`, class: 'text-emerald-500' }
    : { text: `IPv6 is ${-delta} ms slower`, class: 'text-pink-500' }
})

const ms = (v: number | null) => (v === null ? '—' : `${v} ms`)

// An all-null block is noise, not information — every row would read
// "No result". Render nothing and let the status card end at its footer.
const hasAnything = computed(
  () => hasLatency.value || rows.value.some((r) => props.informational[r.key] !== null),
)
</script>

<template>
  <div v-if="hasAnything" class="mt-6 p-4 rounded bg-gray-800/50 border border-gray-700/50">
    <div class="flex items-baseline justify-between gap-3 mb-4">
      <h3 class="text-sm font-medium text-gray-300">Informational</h3>
      <span class="text-xs text-gray-500">Advisory — never affects the rating</span>
    </div>

    <dl class="grid grid-cols-1 sm:grid-cols-2 gap-x-8 gap-y-3">
      <div v-for="row in rows" :key="row.key">
        <div class="flex items-baseline justify-between gap-3">
          <dt class="text-sm text-gray-300">{{ row.label }}</dt>
          <dd class="inline-flex items-center gap-1.5 text-xs shrink-0" :class="row.view.class">
            <component :is="glyphs[row.view.icon]" aria-hidden="true" />
            {{ row.view.label }}
          </dd>
        </div>
        <p class="mt-0.5 text-xs text-gray-500">{{ row.desc }}</p>
      </div>
    </dl>

    <div
      v-if="hasLatency"
      class="mt-4 pt-3 border-t border-gray-700/50 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1"
    >
      <span class="text-sm text-gray-300">
        Response time (TTFB)
        <span class="text-xs text-gray-500 ml-1 font-mono"
          >IPv4 {{ ms(v4) }} · IPv6 {{ ms(v6) }}</span
        >
      </span>
      <span v-if="latencyVerdict" class="text-xs" :class="latencyVerdict.class">
        {{ latencyVerdict.text }}
      </span>
    </div>
  </div>
</template>
