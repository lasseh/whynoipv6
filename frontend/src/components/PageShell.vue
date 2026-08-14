<script setup lang="ts">
import Header from '@/partials/Header.vue'
import PageIllustration from '@/partials/PageIllustration.vue'
import Footer from '@/partials/Footer.vue'
</script>

<!-- The one page chrome: header, illustration, footer. Pages render their
     sections into the default slot — never hand-roll this scaffold. -->
<template>
  <div class="flex flex-col min-h-screen overflow-hidden">
    <a
      href="#main-content"
      class="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:top-2 focus:left-2 focus:bg-gray-800 focus:text-white focus:px-4 focus:py-2 focus:rounded"
      >Skip to content</a
    >
    <Header />

    <main id="main-content" tabindex="-1" class="grow outline-none">
      <div class="relative max-w-6xl mx-auto h-0 pointer-events-none" aria-hidden="true">
        <PageIllustration />
      </div>

      <slot />
    </main>

    <Footer />
  </div>
</template>
