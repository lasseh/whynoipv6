<script setup lang="ts">
import { computed } from 'vue'
import type { StatusValue } from '@/api'
import { statusIcon, statusTextClass, statusTooltip } from '@/utils/status'
import CheckIcon from '@/partials/icons/Check.vue'
import CrossIcon from '@/partials/icons/Cross.vue'
import MinusIcon from '@/partials/icons/Minus.vue'

const props = defineProps<{ value: StatusValue }>()

const glyphs = { check: CheckIcon, cross: CrossIcon, minus: MinusIcon } as const
const glyph = computed(() => glyphs[statusIcon(props.value)])
</script>

<template>
  <div class="has-tooltip relative inline-block" :class="statusTextClass(value)">
    <span
      class="tooltip absolute rounded border border-slate-700 shadow-lg p-1 bg-gray-800 text-fuchsia-600 normal-case transform -translate-x-1/2 -translate-y-full"
      >{{ statusTooltip(value) }}</span
    >
    <component :is="glyph" aria-hidden="true" />
    <span class="sr-only">{{ statusTooltip(value) }}</span>
  </div>
</template>

<style scoped>
.tooltip {
  visibility: hidden;
  left: 50%;
  bottom: -50%;
  white-space: nowrap;
}

.has-tooltip:hover .tooltip {
  visibility: visible;
  z-index: 50;
}
</style>
