// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import DomainDetail from '@/pages/DomainDetail.vue'
import { domainDetail, emptyChangelog, emptyHistory, layoutStubs } from './fixtures'

vi.mock('@/api', () => ({
  getDomain: vi.fn(() => Promise.resolve(domainDetail)),
  getDomainChangelog: vi.fn(() => Promise.resolve(emptyChangelog)),
  getDomainHistory: vi.fn(() => Promise.resolve(emptyHistory)),
}))

describe('DomainDetail page', () => {
  it('mounts and renders the domain without errors', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/domains', component: { template: '<div />' } },
        {
          path: '/domains/:domain([^/]+)/not-found',
          name: 'DomainNotFound',
          component: { template: '<div />' },
        },
        { path: '/domains/:domain([^/]+)', name: 'DomainDetail', component: DomainDetail },
      ],
    })
    await router.push('/domains/example.com')
    await router.isReady()

    const wrapper = mount(DomainDetail, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('example.com')
    expect(wrapper.text()).toContain('Provider: Telenor Norge AS (AS2119)')
    expect(wrapper.text()).toContain('Rank: 1337')
    expect(wrapper.text()).toContain('Last checked: 10 July 2026')
    expect(wrapper.text()).not.toContain('Request failed')
    expect(warn).not.toHaveBeenCalled()
    expect(error).not.toHaveBeenCalled()
    warn.mockRestore()
    error.mockRestore()
  })
})
