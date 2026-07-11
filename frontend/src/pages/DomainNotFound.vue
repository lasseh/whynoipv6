<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

import Header from '@/partials/Header.vue'
import PageIllustration from '@/partials/PageIllustration.vue'
import Footer from '@/partials/Footer.vue'

const route = useRoute()

const domainName = computed(() => (route.params.domain as string) || 'unknown.domain')
</script>

<template>
  <div class="flex flex-col min-h-screen overflow-hidden">
    <!-- Site header -->
    <Header />

    <!-- Page content -->
    <main class="grow">
      <!-- Page illustration -->
      <div class="relative max-w-6xl mx-auto h-0 pointer-events-none" aria-hidden="true">
        <PageIllustration />
      </div>

      <!-- Page sections -->
      <section class="relative">
        <div class="max-w-6xl mx-auto px-4 sm:px-6">
          <div class="pt-20 pb-12 md:pt-24 md:pb-20">
            <!-- 404 Content -->
            <div class="text-center">
              <!-- Main heading -->
              <h1 class="h1 mb-4 text-white">Domain Not Found</h1>

              <!-- Funny message -->
              <div class="max-w-3xl mx-auto mb-8">
                <p class="text-xl text-gray-300 mb-4">
                  Oops! The domain
                  <span class="font-mono text-pink-600">{{ domainName }}</span> seems to have
                  vanished into the void.
                </p>
                <p class="text-lg text-gray-400 mb-6">
                  It's either playing hide and seek in the DNS records, or it never existed in our
                  database to begin with.
                </p>
              </div>

              <!-- Nerdy explanations -->
              <div class="max-w-4xl mx-auto mb-8">
                <div class="grid md:grid-cols-2 gap-6">
                  <!-- Left card -->
                  <div class="bg-gray-800 rounded-lg p-6 border border-gray-700">
                    <div class="flex justify-center mb-3">
                      <svg
                        class="w-8 h-8 text-purple-400"
                        xmlns="http://www.w3.org/2000/svg"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke-width="1.5"
                        stroke="currentColor"
                      >
                        <path
                          stroke-linecap="round"
                          stroke-linejoin="round"
                          d="m21 21-5.197-5.197m0 0A7.5 7.5 0 1 0 5.196 5.196a7.5 7.5 0 0 0 10.607 10.607Z"
                        />
                      </svg>
                    </div>
                    <h3 class="text-lg font-semibold text-white mb-2">DNS Resolution Failed</h3>
                    <p class="text-gray-400 text-sm">
                      Our crawlers couldn't find this domain in the wild. It might be:
                    </p>
                    <ul class="text-gray-400 text-sm mt-2 text-left">
                      <li>• Not yet crawled by our bots</li>
                      <li>• A typo in the domain name</li>
                      <li>• Living in a parallel universe</li>
                    </ul>
                  </div>

                  <!-- Right card -->
                  <div class="bg-gray-800 rounded-lg p-6 border border-gray-700">
                    <div class="flex justify-center mb-3">
                      <svg
                        class="w-8 h-8 text-purple-400"
                        xmlns="http://www.w3.org/2000/svg"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke-width="1.5"
                        stroke="currentColor"
                      >
                        <path
                          stroke-linecap="round"
                          stroke-linejoin="round"
                          d="M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 0 1 3 19.875v-6.75ZM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V8.625ZM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V4.125Z"
                        />
                      </svg>
                    </div>
                    <h3 class="text-lg font-semibold text-white mb-2">
                      Want Us to Track This Domain?
                    </h3>
                    <p class="text-gray-400 text-sm mb-3">
                      Help us expand IPv6 monitoring! Submit this domain to our community campaign
                      and we'll start tracking its IPv6 adoption progress.
                    </p>
                    <a
                      href="https://github.com/lasseh/whynoipv6-campaign"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="inline-flex items-center text-sm text-purple-400 hover:text-purple-300 transition-colors"
                    >
                      <svg
                        class="w-4 h-4 mr-2"
                        xmlns="http://www.w3.org/2000/svg"
                        fill="currentColor"
                        viewBox="0 0 20 20"
                      >
                        <path
                          fill-rule="evenodd"
                          d="M10 0C4.477 0 0 4.484 0 10.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0110 4.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.203 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.942.359.31.678.921.678 1.856 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0020 10.017C20 4.484 15.522 0 10 0z"
                          clip-rule="evenodd"
                        ></path>
                      </svg>
                      Submit to Campaign →
                    </a>
                  </div>
                </div>
              </div>

              <!-- Action buttons -->
              <div class="flex flex-col sm:flex-row gap-4 justify-center items-center">
                <router-link
                  to="/domains"
                  class="btn text-white bg-purple-600 hover:bg-purple-700 w-full sm:w-auto"
                >
                  Browse All Domains
                </router-link>
                <router-link
                  to="/search"
                  class="btn text-purple-600 bg-gray-800 hover:bg-gray-700 border border-gray-600 w-full sm:w-auto"
                >
                  Search Domains
                </router-link>
                <router-link
                  to="/"
                  class="btn text-gray-400 bg-transparent hover:bg-gray-800 border border-gray-600 w-full sm:w-auto"
                >
                  Go Home
                </router-link>
              </div>
            </div>
          </div>
        </div>
      </section>
    </main>

    <!-- Site footer -->
    <Footer />
  </div>
</template>
