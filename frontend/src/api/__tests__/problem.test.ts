import { describe, expect, it } from 'vitest'
import { ApiProblem } from '@/api/problem'

describe('ApiProblem', () => {
  it('falls back to the HTTP status when the body carries no title', () => {
    const p = new ApiProblem({}, 502)
    expect(p.title).toBe('HTTP 502')
    expect(p.status).toBe(502)
    expect(p.type).toBe('about:blank')
    expect(p.detail).toBeNull()
    expect(p.retryAfter).toBeNull()
  })

  it('composes the Error message from title and detail', () => {
    const p = new ApiProblem({ title: 'Invalid', detail: 'host is not a domain' }, 400)
    expect(p.message).toBe('Invalid: host is not a domain')
    expect(new ApiProblem({ title: 'Invalid' }, 400).message).toBe('Invalid')
  })

  it('derives code from the type-URI tail', () => {
    const p = new ApiProblem({ type: 'https://whynoipv6.com/problems/not-found' }, 404)
    expect(p.code).toBe('not-found')
    // A trailing slash leaves an empty tail; that must not read as a real code.
    expect(new ApiProblem({ type: 'https://x/' }, 404).code).toBe('unknown')
  })

  it('carries retry_after through for rate limiting', () => {
    const p = new ApiProblem({ type: 'https://x/problems/rate-limited', retry_after: 42 }, 429)
    expect(p.code).toBe('rate-limited')
    expect(p.retryAfter).toBe(42)
  })

  it('parses a problem+json response body', async () => {
    const res = new Response(
      JSON.stringify({ type: 'https://x/problems/not-found', title: 'Not found', status: 404 }),
      { status: 404, statusText: 'Not Found' },
    )
    const p = await ApiProblem.fromResponse(res)
    expect(p.code).toBe('not-found')
    expect(p.title).toBe('Not found')
    expect(p.status).toBe(404)
  })

  it('survives a non-JSON error body via the status text', async () => {
    const res = new Response('<html>gateway error</html>', {
      status: 502,
      statusText: 'Bad Gateway',
    })
    const p = await ApiProblem.fromResponse(res)
    expect(p.title).toBe('Bad Gateway')
    expect(p.status).toBe(502)
  })

  it('wraps arbitrary failures and passes existing problems through', () => {
    const original = new ApiProblem({ title: 'Invalid' }, 400)
    expect(ApiProblem.from(original)).toBe(original)
    const wrapped = ApiProblem.from(new TypeError('fetch failed'))
    expect(wrapped).toBeInstanceOf(ApiProblem)
    expect(wrapped.title).toBe('Request failed')
    expect(wrapped.status).toBe(0)
  })
})
