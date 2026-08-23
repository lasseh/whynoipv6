// The one generic list-page engine (§9.1). The URL query is the single source
// of truth: next()/prev()/setFilter() only navigate; the route watcher is the
// sole fetch trigger, so back/forward and reload just work and there is no
// state↔URL feedback loop to guard.
import { computed, onScopeDispose, shallowRef, ref, watch } from 'vue'
import type { Ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { LocationQuery, LocationQueryValue } from 'vue-router'
import { ApiProblem } from '@/api/problem'
import { setPageNoindex } from '@/composables/usePageMeta'
import type { Meta, Page } from '@/api'

export interface ItemCollection<T> {
  items: T[]
  page: Page
  meta?: Meta
}

export interface FilterSpec {
  /** The closed vocabulary; omit for free-text keys (?q=). */
  values?: readonly string[]
  /** Coercion target for absent or out-of-set values; '' when omitted. */
  default?: string
}

export interface CursorListOptions<T, K extends string = never> {
  fetch: (
    params: { cursor?: string; [k: string]: string | undefined },
    signal: AbortSignal,
  ) => Promise<ItemCollection<T>>
  /**
   * Query keys synced alongside cursor, each coerced to its closed set —
   * the returned `filters` map is the ONE place the URL→value rule lives,
   * feeding both the fetch params and the page's tab state. Changing one
   * clears the cursor.
   */
  filters?: Record<K, FilterSpec>
  /**
   * The entity this list belongs to, when `fetch` closes over a path param
   * (/countries/:code, /campaigns/:uuid). vue-router reuses the instance on
   * param-only navigation, so without this the engine would keep serving the
   * previous entity's rows: a key change clears items/page/meta and refetches.
   *
   * It completes the trigger set. What makes this list reload is exactly
   * cursor, filters and key — all three owned here, none left to the caller.
   */
  key?: () => string | undefined
  /** Scrolled into view on next()/prev() so pagination lands at the list top. */
  anchor?: Ref<HTMLElement | null>
}

function first(v: LocationQueryValue | LocationQueryValue[] | undefined): string | undefined {
  const s = Array.isArray(v) ? v[0] : v
  return s == null || s === '' ? undefined : s
}

export function useCursorList<T, K extends string = never>(opts: CursorListOptions<T, K>) {
  const route = useRoute()
  const router = useRouter()
  const filterSpecs: Record<string, FilterSpec> = opts.filters ?? {}

  const items = shallowRef<T[]>([])
  const page = ref<Page | null>(null)
  const meta = ref<Meta | null>(null)
  const loading = ref(false)
  const error = ref<ApiProblem | null>(null)

  const cursor = computed(() => first(route.query.cursor))
  const key = computed(() => opts.key?.())
  const filters = computed(
    () =>
      Object.fromEntries(
        Object.entries(filterSpecs).map(([k, spec]) => {
          const raw = first(route.query[k])
          const valid = raw !== undefined && (!spec.values || spec.values.includes(raw))
          return [k, valid ? raw : (spec.default ?? '')]
        }),
      ) as Record<K, string>,
  )

  let controller: AbortController | null = null
  onScopeDispose(() => controller?.abort())

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
      // A list that failed to load renders an error card at HTTP 200, same
      // as the detail surfaces (useEntity) — not a page worth indexing.
      setPageNoindex()
      error.value = ApiProblem.from(e)
    } finally {
      if (controller === c) loading.value = false
    }
  }

  watch([cursor, filters], (next, prev) => {
    // filters is a fresh object each recompute; only reload on value change.
    if (JSON.stringify(next) !== JSON.stringify(prev)) void load()
  })

  // A key change is a different entity, not a different page of the same one:
  // drop what the previous entity loaded before refetching, so nothing paints
  // the old rows under the new heading. The falsy guard skips the fire on
  // route leave, when the param flips to '' before unmount.
  watch(key, (k) => {
    if (!k) return
    items.value = []
    page.value = null
    meta.value = null
    void load()
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

  return { items, page, meta, loading, error, filters, next, prev, setFilter, reload }
}
