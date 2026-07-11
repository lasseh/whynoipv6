<script setup lang="ts">
import type { ChangelogItem } from '@/api'
import { changelogMessage } from '@/utils/changelog'
import { formatDateTime } from '@/utils/date'

// Structured rows (§7.4): the message + color derive client-side from
// (field, old_value, new_value) via utils/changelog — never a server string.
withDefaults(
  defineProps<{
    changelogs?: ChangelogItem[]
    header?: string
    /** Route target for host links; campaign pages point into their scope. */
    domainRoute?: (host: string) => { name: string; params: Record<string, string> }
  }>(),
  {
    changelogs: () => [],
    header: 'Changelog',
    domainRoute: (host: string) => ({ name: 'DomainDetail', params: { domain: host } }),
  },
)
</script>

<template>
  <div class="max-w-6xl mx-auto px-4 sm:px-6">
    <header class="mb-4">
      <div class="text-left">
        <h1 class="h3">{{ header }}</h1>
      </div>
    </header>

    <!-- Mobile Version -->
    <div class="grid grid-cols-1 gap-3 md:hidden">
      <template v-if="changelogs.length > 0">
        <div
          v-for="(logItem, index) in changelogs"
          :key="index"
          class="p-3 rounded-lg shadow bg-gray-800"
        >
          <div class="font-mono text-sm">
            <div class="font-xs" :class="changelogMessage(logItem).colorClass">
              {{ changelogMessage(logItem).message }}
            </div>
            <div class="text-gray-500 text-xs">{{ formatDateTime(logItem.ts) }}</div>
          </div>
        </div>
      </template>
      <div v-else class="text-center py-1 font-medium">No changes yet</div>
    </div>

    <!-- Desktop Version -->
    <div class="hidden md:block overflow-x-auto">
      <!-- Table -->
      <div class="overflow-x-auto">
        <table class="table-auto w-full text-slate-300">
          <tbody class="text-xs font-mono divide-y divide-slate-700 border-b border-slate-700">
            <tr
              v-for="(logItem, index) in changelogs"
              :key="index"
              :class="index % 2 === 0 ? '' : 'bg-gray-800'"
            >
              <td class="px-2 first:pl-5 last:pr-5 py-2 whitespace-nowrap">
                <div class="flex">
                  <div class="text-violet-500">{{ formatDateTime(logItem.ts) }}</div>
                </div>
              </td>
              <td class="px-2 first:pl-5 last:pr-5 py-2 whitespace-nowrap text-right">
                <div class="flex">
                  <div class="text-fuchsia-600">
                    <router-link :to="domainRoute(logItem.host)">{{ logItem.host }}</router-link>
                  </div>
                </div>
              </td>
              <td class="px-2 first:pl-5 last:pr-5 py-2 whitespace-nowrap md:w-1/2 w-1/2">
                <div class="flex">
                  <div class="font-xs" :class="changelogMessage(logItem).colorClass">
                    {{ changelogMessage(logItem).message }}
                  </div>
                </div>
              </td>
            </tr>
            <tr v-if="changelogs.length === 0">
              <td class="py-1 px-6 text-center" colspan="3">
                <div class="font-medium">No changes yet</div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
