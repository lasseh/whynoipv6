// The one entity-fetch lifecycle shared by every detail surface: abort on
// supersede/unmount, not-found capture, and param-only re-navigation via
// the key watcher (vue-router reuses the component instance, so the
// watcher — not onMounted — drives the fetch). useDomainDetail composes
// it; the country/campaign pages consume it directly.
import { onScopeDispose, shallowRef, watch } from 'vue'
import { ApiProblem } from '@/api/problem'
import { setPageNoindex } from '@/composables/usePageMeta'

export interface EntityOptions {
  /** Runs on a not-found problem (e.g. a redirect); notFound is set either way. */
  onNotFound?: (key: string) => void
}

export function useEntity<T>(
  key: () => string,
  fetch: (key: string, signal: AbortSignal) => Promise<T>,
  opts: EntityOptions = {},
) {
  const data = shallowRef<T | null>(null)
  const notFound = shallowRef(false)
  const error = shallowRef<ApiProblem | null>(null)

  let controller: AbortController | null = null
  onScopeDispose(() => controller?.abort())

  async function load(k: string): Promise<void> {
    controller?.abort()
    const c = new AbortController()
    controller = c
    data.value = null
    notFound.value = false
    error.value = null
    try {
      const result = await fetch(k, c.signal)
      if (c.signal.aborted) return
      data.value = result
    } catch (e) {
      if (c.signal.aborted) return
      // Both branches render an explanation at HTTP 200 — a missing country
      // and a failed fetch look the same to a crawler, and Google files them
      // as soft 404s. Marking it here rather than per page is what keeps a
      // new detail surface from quietly becoming indexable.
      setPageNoindex()
      if (e instanceof ApiProblem && e.code === 'not-found') {
        notFound.value = true
        opts.onNotFound?.(k)
        return
      }
      error.value = ApiProblem.from(e)
    }
  }

  watch(
    key,
    (k) => {
      if (k) void load(k)
    },
    { immediate: true },
  )

  /** Re-run the current fetch in place. */
  function reload(): void {
    void load(key())
  }

  return { data, notFound, error, reload }
}
