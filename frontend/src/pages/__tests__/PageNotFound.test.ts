// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import PageNotFound from '@/pages/PageNotFound.vue'
import { layoutStubs } from './test-utils'

describe('PageNotFound page', () => {
  it('mounts and renders the 404 copy', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/:catchAll(.*)', name: 'PageNotFound', component: PageNotFound },
      ],
    })
    await router.push('/nope')
    await router.isReady()

    const wrapper = mount(PageNotFound, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()

    expect(wrapper.text()).toContain("Shame on us. This page doesn't resolve.")
    expect(warn).not.toHaveBeenCalled()
    expect(error).not.toHaveBeenCalled()
    warn.mockRestore()
    error.mockRestore()
  })
})
