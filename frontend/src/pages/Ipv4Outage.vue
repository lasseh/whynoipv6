<script setup lang="ts">
import PageShell from '@/components/PageShell.vue'
import { drillPhase, formatWindow, nextWindow } from '@/utils/ipv4-drill'

// The year-round explanation of the monthly drill. The 503 body an IPv4
// visitor gets during a window is public/ipv4-unavailable.html, which is a
// standalone file because nothing on this origin is reachable for that
// reader — so the two overlap on purpose, and this one carries the detail.
// nextWindow stays on the current month for the whole of the 6th, so on the
// one day this page gets real traffic "next window" would name today.
const now = new Date()
const open = drillPhase(now) === 'active'
const next = formatWindow(nextWindow(now))
</script>

<template>
  <PageShell>
    <div class="relative max-w-6xl mx-auto px-4 sm:px-6">
      <div class="pt-24 pb-12 md:pt-32 md:pb-20">
        <div class="max-w-3xl mx-auto">
          <div class="mb-8">
            <h1 class="h2 mb-4">We switch off IPv4 once a month</h1>
            <p class="text-base text-gray-400">
              On the 6th of every month, 00:00 to 24:00 UTC, this site stops answering over IPv4.
              Over IPv6 it runs exactly as it always does.
              <template v-if="open">The window is open right now.</template>
              <template v-else
                >Next window: <span class="font-mono text-gray-200">{{ next }}</span
                >.</template
              >
            </p>
          </div>

          <ul class="-my-4">
            <li class="py-4">
              <h4 class="text-xl font-medium mb-2">Why</h4>
              <p class="text-base text-gray-400 mb-2">
                We keep a public scoreboard of sites that have not turned on IPv6. Staying
                comfortably reachable over IPv4 while doing that is a weak position to argue from.
              </p>
              <p class="text-base text-gray-400">
                The wider point is not about us. Everyone who eventually goes IPv6-only has to
                remove IPv4, and removing it is nothing like adding it. Dual-stack takes nothing
                away, so it never tells you what still depends on the old protocol. A short,
                announced, reversible outage does, and it does it while someone is watching.
              </p>
            </li>

            <li class="py-4">
              <h4 class="text-xl font-medium mb-2">What an IPv4 visitor sees</h4>
              <p class="text-base text-gray-400 mb-2">
                Not a timeout. A page that says what happened and what to ask their provider,
                carried by a deliberate HTTP signal:
              </p>
              <p class="font-mono text-sm text-gray-200 bg-gray-800 rounded-sm px-3 py-2 mb-2">
                HTTP/2 503<br />Retry-Over-IPv6: ?1<br />Cache-Control: private, no-store
              </p>
              <p class="text-base text-gray-400">
                The status code alone would be ambiguous with an ordinary overload, which is why the
                header is the part that matters. A client that understands it can close the IPv4
                connection, retry over IPv6, and succeed without bothering anyone. Machines asking
                for <span class="font-mono text-gray-200">application/problem+json</span> get the
                same thing in structured form.
              </p>
            </li>

            <li class="py-4">
              <h4 class="text-xl font-medium mb-2">The signal is a proposed standard</h4>
              <p class="text-base text-gray-400 mb-2">
                This follows
                <a
                  href="https://datatracker.ietf.org/doc/draft-martin-retry-over-ipv6/"
                  class="a-gradient"
                  >draft-martin-retry-over-ipv6</a
                >, an Internet-Draft for signalling planned IPv4 unavailability, and the coordinated
                window its
                <a
                  href="https://github.com/franckhlmartin/ietf-draft-retry-over-ipv6"
                  class="a-gradient"
                  >call for volunteers</a
                >
                proposes: the 6th of the month, with a notice up at least a week beforehand.
              </p>
              <p class="text-base text-gray-400">
                Sharing a window matters more than it sounds. It lets the operators running these
                drills compare notes on what broke, which is the entire value of doing it in the
                open rather than quietly at 3am.
              </p>
            </li>

            <li class="py-4">
              <h4 class="text-xl font-medium mb-2">If it reaches you</h4>
              <p class="text-base text-gray-400 mb-2">
                Then your connection has no IPv6, and that is worth knowing. Ask your internet
                provider when they plan to offer it, or whoever runs your work network. It costs
                nothing and it is the default on modern networks.
              </p>
              <p class="text-base text-gray-400">
                In the meantime the site is back the following day, and the rest of the month is
                unaffected. Short answer: start using IPv6.
              </p>
            </li>
          </ul>
        </div>
      </div>
    </div>
  </PageShell>
</template>
