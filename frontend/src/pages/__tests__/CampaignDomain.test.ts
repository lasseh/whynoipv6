// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import CampaignDomain from '@/pages/CampaignDomain.vue'
import { campaignDetail, domainDetail, emptyChangelog, emptyHistory, layoutStubs } from './fixtures'

vi.mock('@/api', () => ({
  getDomain: vi.fn(() => Promise.resolve(domainDetail)),
  getCampaign: vi.fn(() => Promise.resolve(campaignDetail)),
  getCampaignDomainChangelog: vi.fn(() => Promise.resolve(emptyChangelog)),
  getDomainHistory: vi.fn(() => Promise.resolve(emptyHistory)),
}))

describe('CampaignDomain page', () => {
  it('mounts and renders the campaign breadcrumb and domain', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})

    const uuid = campaignDetail.uuid
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/campaigns', component: { template: '<div />' } },
        { path: '/campaigns/:uuid', component: { template: '<div />' } },
        {
          path: '/campaigns/:uuid/:domain([^/]+)/not-found',
          name: 'CampaignDomainNotFound',
          component: { template: '<div />' },
        },
        {
          path: '/campaigns/:uuid/:domain([^/]+)',
          name: 'CampaignDomain',
          component: CampaignDomain,
        },
      ],
    })
    await router.push(`/campaigns/${uuid}/example.com`)
    await router.isReady()

    const wrapper = mount(CampaignDomain, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('example.com')
    expect(wrapper.text()).toContain('Test Campaign')
    expect(wrapper.text()).not.toContain('Request failed')
    expect(warn).not.toHaveBeenCalled()
    expect(error).not.toHaveBeenCalled()
    warn.mockRestore()
    error.mockRestore()
  })
})
