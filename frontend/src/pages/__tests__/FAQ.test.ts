// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import FAQ from '@/pages/FAQ.vue'
import { layoutStubs, makeRouter } from './test-utils'

describe('FAQ', () => {
  it('mounts on the general page and switches to the API page via ?page=rules', async () => {
    const router = await makeRouter('/faq', FAQ)
    const wrapper = mount(FAQ, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Frequently Asked Questions')

    await router.push('/faq?page=rules')
    await flushPromises()
    expect(wrapper.text()).toContain('Rules, Frequency, and API Access')
    expect(wrapper.text()).toContain('https://api.whynoipv6.com')
    expect(wrapper.text()).toContain('![IPv6](https://api.whynoipv6.com/badge/yourdomain.com.svg)')
    wrapper.unmount()
  })

  it('keeps legacy numeric links working and canonicalizes the URL', async () => {
    const router = await makeRouter('/faq', FAQ)
    const wrapper = mount(FAQ, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await router.push('/faq?page=2')
    await flushPromises()
    expect(wrapper.text()).toContain('Rules, Frequency, and API Access')
    expect(router.currentRoute.value.query.page).toBe('rules')
    wrapper.unmount()
  })
})
