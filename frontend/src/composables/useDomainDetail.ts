// The one domain-detail fetch body shared by DomainDetail and its
// CampaignDomain variant: fatal getDomain (not-found redirects), then the
// two non-fatal side surfaces (changelog + history). The host is a getter:
// vue-router reuses the component instance on param-only navigation
// (/domains/a → /domains/b), so the watcher — not onMounted — drives the
// fetch, and superseded/unmounted loads are aborted.
import { onScopeDispose, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import type { RouteLocationRaw } from 'vue-router'
import { getDomain, getDomainHistory } from '@/api'
import type { ChangelogItem, DomainDetail, HistoryPoint } from '@/api'
import { ApiProblem } from '@/api/problem'

export interface DomainDetailOptions {
  /** Where to land when the host does not exist. */
  notFoundRoute: (host: string) => RouteLocationRaw
  /** Which changelog scope backs the page (domain vs campaign-domain). */
  fetchChangelog: (host: string, signal: AbortSignal) => Promise<{ items: ChangelogItem[] }>
}

export function useDomainDetail(host: () => string, opts: DomainDetailOptions) {
  const router = useRouter()

  const domain = ref<DomainDetail | null>(null)
  const changelogs = ref<ChangelogItem[]>([])
  const history = ref<HistoryPoint[]>([])
  const error = ref<ApiProblem | null>(null)

  let controller: AbortController | null = null
  onScopeDispose(() => controller?.abort())

  async function load(h: string): Promise<void> {
    controller?.abort()
    const c = new AbortController()
    controller = c
    domain.value = null
    changelogs.value = []
    history.value = []
    error.value = null
    try {
      domain.value = await getDomain(h, c.signal)
    } catch (e) {
      if (c.signal.aborted) return
      if (e instanceof ApiProblem && e.code === 'not-found') {
        void router.replace(opts.notFoundRoute(h))
        return
      }
      error.value = ApiProblem.from(e)
      return
    }
    // Non-fatal side surfaces — an error just leaves them empty.
    opts
      .fetchChangelog(h, c.signal)
      .then((res) => {
        if (!c.signal.aborted) changelogs.value = res.items
      })
      .catch(() => {})
    getDomainHistory(h, undefined, c.signal)
      .then((res) => {
        if (!c.signal.aborted) history.value = res.points
      })
      .catch(() => {})
  }

  watch(
    host,
    (h) => {
      if (h) void load(h)
    },
    { immediate: true },
  )

  return { domain, changelogs, history, error }
}
