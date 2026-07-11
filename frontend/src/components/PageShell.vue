<script setup lang="ts">
import Header from '@/partials/Header.vue'
import PageIllustration from '@/partials/PageIllustration.vue'
import Footer from '@/partials/Footer.vue'
</script>

<!-- The one page chrome: header, illustration, footer. Pages render their
     sections into the default slot — never hand-roll this scaffold. -->
<template>
  <div class="flex flex-col min-h-screen overflow-hidden">
    <Header />

    <main class="grow">
      <div class="relative max-w-6xl mx-auto h-0 pointer-events-none" aria-hidden="true">
        <PageIllustration />
      </div>

      <slot />
    </main>

    <Footer />
  </div>
</template>
