// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { useEntity } from '@/composables/useEntity'
import { ApiProblem } from '@/api/problem'

type Entity = ReturnType<typeof useEntity<string>>

function setup(
  key: () => string,
  fetch: (k: string, signal: AbortSignal) => Promise<string>,
  onNotFound?: (k: string) => void,
): Entity {
  let entity: Entity | undefined
  const Host = defineComponent({
    setup() {
      entity = useEntity(key, fetch, onNotFound ? { onNotFound } : {})
      return () => h('div')
    },
  })
  mount(Host)
  if (!entity) throw new Error('composable did not run')
  return entity
}

describe('useEntity', () => {
  it('loads immediately and reloads when the key changes', async () => {
    const key = ref('a')
    const fetch = vi.fn((k: string) => Promise.resolve(k.toUpperCase()))
    const entity = setup(() => key.value, fetch)
    await flushPromises()
    expect(entity.data.value).toBe('A')

    key.value = 'b'
    await flushPromises()
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(entity.data.value).toBe('B')
  })

  it('aborts a superseded fetch instead of racing it', async () => {
    const key = ref('slow')
    const signals: AbortSignal[] = []
    let resolveSlow: ((v: string) => void) | undefined
    const fetch = vi.fn((k: string, signal: AbortSignal) => {
      signals.push(signal)
      if (k === 'slow') {
        return new Promise<string>((resolve) => {
          resolveSlow = resolve
        })
      }
      return Promise.resolve('FAST')
    })
    const entity = setup(() => key.value, fetch)

    key.value = 'fast'
    await flushPromises()
    expect(signals[0]?.aborted).toBe(true)
    expect(entity.data.value).toBe('FAST')

    // The slow response lands late — it must not clobber the winner.
    resolveSlow?.('SLOW')
    await flushPromises()
    expect(entity.data.value).toBe('FAST')
  })

  it('captures not-found and runs the hook instead of erroring', async () => {
    const missing = new ApiProblem(
      { type: 'https://whynoipv6.com/problems/not-found', title: 'Missing', status: 404 },
      404,
    )
    const onNotFound = vi.fn()
    const entity = setup(
      () => 'gone',
      () => Promise.reject(missing),
      onNotFound,
    )
    await flushPromises()
    expect(entity.notFound.value).toBe(true)
    expect(entity.error.value).toBeNull()
    expect(onNotFound).toHaveBeenCalledWith('gone')
  })

  it('surfaces other failures as error state', async () => {
    const boom = new ApiProblem(
      { type: 'https://whynoipv6.com/problems/internal', title: 'Broken', status: 500 },
      500,
    )
    const entity = setup(
      () => 'x',
      () => Promise.reject(boom),
    )
    await flushPromises()
    expect(entity.error.value).toBe(boom)
    expect(entity.notFound.value).toBe(false)
    expect(entity.data.value).toBeNull()
  })
})
