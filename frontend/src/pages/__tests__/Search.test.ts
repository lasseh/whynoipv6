// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Search from '@/pages/Search.vue'
import { searchDomains } from '@/api'
import { layoutStubs, emptyCollection, makeRouter } from './test-utils'

vi.mock('@/api', () => ({
  searchDomains: vi.fn(),
}))

describe('Search (smoke)', () => {
  it('mounts without fetching when q is absent', async () => {
    const router = await makeRouter('/search', Search)
    const wrapper = mount(Search, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Domains')
    expect(searchDomains).not.toHaveBeenCalled()
  })

  it('fetches when q is present in the URL', async () => {
    vi.mocked(searchDomains).mockResolvedValue(emptyCollection)
    const router = await makeRouter('/search', Search)
    await router.push({ path: '/search', query: { q: 'example' } })
    const wrapper = mount(Search, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()
    expect(searchDomains).toHaveBeenCalledWith(
      expect.objectContaining({ q: 'example' }),
      expect.anything(),
    )
    expect(wrapper.find('input#search').element).toBeTruthy()
  })
})
