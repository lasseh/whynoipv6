<script setup lang="ts">
// The one hover-tooltip adapter: bordered gray bubble, fuchsia text,
// revealed on hover. `center` floats the bubble centered above the trigger
// (icon usage); the default hangs it above the trigger (table headers).
// `disabled` renders the slot alone — for callers whose tooltip is
// conditional (e.g. only the muted rating star explains itself).
withDefaults(defineProps<{ text: string; center?: boolean; disabled?: boolean }>(), {
  center: false,
  disabled: false,
})
</script>

<template>
  <div :class="disabled ? 'inline-block' : 'has-tooltip relative inline-block'">
    <span
      v-if="!disabled"
      class="tooltip rounded border border-slate-700 shadow-lg p-1 bg-gray-800 text-fuchsia-600 normal-case"
      :class="center ? 'center' : '-mt-8'"
      >{{ text }}</span
    >
    <slot />
  </div>
</template>

<style scoped>
.tooltip {
  visibility: hidden;
  position: absolute;
}

.center {
  left: 50%;
  bottom: -50%;
  transform: translate(-50%, -100%);
  white-space: nowrap;
}

.has-tooltip:hover .tooltip {
  visibility: visible;
  z-index: 50;
}
</style>
