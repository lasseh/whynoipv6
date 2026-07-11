// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Metrics from '@/pages/Metrics.vue'
import { chromeStubs, makeRouter } from './smoke-helpers'

describe('Metrics (smoke)', () => {
  it('mounts on the overview tab by default', async () => {
    const router = await makeRouter('/metrics', Metrics)
    const wrapper = mount(Metrics, {
      global: {
        plugins: [router],
        stubs: {
          ...chromeStubs,
          MetricCrawler: { template: '<div data-test="crawler" />' },
          MetricASN: { template: '<div data-test="asn" />' },
        },
      },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Metrics')
    expect(wrapper.find('[data-test="crawler"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="asn"]').exists()).toBe(false)
  })

  it('shows the ASN tab when ?t=asn', async () => {
    const router = await makeRouter('/metrics', Metrics)
    await router.push({ path: '/metrics', query: { t: 'asn' } })
    const wrapper = mount(Metrics, {
      global: {
        plugins: [router],
        stubs: {
          ...chromeStubs,
          MetricCrawler: { template: '<div data-test="crawler" />' },
          MetricASN: { template: '<div data-test="asn" />' },
        },
      },
    })
    await flushPromises()
    expect(wrapper.find('[data-test="asn"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="crawler"]').exists()).toBe(false)
  })
})
