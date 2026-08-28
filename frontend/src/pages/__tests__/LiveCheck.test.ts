// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import LiveCheck from '@/pages/LiveCheck.vue'
import { ApiProblem } from '@/api/problem'
import type { CheckAccepted, CheckEnvelope } from '@/api'
import { layoutStubs, makeRouter } from './test-utils'

const createCheck = vi.fn()
const getCheck = vi.fn()
const getLatestCheck = vi.fn()

vi.mock('@/api', () => ({
  createCheck: (...args: unknown[]) => createCheck(...args),
  getCheck: (...args: unknown[]) => getCheck(...args),
  getLatestCheck: (...args: unknown[]) => getLatestCheck(...args),
  isCheckEnvelope: (r: object) => 'cached' in r,
}))

const accepted: CheckAccepted = {
  id: 42,
  host: 'example.com',
  status: 'pending',
  created_at: '2026-07-27T12:00:00Z',
}

const doneEnvelope: CheckEnvelope = {
  id: 42,
  host: 'example.com',
  status: 'done',
  cached: false,
  created_at: '2026-07-27T12:00:00Z',
  completed_at: '2026-07-27T12:00:17Z',
  error: null,
  result: {
    checked_at: '2026-07-27T12:00:17Z',
    duration_ms: 17385,
    checks: {
      base: { status: 'supported' },
      www: { status: 'supported' },
      ns: { status: 'supported' },
      mx: { status: 'unsupported' },
      conn: { status: 'supported' },
      resources: { status: 'not_applicable' },
      tls: { status: 'supported' },
      smtp: { status: 'not_applicable' },
      parity: { status: 'supported' },
      dnssec: { status: 'unsupported' },
      ptr: { status: 'partial' },
      spf: { status: 'supported' },
    },
    latency: { v4_ms: 34, v6_ms: 29 },
  },
  confirmed: null,
}

async function mountPage(initial = '/check') {
  const router = await makeRouter('/check/:target?', LiveCheck, initial)
  const wrapper = mount(LiveCheck, { global: { plugins: [router], stubs: layoutStubs } })
  return Object.assign(wrapper, { router })
}

async function submitHost(wrapper: Awaited<ReturnType<typeof mountPage>>, host: string) {
  await wrapper.find('input').setValue(host)
  await wrapper.find('form').trigger('submit')
  await flushPromises()
}

describe('LiveCheck page', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    createCheck.mockReset()
    getCheck.mockReset()
    getLatestCheck.mockReset()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows a labelled example before the first check', async () => {
    const wrapper = await mountPage()

    expect(wrapper.text()).toContain('example.com')
    expect(wrapper.text()).toContain('Example data')
    expect(wrapper.text()).not.toContain('Example result')
    expect(wrapper.text()).not.toContain('Copy link')
  })

  it('enqueues, polls every 2s, and renders the live observation result', async () => {
    createCheck.mockResolvedValue(accepted)
    getCheck.mockResolvedValue(doneEnvelope)

    const wrapper = await mountPage()
    await submitHost(wrapper, 'example.com')

    // In flight: staged progress narration, no result yet.
    expect(wrapper.text()).toContain('Resolving DNS records')
    expect(getCheck).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(getCheck).toHaveBeenCalledWith(42, expect.anything())
    expect(wrapper.text()).toContain('Nameservers')
    expect(wrapper.text()).toContain('Missing') // mx unsupported
    expect(wrapper.text()).not.toContain('Resolvers disagreed')
    expect(wrapper.text()).toContain('IPv6 29 ms')
    expect(wrapper.text()).not.toContain('stored result')
    // resources not_applicable + conn supported → no live host remained to grade
    expect(wrapper.text()).toContain('No live external resource host remained to grade.')
  })

  it('explains resources not_applicable as not evaluated when the site has no IPv6', async () => {
    createCheck.mockResolvedValue({
      ...doneEnvelope,
      cached: true,
      result: {
        ...doneEnvelope.result!,
        checks: {
          ...doneEnvelope.result!.checks,
          conn: { status: 'unsupported' },
          resources: { status: 'not_applicable' },
        },
      },
    })

    const wrapper = await mountPage()
    await submitHost(wrapper, 'example.com')

    expect(wrapper.text()).toContain('page resources can’t be evaluated')
  })

  it('keeps polling while the job is processing', async () => {
    createCheck.mockResolvedValue(accepted)
    getCheck
      .mockResolvedValueOnce({ ...doneEnvelope, status: 'processing', result: null })
      .mockResolvedValueOnce(doneEnvelope)

    const wrapper = await mountPage()
    await submitHost(wrapper, 'example.com')

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(wrapper.text()).toContain('Resolving DNS records')

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(getCheck).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Copy link')
  })

  it('strips pasted URLs down to the hostname', async () => {
    createCheck.mockResolvedValue(accepted)
    getCheck.mockResolvedValue(doneEnvelope)

    const wrapper = await mountPage()
    await submitHost(wrapper, 'http://vg.no/')

    expect(createCheck).toHaveBeenCalledWith('vg.no', expect.anything())
    expect(wrapper.find('input').element.value).toBe('vg.no')

    createCheck.mockClear()
    const w2 = await mountPage()
    await submitHost(w2, 'https://www.vg.no:8443/some/path?q=1#frag')
    expect(createCheck).toHaveBeenCalledWith('www.vg.no', expect.anything())
  })

  it('narrates queue state and advances the stage messages over time', async () => {
    createCheck.mockResolvedValue(accepted)
    getCheck.mockResolvedValue({ ...doneEnvelope, status: 'pending', result: null })

    const wrapper = await mountPage()
    await submitHost(wrapper, 'example.com')

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(wrapper.text()).toContain('Waiting in queue')

    getCheck.mockResolvedValue({ ...doneEnvelope, status: 'processing', result: null })
    await vi.advanceTimersByTimeAsync(8_000) // elapsed ≥ 9s
    await flushPromises()
    expect(wrapper.text()).toContain('Connecting to the site over IPv6 only')
    expect(wrapper.text()).toContain('example.com')
    expect(wrapper.find('[role="progressbar"]').exists()).toBe(true)
  })

  it('cancel aborts the poll and unlocks the form', async () => {
    createCheck.mockResolvedValue(accepted)
    getCheck.mockResolvedValue({ ...doneEnvelope, status: 'processing', result: null })

    const wrapper = await mountPage()
    await submitHost(wrapper, 'example.com')

    await wrapper.find('button[type="button"]').trigger('click') // Cancel
    expect(wrapper.find('[role="progressbar"]').exists()).toBe(false)
    expect(wrapper.find('button[type="submit"]').attributes('disabled')).toBeUndefined()

    const polls = getCheck.mock.calls.length
    await vi.advanceTimersByTimeAsync(10_000)
    await flushPromises()
    expect(getCheck.mock.calls.length).toBe(polls) // polling stopped
  })

  it('renders a dedupe hit immediately with the checked-recently note', async () => {
    createCheck.mockResolvedValue({ ...doneEnvelope, cached: true })

    const wrapper = await mountPage()
    await submitHost(wrapper, 'example.com')

    expect(getCheck).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('stored result')
    expect(wrapper.text()).toContain('Copy link')
  })

  it('shows the failed envelope error', async () => {
    createCheck.mockResolvedValue(accepted)
    getCheck.mockResolvedValue({
      ...doneEnvelope,
      status: 'failed',
      result: null,
      error: 'scan aborted',
    })

    const wrapper = await mountPage()
    await submitHost(wrapper, 'example.com')
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(wrapper.text()).toContain('Check failed')
    expect(wrapper.text()).toContain('scan aborted')
  })

  it('handles 429 with a retry_after countdown', async () => {
    createCheck.mockRejectedValue(
      new ApiProblem(
        {
          type: 'https://whynoipv6.com/problems/rate-limited',
          title: 'Rate limit exceeded',
          retry_after: 30,
        },
        429,
      ),
    )

    const wrapper = await mountPage()
    await submitHost(wrapper, 'example.com')

    expect(wrapper.text()).toContain('Rate limit reached')
    expect(wrapper.text()).toContain('30s')
    expect(wrapper.find('button').attributes('disabled')).toBeDefined()

    await vi.advanceTimersByTimeAsync(30_000)
    await flushPromises()
    expect(wrapper.find('button').attributes('disabled')).toBeUndefined()
  })

  it('reflects the host into the URL so the result is linkable', async () => {
    createCheck.mockResolvedValue(accepted)
    getCheck.mockResolvedValue(doneEnvelope)

    const wrapper = await mountPage()
    await submitHost(wrapper, 'example.com')
    await flushPromises()

    expect(wrapper.router.currentRoute.value.path).toBe('/check/example.com')

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(wrapper.text()).toContain('Copy link')
  })

  it('serves a /check/{domain} link from the stored result without a recheck', async () => {
    getLatestCheck.mockResolvedValue({ ...doneEnvelope, cached: true })

    const wrapper = await mountPage('/check/example.com')
    await flushPromises()

    expect(getLatestCheck).toHaveBeenCalledWith('example.com', expect.anything())
    expect(createCheck).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('stored result')
    expect(wrapper.text()).toContain('Copy link')
    expect(wrapper.find('input').element.value).toBe('example.com')
  })

  it('auto-rechecks a /check/{domain} link with nothing stored in 7 days', async () => {
    getLatestCheck.mockRejectedValue(
      new ApiProblem({ type: 'https://whynoipv6.com/problems/not-found', title: 'Not found' }, 404),
    )
    createCheck.mockResolvedValue(accepted)
    getCheck.mockResolvedValue(doneEnvelope)

    const wrapper = await mountPage('/check/example.com')
    await flushPromises()

    expect(createCheck).toHaveBeenCalledWith('example.com', expect.anything())

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(wrapper.text()).toContain('Copy link')
  })

  it('loads a legacy /check/{id} link and upgrades the URL to the domain', async () => {
    getCheck.mockResolvedValue(doneEnvelope)

    const wrapper = await mountPage('/check/42')
    await flushPromises()

    expect(createCheck).not.toHaveBeenCalled()
    expect(getCheck).toHaveBeenCalledWith(42, expect.anything())
    expect(wrapper.text()).toContain('Copy link')
    expect(wrapper.find('input').element.value).toBe('example.com')
    expect(wrapper.router.currentRoute.value.path).toBe('/check/example.com')
  })

  it('resumes polling on a shared in-flight link', async () => {
    getCheck
      .mockResolvedValueOnce({ ...doneEnvelope, status: 'processing', result: null })
      .mockResolvedValueOnce(doneEnvelope)

    const wrapper = await mountPage('/check/42')
    await flushPromises()
    expect(wrapper.find('[role="progressbar"]').exists()).toBe(true)

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(wrapper.text()).toContain('Copy link')
  })

  it('shows the expiry note for a reaped job id', async () => {
    getCheck.mockRejectedValue(
      new ApiProblem({ type: 'https://whynoipv6.com/problems/not-found', title: 'Not found' }, 404),
    )

    const wrapper = await mountPage('/check/999')
    await flushPromises()

    expect(wrapper.text()).toContain('expired')
  })

  it('renders the confirmed block with a link to the tracked domain', async () => {
    createCheck.mockResolvedValue({
      ...doneEnvelope,
      cached: true,
      confirmed: {
        classification: 'hero',
        class_flags: [],
        saint: true,
        status: {},
        as_of: '2026-07-27T03:30:00Z',
      },
    })

    const wrapper = await mountPage()
    await submitHost(wrapper, 'example.com')

    expect(wrapper.text()).toContain('Tracked status')
    expect(wrapper.text()).toContain('hero')
    expect(wrapper.text()).toContain('Saint')
    expect(wrapper.find('a[href="/domains/example.com"]').exists()).toBe(true)
  })
})
