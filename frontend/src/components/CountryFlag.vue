<script lang="ts">
// Self-hosted: the circle-flags npm package is bundled at build time, so no
// runtime requests leave our origin. `no-inline` keeps the SVGs as emitted
// files instead of data URIs baked into the JS bundle. Module scope, not
// setup: the eager glob expands to a literal object that would otherwise be
// rebuilt on every one of the ~250 instances a /countries visit mounts.
const flags = import.meta.glob<string>('/node_modules/circle-flags/flags/*.svg', {
  query: '?url&no-inline',
  import: 'default',
  eager: true,
})
</script>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  countryCode: string
}>()

const flagUrl = computed(
  () =>
    flags[`/node_modules/circle-flags/flags/${props.countryCode.toLowerCase()}.svg`] ??
    flags['/node_modules/circle-flags/flags/xx.svg'],
)
</script>

<template>
  <img :src="flagUrl" alt="" width="48" height="48" loading="lazy" decoding="async" />
</template>
