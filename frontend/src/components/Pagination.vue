<script setup lang="ts">
import { computed } from 'vue'
import type { Page } from '@/api'

// Cursor pagination (§9.1): Next enabled iff has_more, Previous iff
// prev_cursor is non-null — the campaign-scoped changelog's null cursors
// self-disable both with zero special-casing. Same buttons as the old site.
const props = defineProps<{ page: Page | null }>()

const emit = defineEmits<{ previous: []; next: [] }>()

const isPreviousDisabled = computed(() => !props.page?.prev_cursor)
const isNextDisabled = computed(() => !props.page?.has_more || !props.page.next_cursor)
</script>

<template>
  <div class="mt-2">
    <div class="flex justify-center">
      <nav class="flex" role="navigation" aria-label="Navigation">
        <div class="mr-2">
          <button
            :disabled="isPreviousDisabled"
            class="inline-flex items-center justify-center rounded leading-5 px-2.5 py-2 bg-zinc-700 hover:bg-zinc-800 border border-zinc-700 text-zinc-300 hover:text-white shadow-sm"
            :class="{ 'cursor-not-allowed opacity-50': isPreviousDisabled }"
            @click="emit('previous')"
          >
            <span>Previous</span>
          </button>
        </div>
        <div class="ml-2">
          <button
            :disabled="isNextDisabled"
            class="inline-flex items-center justify-center rounded leading-5 px-2.5 py-2 bg-zinc-700 hover:bg-zinc-800 border border-zinc-700 text-zinc-300 hover:text-white shadow-sm"
            :class="{ 'cursor-not-allowed opacity-50': isNextDisabled }"
            @click="emit('next')"
          >
            <span>Next</span>
          </button>
        </div>
      </nav>
    </div>
  </div>
</template>
