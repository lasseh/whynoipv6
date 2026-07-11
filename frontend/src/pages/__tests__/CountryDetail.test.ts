// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CountryDetail from '@/pages/CountryDetail.vue'
import { layoutStubs, makeRouter } from './helpers'

vi.mock('@/api', () => ({
  getCountry: vi.fn().mockResolvedValue({
    code: 'NO',
    name: 'Norway',
    tld: '.NO',
    sites: 100,
    v6_sites: 25,
    percent: 25,
  }),
  listCountryDomains: vi.fn().mockResolvedValue({
    items: [],
    page: { next_cursor: null, prev_cursor: null, has_more: false },
    meta: { as_of: '2026-07-11T00:00:00Z', generation: 20260711, license: 'CC-BY-NC-4.0' },
  }),
}))

describe('CountryDetail', () => {
  it('mounts and renders the country header', async () => {
    const router = makeRouter('/countries/:id', CountryDetail)
    await router.push('/countries/NO')
    await router.isReady()
    const wrapper = mount(CountryDetail, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Norway')
    expect(wrapper.text()).toContain('100 Domains')
    expect(wrapper.text()).toContain('25%')
    wrapper.unmount()
  })
})
