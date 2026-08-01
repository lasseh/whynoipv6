// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { Router } from 'vue-router'
import { useLiveCheck } from '@/composables/useLiveCheck'
import { ApiProblem } from '@/api/problem'
import type { CheckEnvelope } from '@/api'

vi.mock('@/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api')>()),
  createCheck: vi.fn(),
  getCheck: vi.fn(),
  getLatestCheck: vi.fn(),
}))
import { createCheck, getCheck, getLatestCheck } from '@/api'

type Machine = ReturnType<typeof useLiveCheck>

function envelope(over: Partial<CheckEnvelope> = {}): CheckEnvelope {
  return {
    id: 7,
    host: 'vg.no',
    status: 'done',
    cached: false,
    created_at: '2026-08-01T12:00:00Z',
    completed_at: '2026-08-01T12:01:00Z',
    error: null,
    result: null,
    confirmed: null,
    ...over,
  } as CheckEnvelope
}

async function setup(path = '/check'): Promise<{ router: Router; m: Machine }> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/check/:target?', name: 'LiveCheck', component: { template: '<div />' } }],
  })
  await router.push(path)
  await router.isReady()
  let m: Machine | undefined
  const Host = defineComponent({
    setup() {
      m = useLiveCheck()
      return () => h('div')
    },
  })
  mount(Host, { global: { plugins: [router] } })
  await flushPromises()
  if (!m) throw new Error('composable did not run')
  return { router, m }
}

describe('useLiveCheck', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('submits, polls to done, and reflects the canonical URL', async () => {
    vi.mocked(createCheck).mockResolvedValue({
      id: 7,
      host: 'vg.no',
      status: 'pending',
      created_at: '2026-08-01T12:00:00Z',
    })
    vi.mocked(getCheck)
      .mockResolvedValueOnce(envelope({ status: 'processing' }))
      .mockResolvedValueOnce(envelope({ status: 'done' }))
    const { router, m } = await setup()

    m.host.value = 'https://vg.no/some/path'
    void m.submit()
    await flushPromises()
    expect(m.host.value).toBe('vg.no') // cleaned into the input
    expect(m.running.value).toBe(true)
    expect(router.currentRoute.value.fullPath).toBe('/check/vg.no')

    await vi.advanceTimersByTimeAsync(2_000)
    expect(m.running.value).toBe(true) // still running
    await vi.advanceTimersByTimeAsync(2_000)
    expect(m.running.value).toBe(false)
    expect(m.envelope.value?.status).toBe('done')
  })

  it('serves a dedupe envelope without polling', async () => {
    vi.mocked(createCheck).mockResolvedValue(envelope({ cached: true }))
    const { m } = await setup()

    m.host.value = 'vg.no'
    void m.submit()
    await flushPromises()
    expect(m.running.value).toBe(false)
    expect(m.envelope.value?.cached).toBe(true)
    expect(getCheck).not.toHaveBeenCalled()
  })

  it('rate-limit starts the retry countdown and blocks resubmits', async () => {
    vi.mocked(createCheck).mockRejectedValue(
      new ApiProblem(
        {
          type: 'https://whynoipv6.com/problems/rate-limited',
          title: 'Rate limited',
          status: 429,
          retry_after: 3,
        },
        429,
      ),
    )
    const { m } = await setup()

    m.host.value = 'vg.no'
    void m.submit()
    await flushPromises()
    expect(m.problem.value?.code).toBe('rate-limited')
    expect(m.retryLeft.value).toBe(3)

    void m.submit() // blocked while the countdown runs
    expect(createCheck).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(3_000)
    expect(m.retryLeft.value).toBe(0)
  })

  it('cancel orphans the poll loop', async () => {
    vi.mocked(createCheck).mockResolvedValue({
      id: 7,
      host: 'vg.no',
      status: 'pending',
      created_at: '2026-08-01T12:00:00Z',
    })
    vi.mocked(getCheck).mockResolvedValue(envelope({ status: 'processing' }))
    const { m } = await setup()

    m.host.value = 'vg.no'
    void m.submit()
    await flushPromises()
    m.cancel()
    expect(m.running.value).toBe(false)

    await vi.advanceTimersByTimeAsync(10_000)
    expect(getCheck).not.toHaveBeenCalled() // the orphaned loop never fired
  })

  it('a /check/{domain} link loads the stored result inside the TTL', async () => {
    vi.mocked(getLatestCheck).mockResolvedValue(envelope({ cached: true }))
    const { m } = await setup('/check/vg.no')
    await flushPromises()

    expect(getLatestCheck).toHaveBeenCalledWith('vg.no', expect.anything())
    expect(m.envelope.value?.cached).toBe(true)
    expect(m.host.value).toBe('vg.no')
    expect(m.running.value).toBe(false)
  })

  it('a stored-result miss falls through to a fresh check', async () => {
    vi.mocked(getLatestCheck).mockRejectedValue(
      new ApiProblem(
        { type: 'https://whynoipv6.com/problems/not-found', title: 'Missing', status: 404 },
        404,
      ),
    )
    vi.mocked(createCheck).mockResolvedValue(envelope({ cached: true }))
    const { m } = await setup('/check/vg.no')
    await flushPromises()

    expect(createCheck).toHaveBeenCalledWith('vg.no', expect.anything())
    expect(m.envelope.value?.cached).toBe(true)
  })

  it('a legacy numeric link upgrades to the domain URL', async () => {
    vi.mocked(getCheck).mockResolvedValue(envelope({ status: 'done' }))
    const { router, m } = await setup('/check/7')
    await flushPromises()

    expect(getCheck).toHaveBeenCalledWith(7, expect.anything())
    expect(m.envelope.value?.status).toBe('done')
    expect(router.currentRoute.value.fullPath).toBe('/check/vg.no')
  })
})
