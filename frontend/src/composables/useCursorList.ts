// The one generic list-page engine (§9.1). The URL query is the single source
// of truth: next()/prev()/setFilter() only navigate; the route watcher is the
// sole fetch trigger, so back/forward and reload just work and there is no
// state↔URL feedback loop to guard.
import { computed, shallowRef, ref, watch } from 'vue'
import type { Ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { LocationQuery, LocationQueryValue } from 'vue-router'
import { ApiProblem } from '@/api/problem'
import type { Meta, Page } from '@/api'

export interface ItemCollection<T> {
  items: T[]
  page: Page
  meta?: Meta
}

export interface CursorListOptions<T> {
  fetch: (
    params: { cursor?: string; [k: string]: string | undefined },
    signal: AbortSignal,
  ) => Promise<ItemCollection<T>>
  /** Query keys (e.g. ['filter']) synced alongside cursor; changing one clears the cursor. */
  filterKeys?: string[]
  /** Scrolled into view on next()/prev() so pagination lands at the list top. */
  anchor?: Ref<HTMLElement | null>
}

function first(v: LocationQueryValue | LocationQueryValue[] | undefined): string | undefined {
  const s = Array.isArray(v) ? v[0] : v
  return s == null || s === '' ? undefined : s
}

export function useCursorList<T>(opts: CursorListOptions<T>) {
  const route = useRoute()
  const router = useRouter()
  const filterKeys = opts.filterKeys ?? []

  const items = shallowRef<T[]>([])
  const page = ref<Page | null>(null)
  const meta = ref<Meta | null>(null)
  const loading = ref(false)
  const error = ref<ApiProblem | null>(null)

  const cursor = computed(() => first(route.query.cursor))
  const filters = computed(() =>
    Object.fromEntries(filterKeys.map((k) => [k, first(route.query[k])])),
  )

  let controller: AbortController | null = null

  async function load(): Promise<void> {
    controller?.abort()
    const c = new AbortController()
    controller = c
    loading.value = true
    error.value = null
    try {
      const params: { cursor?: string; [k: string]: string | undefined } = { ...filters.value }
      if (cursor.value !== undefined) params.cursor = cursor.value
      const res = await opts.fetch(params, c.signal)
      if (c.signal.aborted) return
      items.value = res.items
      page.value = res.page
      meta.value = res.meta ?? null
    } catch (e) {
      if (c.signal.aborted) return
      if (e instanceof ApiProblem && e.code === 'invalid-parameter' && cursor.value) {
        // Stale/foreign cursor (07 §3.2) — silently reset to page 1.
        void router.replace({ query: withoutCursor(route.query) })
        return
      }
      error.value = ApiProblem.from(e)
    } finally {
      if (controller === c) loading.value = false
    }
  }

  watch([cursor, filters], (next, prev) => {
    // filters is a fresh object each recompute; only reload on value change.
    if (JSON.stringify(next) !== JSON.stringify(prev)) void load()
  })
  void load()

  function withoutCursor(query: LocationQuery): LocationQuery {
    const rest = { ...query }
    delete rest.cursor
    return rest
  }

  function next(): void {
    opts.anchor?.value?.scrollIntoView({ behavior: 'auto' })
    const target = page.value?.has_more ? page.value.next_cursor : null
    if (target) void router.push({ query: { ...route.query, cursor: target } })
  }

  function prev(): void {
    opts.anchor?.value?.scrollIntoView({ behavior: 'auto' })
    const target = page.value?.prev_cursor
    if (target) void router.push({ query: { ...route.query, cursor: target } })
  }

  function setFilter(key: string, value: string | undefined): void {
    const query = withoutCursor(route.query)
    if (value === undefined) delete query[key]
    else query[key] = value
    void router.push({ query })
  }

  /** Re-run the current fetch in place (e.g. the changelog's 30 s auto-refresh). */
  function reload(): void {
    void load()
  }

  return { items, page, meta, loading, error, next, prev, setFilter, reload }
}
