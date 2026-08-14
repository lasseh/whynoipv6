<script setup lang="ts">
import PageShell from '@/components/PageShell.vue'
</script>

<template>
  <PageShell>
    <div class="relative max-w-6xl mx-auto px-4 sm:px-6">
      <div class="pt-24 pb-12 md:pt-32 md:pb-20">
        <div class="max-w-3xl mx-auto">
          <div class="mb-8">
            <h1 class="h2 mb-4">WhyNoIPv6Bot</h1>
            <p class="text-base text-gray-400">
              You probably found this page through your access logs. This is the measurement crawler
              behind <RouterLink to="/" class="a-gradient">whynoipv6.com</RouterLink>, an IPv6
              adoption tracker. It checks the Tranco top million domains, plus reader-submitted
              campaign lists, for working IPv6 — and publishes the results.
            </p>
          </div>

          <ul class="-my-4">
            <li class="py-4">
              <h4 class="text-xl font-medium mb-2">How to recognize it</h4>
              <p class="text-base text-gray-400 mb-2">Every HTTP request it makes carries:</p>
              <p class="font-mono text-sm text-gray-200 bg-gray-800 rounded-sm px-3 py-2 mb-2">
                User-Agent: WhyNoIPv6Bot/1.0 (+https://whynoipv6.com/bot)
              </p>
              <p class="text-base text-gray-400 mb-2">
                All crawling comes from
                <span class="font-mono text-gray-200">crawler.whynoipv6.com</span>:
              </p>
              <p class="font-mono text-sm text-gray-200 bg-gray-800 rounded-sm px-3 py-2 mb-2">
                195.47.216.68<br />2a0f:b6c0:c200::68
              </p>
              <p class="text-base text-gray-400">
                Both directions of DNS verify: the addresses resolve back to
                <span class="font-mono text-gray-200">crawler.whynoipv6.com</span>, and that name
                resolves to these addresses. Anything else claiming to be WhyNoIPv6Bot isn't us.
              </p>
            </li>

            <li class="py-4">
              <h4 class="text-xl font-medium mb-2">What it does to your site</h4>
              <p class="text-base text-gray-400 mb-2">
                Each domain is checked once per day. For most domains that means DNS lookups only —
                AAAA records for the domain and www (asked through Cloudflare, Google, and Quad9
                public resolvers), plus nameserver and mail host lookups through our own resolvers.
                No IPv6 in DNS, no connection: roughly three quarters of the list never sees a
                single packet from us.
              </p>
              <p class="text-base text-gray-400 mb-2">
                Domains that do publish IPv6 additionally get a small number of connection checks: a
                handful of requests to the front page over IPv4 and IPv6 to confirm the address
                actually answers, measure response time, and compare what's served on each. The
                reachability probes discard the response body; the single page fetch reads at most
                2&nbsp;MiB. Nothing beyond the front page is requested — no link crawling, no
                content indexing.
              </p>
              <p class="text-base text-gray-400">
                Mail hosts get a connection and an <span class="font-mono text-gray-200">EHLO</span>
                over IPv6 to confirm the listed MX actually answers. No mail is ever sent.
              </p>
            </li>

            <li class="py-4">
              <h4 class="text-xl font-medium mb-2">Why it ignores robots.txt</h4>
              <p class="text-base text-gray-400">
                robots.txt governs content crawling — following links, indexing pages. This bot does
                neither: it asks DNS questions and confirms your front page answers, the same way an
                uptime monitor does. A few requests per day, only to domains that already advertise
                IPv6.
              </p>
            </li>

            <li class="py-4">
              <h4 class="text-xl font-medium mb-2">Blocking it</h4>
              <p class="text-base text-gray-400">
                You can block the addresses above or the user agent — it's your server. Be aware of
                what that measures: a blocked probe is indistinguishable from a broken one, so your
                domain's web reachability may show as failing on the site while your AAAA records
                still count. If you want your domain off the list entirely, see the
                <RouterLink to="/faq" class="a-gradient">FAQ</RouterLink> — short answer: start
                using IPv6.
              </p>
            </li>

            <li class="py-4">
              <h4 class="text-xl font-medium mb-2">Contact</h4>
              <p class="text-base text-gray-400">
                Seeing unexpected traffic, or something that doesn't match this page? Email
                <a href="mailto:whynoipv6@protonmail.com" class="a-gradient"
                  >whynoipv6@protonmail.com</a
                >
                or open an issue on
                <a href="https://github.com/lasseh/whynoipv6" target="_blank" class="a-gradient"
                  >GitHub</a
                >. The crawler is open source — what it does is exactly what the code says it does.
              </p>
            </li>
          </ul>
        </div>
      </div>
    </div>
  </PageShell>
</template>
