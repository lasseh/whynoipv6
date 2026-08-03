<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import Breadcrumb from '@/components/Breadcrumb.vue'

import { loadPost, posts } from '@/blog'
import type { Post } from '@/blog'
import { setPageMeta } from '@/composables/usePageMeta'
import { formatDate } from '@/utils/date'

const route = useRoute()
const router = useRouter()

const post = ref<Post | null>(null)
const missing = ref(false)

// Content is a compiled-in chunk, not an API call — the only async is the
// chunk import, so there's no loading state worth rendering. Unknown slugs
// get the inline not-found (nginx serves the SPA with a 200 for them; only
// real posts exist as prerendered files).
watch(
  () => route.params.slug,
  async (slug) => {
    if (typeof slug !== 'string') return // leaving the route
    // Frontmatter is eager (src/blog.ts), so the share tags are restored
    // synchronously — the router guard has just overwritten the prerendered
    // ones with the route's generic fallback, and nothing should be able to
    // observe that state, least of all a crawler waiting on the body chunk.
    const meta = posts.find((p) => p.slug === slug)
    if (meta) setPageMeta(meta.title, meta.description)

    const loaded = await loadPost(slug)
    if (route.params.slug !== slug) return // raced a later navigation
    post.value = loaded
    missing.value = loaded === null
  },
  { immediate: true },
)

// Article bodies are v-html, so in-content site links (/domains, /metrics)
// are plain anchors — route them through the SPA instead of a full reload.
// Modified clicks and target="_blank" externals keep browser behavior.
function onArticleClick(event: MouseEvent) {
  if (event.defaultPrevented || event.button !== 0) return
  if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
  const anchor = (event.target as HTMLElement).closest('a')
  const href = anchor?.getAttribute('href')
  if (!anchor || !href || !href.startsWith('/') || anchor.target === '_blank') return
  event.preventDefault()
  void router.push(href)
}
</script>

<template>
  <PageShell>
    <!-- Page sections -->
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-20 pb-12 md:pt-24 md:pb-16">
          <Breadcrumb :trail="[{ label: 'Blog', to: '/blog' }]" />

          <!-- Unknown slug -->
          <div v-if="missing" class="py-12">
            <h1 class="h3 mb-4">No post here</h1>
            <p class="text-base text-gray-400 mb-6">
              Nothing is published at this URL. Everything we have written lives on the blog index.
            </p>
            <router-link
              to="/blog"
              class="btn-sm text-white bg-fuchsia-700 hover:bg-fuchsia-800 transition duration-150 ease-in-out"
              >All posts</router-link
            >
          </div>

          <article v-else-if="post">
            <header class="mb-8">
              <div class="text-sm text-gray-500 mb-3">
                <time :datetime="post.meta.date">{{ formatDate(post.meta.date) }}</time>
                <span aria-hidden="true"> · </span>{{ post.meta.minutes }} min read
              </div>
              <h1 class="h2 mb-4">{{ post.meta.title }}</h1>
              <p class="text-lg text-gray-400">{{ post.meta.description }}</p>
            </header>

            <!-- Compiled from repo markdown at build time (scripts/posts.ts) —
                 no user input ever reaches this sink. -->
            <!-- eslint-disable-next-line vue/no-v-html -->
            <div class="prose prose-invert max-w-none" @click="onArticleClick" v-html="post.html" />

            <footer
              class="mt-12 pt-6 border-t border-gray-700/50 flex items-center justify-between"
            >
              <router-link
                to="/blog"
                class="text-sm font-medium text-gray-400 hover:text-gray-200 transition duration-150 ease-in-out"
                >&larr; All posts</router-link
              >
              <a
                href="/blog/rss.xml"
                class="inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-gray-200 transition duration-150 ease-in-out"
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
            </footer>
          </article>
        </div>
      </div>
    </section>
  </PageShell>
</template>
