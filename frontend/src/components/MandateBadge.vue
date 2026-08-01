<script setup lang="ts">
// The government-mandate badge (07 §5.6): shown on campaigns carrying the
// literal 'mandate' tag and on domains that belong to one. Amber on
// purpose — distinct from the fuchsia brand accents and the status hues.
import { computed } from 'vue'
import Tooltip from '@/components/Tooltip.vue'

const props = withDefaults(defineProps<{ names?: string[] }>(), { names: () => [] })

const tip = computed(() =>
  props.names.length
    ? `Covered by a government IPv6 mandate: ${props.names.join(', ')}`
    : 'Covered by a government IPv6 mandate',
)
</script>

<template>
  <Tooltip center :text="tip">
    <span
      class="inline-flex items-center rounded-full border border-amber-500/60 bg-amber-500/10 px-2 py-0.5 text-xs font-semibold uppercase tracking-wider text-amber-400"
    >
      Mandate
    </span>
  </Tooltip>
</template>
