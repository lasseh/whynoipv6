<script setup lang="ts">
import PageShell from '@/components/PageShell.vue'

// The records the resolver would have tried before giving up. Decorative, but
// it is the joke: every lookup came back empty, not just the AAAA one.
const records = ['A', 'AAAA', 'MX', 'CNAME', 'TXT']
</script>

<template>
  <PageShell>
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-28 pb-12 md:pt-32 md:pb-20">
          <!-- Side by side only from lg: the 480px artwork leaves the 52px
               heading too little room at md, and it overflows. -->
          <div class="flex flex-col items-center gap-10 lg:flex-row lg:gap-16">
            <!-- Artwork -->
            <img
              src="/images/NxdomainNun.webp"
              width="480"
              height="480"
              alt="Woodcut nun ringing a bell in an empty cathedral"
              class="w-full max-w-[480px] shrink-0 rounded-[20px] border border-fuchsia-500/40 shadow-[0_24px_80px_-20px_rgba(162,28,175,0.4)]"
            />

            <!-- 404 content -->
            <div class="w-full">
              <div class="flex items-center gap-2.5 mb-6">
                <span class="text-[13px] font-extrabold tracking-[0.154em] text-fuchsia-400"
                  >404</span
                >
                <span class="w-10 h-px bg-fuchsia-500/40"></span>
                <span class="text-[13px] font-extrabold tracking-[0.154em] text-gray-500"
                  >NXDOMAIN</span
                >
              </div>

              <h1 class="h1 mb-6 text-gray-200">Shame on us. This page doesn't resolve.</h1>

              <p class="text-lg leading-relaxed text-gray-400 mb-8">
                We checked every record: A, AAAA, even MX. Unlike our Sinners, this URL has a valid
                excuse for being unreachable: it doesn't exist.
              </p>

              <div class="flex flex-wrap items-center gap-3">
                <router-link to="/" class="btn text-white bg-fuchsia-700 hover:bg-fuchsia-800">
                  Back to homepage
                  <svg
                    class="w-4 h-4 ml-2.5 shrink-0"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    aria-hidden="true"
                  >
                    <path d="M5 12h14" />
                    <path d="m12 5 7 7-7 7" />
                  </svg>
                </router-link>
                <router-link
                  to="/check"
                  class="btn text-gray-200 border-gray-700 hover:border-gray-500"
                >
                  Check a domain
                </router-link>
              </div>

              <div class="pt-8">
                <div class="text-[11px] font-bold tracking-[0.145em] text-gray-500 mb-2.5">
                  WE LOOKED EVERYWHERE
                </div>
                <div class="flex flex-wrap items-center gap-2">
                  <span
                    v-for="record in records"
                    :key="record"
                    class="inline-flex items-center gap-[7px] rounded-sm border border-gray-700 bg-gray-800/60 px-2.5 py-1.5 font-mono text-[13px] text-gray-400"
                  >
                    <svg
                      class="w-3 h-3 shrink-0 text-fuchsia-500"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2.5"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      aria-hidden="true"
                    >
                      <path d="M18 6 6 18" />
                      <path d="m6 6 12 12" />
                    </svg>
                    {{ record }}
                  </span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  </PageShell>
</template>
