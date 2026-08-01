<script setup lang="ts">
// The one big domain-search form — Home's Searchbar and /search both render
// it. Keeps action="/search" + name="q" so the native GET still works as the
// no-JS fallback; with JS the submit event takes over.
defineProps<{ modelValue: string }>()

const emit = defineEmits<{ 'update:modelValue': [value: string]; submit: [] }>()
</script>

<template>
  <form action="/search" method="get" @submit.prevent="emit('submit')">
    <label for="search" class="mb-2 text-sm font-medium sr-only text-white">Search domains</label>
    <div class="relative">
      <div class="absolute inset-y-0 left-0 flex items-center pl-3 pointer-events-none">
        <svg
          aria-hidden="true"
          class="w-5 h-5 text-gray-400"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
          xmlns="http://www.w3.org/2000/svg"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
          ></path>
        </svg>
      </div>
      <input
        id="search"
        type="search"
        name="q"
        :value="modelValue"
        class="block w-full p-4 pl-10 text-sm border rounded-sm bg-gray-800 border-gray-700 placeholder-gray-400 text-white focus:ring-fuchsia-900 focus:border-fuchsia-900"
        placeholder="Search domains"
        required
        @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      />
      <button
        type="submit"
        class="text-white absolute right-2.5 bottom-2.5 focus:ring-3 focus:outline-none font-medium rounded-sm text-sm px-4 py-2 bg-fuchsia-700 hover:bg-fuchsia-900 focus:ring-fuchsia-800"
      >
        Search
      </button>
    </div>
  </form>
</template>
