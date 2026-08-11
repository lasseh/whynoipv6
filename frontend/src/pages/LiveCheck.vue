<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import ApiError from '@/components/ApiError.vue'
import CheckIcon from '@/components/icons/Check.vue'
import CrossIcon from '@/components/icons/Cross.vue'
import MinusIcon from '@/components/icons/Minus.vue'

import { useLiveCheck } from '@/composables/useLiveCheck'
import { setPageTitle } from '@/composables/usePageMeta'
import { liveStatus } from '@/utils/status'
import { formatDateTime } from '@/utils/date'

const route = useRoute()

// The machine — poll loop, rate-limit countdown, and the shareable-URL
// contract — lives in useLiveCheck; this page narrates and renders it.
const { host, envelope, problem, running, retryLeft, submit, cancel } = useLiveCheck()

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

const checks = computed<Record<string, { status?: string }>>(
  () => (envelope.value?.result?.checks ?? {}) as Record<string, { status?: string }>,
)
const latency = computed(
  () => (envelope.value?.result?.latency ?? null) as { v4_ms?: number; v6_ms?: number } | null,
)
const done = computed(() => envelope.value?.status === 'done')
const failed = computed(() => envelope.value?.status === 'failed')

// "Not applicable" on resources is ambiguous (mirrors DomainStatusCard):
// discovery only runs when the site loads over IPv6, so the same value means
// either "no live external host remained" or "couldn't evaluate".
const resourcesNote = computed(() => {
  if (checks.value.resources?.status !== 'not_applicable') return null
  return checks.value.conn?.status === 'supported'
    ? 'No live external resource host remained to grade.'
    : 'The site isn’t reachable over IPv6, so page resources can’t be evaluated.'
})

// The waiting state (Nielsen H1/H2): narrate the scan's real phases with an
// elapsed counter and an asymptotic progress bar instead of a static line.
// Stage timings approximate the engine's two-phase run; the sequencing is
// cosmetic but the phases are what the scan actually does.
const SCAN_STAGES: [number, string][] = [
  [0, 'Resolving DNS records — AAAA, nameservers, mail…'],
  [4, 'Cross-checking three public resolvers; two must agree…'],
  [9, 'Connecting to the site over IPv6 only…'],
  [16, 'Checking mail servers and TLS over IPv6…'],
  [24, 'Fetching the page and discovering its resources…'],
  [45, 'Still working — slow targets can take up to 90 seconds…'],
]

const elapsed = ref(0)
let elapsedTimer: ReturnType<typeof setInterval> | null = null

watch(running, (r) => {
  if (r) {
    elapsed.value = 0
    elapsedTimer = setInterval(() => elapsed.value++, 1_000)
  } else if (elapsedTimer !== null) {
    clearInterval(elapsedTimer)
    elapsedTimer = null
  }
})

const waitMessage = computed(() => {
  if (envelope.value?.status === 'pending') return 'Waiting in queue…'
  let msg = SCAN_STAGES[0]![1]
  for (const [at, m] of SCAN_STAGES) {
    if (elapsed.value >= at) msg = m
  }
  return msg
})

// Asymptotic fill toward a 95 % cap: fast early motion, never falsely done.
const progress = computed(() => Math.min(95, Math.round(100 * (1 - Math.exp(-elapsed.value / 18)))))

const copied = ref(false)
async function copyLink() {
  if (!envelope.value) return
  await navigator.clipboard.writeText(`${location.origin}/check/${envelope.value.host}`)
  copied.value = true
  setTimeout(() => (copied.value = false), 2_000)
}

// Data-driven title once a result is on screen; also watches the target so
// the canonicalizing router.replace (whose beforeEach resets the static
// title) does not clobber it.
watch([envelope, () => route.params.target], ([env]) => {
  if (env) setPageTitle(`${env.host} Live IPv6 Check`)
})
</script>

<template>
  <PageShell>
    <section class="relative">
      <div class="max-w-3xl mx-auto px-4 sm:px-6">
        <div class="pt-20 pb-12 md:pt-24 md:pb-16">
          <div class="text-center mb-8">
            <h1 class="h2 mb-4">Live IPv6 Check</h1>
            <p class="text-lg text-gray-400">
              The live check scans DNS and mail, then attempts a real IPv6 connection. This is a
              live observation; tracked, confirmed status updates on the crawler's schedule, not
              yours.
            </p>
          </div>

          <!-- Input -->
          <form @submit.prevent="submit()">
            <label for="check-host" class="mb-2 text-sm font-medium sr-only text-white"
              >Domain</label
            >
            <div class="relative">
              <input
                id="check-host"
                v-model="host"
                type="text"
                name="host"
                autocomplete="off"
                autocapitalize="none"
                spellcheck="false"
                class="block w-full p-4 text-sm border rounded-sm bg-gray-800 border-gray-700 placeholder-gray-400 text-white font-mono focus:ring-fuchsia-900 focus:border-fuchsia-900"
                placeholder="example.com"
                required
                :disabled="running"
              />
              <button
                type="submit"
                class="text-white absolute right-2.5 bottom-2.5 focus:ring-3 focus:outline-none font-medium rounded-sm text-sm px-4 py-2 bg-fuchsia-700 hover:bg-fuchsia-900 focus:ring-fuchsia-800 disabled:opacity-50 disabled:cursor-not-allowed"
                :disabled="running || retryLeft > 0"
              >
                {{ retryLeft > 0 ? `Wait ${retryLeft}s` : 'Check' }}
              </button>
            </div>
          </form>

          <!-- Rate limited -->
          <div
            v-if="problem?.code === 'rate-limited'"
            class="mt-6 p-4 rounded-sm bg-gray-800 border border-amber-600/50 text-amber-500 text-sm"
          >
            Rate limit reached. Next check in {{ retryLeft }}s.
          </div>
          <!-- Other errors -->
          <ApiError v-else-if="problem" class="mt-6" :problem="problem" />

          <!-- In flight -->
          <div v-if="running" class="mt-8">
            <div class="flex items-center justify-between mb-2 text-sm">
              <span class="font-mono text-gray-200">{{ host.trim() }}</span>
              <span class="text-gray-500 tabular-nums">{{ elapsed }}s</span>
            </div>
            <div class="w-full h-2 rounded-md bg-gray-800 overflow-hidden">
              <div
                class="h-2 rounded-md bg-gradient-to-r from-fuchsia-700 to-fuchsia-500 transition-all duration-1000 ease-linear"
                :style="{ width: `${progress}%` }"
                role="progressbar"
                :aria-valuenow="progress"
                aria-valuemin="0"
                aria-valuemax="100"
              ></div>
            </div>
            <div class="mt-3 flex items-center justify-between gap-4">
              <p class="text-sm text-gray-400" aria-live="polite">{{ waitMessage }}</p>
              <button
                type="button"
                class="text-xs text-gray-400 hover:text-pink-500 underline underline-offset-2 cursor-pointer shrink-0"
                @click="cancel"
              >
                Cancel
              </button>
            </div>
          </div>

          <!-- Failed -->
          <div
            v-else-if="failed"
            class="mt-8 p-4 rounded-sm bg-gray-800 border border-pink-600/50 text-sm"
          >
            <span class="text-pink-500 font-medium">Check failed.</span>
            <span class="text-gray-400 ml-1">{{
              envelope?.error ?? 'The scan could not complete — try again later.'
            }}</span>
          </div>

          <!-- Result -->
          <div v-else-if="done && envelope" class="mt-8">
            <div class="flex items-center justify-between mb-3">
              <h2 class="text-xl font-bold text-pink-600 font-mono">{{ envelope.host }}</h2>
              <span class="inline-flex items-center gap-2">
                <button
                  type="button"
                  class="text-xs text-gray-400 hover:text-fuchsia-400 underline underline-offset-2 cursor-pointer"
                  @click="copyLink"
                >
                  {{ copied ? 'Copied' : 'Copy link' }}
                </button>
                <span
                  class="text-xs uppercase tracking-wide text-gray-400 border border-gray-700 rounded px-2 py-0.5"
                  >Live observation</span
                >
              </span>
            </div>

            <p v-if="envelope.cached" class="mb-4 text-sm text-gray-400">
              Showing a stored result. A fresh check runs automatically once it's older than 7 days.
            </p>

            <!-- Core dimensions -->
            <ul class="space-y-2">
              <li
                v-for="[key, label] in CORE_CHECKS"
                :key="key"
                class="p-3 rounded bg-gray-800 text-sm"
              >
                <div class="flex items-center justify-between">
                  <span class="text-gray-200">{{ label }}</span>
                  <span
                    class="inline-flex items-center gap-2"
                    :class="liveStatus(checks[key]?.status).class"
                  >
                    <component
                      :is="glyphs[liveStatus(checks[key]?.status).icon]"
                      aria-hidden="true"
                    />
                    {{ liveStatus(checks[key]?.status).label }}
                  </span>
                </div>
                <p v-if="key === 'resources' && resourcesNote" class="mt-1 text-xs text-gray-500">
                  {{ resourcesNote }}
                </p>
              </li>
            </ul>

            <!-- Informational checks -->
            <div class="mt-4 p-3 rounded bg-gray-800/50 border border-gray-700/50">
              <div class="text-xs font-medium text-gray-400 mb-2">Informational</div>
              <ul class="grid grid-cols-2 sm:grid-cols-3 gap-2">
                <li
                  v-for="[key, label] in INFO_CHECKS"
                  :key="key"
                  class="flex items-center justify-between text-xs px-2 py-1"
                >
                  <span class="text-gray-400">{{ label }}</span>
                  <span
                    class="inline-flex items-center gap-1"
                    :class="liveStatus(checks[key]?.status).class"
                  >
                    <component
                      :is="glyphs[liveStatus(checks[key]?.status).icon]"
                      aria-hidden="true"
                    />
                    {{ liveStatus(checks[key]?.status).label }}
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
                <span class="text-gray-200 capitalize">{{
                  envelope.confirmed.classification
                }}</span>
                <span v-if="envelope.confirmed.saint" class="text-emerald-500"> · Saint</span>
              </span>
              <RouterLink
                :to="`/domains/${envelope.host}`"
                class="text-fuchsia-500 hover:text-fuchsia-400 underline underline-offset-2"
              >
                Full history →
              </RouterLink>
            </div>

            <div class="mt-3 text-xs text-gray-500">
              Checked
              {{
                envelope.result?.checked_at
                  ? formatDateTime(envelope.result.checked_at)
                  : 'just now'
              }}
              <template v-if="envelope.result?.duration_ms">
                · scan took {{ ((envelope.result.duration_ms as number) / 1000).toFixed(1) }}s
              </template>
            </div>
          </div>
        </div>
      </div>
    </section>
  </PageShell>
</template>
