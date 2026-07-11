<script setup lang="ts">
// The one segmented filter-toggle contract — never inline these button
// class ternaries in a page (they have drifted apart before).
export interface TabOption {
  value: string
  label: string
}

defineProps<{ options: TabOption[]; modelValue: string }>()

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
</script>

<template>
  <div class="w-full flex flex-wrap -space-x-px">
    <button
      v-for="opt in options"
      :key="opt.value"
      :class="[
        'btn grow border-zinc-700 hover:bg-zinc-800/20 rounded-none first:rounded-l last:rounded-r',
        modelValue === opt.value
          ? 'text-fuchsia-600 bg-zinc-500/20'
          : 'text-slate-300 bg-zinc-700/20',
      ]"
      :aria-pressed="modelValue === opt.value"
      @click="emit('update:modelValue', opt.value)"
    >
      {{ opt.label }}
    </button>
  </div>
</template>
