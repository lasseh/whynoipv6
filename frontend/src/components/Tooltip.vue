<script setup lang="ts">
// The one hover-tooltip adapter: bordered gray bubble, fuchsia text,
// revealed on hover. `center` floats the bubble centered above the trigger
// (icon usage); the default hangs it below the trigger, right-anchored
// (table headers — the tables' overflow-x-auto wrapper clips anything
// above or right of the table, so the bubble must open down-left).
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
      :class="center ? 'center' : 'top-full mt-1 right-0 w-max max-w-72'"
      >{{ text }}</span
    >
    <slot />
  </div>
</template>

<style scoped>
/* display, not visibility: a visibility-hidden absolute box still counts
   as scrollable overflow, which gave every domain list's overflow-x-auto
   wrapper a phantom scrollbar from the widest header bubble. */
.tooltip {
  display: none;
  position: absolute;
}

.center {
  left: 50%;
  bottom: -50%;
  transform: translate(-50%, -100%);
  white-space: nowrap;
}

.has-tooltip:hover .tooltip {
  display: block;
  z-index: 50;
}
</style>
