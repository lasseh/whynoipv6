<script setup lang="ts">
import { computed } from 'vue'
import type { StatusValue } from '@/api'
import { statusIcon, statusTextClass, statusTooltip } from '@/utils/status'
import Tooltip from '@/components/Tooltip.vue'
import CheckIcon from '@/components/icons/Check.vue'
import CrossIcon from '@/components/icons/Cross.vue'
import MinusIcon from '@/components/icons/Minus.vue'

const props = defineProps<{ value: StatusValue }>()

const glyphs = { check: CheckIcon, cross: CrossIcon, minus: MinusIcon } as const
const glyph = computed(() => glyphs[statusIcon(props.value)])
</script>

<template>
  <Tooltip center :text="statusTooltip(value)" :class="statusTextClass(value)">
    <component :is="glyph" aria-hidden="true" />
    <span class="sr-only">{{ statusTooltip(value) }}</span>
  </Tooltip>
</template>
