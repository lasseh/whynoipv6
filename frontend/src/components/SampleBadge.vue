<script setup lang="ts">
// Marks a panel whose numbers come from src/fixtures/metrics.ts rather than
// the API. The whole point of this site is that the numbers are measured, so
// anything fabricated has to say so on the panel itself — not only in a
// commit message. Delete this component together with the fixtures.
</script>

<template>
  <span
    class="inline-flex shrink-0 items-center gap-1 rounded-sm border border-amber-600/40 bg-amber-600/10 px-1.5 py-0.5 text-[11px] font-medium text-amber-500"
    title="Placeholder numbers — this panel has no API endpoint yet"
  >
    <svg class="size-3 fill-current" viewBox="0 0 16 16" aria-hidden="true">
      <path
        d="M8 1.5 15 14H1L8 1.5Zm0 3.6L3.6 13h8.8L8 5.1ZM7.3 7.2h1.4v3H7.3v-3Zm0 3.8h1.4v1.3H7.3V11Z"
      />
    </svg>
    Sample data
  </span>
</template>
