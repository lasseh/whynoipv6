// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CampaignDetail from '@/pages/CampaignDetail.vue'
import { layoutStubs, makeRouter } from './helpers'

vi.mock('@/api', () => ({
  getCampaign: vi.fn().mockResolvedValue({
    uuid: 'f9a0a3c4-0000-0000-0000-000000000000',
    name: 'Test Campaign',
    description: 'A campaign for testing',
    source_file: null,
    tags: [],
    disabled: false,
    adoption: null,
    domains: {
      items: [],
      page: { next_cursor: null, prev_cursor: null, has_more: false },
    },
    meta: {
      as_of: '2026-07-11T00:00:00Z',
      generation: 20260711,
      license: 'CC-BY-NC-4.0',
      count: 0,
    },
  }),
  getCampaignChangelog: vi.fn().mockResolvedValue({
    items: [],
    page: { next_cursor: null, prev_cursor: null, has_more: false },
    meta: { as_of: '2026-07-11T00:00:00Z', generation: 20260711, license: 'CC-BY-NC-4.0' },
  }),
}))

describe('CampaignDetail', () => {
  it('mounts, renders the campaign header and null-adoption empty bar', async () => {
    const router = makeRouter('/campaigns/:uuid', CampaignDetail)
    await router.push('/campaigns/f9a0a3c4-0000-0000-0000-000000000000')
    await router.isReady()
    const wrapper = mount(CampaignDetail, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Test Campaign')
    expect(wrapper.text()).toContain('A campaign for testing')
    // adoption: null → Unknown badge, 0-width bar, no crash (§7.3).
    expect(wrapper.text()).toContain('Rating: Unknown')
    wrapper.unmount()
  })
})
