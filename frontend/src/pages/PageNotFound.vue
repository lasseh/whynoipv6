<script setup lang="ts">
import PageShell from '@/components/PageShell.vue'
</script>

<template>
  <PageShell>
    <section class="relative">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <div class="pt-32 pb-12 md:pt-40 md:pb-20">
          <div class="max-w-3xl mx-auto text-center">
            <!-- Top image -->
            <div class="relative inline-flex flex-col mb-6">
              <img src="/images/WhyNoLogo.webp" width="196" height="186" alt="404" />
            </div>
            <!-- 404 content -->
            <h1 class="h2 mb-4">Shame! Shame! This page doesn't exist.</h1>
            <p class="text-lg text-gray-400">
              We checked every record — A, AAAA, even MX. Nothing here resolves. Unlike our IPv6
              sinners, this URL has a valid excuse for being unreachable: it doesn't exist. Head to
              our
              <router-link
                to="/"
                class="text-purple-600 hover:text-gray-200 transition duration-150 ease-in-out"
                >homepage</router-link
              >
              that does exist, or double-check the URL.
            </p>
          </div>
        </div>
      </div>
    </section>
  </PageShell>
</template>
