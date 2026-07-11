// The one domain-detail fetch body shared by DomainDetail and its
// CampaignDomain variant: fatal getDomain (not-found redirects), then the
// two non-fatal side surfaces (changelog + history).
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import type { RouteLocationRaw } from 'vue-router'
import { getDomain, getDomainHistory } from '@/api'
import type { ChangelogItem, DomainDetail, HistoryPoint } from '@/api'
import { ApiProblem } from '@/api/problem'

export interface DomainDetailOptions {
  /** Where to land when the host does not exist. */
  notFoundRoute: RouteLocationRaw
  /** Which changelog scope backs the page (domain vs campaign-domain). */
  fetchChangelog: () => Promise<{ items: ChangelogItem[] }>
}

export function useDomainDetail(host: string, opts: DomainDetailOptions) {
  const router = useRouter()

  const domain = ref<DomainDetail | null>(null)
  const changelogs = ref<ChangelogItem[]>([])
  const history = ref<HistoryPoint[]>([])
  const error = ref<ApiProblem | null>(null)

  onMounted(async () => {
    try {
      domain.value = await getDomain(host)
    } catch (e) {
      if (e instanceof ApiProblem && e.code === 'not-found') {
        void router.replace(opts.notFoundRoute)
        return
      }
      error.value = ApiProblem.from(e)
      return
    }
    // Non-fatal side surfaces — an error just leaves them empty.
    opts
      .fetchChangelog()
      .then((res) => (changelogs.value = res.items))
      .catch(() => (changelogs.value = []))
    getDomainHistory(host)
      .then((res) => (history.value = res.points))
      .catch(() => (history.value = []))
  })

  return { domain, changelogs, history, error }
}
