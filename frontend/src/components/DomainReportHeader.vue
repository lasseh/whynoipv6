<script setup lang="ts">
// The one domain report header — hostname + mandate badge, network line, and
// the parent link — shared by DomainDetail and CampaignDomain so the two
// report surfaces can't drift. `showRank` adds the leaderboard rank badge
// (campaign member pages carry no rank).
import MandateBadge from '@/components/MandateBadge.vue'
import type { DomainDetail } from '@/api'

withDefaults(defineProps<{ domain: DomainDetail; showRank?: boolean }>(), {
  showRank: false,
})
</script>

<template>
  <div class="flex justify-between items-center mb-8">
    <div class="text-left">
      <div class="flex items-center gap-3">
        <h1 class="h2">{{ domain.host }}</h1>
        <MandateBadge
          v-if="domain.mandates.length > 0"
          :names="domain.mandates.map((m) => m.name)"
        />
      </div>
      <p class="text-base text-gray-400 pl-1">
        Network: {{ domain.asn.name }} (AS{{ domain.asn.number }})
      </p>
      <p v-if="domain.parent" class="text-base text-gray-400 pl-1">
        Subdomain of
        <router-link
          :to="{ name: 'DomainDetail', params: { domain: domain.parent } }"
          class="text-fuchsia-500 hover:text-fuchsia-400 underline underline-offset-2"
        >
          {{ domain.parent }}
        </router-link>
      </p>
    </div>
    <div v-if="showRank && domain.rank !== null" class="text-center">
      <div
        class="inline-flex text-center font-mono py-1 px-3 rounded-sm bg-fuchsia-900 hover:bg-fuchsia-900 transition duration-150 ease-in-out"
      >
        Rank: {{ domain.rank }}
      </div>
    </div>
  </div>
</template>
