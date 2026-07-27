// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import LiveCheck from '@/pages/LiveCheck.vue'
import { ApiProblem } from '@/api/problem'
import type { CheckAccepted, CheckEnvelope } from '@/api'
import { layoutStubs, makeRouter } from './test-utils'

const createCheck = vi.fn()
const getCheck = vi.fn()

vi.mock('@/api', () => ({
  createCheck: (...args: unknown[]) => createCheck(...args),
  getCheck: (...args: unknown[]) => getCheck(...args),
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

async function mountPage() {
  const router = await makeRouter('/check', LiveCheck)
  return mount(LiveCheck, { global: { plugins: [router], stubs: layoutStubs } })
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
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('enqueues, polls every 2s, and renders the live observation result', async () => {
    createCheck.mockResolvedValue(accepted)
    getCheck.mockResolvedValue(doneEnvelope)

    const wrapper = await mountPage()
    await submitHost(wrapper, 'example.com')

    // In flight: queued message, no result yet.
    expect(wrapper.text()).toContain('Queued')
    expect(getCheck).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(getCheck).toHaveBeenCalledWith(42, expect.anything())
    expect(wrapper.text()).toContain('Live observation')
    expect(wrapper.text()).toContain('Nameservers')
    expect(wrapper.text()).toContain('Missing') // mx unsupported
    expect(wrapper.text()).not.toContain('Resolvers disagreed')
    expect(wrapper.text()).toContain('IPv6 29 ms')
    expect(wrapper.text()).not.toContain('checked recently')
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
    expect(wrapper.text()).toContain('Scanning')

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(getCheck).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Live observation')
  })

  it('renders a dedupe hit immediately with the checked-recently note', async () => {
    createCheck.mockResolvedValue({ ...doneEnvelope, cached: true })

    const wrapper = await mountPage()
    await submitHost(wrapper, 'example.com')

    expect(getCheck).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('checked recently')
    expect(wrapper.text()).toContain('Live observation')
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
    expect(wrapper.text()).toContain('saint')
    expect(wrapper.find('a[href="/domains/example.com"]').exists()).toBe(true)
  })
})
