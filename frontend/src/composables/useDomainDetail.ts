// The one domain-detail fetch body shared by DomainDetail and its
// CampaignDomain variant: fatal getDomain (not-found redirects) through
// the useEntity lifecycle, then the three non-fatal side surfaces
// (changelog, history, subdomains) riding the same abort signal.
import { shallowRef } from 'vue'
import { useRouter } from 'vue-router'
import type { RouteLocationRaw } from 'vue-router'
import { getDomain, getDomainHistory, listSubdomains } from '@/api'
import type { ChangelogItem, DomainSummary, HistoryPoint } from '@/api'
import { useEntity } from '@/composables/useEntity'

// One page of children is all the card shows; a domain with more says so
// rather than paginating a side surface.
export const SUBDOMAIN_PAGE_SIZE = 100

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
  const subdomains = shallowRef<DomainSummary[]>([])

  const { data: domain, error } = useEntity(
    host,
    async (h, signal) => {
      changelogs.value = []
      history.value = []
      subdomains.value = []
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
      // The detail already carries the count, so the vast majority of domains
      // (which have no children) never make this call.
      if (d.subdomain_count > 0) {
        listSubdomains(h, { limit: SUBDOMAIN_PAGE_SIZE }, signal)
          .then((res) => {
            if (!signal.aborted) subdomains.value = res.items
          })
          .catch(() => {})
      }
      return d
    },
    { onNotFound: (h) => void router.replace(opts.notFoundRoute(h)) },
  )

  return { domain, changelogs, history, subdomains, error }
}
