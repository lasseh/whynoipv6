<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import ApiError from '@/components/ApiError.vue'
import LiveCheckProgress from '@/components/livecheck/LiveCheckProgress.vue'
import LiveCheckResult from '@/components/livecheck/LiveCheckResult.vue'

import type { CheckEnvelope } from '@/api'
import { useLiveCheck } from '@/composables/useLiveCheck'
import { setPageTitle } from '@/composables/usePageMeta'

const route = useRoute()

// The machine — poll loop, rate-limit countdown, and the shareable-URL
// contract — lives in useLiveCheck; this page holds the form and picks
// which of the narrator/failed/result surfaces to show.
const { host, envelope, problem, running, retryLeft, submit, cancel } = useLiveCheck()

const done = computed(() => envelope.value?.status === 'done')
const failed = computed(() => envelope.value?.status === 'failed')

const exampleEnvelope: CheckEnvelope = {
  id: null,
  host: 'example.com',
  status: 'done',
  cached: false,
  created_at: '',
  completed_at: null,
  error: null,
  result: {
    checks: {
      base: { status: 'supported' },
      www: { status: 'supported' },
      ns: { status: 'supported' },
      mx: { status: 'unsupported' },
      conn: { status: 'supported' },
      resources: { status: 'partial' },
      tls: { status: 'supported' },
      smtp: { status: 'unsupported' },
      parity: { status: 'supported' },
      dnssec: { status: 'supported' },
      ptr: { status: 'not_applicable' },
      spf: { status: 'supported' },
    },
    latency: { v4_ms: 31, v6_ms: 27 },
  },
  confirmed: null,
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
              Run a live IPv6 check against any domain. We inspect the website, DNS, mail servers,
              and page resources, then show exactly where IPv6 works and where it gives up.
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
          <LiveCheckProgress
            v-if="running"
            :host="host.trim()"
            :pending="envelope?.status === 'pending'"
            @cancel="cancel"
          />

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
          <LiveCheckResult v-else-if="done && envelope" :envelope="envelope" />

          <!-- Empty state -->
          <template v-else-if="!problem">
            <div class="flex items-center gap-4 mt-10" aria-hidden="true">
              <hr class="grow border-gray-700" />
              <span class="text-xs uppercase tracking-wide text-gray-500">Example</span>
              <hr class="grow border-gray-700" />
            </div>
            <LiveCheckResult :envelope="exampleEnvelope" example />
          </template>
        </div>
      </div>
    </section>
  </PageShell>
</template>
