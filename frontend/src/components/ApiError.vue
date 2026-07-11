<script setup lang="ts">
import type { ApiProblem } from '@/api/problem'

defineProps<{ problem: ApiProblem }>()
</script>

<!-- The one RFC 9457 error card — pages render this instead of hand-rolling
     the zinc panel. -->
<template>
  <div class="bg-zinc-800/50 border border-zinc-700 rounded-sm shadow-lg p-5 text-center">
    <div class="text-xl font-medium">{{ problem.title }}</div>
    <p v-if="problem.detail" class="text-gray-400 mt-2">{{ problem.detail }}</p>
  </div>
</template>
