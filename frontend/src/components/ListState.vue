<script setup lang="ts">
// The one render order for a list surface: error, then the first-load
// spinner, then the empty message, then the list itself.
//
// It exists because the order was previously re-decided at every call site
// from the { loading, error, items } triple the list engines return, and the
// copies disagreed: one page gated the table on a non-empty list (making the
// table's own empty state unreachable), another passed `loading` but rendered
// no spinner at all, so a slow first page showed blank space.
//
// `loading` here means *first* load. A pagination fetch keeps the current
// rows on screen rather than flashing a spinner between pages, which is why
// the spinner only shows while there is nothing to show yet.
import ApiError from '@/components/ApiError.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'
import type { ApiProblem } from '@/api/problem'

const props = withDefaults(
  defineProps<{
    /** True while a fetch is in flight. */
    loading?: boolean
    /** Non-null renders the error card and nothing else. */
    error?: ApiProblem | null
    /** How many rows the slot would render. */
    count?: number
    /** Shown when the load succeeded and returned nothing. */
    emptyText?: string
  }>(),
  { loading: false, error: null, count: 0, emptyText: 'No results found' },
)

const isEmpty = () => !props.loading && !props.error && props.count === 0
</script>

<template>
  <ApiError v-if="error" :problem="error" />
  <LoadingSpinner v-else-if="loading && count === 0" />
  <div v-else-if="isEmpty()" class="flex justify-center">
    <div class="text-center">
      <div class="text-xl font-medium">{{ emptyText }}</div>
    </div>
  </div>
  <slot v-else />
</template>
