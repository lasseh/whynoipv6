<script setup lang="ts">
// The one filter/search input — never re-paste the magnifier form into a
// page. Width/height variances ride in via class (form), input-class and
// button-class; pages that filter client-side simply attach no @submit, so
// Enter never reloads the page.
withDefaults(
  defineProps<{
    modelValue: string
    inputId: string
    label?: string
    placeholder?: string
    inputClass?: string
    buttonClass?: string
  }>(),
  { label: 'Filter', placeholder: 'Filter…', inputClass: '', buttonClass: '' },
)

const emit = defineEmits<{ 'update:modelValue': [value: string]; submit: [] }>()
</script>

<template>
  <form class="relative" @submit.prevent="emit('submit')">
    <label :for="inputId" class="sr-only">{{ label }}</label>
    <input
      :id="inputId"
      :class="['form-input pl-9 bg-zinc-800', inputClass]"
      type="search"
      :placeholder="placeholder"
      :value="modelValue"
      @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
    />
    <button
      :class="['absolute inset-0 right-auto group', buttonClass]"
      type="submit"
      aria-label="Apply filter"
    >
      <svg
        class="w-4 h-4 shrink-0 fill-current text-zinc-500 group-hover:text-zinc-400 ml-3 mr-2"
        viewBox="0 0 16 16"
        xmlns="http://www.w3.org/2000/svg"
      >
        <path
          d="M7 14c-3.86 0-7-3.14-7-7s3.14-7 7-7 7 3.14 7 7-3.14 7-7 7zM7 2C4.243 2 2 4.243 2 7s2.243 5 5 5 5-2.243 5-5-2.243-5-5-5z"
        />
        <path
          d="M15.707 14.293L13.314 11.9a8.019 8.019 0 01-1.414 1.414l2.393 2.393a.997.997 0 001.414 0 .999.999 0 000-1.414z"
        />
      </svg>
    </button>
  </form>
</template>
