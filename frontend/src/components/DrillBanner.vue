<script setup lang="ts">
import { ref, watchEffect } from 'vue'
import { drillBannerVisible } from '@/components/drill-banner-state'
import { drillPhase, formatWindow, nextWindow } from '@/utils/ipv4-drill'

// The advance notice draft-martin-retry-over-ipv6 asks for: at least seven
// days before the site stops answering over IPv4, say so. The schedule lives
// in @/utils/ipv4-drill, which is the same window deploy/nginx gates on.
//
// During a window an IPv4 visitor never reaches the SPA at all — nginx answers
// them with the 503 and public/ipv4-unavailable.html. So the "active" copy is
// only ever read by someone on IPv6, and needs no per-visitor branching.
const now = new Date()
const phase = drillPhase(now)
const window_ = nextWindow(now)

// Dismissal is keyed by the window, so it comes back next month rather than
// once and never again. Storage can throw outright in a private window.
const storageKey = 'wni6-ipv4-drill-dismissed'
const dismissedFor = (() => {
  try {
    return localStorage.getItem(storageKey)
  } catch {
    return null
  }
})()

const windowKey = window_.toISOString().slice(0, 10)
const show = ref(phase !== 'idle' && dismissedFor !== windowKey)

// Notification.vue reads this to keep its toast clear of the bar.
watchEffect(() => {
  drillBannerVisible.value = show.value
})

function dismiss() {
  show.value = false
  try {
    localStorage.setItem(storageKey, windowKey)
  } catch {
    // A visitor who cannot store the dismissal sees it again next page. Fine.
  }
}
</script>

<template>
  <div
    v-if="show"
    role="status"
    class="fixed inset-x-0 bottom-0 z-40 border-t border-zinc-700 bg-zinc-800/95 backdrop-blur-sm"
  >
    <div class="max-w-6xl mx-auto px-4 sm:px-6 py-3 flex items-start gap-3 text-sm text-slate-300">
      <svg
        class="w-4 h-4 shrink-0 fill-current text-amber-500 mt-[3px]"
        viewBox="0 0 16 16"
        aria-hidden="true"
      >
        <path
          d="M8 0C3.6 0 0 3.6 0 8s3.6 8 8 8 8-3.6 8-8-3.6-8-8-8zm0 12c-.6 0-1-.4-1-1s.4-1 1-1 1 .4 1 1-.4 1-1 1zm1-3H7V4h2v5z"
        />
      </svg>

      <p class="grow">
        <template v-if="phase === 'active'">
          <span class="font-medium text-slate-200">IPv4 is switched off today.</span>
          You are reading this over IPv6, so you would not have noticed.
        </template>
        <template v-else>
          <span class="font-medium text-slate-200"
            >Planned IPv4 outage on {{ formatWindow(window_) }}.</span
          >
          This site will stop answering over IPv4 for the day, 00:00 to 24:00 UTC. Over IPv6 nothing
          changes.
        </template>
        <RouterLink to="/ipv4-outage" class="a-gradient font-medium whitespace-nowrap"
          >Why we do this</RouterLink
        >
      </p>

      <button type="button" class="shrink-0 opacity-70 hover:opacity-100 p-1 -m-1" @click="dismiss">
        <span class="sr-only">Dismiss</span>
        <svg class="w-4 h-4 fill-current" aria-hidden="true">
          <path
            d="M7.95 6.536l4.242-4.243a1 1 0 111.415 1.414L9.364 7.95l4.243 4.242a1 1 0 11-1.415 1.415L7.95 9.364l-4.243 4.243a1 1 0 01-1.414-1.415L6.536 7.95 2.293 3.707a1 1 0 011.414-1.414L7.95 6.536z"
          />
        </svg>
      </button>
    </div>
  </div>
</template>
