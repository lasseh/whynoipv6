<script setup lang="ts">
// One status cell for the domain tables: the StatusIcon in its inline-flex
// box. Per-table width classes (md:w-[10%] vs md:w-[12%] — five vs four
// status columns sharing the same total width) ride in via the class
// attribute (merged onto the td).
import StatusIcon from '@/components/StatusIcon.vue'
import type { StatusValue } from '@/api'

defineProps<{ value: StatusValue }>()
</script>

<template>
  <td class="px-2 py-3 whitespace-nowrap w-px text-center">
    <div class="inline-flex px-2.5 py-1">
      <StatusIcon :value="value" />
    </div>
  </td>
</template>
