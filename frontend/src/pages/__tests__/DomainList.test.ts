// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import DomainList from '@/pages/DomainList.vue'
import { listHeroes, listSinners } from '@/api'
import { layoutStubs, emptyCollection, makeRouter } from './test-utils'

vi.mock('@/api', () => ({
  listSinners: vi.fn(),
  listHeroes: vi.fn(),
  listGold: vi.fn(),
  listAlmost: vi.fn(),
  listMail: vi.fn(),
}))

describe('DomainList (smoke)', () => {
  it('mounts and fetches the default sinners tier', async () => {
    vi.mocked(listSinners).mockResolvedValue(emptyCollection)
    vi.mocked(listHeroes).mockResolvedValue(emptyCollection)
    const router = await makeRouter('/domains', DomainList)
    const wrapper = mount(DomainList, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Unmasking the Top 1M Websites of the World')
    expect(listSinners).toHaveBeenCalled()
    expect(listHeroes).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('No domains found')
  })
})
