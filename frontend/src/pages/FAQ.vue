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
                <h2 class="h2 mb-4">Frequently Asked Questions</h2>
              </div>
              <ul class="-my-4">
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">What is WhyNoIPv6.com?</h4>
                  <p class="text-md text-gray-400">
                    WhyNoIPv6.com is a specialized platform committed to monitoring and promoting
                    the adoption of IPv6 among the 1 Million top-ranked websites and user-submitted
                    campaigns. We offer insightful metrics to help you assess the current landscape
                    of IPv6 implementation.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Why is IPv6 Important?</h4>
                  <p class="text-md text-gray-400 mb-2">
                    IPv6 is not merely an upgrade; it's a fundamental pillar for the Internet's
                    sustainable future. As we edge closer to exhausting the IPv4 address space, the
                    immense address capacity of IPv6 becomes indispensable. Beyond the scalability,
                    IPv6 brings along robust security protocols and superior performance, making it
                    the linchpin for modern, efficient, and secure internet communications.
                  </p>
                  <p class="text-md text-gray-400">
                    Failing to adopt IPv6 is tantamount to inhibiting the Internet's evolution. For
                    top websites, this isn't just negligence—it's an abdication of their role as
                    industry leaders. That's why our mission at WhyNoIPv6.com is not just to
                    monitor, but to actively push for the closing of these alarming gaps in IPv6
                    adoption.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">How does WhyNoIPv6.com work?</h4>
                  <p class="text-md text-gray-400">
                    At WhyNoIPv6.com, we meticulously scan each domain from Tranco's top-ranked list
                    every day to evaluate critical IPv6 adoption metrics. Specifically, we check for
                    the existence of IPv6 DNS records and MX records. The data gleaned from these
                    scans is then aggregated, analyzed, and made publicly available, providing a
                    comprehensive and up-to-date snapshot of IPv6 implementation across influential
                    websites.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Tranco?</h4>
                  <p class="text-md text-gray-400 mb-2">
                    The
                    <a href="https://tranco-list.eu/" target="_blank" class="a-gradient"
                      >Tranco List</a
                    >
                    offers an alternative way to gauge a website's standing on the internet,
                    diverging from traditional metrics such as those provided by Alexa rankings.
                    Unlike Alexa, which ranks websites based on a combination of average daily
                    visitors and pageviews over a three-month period, the Tranco List employs a
                    robust methodology that aggregates data from various sources to compile its
                    rankings.
                  </p>
                  <p class="text-md text-gray-400">
                    This approach aims to provide a more comprehensive and reliable measure of a
                    website's popularity and traffic, addressing some of the accuracy concerns
                    associated with Alexa's data. As a result, the Tranco List is increasingly
                    recognized as a valuable tool for understanding website prominence in a way that
                    accounts for a broader spectrum of internet activity.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">How accurate is our data?</h4>
                  <p class="text-md text-gray-400">
                    While we make every effort to ensure the precision of our metrics, it's
                    important to interpret them as indicative rather than absolute. Several
                    variables can introduce fluctuations in real-time accuracy. For instance, DNS
                    propagation delays and the dynamic nature of Content Delivery Networks (CDNs)
                    can alter the data based on the anycast DNS location. Therefore, our metrics
                    offer a valuable yet approximate view of the current state of IPv6 adoption.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">
                    Why does a domain show as not supporting IPv6 when it does?
                  </h4>
                  <p class="text-md text-gray-400 mb-2">
                    This could be due to various reasons like DNS propagation delays or temporary
                    server issues. If you notice inconsistencies, please contact us.
                  </p>
                  <p class="text-md text-gray-400">
                    For instance, DNS propagation delays and the dynamic nature of Content Delivery
                    Networks (CDNs) can alter the data based on the anycast DNS location.
                  </p>
                </li>
              </ul>
            </div>

            <!-- Rules and API -->
            <div v-show="page === '2'">
              <div class="mb-8">
                <h2 class="h2 mb-4">Rules, Frequency, and API Access</h2>
              </div>
              <ul class="-my-4">
                <li class="pt-4">
                  <h3 class="text-xl font-medium underline">Crawler</h3>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Crawler Rules</h4>
                  <p class="text-md text-gray-400 mb-2">
                    The crawler checks AAAA records on domain.com, www.domain.com, and the domain's
                    NS and MX records. It also opens a real HTTP connection over IPv6 — publishing
                    an AAAA record that doesn't answer won't fool anyone.
                  </p>
                  <p class="text-md text-gray-400">
                    The domain and www lookups go through three independent public resolvers, and
                    two out of three must agree.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Crawler Frequency</h4>
                  <p class="text-md text-gray-400">
                    Every domain is scanned once per day. A status only changes after 3 consecutive
                    scans agree, so one flaky DNS answer won't flip your verdict.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">What does “Not applicable” mean?</h4>
                  <p class="text-md text-gray-400 mb-2">
                    There was nothing to grade — it never counts against a domain.
                  </p>
                  <p class="text-md text-gray-400 mb-2">
                    For E-Mail it means the domain publishes no MX records: no mail service, nothing
                    to check. A domain without mail can still become a hero.
                  </p>
                  <p class="text-md text-gray-400">
                    For Page resources it means one of two things: the page loads over IPv6 and
                    pulls no resources from external hosts, or the site isn't reachable over IPv6 at
                    all — then its resources can't be evaluated. The domain status card and the live
                    check both spell out which one applies.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Crawler Errors</h4>
                  <p class="text-md text-gray-400">
                    Did you find any errors from the crawler? PR's are welcome
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Heroes</h4>
                  <p class="text-md text-gray-400">
                    To become one of the IPv6 heroes here, you need IPv6 on domain.com,
                    www.domain.com and the nameservers. MX records need IPv6 or be empty, and the
                    site has to actually respond over IPv6.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Saints</h4>
                  <p class="text-md text-gray-400">
                    Saints are heroes that also load all their page resources — scripts, fonts,
                    images — over IPv6. The full package: the site works on an IPv6-only connection.
                  </p>
                </li>
                <li class="pt-4">
                  <h3 class="text-xl font-medium underline">Campaign Crawler</h3>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">How do i create my own campaign?</h4>
                  <p class="text-md text-gray-400">
                    Create a new issue on the
                    <a
                      href="https://github.com/lasseh/whynoipv6-campaign"
                      target="_blank"
                      class="a-gradient"
                      >Github repo</a
                    >
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">
                    How can i get my domain removed from the list?
                  </h4>
                  <p class="text-md text-gray-400">Yes, you can start using IPv6!</p>
                </li>
                <li class="pt-3">
                  <h3 class="text-xl font-medium underline">API</h3>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Can i get access to the API?</h4>
                  <p class="text-md text-gray-400 mb-2">
                    Yes, the API is open — no key, no signup. Everything on this site is served from
                    it, at
                    <span class="a-gradient">https://api.whynoipv6.com</span> (no version prefix).
                  </p>
                  <p class="text-md text-gray-400 mb-2">
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
                  <h4 class="text-xl font-medium mb-2">Can i download the whole dataset?</h4>
                  <p class="text-md text-gray-400 mb-2">
                    Yes — daily snapshots (CSV and Parquet) are published at
                    <a href="https://api.whynoipv6.com/datasets" target="_blank" class="a-gradient"
                      >api.whynoipv6.com/datasets</a
                    >. Please don't paginate the whole API when a bulk file exists.
                  </p>
                  <p class="text-md text-gray-400">
                    The data is licensed CC-BY-NC-4.0. Attribution: Data: whynoipv6.com
                    (CC-BY-NC-4.0). Ranks: Tranco.
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Is there a badge for my README?</h4>
                  <p class="text-md text-gray-400 mb-2">
                    Every domain has an SVG status badge. Embed it in markdown:
                  </p>
                  <p class="text-md text-gray-400 font-mono">
                    ![IPv6](https://api.whynoipv6.com/badge/yourdomain.com.svg)
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Can i follow changes as a feed?</h4>
                  <p class="text-md text-gray-400">
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
                <h2 class="h2 mb-4">Resources</h2>
              </div>
              <ul class="-my-4">
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">IPv6</h4>
                  <p class="text-md text-gray-400">
                    <a
                      href="https://www.internetsociety.org/deploy360/ipv6/"
                      target="_blank"
                      class="a-gradient"
                      >Internet Society IPv6</a
                    >
                  </p>
                  <p class="text-md text-gray-400">
                    <a href="https://ready.chair6.net/" target="_blank" class="a-gradient"
                      >IPv6 Ready test</a
                    >
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">IPv6 Networking Best Practices</h4>
                  <p class="text-md text-gray-400">
                    <a
                      href="https://blog.apnic.net/2023/04/04/ipv6-architecture-and-subnetting-guide-for-network-engineers-and-operators/"
                      class="a-gradient"
                      >IPv6 Subnetting - Best Practices</a
                    >
                  </p>
                  <p class="text-md text-gray-400">
                    <a
                      href="https://www.internetsociety.org/deploy360/ipv6/security/"
                      class="a-gradient"
                      >IPv6 Security Considerations</a
                    >
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Community and Forums</h4>
                  <p class="text-md text-gray-400">
                    <a href="https://www.reddit.com/r/ipv6/" class="a-gradient">Reddit's r/ipv6</a>
                  </p>
                  <p class="text-md text-gray-400">
                    <a href="https://www.ipv6forum.com/" class="a-gradient">IPv6 Forum</a>
                  </p>
                  <p class="text-md text-gray-400">
                    <a href="https://packetpushers.net/podcasts/ipv6-buzz/" class="a-gradient"
                      >IPv6 Buzz Podcast</a
                    >
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Online Courses and Webinars</h4>
                  <p class="text-md text-gray-400">
                    <a href="https://ipv6.he.net/certification/" target="_blank" class="a-gradient"
                      >Hurricane Electric IPv6 Certification Project</a
                    >
                  </p>
                  <p class="text-md text-gray-400">
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
                  <p class="text-md text-gray-400">
                    <a
                      href="https://bgp.he.net/ipv6-progress-report.cgi"
                      target="_blank"
                      class="a-gradient"
                      >Global IPv6 Deployment Progress Report</a
                    >
                  </p>
                  <p class="text-md text-gray-400">
                    <a href="https://www.worldipv6launch.org/" target="_blank" class="a-gradient"
                      >World IPv6 Launch</a
                    >
                  </p>
                  <p class="text-md text-gray-400">
                    <a
                      href="https://www.google.com/intl/en/ipv6/statistics.html"
                      target="_blank"
                      class="a-gradient"
                      >Google v6 Statistics</a
                    >
                  </p>
                  <p class="text-md text-gray-400">
                    <a href="https://www.vyncke.org/ipv6status/" target="_blank" class="a-gradient"
                      >IPv6 Deployment Aggregated Status</a
                    >
                  </p>
                  <p class="text-md text-gray-400">
                    <a href="https://awsipv6.neveragain.de/" target="_blank" class="a-gradient"
                      >AWS service endpoints by region and IPv6 support</a
                    >
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Stickers</h4>
                  <p class="text-md text-gray-400">
                    Become part of a community of like-minded individuals and show your support for
                    IPv6.
                  </p>
                  <p class="text-md text-gray-400 mb-2">
                    Add stickers to your laptop, or vandalize a building of a IPv6 sinner!
                  </p>
                  <p class="text-md text-gray-400">
                    Order your's today:
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
                        alt="Sticker"
                      />
                    </div>
                  </div>
                </li>
              </ul>
            </div>

            <!-- About -->
            <div v-show="page === '4'">
              <div class="mb-8">
                <h2 class="h2 mb-4">About</h2>
              </div>
              <ul class="-my-4">
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2"># whoami</h4>
                  <p class="text-md text-gray-400 mb-2">
                    Hello! I'm Lasse, Norway's own network maestro, on a personal crusade to spread
                    the magic of IPv6 across every corner of the internet.
                  </p>
                  <p class="text-md text-gray-400 mb-2">
                    By day, I'm a wizard of wires and a sorcerer of switches, tirelessly weaving the
                    intricate web of networks that keep our digital world in motion. By night, I
                    prowl the internet, seeking out IPv6 slackers, nudging them to embrace the
                    future of the internet.
                  </p>
                  <p class="text-md text-gray-400">
                    Join me on this journey towards an IPv6-enabled future, where 'IP exhaustion'
                    becomes just a spooky story of the past!
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Contact</h4>
                  <p class="text-md text-gray-400">
                    Twitter / X:
                    <a href="https://twitter.com/WhyNoIPv6" target="_blank" class="a-gradient"
                      >@whynoipv6</a
                    >
                  </p>
                  <p class="text-md text-gray-400">
                    E-Mail:
                    <span class="a-gradient">whynoipv6@protonmail.com</span>
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Status page</h4>
                  <p class="text-md text-gray-400 mb-2">
                    We maintain a status page to show our operations and availability:
                    <a href="https://status.whynoipv6.com/" target="_blank" class="a-gradient"
                      >status.whynoipv6.com</a
                    >
                  </p>
                </li>
                <li class="py-4">
                  <h4 class="text-xl font-medium mb-2">Our Supporters</h4>
                  <p class="text-md text-gray-400 mb-4">
                    We have been fortunate to have support of some great organizations early on in
                    our existence.
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
