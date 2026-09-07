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
  getDomain: vi.fn(),
  getLatestCheck: vi.fn(),
}))
import { createCheck, getCheck, getDomain, getLatestCheck } from '@/api'
import type { DomainDetail } from '@/api'

type Machine = ReturnType<typeof useLiveCheck>

/** Only the two fields the tracked-domain guard reads. */
function domainRow(over: Partial<DomainDetail> = {}): DomainDetail {
  return { host: 'vg.no', rank: 42, disabled: false, ...over } as DomainDetail
}

/** The guard's fall-through: nothing tracked, so a check runs. */
function untracked() {
  vi.mocked(getDomain).mockRejectedValue(
    new ApiProblem(
      { type: 'https://whynoipv6.com/problems/not-found', title: 'Missing', status: 404 },
      404,
    ),
  )
}

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
  }
}

async function setup(path = '/check'): Promise<{ router: Router; m: Machine }> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/check/:target?', name: 'LiveCheck', component: { template: '<div />' } },
      {
        path: '/domains/:domain([^/]+)',
        name: 'DomainDetail',
        component: { template: '<div />' },
      },
    ],
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
    untracked() // the guard is off the path unless a test opts in
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

// A host we crawl daily costs a full engine run to re-derive what its detail
// page already shows, so the submit path resolves it first and redirects.
describe('useLiveCheck tracked-domain guard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    untracked()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('redirects a ranked domain to its page instead of checking it', async () => {
    vi.mocked(getDomain).mockResolvedValue(domainRow())
    const { router, m } = await setup()

    m.host.value = 'vg.no'
    void m.submit()
    await flushPromises()

    expect(createCheck).not.toHaveBeenCalled()
    expect(router.currentRoute.value.fullPath).toBe('/domains/vg.no?from=check')
    expect(m.running.value).toBe(false)
  })

  it('redirects to the canonical host the API returned, not the typed one', async () => {
    vi.mocked(getDomain).mockResolvedValue(domainRow({ host: 'vg.no' }))
    const { router, m } = await setup()

    m.host.value = 'VG.no.'
    void m.submit()
    await flushPromises()

    expect(router.currentRoute.value.fullPath).toBe('/domains/vg.no?from=check')
  })

  it('checks a rank-NULL host — it is in the table, not in the list', async () => {
    vi.mocked(getDomain).mockResolvedValue(domainRow({ rank: null }))
    vi.mocked(createCheck).mockResolvedValue(envelope({ cached: true }))
    const { m } = await setup()

    m.host.value = 'vg.no'
    void m.submit()
    await flushPromises()

    expect(createCheck).toHaveBeenCalledWith('vg.no', expect.anything())
  })

  it('checks a disabled domain rather than landing on its stale page', async () => {
    vi.mocked(getDomain).mockResolvedValue(domainRow({ disabled: true }))
    vi.mocked(createCheck).mockResolvedValue(envelope({ cached: true }))
    const { m } = await setup()

    m.host.value = 'vg.no'
    void m.submit()
    await flushPromises()

    expect(createCheck).toHaveBeenCalledWith('vg.no', expect.anything())
  })

  it('fails open when the domain lookup errors', async () => {
    vi.mocked(getDomain).mockRejectedValue(new TypeError('network down'))
    vi.mocked(createCheck).mockResolvedValue(envelope({ cached: true }))
    const { m } = await setup()

    m.host.value = 'vg.no'
    void m.submit()
    await flushPromises()

    expect(createCheck).toHaveBeenCalledWith('vg.no', expect.anything())
    expect(m.problem.value).toBeNull()
  })

  // A pasted URL reduces to www.<host>, which is its own rank-NULL row — the
  // apex behind it is the one we crawl daily.
  it('falls back to the apex when a www host is not itself ranked', async () => {
    vi.mocked(getDomain).mockImplementation((h) =>
      h === 'vg.no'
        ? Promise.resolve(domainRow())
        : Promise.resolve(domainRow({ host: 'www.vg.no', rank: null })),
    )
    const { router, m } = await setup()

    m.host.value = 'https://www.vg.no/some/path'
    void m.submit()
    await flushPromises()

    expect(createCheck).not.toHaveBeenCalled()
    expect(router.currentRoute.value.fullPath).toBe('/domains/vg.no?from=check')
  })

  it('checks a www host when no apex is ranked either', async () => {
    vi.mocked(getDomain).mockResolvedValue(domainRow({ host: 'www.vg.no', rank: null }))
    vi.mocked(createCheck).mockResolvedValue(envelope({ host: 'www.vg.no', cached: true }))
    const { m } = await setup()

    m.host.value = 'www.vg.no'
    void m.submit()
    await flushPromises()

    expect(createCheck).toHaveBeenCalledWith('www.vg.no', expect.anything())
  })

  // The plan hooks only submit(); this is the chain that relies on it — an
  // expired shareable link must redirect rather than re-scan.
  it('an expired /check/{host} link redirects instead of rechecking', async () => {
    vi.mocked(getLatestCheck).mockRejectedValue(
      new ApiProblem(
        { type: 'https://whynoipv6.com/problems/not-found', title: 'Missing', status: 404 },
        404,
      ),
    )
    vi.mocked(getDomain).mockResolvedValue(domainRow())
    const { router } = await setup('/check/vg.no')
    await flushPromises()

    expect(createCheck).not.toHaveBeenCalled()
    expect(router.currentRoute.value.fullPath).toBe('/domains/vg.no?from=check')
  })

  it('?recheck=1 runs the check on a ranked domain and cleans the URL', async () => {
    vi.mocked(getDomain).mockResolvedValue(domainRow())
    vi.mocked(createCheck).mockResolvedValue(envelope({ cached: true }))
    const { router } = await setup('/check/vg.no?recheck=1')
    await flushPromises()

    expect(createCheck).toHaveBeenCalledWith('vg.no', expect.anything())
    expect(getDomain).not.toHaveBeenCalled() // guard bypassed, no bounce back
    expect(getLatestCheck).not.toHaveBeenCalled() // stored-result read skipped
    expect(router.currentRoute.value.fullPath).toBe('/check/vg.no')
  })
})
