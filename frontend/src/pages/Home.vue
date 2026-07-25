<script setup lang="ts">
import PageShell from '@/components/PageShell.vue'
import HomeSaaS from '@/partials/HomeSaaS.vue'
import HomeSinners from '@/partials/HomeSinners.vue'
import Searchbar from '@/partials/Searchbar.vue'
import HomeDomains from '@/partials/HomeDomains.vue'
import Notification from '@/components/Notification.vue'
</script>

<template>
  <PageShell>
    <!-- Page sections -->
    <HomeSaaS />
    <Searchbar />
    <HomeSinners />
    <HomeDomains />
    <Notification />
  </PageShell>
</template>
