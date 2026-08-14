<script setup lang="ts">
// The one hover-tooltip adapter: bordered gray bubble, fuchsia text,
// revealed on hover or keyboard focus (the trigger is tabbable; Escape
// dismisses). Reveal is JS state, not CSS :hover, so Escape can dismiss a
// hover-only bubble (WCAG 1.4.13) and the text is announced via
// aria-describedby (works from display:none targets). `center` floats the
// bubble centered above the trigger (icon usage); the default hangs it
// below the trigger, right-anchored (table headers — the tables'
// overflow-x-auto wrapper clips anything above or right of the table, so
// the bubble must open down-left). `disabled` renders the slot alone — for
// callers whose tooltip is conditional (e.g. only the muted rating star
// explains itself).
import { onUnmounted, ref, useId, watch } from 'vue'

withDefaults(defineProps<{ text: string; center?: boolean; disabled?: boolean }>(), {
  center: false,
  disabled: false,
})

const id = useId()
const focused = ref(false)
const hovered = ref(false)

function dismiss() {
  focused.value = false
  hovered.value = false
}

// A hover-only bubble (no focus) never receives keydown on the wrapper, so
// Escape rides on window while the bubble is hover-shown.
const onWindowEsc = (e: KeyboardEvent) => {
  if (e.key === 'Escape') hovered.value = false
}
watch(hovered, (h) => {
  if (h) window.addEventListener('keydown', onWindowEsc)
  else window.removeEventListener('keydown', onWindowEsc)
})
onUnmounted(() => window.removeEventListener('keydown', onWindowEsc))
</script>

<template>
  <div
    :class="disabled ? 'inline-block' : 'relative inline-block'"
    :tabindex="disabled ? undefined : 0"
    :aria-describedby="disabled ? undefined : id"
    @focusin="focused = true"
    @focusout="focused = false"
    @mouseenter="hovered = true"
    @mouseleave="hovered = false"
    @keydown.esc="dismiss"
  >
    <span
      v-if="!disabled"
      :id="id"
      role="tooltip"
      class="tooltip rounded border border-slate-700 shadow-lg p-1 bg-gray-800 text-fuchsia-600 normal-case"
      :class="[
        center ? 'center' : 'top-full mt-1 right-0 w-max max-w-72 whitespace-normal',
        { open: focused || hovered },
      ]"
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

.tooltip.open {
  display: block;
  z-index: 50;
}
</style>
