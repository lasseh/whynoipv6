<script setup lang="ts"></script>

<template>
  <section class="relative">
    <div class="max-w-6xl mx-auto px-4 sm:px-6">
      <div class="pt-24 pb-4 md:pt-32 md:pb-4">
        <article>
          <header class="mb-8">
            <!-- Title -->
            <div class="text-center md:text-left">
              <h1 class="h1 mb-4">IPv6 Shame as a Service!</h1>
            </div>
          </header>

          <!-- Content -->
          <div class="text-lg text-gray-400">
            <p class="mb-2">
              IPv6 has been a standard since 1998. IPv4 ran out of addresses more than a decade ago.
              The most-visited websites on the internet still haven't connected those two facts, so
              we keep score. Publicly,
              <span class="font-semibold a-gradient"
                >until a globally routable AAAA record says otherwise.</span
              >
            </p>
          </div>
        </article>

        <div class="py-2 md:py-8">
          <!-- Items -->
          <div class="grid gap-20">
            <!-- Item -->
            <div class="md:grid md:grid-cols-12 md:gap-6">
              <!-- SaaS -->
              <div
                class="max-w-xl md:max-w-none md:w-full mx-auto md:col-span-5 lg:col-span-6 mb-8 md:mb-0 md:order-1"
              >
                <h2 class="h4 mb-3">How does it work?</h2>
                <p class="text-base text-gray-400 mb-0">
                  The crawler checks active domains on a daily schedule: AAAA records for the
                  domain, www, nameserver hosts, and mail hosts, plus a real connection over IPv6.
                  Failures can back off. The results determine each domain's tier and stay public.
                  Anyone can submit a campaign list for the crawler too.
                </p>
                <h2 class="h4 mb-3 mt-6">Why shame?</h2>
                <p class="text-base text-gray-400 mb-0">
                  World IPv6 Launch was in 2012. The missing part isn't another conference talk, so
                  here is the public list.
                </p>
              </div>
              <!-- Buttons -->
              <div class="max-w-xl md:max-w-none md:w-full mx-auto md:col-span-7 lg:col-span-6">
                <div class="md:pr-4 lg:pr-12 xl:pr-16">
                  <!-- Tabs buttons -->
                  <div class="mb-8 md:mb-0 space-y-2">
                    <div
                      class="text-left flex items-center px-6 py-3 rounded border border-transparent hover:bg-zinc-800"
                    >
                      <svg
                        class="shrink-0 fill-zinc-400 mr-3"
                        xmlns="http://www.w3.org/2000/svg"
                        width="24"
                        height="24"
                      >
                        <path
                          stroke-width="1.5"
                          stroke="currentColor"
                          fill="none"
                          d="M14.857 17.082a23.848 23.848 0 005.454-1.31A8.967 8.967 0 0118 9.75v-.7V9A6 6 0 006 9v.75a8.967 8.967 0 01-2.312 6.022c1.733.64 3.56 1.085 5.455 1.31m5.714 0a24.255 24.255 0 01-5.714 0m5.714 0a3 3 0 11-5.714 0M3.124 7.5A8.969 8.969 0 015.292 3m13.416 0a8.969 8.969 0 012.168 4.5"
                        />
                      </svg>
                      <div>
                        <div class="text-base font-semibold text-zinc-200 mb-1 a-gradient">
                          Public Accountability
                        </div>
                        <div class="text-zinc-400 text-base">
                          Our public reports make missing IPv6 support visible and harder for
                          companies to ignore.
                        </div>
                      </div>
                    </div>
                    <div
                      class="text-left flex items-center px-6 py-3 rounded border border-transparent hover:bg-zinc-800"
                    >
                      <svg
                        class="shrink-0 fill-zinc-400 mr-3"
                        xmlns="http://www.w3.org/2000/svg"
                        width="24"
                        height="24"
                      >
                        <path
                          fill="none"
                          stroke="currentColor"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                          d="M18 18.72a9.094 9.094 0 003.741-.479 3 3 0 00-4.682-2.72m.94 3.198l.001.031c0 .225-.012.447-.037.666A11.944 11.944 0 0112 21c-2.17 0-4.207-.576-5.963-1.584A6.062 6.062 0 016 18.719m12 0a5.971 5.971 0 00-.941-3.197m0 0A5.995 5.995 0 0012 12.75a5.995 5.995 0 00-5.058 2.772m0 0a3 3 0 00-4.681 2.72 8.986 8.986 0 003.74.477m.94-3.197a5.971 5.971 0 00-.94 3.197M15 6.75a3 3 0 11-6 0 3 3 0 016 0zm6 3a2.25 2.25 0 11-4.5 0 2.25 2.25 0 014.5 0zm-13.5 0a2.25 2.25 0 11-4.5 0 2.25 2.25 0 014.5 0z"
                        />
                      </svg>
                      <div>
                        <div class="text-base font-semibold text-zinc-200 mb-1 a-gradient">
                          Community Action
                        </div>
                        <div class="text-zinc-400 text-base">
                          Share what you find, start conversations, and use the data to push for
                          wider IPv6 adoption.
                        </div>
                      </div>
                    </div>
                    <div
                      class="text-left flex items-center px-6 py-3 rounded border border-transparent hover:bg-zinc-800"
                    >
                      <svg
                        class="shrink-0 fill-zinc-400 mr-3"
                        xmlns="http://www.w3.org/2000/svg"
                        width="24"
                        height="24"
                      >
                        <path
                          stroke="currentColor"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                          stroke-width="1"
                          fill="none"
                          d="M13 8h6m-3 3V5m-6-.5a3.5 3.5 0 1 1-7 0 3.5 3.5 0 0 1 7 0ZM5 11h3a4 4 0 0 1 4 4v2H1v-2a4 4 0 0 1 4-4Z"
                        />
                      </svg>
                      <div>
                        <div class="text-base font-semibold text-zinc-200 mb-1 a-gradient">
                          User-Led Shaming
                        </div>
                        <div class="text-zinc-400 text-base">
                          Found a well-known site without IPv6? Submit it, and we'll add it to our
                          daily checks.
                        </div>
                      </div>
                    </div>
                  </div>
                  <!-- End Tabs buttons -->
                </div>
              </div>
              <!-- End Buttons -->
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
