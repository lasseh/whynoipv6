// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { useResource } from '@/composables/useResource'
import type { Resource } from '@/composables/useResource'
import { ApiProblem } from '@/api/problem'

// host mounts the composable and hands back both the resource and an
// unmount, so teardown behaviour is testable.
function host<T>(
  fetch: (signal: AbortSignal) => Promise<T>,
  opts?: { fallback?: T },
): { res: Resource<T>; unmount: () => void } {
  let res: Resource<T> | undefined
  const Host = defineComponent({
    setup() {
      res = useResource(fetch, opts ?? {})
      return () => h('div')
    },
  })
  const wrapper = mount(Host)
  if (!res) throw new Error('composable did not run')
  return { res, unmount: () => wrapper.unmount() }
}

describe('useResource', () => {
  it('loads once and settles loading', async () => {
    const fetch = vi.fn().mockResolvedValue(['a'])
    const { res } = host(fetch)

    expect(res.loading.value).toBe(true)
    await flushPromises()

    expect(fetch).toHaveBeenCalledTimes(1)
    expect(res.data.value).toEqual(['a'])
    expect(res.loading.value).toBe(false)
    expect(res.error.value).toBeNull()
  })

  // Without a fallback the failure is the caller's to render.
  it('surfaces a failure as a problem', async () => {
    const { res } = host(() => Promise.reject(new Error('boom')))
    await flushPromises()

    expect(res.error.value).toBeInstanceOf(ApiProblem)
    expect(res.data.value).toBeNull()
    expect(res.loading.value).toBe(false)
  })

  // With one, going quiet is non-fatal — the rule the metrics panels each
  // used to spell out in their own catch.
  it('installs the fallback instead of an error', async () => {
    const { res } = host(() => Promise.reject(new Error('boom')), { fallback: [] })
    await flushPromises()

    expect(res.data.value).toEqual([])
    expect(res.error.value).toBeNull()
  })

  it('aborts the in-flight request on teardown', () => {
    let seen: AbortSignal | undefined
    const { unmount } = host((signal) => {
      seen = signal
      return new Promise<string[]>(() => {}) // never settles
    })

    expect(seen?.aborted).toBe(false)
    unmount()
    expect(seen?.aborted).toBe(true)
  })

  // The invariant every hand-rolled copy re-typed: a response that lands
  // after teardown must not write anything.
  it('discards a late success', async () => {
    let release: ((v: string[]) => void) | undefined
    const { res, unmount } = host(
      () =>
        new Promise<string[]>((resolve) => {
          release = resolve
        }),
    )
    unmount()
    release?.(['late'])
    await flushPromises()

    expect(res.data.value).toBeNull()
  })

  it('discards a late failure, with and without a fallback', async () => {
    for (const opts of [undefined, { fallback: ['fb'] }]) {
      let reject: ((e: unknown) => void) | undefined
      const { res, unmount } = host<string[]>(
        () =>
          new Promise<string[]>((_, rej) => {
            reject = rej
          }),
        opts,
      )
      unmount()
      reject?.(new Error('late boom'))
      await flushPromises()

      expect(res.error.value).toBeNull()
      expect(res.data.value).toBeNull()
    }
  })
})
