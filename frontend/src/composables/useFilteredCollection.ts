// The bounded-collection page engine (countries, campaigns): one fetch on
// mount — aborted on unmount — plus a client-side substring filter and a
// loading state, so an in-flight first load and "no matches" stop
// rendering as the same empty grid.
import { computed, onScopeDispose, ref, shallowRef } from 'vue'
import { ApiProblem } from '@/api/problem'

export function useFilteredCollection<T>(
  fetch: (signal: AbortSignal) => Promise<{ items: T[] }>,
  keyOf: (item: T) => string,
) {
  const items = shallowRef<T[]>([])
  const query = ref('')
  const loading = ref(true)
  const error = shallowRef<ApiProblem | null>(null)

  const controller = new AbortController()
  onScopeDispose(() => controller.abort())

  fetch(controller.signal)
    .then((res) => {
      items.value = res.items
    })
    .catch((e: unknown) => {
      if (!controller.signal.aborted) error.value = ApiProblem.from(e)
    })
    .finally(() => {
      loading.value = false
    })

  const filtered = computed(() => {
    const q = query.value.trim().toLowerCase()
    if (!q) return items.value
    return items.value.filter((item) => keyOf(item).toLowerCase().includes(q))
  })

  return { items, filtered, query, loading, error }
}
