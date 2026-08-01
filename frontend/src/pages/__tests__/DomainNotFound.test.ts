// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import DomainNotFound from '@/pages/DomainNotFound.vue'
import { layoutStubs } from './test-utils'

describe('DomainNotFound page', () => {
  it('mounts and names the missing domain', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/domains', component: { template: '<div />' } },
        { path: '/search', component: { template: '<div />' } },
        { path: '/check/:target?', component: { template: '<div />' } },
        {
          path: '/domains/:domain([^/]+)/not-found',
          name: 'DomainNotFound',
          component: DomainNotFound,
        },
      ],
    })
    await router.push('/domains/gone.example/not-found')
    await router.isReady()

    const wrapper = mount(DomainNotFound, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Domain not found')
    expect(wrapper.text()).toContain('gone.example')
    expect(warn).not.toHaveBeenCalled()
    expect(error).not.toHaveBeenCalled()
    warn.mockRestore()
    error.mockRestore()
  })
})
