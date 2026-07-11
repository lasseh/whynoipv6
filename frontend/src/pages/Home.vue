<script setup lang="ts">
import Header from '@/partials/Header.vue'
import PageIllustration from '@/partials/PageIllustration.vue'
import Footer from '@/partials/Footer.vue'
import HomeSaaS from '@/partials/HomeSaaS.vue'
import TopSinners from '@/partials/HomeSinners.vue'
import Searchbar from '@/partials/Searchbar.vue'
import HomeDomains from '@/partials/HomeDomains.vue'
import Notification from '@/components/Notification.vue'
</script>

<template>
  <div class="flex flex-col min-h-screen overflow-hidden">
    <!-- Site header -->
    <Header />

    <!-- Page content -->
    <main class="grow">
      <!-- Page illustration -->
      <div class="relative max-w-6xl mx-auto h-0 pointer-events-none" aria-hidden="true">
        <PageIllustration />
      </div>

      <!-- Page sections -->
      <HomeSaaS />
      <Searchbar />
      <TopSinners />
      <HomeDomains />
      <Notification />
    </main>

    <!-- Site footer -->
    <Footer />
  </div>
</template>
