<script setup lang="ts">
// Home's search hero: the section chrome around the one DomainSearchForm,
// routing submits to /search without a full page load.
import { ref } from 'vue'
import { useRouter } from 'vue-router'

import DomainSearchForm from '@/components/DomainSearchForm.vue'

const router = useRouter()
const searchString = ref('')

function submitSearch() {
  void router.push({ path: '/search', query: { q: searchString.value } })
}
</script>

<template>
  <section class="relative">
    <div class="max-w-6xl mx-auto px-4 sm:px-6">
      <div class="py-2 md:py-2">
        <DomainSearchForm v-model="searchString" @submit="submitSearch" />
      </div>
    </div>
  </section>
</template>
