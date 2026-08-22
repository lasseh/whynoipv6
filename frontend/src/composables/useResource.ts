// The one-shot fetch engine: load once on setup, abort on scope teardown,
// and never write state after teardown. It is the fetch-and-abort half that
// useFilteredCollection already implemented and every panel then re-typed.
//
// One-shot only — deliberately not the superseding engines. useCursorList,
// useEntity, useLiveCheck and useDomainDetail re-fetch the same surface as
// inputs change and must drop a *late* response in favour of a newer one;
// that currency rule is a different behaviour, and each of them already
// tests its own race. This module is for the loads that happen once.
//
// Failure is stated at the interface rather than left to each caller: pass
// `fallback` for a surface whose going quiet must not blank its neighbours
// (the metrics panels), omit it to surface the problem in `error`.
import { onScopeDispose, ref, shallowRef } from 'vue'
import type { Ref, ShallowRef } from 'vue'
import { ApiProblem } from '@/api/problem'

export interface ResourceOptions<T> {
  /**
   * Installed instead of an error when the fetch fails. Present = failures
   * are non-fatal for this surface; absent = failures populate `error`.
   */
  fallback?: T
}

export interface Resource<T> {
  /** The loaded value; null until the first successful load. */
  data: ShallowRef<T | null>
  /** True until the one load settles (aborted loads settle too). */
  loading: Ref<boolean>
  /** Populated on failure unless a fallback was supplied. */
  error: ShallowRef<ApiProblem | null>
}

export function useResource<T>(
  fetch: (signal: AbortSignal) => Promise<T>,
  opts: ResourceOptions<T> = {},
): Resource<T> {
  const data = shallowRef<T | null>(null)
  const loading = ref(true)
  const error = shallowRef<ApiProblem | null>(null)

  const controller = new AbortController()
  onScopeDispose(() => controller.abort())

  fetch(controller.signal)
    .then((res) => {
      if (controller.signal.aborted) return
      data.value = res
    })
    .catch((e: unknown) => {
      // A late failure after teardown is discarded: the scope that would
      // have rendered it is gone.
      if (controller.signal.aborted) return
      if ('fallback' in opts) data.value = opts.fallback ?? null
      else error.value = ApiProblem.from(e)
    })
    .finally(() => {
      loading.value = false
    })

  return { data, loading, error }
}
