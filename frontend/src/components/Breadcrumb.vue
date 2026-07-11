<script setup lang="ts">
import type { RouteLocationRaw } from 'vue-router'

// The Home crumb is implicit; trail lists the crumbs after it, in order.
export interface Crumb {
  label: string
  to: RouteLocationRaw
}

defineProps<{ trail: Crumb[] }>()
</script>

<template>
  <div class="mb-4">
    <nav class="flex" aria-label="Breadcrumb">
      <ol class="inline-flex items-center space-x-1 md:space-x-3">
        <li class="inline-flex items-center">
          <router-link
            to="/"
            class="inline-flex items-center text-sm font-medium text-gray-400 hover:text-white"
          >
            <svg
              class="w-3 h-3 mr-2.5"
              aria-hidden="true"
              xmlns="http://www.w3.org/2000/svg"
              fill="currentColor"
              viewBox="0 0 20 20"
            >
              <path
                d="m19.707 9.293-2-2-7-7a1 1 0 0 0-1.414 0l-7 7-2 2a1 1 0 0 0 1.414 1.414L2 10.414V18a2 2 0 0 0 2 2h3a1 1 0 0 0 1-1v-4a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v4a1 1 0 0 0 1 1h3a2 2 0 0 0 2-2v-7.586l.293.293a1 1 0 0 0 1.414-1.414Z"
              />
            </svg>
            Home
          </router-link>
        </li>
        <li
          v-for="(crumb, i) in trail"
          :key="i"
          :aria-current="i === trail.length - 1 ? 'page' : undefined"
        >
          <div class="flex items-center">
            <svg
              class="w-3 h-3 text-gray-400 mx-1"
              aria-hidden="true"
              xmlns="http://www.w3.org/2000/svg"
              fill="none"
              viewBox="0 0 6 10"
            >
              <path
                stroke="currentColor"
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="m1 9 4-4-4-4"
              />
            </svg>
            <router-link
              :to="crumb.to"
              class="ml-1 text-sm font-medium md:ml-2 text-gray-400 hover:text-white truncate"
            >
              {{ crumb.label }}
            </router-link>
          </div>
        </li>
      </ol>
    </nav>
  </div>
</template>
