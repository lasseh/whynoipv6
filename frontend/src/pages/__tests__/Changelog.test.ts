// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Changelog from '@/pages/Changelog.vue'
import { layoutStubs, makeRouter } from './test-utils'

vi.mock('@/api', () => ({
  listChangelog: vi.fn().mockResolvedValue({
    items: [
      {
        ts: '2026-07-11T08:00:00Z',
        host: 'example.com',
        field: 'base',
        old_value: 'unsupported',
        new_value: 'supported',
      },
    ],
    page: { next_cursor: null, prev_cursor: null, has_more: false },
    meta: { as_of: '2026-07-11T00:00:00Z', generation: 20260711, license: 'CC-BY-NC-4.0' },
  }),
}))

describe('Changelog', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('mounts, renders derived messages, and clears its refresh interval', async () => {
    const clearSpy = vi.spyOn(window, 'clearInterval')
    const router = await makeRouter('/changelog', Changelog)
    const wrapper = mount(Changelog, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await vi.runOnlyPendingTimersAsync()
    await flushPromises()
    expect(wrapper.text()).toContain('example.com now supports IPv6 on the base domain')
    expect(wrapper.text()).toContain('Domain Changelogs')
    wrapper.unmount()
    expect(clearSpy).toHaveBeenCalled()
  })
})
