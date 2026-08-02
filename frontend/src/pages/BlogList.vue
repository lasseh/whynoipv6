<script setup lang="ts">
import PageShell from '@/components/PageShell.vue'
import { posts } from '@/blog'
import { formatDate } from '@/utils/date'
</script>

<template>
  <PageShell>
    <!-- Page sections -->
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-32 pb-12 md:pt-32 md:pb-16">
          <header class="mb-8 sm:flex sm:items-end sm:justify-between">
            <div class="text-center sm:text-left">
              <h1 class="h3">Blog</h1>
              <p class="text-base text-gray-400 mt-1">
                Write-ups from the crawl data. The numbers do the talking; we hold the flashlight.
              </p>
            </div>
            <a
              href="/blog/rss.xml"
              class="hidden sm:inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-gray-200 transition duration-150 ease-in-out shrink-0"
            >
              <svg
                class="w-4 h-4"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                xmlns="http://www.w3.org/2000/svg"
                aria-hidden="true"
              >
                <path d="M4 11a9 9 0 0 1 9 9" />
                <path d="M4 4a16 16 0 0 1 16 16" />
                <circle cx="5" cy="19" r="1" />
              </svg>
              RSS feed
            </a>
          </header>

          <div class="space-y-5">
            <router-link
              v-for="post in posts"
              :key="post.slug"
              :to="`/blog/${post.slug}`"
              class="group block p-5 rounded border border-gray-700/50 bg-gray-800/50 hover:border-gray-600 transition duration-150 ease-in-out"
            >
              <article>
                <div class="text-xs text-gray-500 mb-2">
                  <time :datetime="post.date">{{ formatDate(post.date) }}</time>
                  <span aria-hidden="true"> · </span>{{ post.minutes }} min read
                </div>
                <h2
                  class="text-xl font-bold text-gray-200 group-hover:text-fuchsia-500 transition duration-150 ease-in-out mb-2"
                >
                  {{ post.title }}
                </h2>
                <p class="text-base text-gray-400">{{ post.description }}</p>
              </article>
            </router-link>
          </div>
        </div>
      </div>
    </section>
  </PageShell>
</template>
