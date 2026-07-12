<script setup lang="ts">
import { computed } from 'vue'

import type { ChangelogItem } from '@/api'
import { changelogParts } from '@/utils/changelog'
import { formatDate, formatDateTime, formatTime } from '@/utils/date'

// Structured rows (§7.4): the phrase + dot color derive client-side from
// (field, old_value, new_value) via utils/changelog — never a server string.
// One feed layout for all widths: time · status dot · host link · phrase.
const props = withDefaults(
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

// Rows grouped under calendar-day headings, preserving API order (newest first).
const groups = computed(() => {
  const today = formatDate(new Date())
  const yesterday = formatDate(new Date(Date.now() - 86_400_000))
  const out: { label: string; rows: ChangelogItem[] }[] = []
  for (const row of props.changelogs) {
    const day = formatDate(row.ts)
    const label = day === today ? 'Today' : day === yesterday ? 'Yesterday' : day
    const last = out[out.length - 1]
    if (last?.label === label) last.rows.push(row)
    else out.push({ label, rows: [row] })
  }
  return out
})
</script>

<template>
  <div class="max-w-6xl mx-auto px-4 sm:px-6">
    <header v-if="header" class="mb-4">
      <div class="text-left">
        <h1 class="h3">{{ header }}</h1>
      </div>
    </header>

    <template v-if="changelogs.length > 0">
      <section v-for="group in groups" :key="group.label" class="mb-6">
        <h2
          class="text-xs font-semibold uppercase tracking-wider text-gray-500 pb-2 border-b border-slate-700"
        >
          {{ group.label }}
        </h2>
        <ul class="divide-y divide-slate-800">
          <li
            v-for="row in group.rows"
            :key="`${row.ts}-${row.host}-${row.field}`"
            class="flex items-start gap-3 py-2.5 font-mono text-xs"
          >
            <time
              class="text-gray-500 tabular-nums shrink-0"
              :datetime="row.ts"
              :title="formatDateTime(row.ts)"
            >
              {{ formatTime(row.ts) }}
            </time>
            <span
              class="size-2 rounded-full shrink-0 mt-1"
              :class="changelogParts(row).dotClass"
              aria-hidden="true"
            ></span>
            <p class="text-slate-300 min-w-0">
              <router-link
                class="text-fuchsia-400 hover:underline focus-visible:underline"
                :to="domainRoute(row.host)"
                >{{ row.host }}</router-link
              >
              {{ changelogParts(row).phrase }}
            </p>
          </li>
        </ul>
      </section>
    </template>
    <div v-else class="text-center py-8 text-gray-400">No changes yet</div>
  </div>
</template>
