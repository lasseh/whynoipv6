// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Home from '@/pages/Home.vue'
import { chromeStubs, makeRouter } from './smoke-helpers'

describe('Home (smoke)', () => {
  it('mounts', async () => {
    const router = await makeRouter('/', Home)
    const wrapper = mount(Home, {
      global: {
        plugins: [router],
        stubs: {
          ...chromeStubs,
          HomeSaaS: { template: '<div />' },
          Searchbar: { template: '<div />' },
          TopSinners: { template: '<div />' },
          HomeDomains: { template: '<div />' },
          Notification: { template: '<div />' },
        },
      },
    })
    await flushPromises()
    expect(wrapper.exists()).toBe(true)
  })
})
