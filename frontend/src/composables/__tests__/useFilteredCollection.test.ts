import { describe, expect, it } from 'vitest'
import { effectScope, nextTick } from 'vue'
import { useFilteredCollection } from '@/composables/useFilteredCollection'
import { ApiProblem } from '@/api/problem'

// The bounded-collection engine: one abortable fetch, a client-side filter,
// and a loading state that separates "in flight" from "no matches".

interface Row {
  name: string
}

function inScope<T>(fn: () => T): { result: T; stop: () => void } {
  const scope = effectScope()
  const result = scope.run(fn)!
  return { result, stop: () => scope.stop() }
}

async function settle(): Promise<void> {
  await Promise.resolve()
  await Promise.resolve()
  await nextTick()
}

describe('useFilteredCollection', () => {
  it('loads the collection once and clears loading', async () => {
    const { result } = inScope(() =>
      useFilteredCollection<Row>(
        () => Promise.resolve({ items: [{ name: 'Norway' }, { name: 'Sweden' }] }),
        (r) => r.name,
      ),
    )
    expect(result.loading.value).toBe(true)
    await settle()
    expect(result.loading.value).toBe(false)
    expect(result.items.value).toHaveLength(2)
    expect(result.error.value).toBeNull()
  })

  it('filters by trimmed, case-insensitive substring', async () => {
    const { result } = inScope(() =>
      useFilteredCollection<Row>(
        () => Promise.resolve({ items: [{ name: 'Norway' }, { name: 'Sweden' }] }),
        (r) => r.name,
      ),
    )
    await settle()
    result.query.value = '  NOR '
    expect(result.filtered.value).toEqual([{ name: 'Norway' }])
    result.query.value = ''
    expect(result.filtered.value).toHaveLength(2)
  })

  it('maps a failed fetch to an ApiProblem and still clears loading', async () => {
    const { result } = inScope(() =>
      useFilteredCollection<Row>(
        () => Promise.reject(new TypeError('network down')),
        (r) => r.name,
      ),
    )
    await settle()
    expect(result.loading.value).toBe(false)
    expect(result.error.value).toBeInstanceOf(ApiProblem)
  })

  it('aborts the fetch on scope dispose and discards the late failure', async () => {
    let seen: AbortSignal | undefined
    const { result, stop } = inScope(() =>
      useFilteredCollection<Row>(
        (signal) => {
          seen = signal
          return new Promise((_, reject) => {
            signal.addEventListener('abort', () => reject(new Error('aborted')))
          })
        },
        (r) => r.name,
      ),
    )
    stop()
    expect(seen?.aborted).toBe(true)
    await settle()
    // The abort rejection must not surface as a user-facing error.
    expect(result.error.value).toBeNull()
  })
})
