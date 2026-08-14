<script setup lang="ts">
// One status-column header cell for the domain tables: the tooltip-carrying
// label, with an optional short label swapped in below the md breakpoint.
// Padding/width tweaks ride in via the class attribute (merged onto the th).
import Tooltip from '@/components/Tooltip.vue'

withDefaults(defineProps<{ tooltip: string; label: string; shortLabel?: string }>(), {
  shortLabel: '',
})
</script>

<template>
  <th class="py-3 whitespace-nowrap">
    <template v-if="shortLabel">
      <div class="font-semibold text-center md:block hidden">
        <Tooltip :text="tooltip">{{ label }}</Tooltip>
      </div>
      <div class="font-semibold text-center md:hidden">{{ shortLabel }}</div>
    </template>
    <Tooltip v-else :text="tooltip">{{ label }}</Tooltip>
  </th>
</template>
