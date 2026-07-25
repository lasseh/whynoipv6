// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Home from '@/pages/Home.vue'
import { layoutStubs, makeRouter } from './test-utils'

describe('Home (smoke)', () => {
  it('mounts', async () => {
    const router = await makeRouter('/', Home)
    const wrapper = mount(Home, {
      global: {
        plugins: [router],
        stubs: {
          ...layoutStubs,
          HomeSaaS: { template: '<div />' },
          Searchbar: { template: '<div />' },
          HomeSinners: { template: '<div />' },
          HomeDomains: { template: '<div />' },
          Notification: { template: '<div />' },
        },
      },
    })
    await flushPromises()
    expect(wrapper.exists()).toBe(true)
  })
})
