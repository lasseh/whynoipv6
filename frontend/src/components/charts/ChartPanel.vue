<script setup lang="ts">
import SampleBadge from '@/components/SampleBadge.vue'

// The card every chart sits in: title, one line of context, the plot. The
// description is not decoration — a stacked area of five classifications is
// unreadable without being told what the bands are counting.
withDefaults(defineProps<{ title: string; description: string; sample?: boolean }>(), {
  sample: false,
})
</script>

<template>
  <section class="rounded border border-gray-700 bg-gray-800/60 p-5">
    <header class="mb-4 flex items-start justify-between gap-3">
      <div>
        <h3 class="text-base font-medium text-zinc-100">{{ title }}</h3>
        <p class="mt-0.5 text-sm text-gray-400">{{ description }}</p>
      </div>
      <SampleBadge v-if="sample" />
    </header>
    <slot />
  </section>
</template>
