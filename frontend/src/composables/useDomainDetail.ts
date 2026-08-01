// The one domain-detail fetch body shared by DomainDetail and its
// CampaignDomain variant: fatal getDomain (not-found redirects) through
// the useEntity lifecycle, then the two non-fatal side surfaces
// (changelog + history) riding the same abort signal.
import { shallowRef } from 'vue'
import { useRouter } from 'vue-router'
import type { RouteLocationRaw } from 'vue-router'
import { getDomain, getDomainHistory } from '@/api'
import type { ChangelogItem, HistoryPoint } from '@/api'
import { useEntity } from '@/composables/useEntity'

export interface DomainDetailOptions {
  /** Where to land when the host does not exist. */
  notFoundRoute: (host: string) => RouteLocationRaw
  /** Which changelog scope backs the page (domain vs campaign-domain). */
  fetchChangelog: (host: string, signal: AbortSignal) => Promise<{ items: ChangelogItem[] }>
}

export function useDomainDetail(host: () => string, opts: DomainDetailOptions) {
  const router = useRouter()

  const changelogs = shallowRef<ChangelogItem[]>([])
  const history = shallowRef<HistoryPoint[]>([])

  const { data: domain, error } = useEntity(
    host,
    async (h, signal) => {
      changelogs.value = []
      history.value = []
      const d = await getDomain(h, signal)
      // Non-fatal side surfaces — an error just leaves them empty.
      opts
        .fetchChangelog(h, signal)
        .then((res) => {
          if (!signal.aborted) changelogs.value = res.items
        })
        .catch(() => {})
      getDomainHistory(h, undefined, signal)
        .then((res) => {
          if (!signal.aborted) history.value = res.points
        })
        .catch(() => {})
      return d
    },
    { onNotFound: (h) => void router.replace(opts.notFoundRoute(h)) },
  )

  return { domain, changelogs, history, error }
}
