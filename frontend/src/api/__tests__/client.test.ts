import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get, post } from '@/api/client'
import { ApiProblem } from '@/api/problem'

// The vendored typed-fetch wrapper: URL construction, envelope handling, and
// failure mapping — the seam every page depends on.

const fetchMock = vi.fn()

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockReset()
})
afterEach(() => {
  vi.unstubAllGlobals()
})

function requestedURL(): string {
  return fetchMock.mock.calls[0]?.[0] as string
}

describe('get', () => {
  it('percent-encodes path params so a hostile host cannot change the route', async () => {
    fetchMock.mockResolvedValue(jsonResponse({}))
    await get('/domains/{host}', { path: { host: 'a/b?c#d' } })
    expect(requestedURL()).toContain('/domains/a%2Fb%3Fc%23d')
  })

  it('throws loudly when a template key has no param instead of emitting "undefined"', async () => {
    await expect(get('/domains/{host}', { path: {} as never })).rejects.toThrow(
      'api: missing path param "host"',
    )
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('builds the query string and strips empty members', async () => {
    fetchMock.mockResolvedValue(jsonResponse({}))
    await get('/domains', {
      query: { q: 'example', cursor: undefined, filter: '' } as never,
    })
    const url = requestedURL()
    expect(url).toContain('?q=example')
    expect(url).not.toContain('cursor')
    expect(url).not.toContain('filter')
  })

  it('returns the parsed success envelope', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ items: [{ host: 'vg.no' }] }))
    const out = await get('/sinners', {})
    expect(out).toEqual({ items: [{ host: 'vg.no' }] })
  })

  it('maps a non-2xx problem body to ApiProblem', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ type: 'https://x/problems/not-found', title: 'Not found' }, 404),
    )
    const err = await get('/domains/{host}', { path: { host: 'gone.example' } }).catch(
      (e: unknown) => e,
    )
    expect(err).toBeInstanceOf(ApiProblem)
    expect((err as ApiProblem).code).toBe('not-found')
  })

  it('always sends a signal so no request can outlive the 15 s timeout', async () => {
    fetchMock.mockResolvedValue(jsonResponse({}))
    await get('/sinners', {})
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit
    expect(init.signal).toBeInstanceOf(AbortSignal)
  })

  it('joins a caller signal with the timeout', async () => {
    fetchMock.mockResolvedValue(jsonResponse({}))
    const controller = new AbortController()
    controller.abort()
    await get('/sinners', { signal: controller.signal })
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit
    expect(init.signal?.aborted).toBe(true)
  })
})

describe('post', () => {
  it('sends the JSON body with the content type', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ id: 7 }, 202))
    await post('/check', { host: 'vg.no' })
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain('/check')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ host: 'vg.no' }))
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
  })
})
