// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import FAQ from '@/pages/FAQ.vue'
import { layoutStubs, makeRouter } from './helpers'

describe('FAQ', () => {
  it('mounts on page 1 and switches to the rewritten API page via ?page=2', async () => {
    const router = makeRouter('/faq', FAQ)
    await router.push('/faq')
    await router.isReady()
    const wrapper = mount(FAQ, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Frequently Asked Questions')

    await router.push('/faq?page=2')
    await flushPromises()
    expect(wrapper.text()).toContain('Rules, Frequency, and API Access')
    expect(wrapper.text()).toContain('https://api.whynoipv6.com')
    expect(wrapper.text()).toContain('![IPv6](https://api.whynoipv6.com/badge/yourdomain.com.svg)')
    wrapper.unmount()
  })
})
