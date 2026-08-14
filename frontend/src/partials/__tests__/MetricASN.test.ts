// @vitest-environment jsdom
// The provider-league partial: URL-driven entity/sort state, and the
// fetch-only-what-shows contract — /asns loads only while the networks tab
// can render it, and returning to networks always refetches with the
// current sort.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import MetricASN from '@/partials/MetricASN.vue'
import { listASNs, listHostingProviders, listProviders } from '@/api'

vi.mock('@/api', () => ({
  listASNs: vi.fn(),
  listProviders: vi.fn(),
  listHostingProviders: vi.fn(),
}))

async function setup(initial: string): Promise<{ router: Router; unmount: () => void }> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/metrics', component: { template: '<div />' } }],
  })
  await router.push(initial)
  await router.isReady()
  const wrapper = mount(MetricASN, {
    global: { plugins: [router] },
    shallow: true,
  })
  await flushPromises()
  return { router, unmount: () => wrapper.unmount() }
}

beforeEach(() => {
  vi.mocked(listASNs).mockResolvedValue({ items: [] } as never)
  vi.mocked(listProviders).mockResolvedValue({ items: [] } as never)
  vi.mocked(listHostingProviders).mockResolvedValue({ items: [] } as never)
  vi.clearAllMocks()
})

describe('MetricASN', () => {
  it('fetches the fixed registries but not /asns on a dns deep link', async () => {
    const { unmount } = await setup('/metrics?e=dns')
    expect(listProviders).toHaveBeenCalledTimes(1)
    expect(listHostingProviders).toHaveBeenCalledTimes(1)
    expect(listASNs).not.toHaveBeenCalled()
    unmount()
  })

  it('fetches /asns on the networks tab with the URL sort', async () => {
    const { unmount } = await setup('/metrics?e=networks&sort=ipv6')
    expect(listASNs).toHaveBeenCalledTimes(1)
    expect(vi.mocked(listASNs).mock.calls[0]?.[0]).toEqual({ sort: 'count_v6' })
    unmount()
  })

  it('refetches with the current sort when returning to networks', async () => {
    const { router, unmount } = await setup('/metrics?e=networks')
    expect(vi.mocked(listASNs).mock.calls[0]?.[0]).toEqual({ sort: 'count_total' })

    // Toggling sort on a client-sorted tab must not refire /asns…
    await router.replace('/metrics?e=dns')
    await flushPromises()
    await router.replace('/metrics?e=dns&sort=ipv6')
    await flushPromises()
    expect(listASNs).toHaveBeenCalledTimes(1)

    // …but coming back to networks refetches with the sort chosen meanwhile.
    await router.replace('/metrics?e=networks&sort=ipv6')
    await flushPromises()
    expect(listASNs).toHaveBeenCalledTimes(2)
    expect(vi.mocked(listASNs).mock.calls[1]?.[0]).toEqual({ sort: 'count_v6' })
    unmount()
  })
})
