<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import PageShell from '@/components/PageShell.vue'
import FaqGeneral from '@/partials/faq/FaqGeneral.vue'
import FaqRulesApi from '@/partials/faq/FaqRulesApi.vue'
import FaqResources from '@/partials/faq/FaqResources.vue'
import FaqAbout from '@/partials/faq/FaqAbout.vue'

const route = useRoute()
const router = useRouter()

// One entry per content partial; drives the sidebar nav and page validation.
const SECTIONS = [
  { id: '1', label: 'Frequently Asked Questions' },
  { id: '2', label: 'Rules and API' },
  { id: '3', label: 'Resources' },
  { id: '4', label: 'About' },
]
const validPages = SECTIONS.map((s) => s.id)

function pageFromQuery(value: unknown): string {
  return typeof value === 'string' && validPages.includes(value) ? value : '1'
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
          <!-- PageShell already renders the page's one <main>; nesting
               another is invalid HTML and a duplicate landmark. -->
          <div class="md:flex-auto md:pl-10 order-1">
            <FaqGeneral v-show="page === '1'" />

            <!-- Rules and API -->
            <FaqRulesApi v-show="page === '2'" />

            <!-- Resources -->
            <FaqResources v-show="page === '3'" />

            <!-- About -->
            <FaqAbout v-show="page === '4'" />
          </div>

          <!-- Nav sidebar -->
          <aside class="md:w-64 mb-16 md:mb-0 md:mr-10 md:shrink-0">
            <nav aria-label="FAQ sections">
              <ul>
                <li v-for="s in SECTIONS" :key="s.id" class="py-2 border-b border-gray-800">
                  <a
                    :class="page === s.id ? 'text-fuchsia-600' : 'text-gray-400'"
                    class="flex items-center px-3 group hover:text-fuchsia-600 transition duration-150 ease-in-out"
                    href="#0"
                    @click.prevent="applyFilterAndUpdateRoute(s.id)"
                  >
                    <span>{{ s.label }}</span>
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
