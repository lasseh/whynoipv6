// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CampaignList from '@/pages/CampaignList.vue'
import { listCampaigns } from '@/api'
import { layoutStubs, emptyCollection, makeRouter, meta } from './test-utils'

vi.mock('@/api', () => ({
  listCampaigns: vi.fn(),
}))

describe('CampaignList (smoke)', () => {
  it('mounts with an empty list', async () => {
    vi.mocked(listCampaigns).mockResolvedValue(emptyCollection)
    const router = await makeRouter('/campaigns', CampaignList)
    const wrapper = mount(CampaignList, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Campaigns')
    expect(listCampaigns).toHaveBeenCalled()
  })

  it('renders adoption from the list row, em-dash when null', async () => {
    vi.mocked(listCampaigns).mockResolvedValue({
      items: [
        {
          uuid: '5f0f6f0a-0000-0000-0000-000000000001',
          name: 'Banks',
          description: 'Banks without IPv6',
          source_file: null,
          tags: [],
          domain_count: 12,
          adoption: { v6_ready_percent: 41.7, day: '2026-07-10' },
        },
        {
          uuid: '5f0f6f0a-0000-0000-0000-000000000002',
          name: 'Fresh',
          description: 'No stats yet',
          source_file: null,
          tags: [],
          domain_count: 3,
          adoption: null,
        },
      ],
      page: emptyCollection.page,
      meta,
    })
    const router = await makeRouter('/campaigns', CampaignList)
    const wrapper = mount(CampaignList, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Banks')
    expect(wrapper.text()).toContain('41.7%')
    expect(wrapper.text()).toContain('—%')
  })
})
