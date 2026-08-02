<script setup lang="ts">
// Slim "from the blog" strip under the search hero — the latest post only.
// The blog deliberately has no header nav slot (the top bar is full); this
// strip plus the footer link are its home-page surface.
import { posts } from '@/blog'
import { formatDate } from '@/utils/date'

const latest = posts[0]
</script>

<template>
  <section v-if="latest" class="relative">
    <div class="max-w-6xl mx-auto px-4 sm:px-6">
      <div class="py-2 md:py-2">
        <router-link
          :to="`/blog/${latest.slug}`"
          class="group flex items-baseline gap-3 rounded border border-gray-700/50 bg-gray-800/50 px-4 py-3 hover:border-gray-600 transition duration-150 ease-in-out"
        >
          <span
            class="text-xs font-semibold uppercase tracking-wider text-fuchsia-500 shrink-0 whitespace-nowrap"
            >From the blog</span
          >
          <span
            class="text-sm font-medium text-gray-200 truncate group-hover:text-white transition duration-150 ease-in-out"
            >{{ latest.title }}</span
          >
          <span class="hidden sm:block text-xs text-gray-500 ml-auto shrink-0">
            {{ formatDate(latest.date) }} &rarr;
          </span>
        </router-link>
      </div>
    </div>
  </section>
</template>
