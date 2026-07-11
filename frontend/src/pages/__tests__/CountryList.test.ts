// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CountryList from '@/pages/CountryList.vue'
import { listCountries } from '@/api'
import { chromeStubs, emptyCollection, makeRouter, meta } from './smoke-helpers'

vi.mock('@/api', () => ({
  listCountries: vi.fn(),
}))

describe('CountryList (smoke)', () => {
  it('mounts with an empty list', async () => {
    vi.mocked(listCountries).mockResolvedValue(emptyCollection)
    const router = await makeRouter('/countries', CountryList)
    const wrapper = mount(CountryList, {
      global: { plugins: [router], stubs: chromeStubs },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Country List')
    expect(listCountries).toHaveBeenCalled()
  })

  it('renders a country card with served percent (no client ÷10)', async () => {
    vi.mocked(listCountries).mockResolvedValue({
      items: [{ code: 'NO', name: 'Norway', tld: '.NO', sites: 100, v6_sites: 25, percent: 24.97 }],
      page: emptyCollection.page,
      meta,
    })
    const router = await makeRouter('/countries', CountryList)
    const wrapper = mount(CountryList, {
      global: { plugins: [router], stubs: chromeStubs },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Norway')
    expect(wrapper.text()).toContain('24.97%')
  })
})
