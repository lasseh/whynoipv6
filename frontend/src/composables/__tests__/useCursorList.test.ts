// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import type { Ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { Router } from 'vue-router'
import { useCursorList } from '@/composables/useCursorList'
import type { CursorListOptions, FilterSpec, ItemCollection } from '@/composables/useCursorList'
import { ApiProblem } from '@/api/problem'
import type { Meta, Page } from '@/api'

type List = ReturnType<typeof useCursorList<string>>

const meta: Meta = { as_of: '2026-07-11T00:00:00Z', generation: 20260711, license: 'CC-BY-NC-4.0' }

function makePage(over: Partial<Page> = {}): Page {
  return { next_cursor: null, prev_cursor: null, has_more: false, ...over }
}

function collection(items: string[], page: Partial<Page> = {}): ItemCollection<string> {
  return { items, page: makePage(page), meta }
}

const tierFilters: Record<string, FilterSpec> = {
  filter: { values: ['sinners', 'heroes'], default: 'sinners' },
}

async function setup(
  fetch: CursorListOptions<string>['fetch'],
  filters?: Record<string, FilterSpec>,
  key?: Ref<string>,
): Promise<{ router: Router; list: List }> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }],
  })
  await router.push('/')
  await router.isReady()
  let list: List | undefined
  const Host = defineComponent({
    setup() {
      list = useCursorList<string>({
        fetch,
        ...(filters && { filters }),
        ...(key && { key: () => key.value }),
      })
      return () => h('div')
    },
  })
  mount(Host, { global: { plugins: [router] } })
  await flushPromises()
  if (!list) throw new Error('composable did not run')
  return { router, list }
}

// setupKeyed drives the `key` option from a ref, the way a page drives it
// from a route param.
async function setupKeyed(
  fetch: CursorListOptions<string>['fetch'],
  key: Ref<string>,
): Promise<{ list: List }> {
  return setup(fetch, undefined, key)
}

describe('useCursorList', () => {
  it('loads page 1 and maps the page block onto next/prev', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(collection(['a'], { next_cursor: 'c2', has_more: true }))
    const { router, list } = await setup(fetch)

    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch.mock.calls[0]?.[0]).toEqual({})
    expect(list.items.value).toEqual(['a'])
    expect(list.meta.value).toEqual(meta)

    list.next()
    await flushPromises()
    expect(router.currentRoute.value.query.cursor).toBe('c2')
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(fetch.mock.calls[1]?.[0]).toEqual({ cursor: 'c2' })
  })

  it('next is a no-op without has_more; prev is a no-op without prev_cursor', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(collection(['a'], { next_cursor: 'x', has_more: false }))
    const { router, list } = await setup(fetch)

    list.next()
    list.prev()
    await flushPromises()
    expect(router.currentRoute.value.query.cursor).toBeUndefined()
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('changing a filter clears the cursor', async () => {
    const fetch = vi.fn().mockResolvedValue(collection([]))
    const { router, list } = await setup(fetch, tierFilters)

    await router.push({ query: { filter: 'heroes', cursor: 'c9' } })
    await flushPromises()
    expect(fetch.mock.calls.at(-1)?.[0]).toEqual({ filter: 'heroes', cursor: 'c9' })

    list.setFilter('filter', 'sinners')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ filter: 'sinners' })
    expect(fetch.mock.calls.at(-1)?.[0]).toEqual({ filter: 'sinners' })
  })

  it('coerces the filter to its closed set — one rule for fetch AND tabs', async () => {
    const fetch = vi.fn().mockResolvedValue(collection([]))
    const { router, list } = await setup(fetch, tierFilters)

    // Absent → default, on both surfaces.
    expect(fetch.mock.calls.at(-1)?.[0]).toEqual({ filter: 'sinners' })
    expect(list.filters.value).toEqual({ filter: 'sinners' })

    // Garbage → default; a coerced-equal value change does not refetch.
    const calls = fetch.mock.calls.length
    await router.push({ query: { filter: 'zzz' } })
    await flushPromises()
    expect(list.filters.value).toEqual({ filter: 'sinners' })
    expect(fetch).toHaveBeenCalledTimes(calls)

    // A real member of the set flows through.
    await router.push({ query: { filter: 'heroes' } })
    await flushPromises()
    expect(list.filters.value).toEqual({ filter: 'heroes' })
    expect(fetch.mock.calls.at(-1)?.[0]).toEqual({ filter: 'heroes' })
  })

  it('silently resets to page 1 on a stale-cursor 400', async () => {
    const stale = new ApiProblem(
      { type: 'https://whynoipv6.com/problems/invalid-parameter', title: 'Invalid', status: 400 },
      400,
    )
    const fetch = vi.fn((params: { cursor?: string }) => {
      if (params.cursor) return Promise.reject(stale)
      return Promise.resolve(collection(['fresh']))
    })
    const { router, list } = await setup(fetch)

    await router.push({ query: { cursor: 'stale' } })
    await flushPromises()

    expect(router.currentRoute.value.query.cursor).toBeUndefined()
    expect(list.error.value).toBeNull()
    expect(list.items.value).toEqual(['fresh'])
  })

  it('aborts a superseded fetch instead of racing it', async () => {
    const signals: AbortSignal[] = []
    let resolveFirst: ((c: ItemCollection<string>) => void) | undefined
    const fetch = vi.fn((params: { cursor?: string }, signal: AbortSignal) => {
      signals.push(signal)
      if (!params.cursor) {
        return new Promise<ItemCollection<string>>((resolve) => {
          resolveFirst = resolve
        })
      }
      return Promise.resolve(collection(['second']))
    })
    const { router, list } = await setup(fetch)

    await router.push({ query: { cursor: 'c2' } })
    await flushPromises()
    expect(signals[0]?.aborted).toBe(true)
    expect(list.items.value).toEqual(['second'])

    // The slow first response lands late — it must not clobber the second.
    resolveFirst?.(collection(['first']))
    await flushPromises()
    expect(list.items.value).toEqual(['second'])
    expect(list.loading.value).toBe(false)
  })

  it('surfaces non-cursor failures as error state', async () => {
    const boom = new ApiProblem(
      { type: 'https://whynoipv6.com/problems/internal', title: 'Broken', status: 500 },
      500,
    )
    const fetch = vi.fn().mockRejectedValue(boom)
    const { list } = await setup(fetch)

    expect(list.error.value).toBe(boom)
    expect(list.loading.value).toBe(false)
    expect(list.items.value).toEqual([])
  })
  // The trigger set is cursor, filters AND key. Before `key` existed this
  // was unreachable: each page hand-wrote its own watch(param, reload), and
  // neither page test ever re-navigated, so nothing asserted it.
  it('refetches when the entity key changes', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(collection(['no-1', 'no-2']))
      .mockResolvedValueOnce(collection(['se-1']))
    const key = ref('NO')
    const { list } = await setupKeyed(fetch, key)

    expect(fetch).toHaveBeenCalledTimes(1)
    expect(list.items.value).toEqual(['no-1', 'no-2'])

    key.value = 'SE'
    await flushPromises()

    expect(fetch).toHaveBeenCalledTimes(2)
    expect(list.items.value).toEqual(['se-1'])
  })

  // A keyed reload must not paint the previous entity's rows under the new
  // heading, so items/page/meta are cleared before the refetch resolves.
  it('clears the previous entity before the new one arrives', async () => {
    let release: ((c: ItemCollection<string>) => void) | undefined
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(collection(['no-1'], { next_cursor: 'c2', has_more: true }))
      .mockImplementationOnce(
        () =>
          new Promise<ItemCollection<string>>((resolve) => {
            release = resolve
          }),
      )
    const key = ref('NO')
    const { list } = await setupKeyed(fetch, key)
    expect(list.items.value).toEqual(['no-1'])
    expect(list.page.value?.has_more).toBe(true)

    key.value = 'SE'
    await flushPromises()

    // In flight: nothing from the old entity is still on screen.
    expect(list.items.value).toEqual([])
    expect(list.page.value).toBeNull()
    expect(list.meta.value).toBeNull()

    release?.(collection(['se-1']))
    await flushPromises()
    expect(list.items.value).toEqual(['se-1'])
  })

  // Route leave flips the param to '' before unmount; that must not fire a
  // fetch for an entity that no longer exists.
  it('ignores an empty key on route leave', async () => {
    const fetch = vi.fn().mockResolvedValue(collection(['a']))
    const key = ref('NO')
    await setupKeyed(fetch, key)
    expect(fetch).toHaveBeenCalledTimes(1)

    key.value = ''
    await flushPromises()
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})
