<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import ApiError from '@/components/ApiError.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'
import CheckIcon from '@/components/icons/Check.vue'
import CrossIcon from '@/components/icons/Cross.vue'
import MinusIcon from '@/components/icons/Minus.vue'

import { createCheck, getCheck, isCheckEnvelope } from '@/api'
import type { CheckEnvelope } from '@/api'
import { ApiProblem } from '@/api/problem'
import { liveStatus } from '@/utils/status'
import { formatDateTime } from '@/utils/date'

// The §10.1 live-check flow: POST /check → 202 → poll GET /check/{id} every
// 2 s to a terminal done|failed; a 200 on the POST is a dedupe envelope.
const POLL_MS = 2_000
const POLL_LIMIT = 60 // ~2 min; the engine's whole-scan budget is 90 s

const route = useRoute()
const router = useRouter()

const host = ref('')
const envelope = ref<CheckEnvelope | null>(null)
const problem = ref<ApiProblem | null>(null)
const running = ref(false)
const retryLeft = ref(0)

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

const copied = ref(false)
async function copyLink() {
  if (envelope.value?.id == null) return
  await navigator.clipboard.writeText(`${location.origin}/check/${envelope.value.id}`)
  copied.value = true
  setTimeout(() => (copied.value = false), 2_000)
}

// One controller + token per submission: a new submit (or unmount) cancels
// the in-flight fetch and orphans the old poll loop.
let controller: AbortController | null = null
let pollTimer: ReturnType<typeof setTimeout> | null = null
let retryTimer: ReturnType<typeof setInterval> | null = null
let pollToken = 0

function stopPolling() {
  pollToken++
  if (pollTimer !== null) clearTimeout(pollTimer)
  pollTimer = null
  controller?.abort()
  controller = null
}

onBeforeUnmount(() => {
  stopPolling()
  if (retryTimer !== null) clearInterval(retryTimer)
})

function startRetryCountdown(seconds: number) {
  retryLeft.value = seconds
  if (retryTimer !== null) clearInterval(retryTimer)
  retryTimer = setInterval(() => {
    retryLeft.value--
    if (retryLeft.value <= 0 && retryTimer !== null) {
      clearInterval(retryTimer)
      retryTimer = null
    }
  }, 1_000)
}

function fail(e: unknown) {
  const p = ApiProblem.from(e)
  problem.value = p
  running.value = false
  if (p.code === 'rate-limited') startRetryCountdown(p.retryAfter ?? 60)
}

function poll(id: number, attempt: number) {
  const token = pollToken
  pollTimer = setTimeout(async () => {
    if (token !== pollToken) return
    try {
      const env = await getCheck(id, controller?.signal)
      if (token !== pollToken) return
      envelope.value = env
      if (env.status === 'done' || env.status === 'failed') {
        running.value = false
        return
      }
      if (attempt >= POLL_LIMIT) {
        running.value = false
        problem.value = new ApiProblem(
          { title: 'Check timed out', detail: 'The scan is taking too long — try again later.' },
          0,
        )
        return
      }
      poll(id, attempt + 1)
    } catch (e) {
      if (token === pollToken) fail(e)
    }
  }, POLL_MS)
}

// The shareable-link contract: once a job id exists, it IS the URL
// (/check/{id}) — the address bar always links to this exact result.
// Domain-side dedupe envelopes have id: null and stay unlinkable.
// activeID marks the job this instance is already handling, so the id
// watcher ignores our own router.replace and only reacts to real
// navigation (shared links, back/forward).
let activeID: number | null = null

function reflectID(id: number | null | undefined) {
  if (id == null) return
  activeID = id
  if (route.params.id !== String(id)) {
    void router.replace(`/check/${id}`)
  }
}

async function submit() {
  const target = host.value.trim()
  if (!target || running.value || retryLeft.value > 0) return
  stopPolling()
  controller = new AbortController()
  envelope.value = null
  problem.value = null
  running.value = true
  try {
    const res = await createCheck(target, controller.signal)
    reflectID(res.id)
    if (isCheckEnvelope(res)) {
      // Dedupe hit: a cached done envelope, no job to poll.
      envelope.value = res
      running.value = false
      return
    }
    poll(res.id, 1)
  } catch (e) {
    fail(e)
  }
}

// A shared /check/{id} link: load the job immediately — render a terminal
// result as-is, resume the 2 s poll on an in-flight one, and translate the
// 404 of a reaped job (30 d retention) into a friendly nudge.
async function loadShared(id: number) {
  stopPolling()
  activeID = id
  controller = new AbortController()
  envelope.value = null
  problem.value = null
  running.value = true
  try {
    const env = await getCheck(id, controller.signal)
    envelope.value = env
    host.value = env.host
    if (env.status === 'done' || env.status === 'failed') {
      running.value = false
      return
    }
    poll(id, 1)
  } catch (e) {
    const p = ApiProblem.from(e)
    if (p.code === 'not-found') {
      running.value = false
      problem.value = new ApiProblem(
        {
          title: 'Check not found',
          detail:
            'This check link has expired (results are kept for 30 days) or never existed — run a fresh check above.',
        },
        404,
      )
      return
    }
    fail(e)
  }
}

function sharedID(): number | null {
  const raw = route.params.id
  return typeof raw === 'string' && raw !== '' ? Number(raw) : null
}

onMounted(() => {
  const id = sharedID()
  if (id !== null) void loadShared(id)
})

// Back/forward between two result links re-loads the target job; navigating
// to the bare /check (nav link) keeps whatever is on screen.
watch(
  () => route.params.id,
  () => {
    const id = sharedID()
    if (id !== null && id !== activeID) void loadShared(id)
  },
)
</script>

<template>
  <PageShell>
    <section class="relative">
      <div class="max-w-3xl mx-auto px-4 sm:px-6">
        <div class="pt-20 pb-12 md:pt-24 md:pb-16">
          <div class="text-center mb-8">
            <h1 class="h2 mb-4">Live IPv6 Check</h1>
            <p class="text-lg text-gray-400">
              Runs a real scan from our crawler right now — DNS, mail, and an actual connection over
              IPv6. Results are live observations, separate from the tracked, confirmed status.
            </p>
          </div>

          <!-- Input -->
          <form @submit.prevent="submit">
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
            Rate limit reached — you can run another check in {{ retryLeft }}s.
          </div>
          <!-- Other errors -->
          <ApiError v-else-if="problem" class="mt-6" :problem="problem" />

          <!-- In flight -->
          <div v-if="running" class="mt-8 text-center">
            <LoadingSpinner />
            <p class="mt-3 text-sm text-gray-400">
              {{
                envelope?.status === 'processing'
                  ? 'Scanning — a full check takes 15–30 seconds…'
                  : 'Queued — waiting for a crawler slot…'
              }}
            </p>
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
                  v-if="envelope.id != null"
                  type="button"
                  class="text-xs text-gray-400 hover:text-fuchsia-400 underline underline-offset-2 cursor-pointer"
                  @click="copyLink"
                >
                  {{ copied ? 'Copied!' : 'Copy link' }}
                </button>
                <span
                  class="text-xs uppercase tracking-wide text-gray-400 border border-gray-700 rounded px-2 py-0.5"
                  >Live observation</span
                >
              </span>
            </div>

            <p v-if="envelope.cached" class="mb-4 text-sm text-gray-400">
              This domain was checked recently — showing the cached result.
            </p>

            <!-- Core dimensions -->
            <ul class="space-y-2">
              <li
                v-for="[key, label] in CORE_CHECKS"
                :key="key"
                class="flex items-center justify-between p-3 rounded bg-gray-800 text-sm"
              >
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
                <span v-if="envelope.confirmed.saint" class="text-emerald-500"> · saint</span>
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
