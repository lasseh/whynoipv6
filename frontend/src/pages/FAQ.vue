<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import PageShell from '@/components/PageShell.vue'

const route = useRoute()
const router = useRouter()
const validPages = ['1', '2', '3', '4']

function pageFromQuery(value: unknown): string {
  const v = String(value ?? '')
  return validPages.includes(v) ? v : '1'
}

const page = ref<string>(pageFromQuery(route.query.page))

const applyFilterAndUpdateRoute = (filterType: string) => {
  page.value = filterType
  void router.push({ query: { page: filterType } })
}

// Watch for changes in route query
watch(
  () => route.query.page,
  (newPage) => {
    page.value = pageFromQuery(newPage)
  },
)
</script>

<template>
  <PageShell>
    <div class="relative max-w-6xl mx-auto px-4 sm:px-6">
      <div class="pt-24 pb-12 md:pt-32 md:pb-20">
        <div class="flex flex-col md:flex-row">
          <!-- Main content -->
          <main class="md:flex-auto md:pl-10 order-1">
            <div v-show="page === '1'">
              <div class="mb-8">
                <h1 class="h2 mb-4">Frequently Asked Questions</h1>
              </div>
              <ul class="-my-4">
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">What is Why No IPv6?</h4>
                  <p class="text-base text-gray-400">
                    Why No IPv6 crawls the active Tranco list on its normal daily schedule, plus
                    user-submitted campaigns, and checks each entry for IPv6: the domain, www,
                    nameserver hosts, mail hosts, and real web reachability. Failures can back off.
                    Then we sort the results into Sinners, Heroes, and Saints and publish the
                    receipts.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Why does IPv6 matter?</h4>
                  <p class="text-base text-gray-400 mb-2">
                    IPv4 ran out. Not 'is running out': ran out. The regional registries held the
                    funeral years ago. Networks still need addresses. IPv6 has them. For a
                    top-ranked site today, skipping it isn't an oversight.
                  </p>
                  <p class="text-base text-gray-400">
                    We check the top million and publish which domains have IPv6 and which don't.
                    The second list is meant to be uncomfortable.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">How does the site work?</h4>
                  <p class="text-base text-gray-400">
                    The crawler works through the active Tranco list, checking AAAA records for the
                    domain, www, nameserver hosts, and mail hosts, plus a real HTTP connection over
                    IPv6. The results feed everything here: the tiers, country stats, and changelog.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Tranco?</h4>
                  <p class="text-base text-gray-400 mb-2">
                    The
                    <a href="https://tranco-list.eu/" target="_blank" class="a-gradient"
                      >Tranco List</a
                    >
                    ranks the top million domains by aggregating several traffic lists, which
                    smooths out the noise and manipulation that made single-source rankings like
                    Alexa easy to game. It's a ranking researchers actually use.
                  </p>
                  <p class="text-base text-gray-400">
                    We use it because the rank is half the shame: 'top 100 site, zero AAAA records'
                    only lands if the ranking is credible.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">How accurate is the data?</h4>
                  <p class="text-base text-gray-400">
                    Treat the data as indicative, not absolute. DNS propagation and CDNs that answer
                    differently per anycast location can shift a result from one scan to the next.
                    First definitive observations publish immediately; later changes need two
                    agreeing scans for DNS and mail, or three for reachability and page resources.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">
                    Why does a domain show as not supporting IPv6 when it does?
                  </h4>
                  <p class="text-base text-gray-400 mb-2">
                    Usually DNS propagation lag, a CDN answering differently from our vantage point,
                    or a server that didn't respond during that scan. A real change has to survive
                    the confirmation window, so give it a few days. If it still looks wrong, contact
                    us.
                  </p>
                  <p class="text-base text-gray-400">
                    The crawler reports DNS and reachability separately. An AAAA record can be
                    supported while the site still fails its IPv6 connection check.
                  </p>
                </li>
              </ul>
            </div>

            <!-- Rules and API -->
            <div v-show="page === '2'">
              <div class="mb-8">
                <h1 class="h2 mb-4">Rules, Frequency, and API Access</h1>
              </div>
              <ul class="-my-4">
                <li class="pt-4">
                  <h3 class="text-xl font-medium underline">Crawler</h3>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Crawler Rules</h4>
                  <p class="text-base text-gray-400 mb-2">
                    The crawler checks AAAA records for domain.com, www.domain.com, and the hosts
                    named by the domain's NS and MX records. It also opens a real HTTP connection
                    over IPv6; an AAAA record that doesn't answer won't pass the reachability check.
                  </p>
                  <p class="text-base text-gray-400">
                    The domain and www lookups go through three independent public resolvers, and
                    two out of three must agree.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Crawler Frequency</h4>
                  <p class="text-base text-gray-400">
                    Active domains normally run once per day. Base or www errors and disagreements
                    can trigger faster retries or longer backoff. First definitive observations
                    publish immediately. Later changes need two agreeing scans for DNS and mail, or
                    three for reachability and page resources, so one flaky answer won't flip the
                    verdict.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">What does “Not applicable” mean?</h4>
                  <p class="text-base text-gray-400 mb-2">
                    There was nothing to grade — it never counts against a domain.
                  </p>
                  <p class="text-base text-gray-400 mb-2">
                    For Mail (MX), it means there is no mail host to grade: a null MX, a subdomain
                    without explicit MX records, or no usable implicit MX fallback. The domain can
                    still become a Hero.
                  </p>
                  <p class="text-base text-gray-400">
                    For Page resources it means one of two things: no live external resource host
                    remained to grade, or the site isn't reachable over IPv6 at all — then its
                    resources can't be evaluated. The domain status card and the live check both
                    spell out which one applies.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Why does a domain list subdomains?</h4>
                  <p class="text-base text-gray-400 mb-2">
                    An apex can score green while the part people actually use, the login portal or
                    the API, is still IPv4 only. Anyone can list those hosts for a domain, and the
                    crawler then checks them exactly like any other domain.
                  </p>
                  <p class="text-base text-gray-400">
                    Subdomain results are informational: they never change the parent domain's
                    rating or any of the country and campaign numbers. What gets listed depends on
                    who took the time to list it, and a domain should not score worse for having
                    attentive users. Add one by opening a PR on the
                    <a
                      href="https://github.com/lasseh/whynoipv6-campaign"
                      target="_blank"
                      class="a-gradient"
                      >campaign repo</a
                    >.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Crawler Errors</h4>
                  <p class="text-base text-gray-400">
                    Found a bug in the crawler? PRs are welcome.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Heroes</h4>
                  <p class="text-base text-gray-400">
                    Hero status requires an AAAA record on the tracked hostname, IPv6 on at least
                    one nameserver host, and a site that answers over IPv6. The www and mail-host
                    checks must pass too when they apply.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Saints</h4>
                  <p class="text-base text-gray-400">
                    Saints are Heroes whose page-resource grade is Supported or Not applicable. The
                    crawler grades up to 50 external hosts found in the page; it does not fetch
                    every resource over IPv6.
                  </p>
                </li>
                <li class="pt-4">
                  <h3 class="text-xl font-medium underline">Campaign Crawler</h3>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">How do I create my own campaign?</h4>
                  <p class="text-base text-gray-400">
                    Open an issue on the
                    <a
                      href="https://github.com/lasseh/whynoipv6-campaign"
                      target="_blank"
                      class="a-gradient"
                      >GitHub repo</a
                    >.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">
                    How can I get my domain removed from the list?
                  </h4>
                  <p class="text-base text-gray-400">Yes, you can start using IPv6!</p>
                </li>
                <li class="pt-3">
                  <h3 class="text-xl font-medium underline">API</h3>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Can I get access to the API?</h4>
                  <p class="text-base text-gray-400 mb-2">
                    Yes, the API is open — no key, no signup. Everything on this site is served from
                    it, at
                    <span class="a-gradient">https://api.whynoipv6.com</span> (no version prefix).
                  </p>
                  <p class="text-base text-gray-400 mb-2">
                    Start with the
                    <a href="https://api.whynoipv6.com/docs" target="_blank" class="a-gradient"
                      >interactive docs</a
                    >, the raw
                    <a
                      href="https://api.whynoipv6.com/openapi.json"
                      target="_blank"
                      class="a-gradient"
                      >OpenAPI spec</a
                    >, or — if you're pointing an LLM agent at the data —
                    <a href="https://api.whynoipv6.com/llms.txt" target="_blank" class="a-gradient"
                      >llms.txt</a
                    >.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Can I download the whole dataset?</h4>
                  <p class="text-base text-gray-400 mb-2">
                    Yes — daily snapshots (CSV and Parquet) are published at
                    <a href="https://api.whynoipv6.com/datasets" target="_blank" class="a-gradient"
                      >api.whynoipv6.com/datasets</a
                    >. Please don't paginate the whole API when a bulk file exists.
                  </p>
                  <p class="text-base text-gray-400">
                    The data is licensed CC-BY-NC-4.0. Attribution: Data: whynoipv6.com
                    (CC-BY-NC-4.0). Ranks: Tranco.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Is there a badge for my README?</h4>
                  <p class="text-base text-gray-400 mb-2">
                    Every domain has an SVG status badge. Embed it in markdown:
                  </p>
                  <p class="text-base text-gray-400 font-mono">
                    ![IPv6](https://api.whynoipv6.com/badge/yourdomain.com.svg)
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Can I follow changes as a feed?</h4>
                  <p class="text-base text-gray-400">
                    The changelog is available as
                    <a
                      href="https://api.whynoipv6.com/changelog.atom"
                      target="_blank"
                      class="a-gradient"
                      >Atom</a
                    >
                    and
                    <a
                      href="https://api.whynoipv6.com/changelog.feed.json"
                      target="_blank"
                      class="a-gradient"
                      >JSON Feed</a
                    >, with per-domain, per-country, and per-campaign variants — see the docs.
                  </p>
                </li>
              </ul>
            </div>

            <!-- Resources -->
            <div v-show="page === '3'">
              <div class="mb-8">
                <h1 class="h2 mb-4">Resources</h1>
              </div>
              <ul class="-my-4">
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">IPv6</h4>
                  <p class="text-base text-gray-400">
                    <a
                      href="https://www.internetsociety.org/deploy360/ipv6/"
                      target="_blank"
                      class="a-gradient"
                      >Internet Society IPv6</a
                    >
                  </p>
                  <p class="text-base text-gray-400">
                    <a href="https://ready.chair6.net/" target="_blank" class="a-gradient"
                      >IPv6 Ready test</a
                    >
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">IPv6 Networking Best Practices</h4>
                  <p class="text-base text-gray-400">
                    <a
                      href="https://blog.apnic.net/2023/04/04/ipv6-architecture-and-subnetting-guide-for-network-engineers-and-operators/"
                      class="a-gradient"
                      >IPv6 Subnetting - Best Practices</a
                    >
                  </p>
                  <p class="text-base text-gray-400">
                    <a
                      href="https://www.internetsociety.org/deploy360/ipv6/security/"
                      class="a-gradient"
                      >IPv6 Security Considerations</a
                    >
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Community and Forums</h4>
                  <p class="text-base text-gray-400">
                    <a href="https://www.reddit.com/r/ipv6/" class="a-gradient">r/ipv6</a>
                  </p>
                  <p class="text-base text-gray-400">
                    <a href="https://www.ipv6forum.com/" class="a-gradient">IPv6 Forum</a>
                  </p>
                  <p class="text-base text-gray-400">
                    <a href="https://packetpushers.net/podcasts/ipv6-buzz/" class="a-gradient"
                      >IPv6 Buzz Podcast</a
                    >
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Courses and Certifications</h4>
                  <p class="text-base text-gray-400">
                    <a href="https://ipv6.he.net/certification/" target="_blank" class="a-gradient"
                      >Hurricane Electric IPv6 Certification Project</a
                    >
                  </p>
                  <p class="text-base text-gray-400">
                    <a
                      href="https://www.coursera.org/projects/ip-address-v6"
                      target="_blank"
                      class="a-gradient"
                      >Getting Started with IPv6 (Coursera)</a
                    >
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Reports and IPv6 Status</h4>
                  <p class="text-base text-gray-400">
                    <a
                      href="https://bgp.he.net/ipv6-progress-report.cgi"
                      target="_blank"
                      class="a-gradient"
                      >Global IPv6 Deployment Progress Report</a
                    >
                  </p>
                  <p class="text-base text-gray-400">
                    <a href="https://www.worldipv6launch.org/" target="_blank" class="a-gradient"
                      >World IPv6 Launch</a
                    >
                  </p>
                  <p class="text-base text-gray-400">
                    <a
                      href="https://www.google.com/intl/en/ipv6/statistics.html"
                      target="_blank"
                      class="a-gradient"
                      >Google IPv6 Statistics</a
                    >
                  </p>
                  <p class="text-base text-gray-400">
                    <a href="https://www.vyncke.org/ipv6status/" target="_blank" class="a-gradient"
                      >IPv6 Deployment Aggregated Status</a
                    >
                  </p>
                  <p class="text-base text-gray-400">
                    <a href="https://awsipv6.neveragain.de/" target="_blank" class="a-gradient"
                      >AWS service endpoints by region and IPv6 support</a
                    >
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Stickers</h4>
                  <p class="text-base text-gray-400">
                    Fly the colors. Nothing says 'ask me about AAAA records' like a protocol
                    sticker.
                  </p>
                  <p class="text-base text-gray-400 mb-2">
                    Put one on your laptop, your rack, or a Sinner's front door. Get permission for
                    that last one.
                  </p>
                  <p class="text-base text-gray-400">
                    Order yours:
                    <br />
                    <a
                      href="https://www.stickermule.com/u/89ea0892a27fc29/item/14732767"
                      class="a-gradient"
                      >Small (2.4" x 3")</a
                    >
                    <br />
                    <a
                      href="https://www.stickermule.com/u/89ea0892a27fc29/item/14732768"
                      class="a-gradient"
                      >Medium (3.2" x 4")</a
                    >
                    <br />
                  </p>
                  <div class="max-w-3xl mx-auto text-center">
                    <div class="relative inline-flex flex-col mb-6">
                      <img
                        class=""
                        src="/images/WhyNoSticker.webp"
                        :width="380"
                        :height="472"
                        alt="Why No IPv6 sticker"
                      />
                    </div>
                  </div>
                </li>
              </ul>
            </div>

            <!-- About -->
            <div v-show="page === '4'">
              <div class="mb-8">
                <h1 class="h2 mb-4">About</h1>
              </div>
              <ul class="-my-4">
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2"># whoami</h4>
                  <p class="text-base text-gray-400 mb-2">
                    I'm Lasse, a network engineer from Norway. I build and run networks. I also run
                    a crawler that keeps finding billion-dollar companies without an AAAA record.
                  </p>
                  <p class="text-base text-gray-400 mb-2">
                    None of it is personal. A domain leaves the Sinners list once a globally
                    routable apex AAAA record is confirmed. Becoming a Hero takes working IPv6
                    across the rest of the required checks too.
                  </p>
                  <p class="text-base text-gray-400">
                    The endgame is an empty Sinners list. IPv6 turns 30 in 2028. I'd like to be done
                    before it turns 40.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Contact</h4>
                  <p class="text-base text-gray-400">
                    Twitter / X:
                    <a href="https://twitter.com/WhyNoIPv6" target="_blank" class="a-gradient"
                      >@whynoipv6</a
                    >
                  </p>
                  <p class="text-base text-gray-400">
                    Email:
                    <span class="a-gradient">whynoipv6@protonmail.com</span>
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Status page</h4>
                  <p class="text-base text-gray-400 mb-2">
                    Uptime and incident history:
                    <a href="https://status.whynoipv6.com/" target="_blank" class="a-gradient"
                      >status.whynoipv6.com</a
                    >
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Our Supporters</h4>
                  <p class="text-base text-gray-400 mb-4">
                    These organizations supported the site early on, back when it was one crawler
                    and a grudge.
                  </p>
                  <!-- Sponsors -->
                  <div
                    class="max-w-xl md:max-w-none md:w-full mx-auto md:col-span-5 lg:col-span-6 mb-8 md:mb-0 md:order-1"
                  >
                    <div class="items-center">
                      <a href="https://blix.com/" target="_blank"
                        ><img
                          class="md:block md:max-w-none"
                          src="/images/Blix.webp"
                          :width="350"
                          :height="66"
                          alt="Blix"
                      /></a>
                    </div>
                  </div>
                </li>
              </ul>
            </div>
          </main>

          <!-- Nav sidebar -->
          <aside class="md:w-64 mb-16 md:mb-0 md:mr-10 md:shrink-0">
            <nav>
              <ul>
                <li class="py-2 border-b border-gray-800">
                  <a
                    :class="page === '1' ? 'text-fuchsia-600' : 'text-gray-400'"
                    class="flex items-center px-3 group hover:text-fuchsia-600 transition duration-150 ease-in-out"
                    href="#0"
                    @click.prevent="applyFilterAndUpdateRoute('1')"
                  >
                    <span>Frequently Asked Questions</span>
                    <svg
                      class="w-3 h-3 fill-current shrink-0 ml-2 opacity-0 group-hover:opacity-100 group-hover:text-fuchsia-600 group-hover:translate-x-1 transition duration-150 ease-in-out transform"
                      viewBox="0 0 12 12"
                      xmlns="http://www.w3.org/2000/svg"
                    >
                      <path
                        d="M11.707 5.293L7 .586 5.586 2l3 3H0v2h8.586l-3 3L7 11.414l4.707-4.707a1 1 0 000-1.414z"
                      />
                    </svg>
                  </a>
                </li>
                <li class="py-2 border-b border-gray-800">
                  <a
                    :class="page === '2' ? 'text-fuchsia-600' : 'text-gray-400'"
                    class="flex items-center px-3 group hover:text-fuchsia-600 transition duration-150 ease-in-out"
                    href="#0"
                    @click.prevent="applyFilterAndUpdateRoute('2')"
                  >
                    <span>Rules and API</span>
                    <svg
                      class="w-3 h-3 fill-current shrink-0 ml-2 opacity-0 group-hover:opacity-100 group-hover:text-fuchsia-600 group-hover:translate-x-1 transition duration-150 ease-in-out transform"
                      viewBox="0 0 12 12"
                      xmlns="http://www.w3.org/2000/svg"
                    >
                      <path
                        d="M11.707 5.293L7 .586 5.586 2l3 3H0v2h8.586l-3 3L7 11.414l4.707-4.707a1 1 0 000-1.414z"
                      />
                    </svg>
                  </a>
                </li>
                <li class="py-2 border-b border-gray-800">
                  <a
                    :class="page === '3' ? 'text-fuchsia-600' : 'text-gray-400'"
                    class="flex items-center px-3 group hover:text-fuchsia-600 transition duration-150 ease-in-out"
                    href="#0"
                    @click.prevent="applyFilterAndUpdateRoute('3')"
                  >
                    <span>Resources</span>
                    <svg
                      class="w-3 h-3 fill-current shrink-0 ml-2 opacity-0 group-hover:opacity-100 group-hover:text-fuchsia-600 group-hover:translate-x-1 transition duration-150 ease-in-out transform"
                      viewBox="0 0 12 12"
                      xmlns="http://www.w3.org/2000/svg"
                    >
                      <path
                        d="M11.707 5.293L7 .586 5.586 2l3 3H0v2h8.586l-3 3L7 11.414l4.707-4.707a1 1 0 000-1.414z"
                      />
                    </svg>
                  </a>
                </li>
                <li class="py-2 border-b border-gray-800">
                  <a
                    :class="page === '4' ? 'text-fuchsia-600' : 'text-gray-400'"
                    class="flex items-center px-3 group hover:text-fuchsia-600 transition duration-150 ease-in-out"
                    href="#0"
                    @click.prevent="applyFilterAndUpdateRoute('4')"
                  >
                    <span>About</span>
                    <svg
                      class="w-3 h-3 fill-current shrink-0 ml-2 opacity-0 group-hover:opacity-100 group-hover:text-fuchsia-600 group-hover:translate-x-1 transition duration-150 ease-in-out transform"
                      viewBox="0 0 12 12"
                      xmlns="http://www.w3.org/2000/svg"
                    >
                      <path
                        d="M11.707 5.293L7 .586 5.586 2l3 3H0v2h8.586l-3 3L7 11.414l4.707-4.707a1 1 0 000-1.414z"
                      />
                    </svg>
                  </a>
                </li>
              </ul>
            </nav>
          </aside>
        </div>
      </div>
    </div>
  </PageShell>
</template>
