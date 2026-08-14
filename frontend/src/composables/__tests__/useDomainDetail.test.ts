// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { Router } from 'vue-router'
import { useDomainDetail } from '@/composables/useDomainDetail'
import type { DomainDetailOptions } from '@/composables/useDomainDetail'
import { getDomain, getDomainHistory, listSubdomains } from '@/api'
import type { DomainDetail, DomainSummary, HistoryPoint } from '@/api'
import { ApiProblem } from '@/api/problem'

vi.mock('@/api', () => ({
  getDomain: vi.fn(),
  getDomainHistory: vi.fn(),
  listSubdomains: vi.fn(),
}))

const domainFixture = { host: 'bad.example', subdomain_count: 0 } as unknown as DomainDetail
const historyPoint = { date: '2026-07-11' } as unknown as HistoryPoint

type Detail = ReturnType<typeof useDomainDetail>

async function setup(
  opts?: Partial<DomainDetailOptions>,
  host: () => string = () => 'bad.example',
): Promise<{
  router: Router
  detail: Detail
}> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/not-found', name: 'NotFound', component: { template: '<div />' } },
    ],
  })
  await router.push('/')
  await router.isReady()
  let detail: Detail | undefined
  const Host = defineComponent({
    setup() {
      detail = useDomainDetail(host, {
        notFoundRoute: () => ({ name: 'NotFound' }),
        fetchChangelog: () => Promise.resolve({ items: [] }),
        ...opts,
      })
      return () => h('div')
    },
  })
  mount(Host, { global: { plugins: [router] } })
  await flushPromises()
  if (!detail) throw new Error('composable did not run')
  return { router, detail }
}

describe('useDomainDetail', () => {
  beforeEach(() => {
    vi.mocked(getDomain).mockReset()
    vi.mocked(getDomainHistory).mockReset()
    vi.mocked(listSubdomains).mockReset()
  })

  it('loads the domain and the non-fatal side surfaces', async () => {
    vi.mocked(getDomain).mockResolvedValue(domainFixture)
    vi.mocked(getDomainHistory).mockResolvedValue({
      host: 'bad.example',
      points: [historyPoint],
      meta: { as_of: '2026-07-11T00:00:00Z', retention_days: 90 },
    })
    const changelog = vi.fn().mockResolvedValue({ items: [{ id: 1 }] })

    const { detail } = await setup({ fetchChangelog: changelog })
    expect(detail.domain.value).toEqual(domainFixture)
    expect(detail.changelogs.value).toEqual([{ id: 1 }])
    expect(detail.history.value).toEqual([historyPoint])
    expect(detail.error.value).toBeNull()
  })

  // Most domains have no children, and the detail response already says so —
  // the extra request is only worth making when it can return something.
  it('skips the subdomain fetch when the domain has no children', async () => {
    vi.mocked(getDomain).mockResolvedValue(domainFixture)
    vi.mocked(getDomainHistory).mockResolvedValue({
      host: 'bad.example',
      points: [],
      meta: { as_of: '2026-07-11T00:00:00Z', retention_days: 90 },
    })
    const { detail } = await setup()
    expect(listSubdomains).not.toHaveBeenCalled()
    expect(detail.subdomains.value).toEqual([])
  })

  it('loads subdomains when the domain has children', async () => {
    const childRow = { host: 'api.bad.example' } as unknown as DomainSummary
    vi.mocked(getDomain).mockResolvedValue({
      ...domainFixture,
      subdomain_count: 2,
    })
    vi.mocked(getDomainHistory).mockResolvedValue({
      host: 'bad.example',
      points: [],
      meta: { as_of: '2026-07-11T00:00:00Z', retention_days: 90 },
    })
    vi.mocked(listSubdomains).mockResolvedValue({
      items: [childRow],
      page: { next_cursor: null, has_more: false },
      meta: { as_of: '2026-07-11T00:00:00Z', count: 2 },
    } as unknown as Awaited<ReturnType<typeof listSubdomains>>)

    const { detail } = await setup()
    expect(vi.mocked(listSubdomains).mock.calls.at(-1)?.[0]).toBe('bad.example')
    expect(detail.subdomains.value).toEqual([childRow])
  })

  it('redirects to the not-found route on a 404', async () => {
    vi.mocked(getDomain).mockRejectedValue(
      new ApiProblem(
        {
          status: 404,
          title: 'Domain not found',
          type: 'https://whynoipv6.com/problems/not-found',
        },
        404,
      ),
    )
    const { router, detail } = await setup()
    expect(router.currentRoute.value.name).toBe('NotFound')
    expect(detail.error.value).toBeNull()
  })

  it('surfaces other failures as the error and skips the side fetches', async () => {
    vi.mocked(getDomain).mockRejectedValue(new ApiProblem({ status: 500, title: 'boom' }, 500))
    const { detail } = await setup()
    expect(detail.error.value?.title).toBe('boom')
    expect(getDomainHistory).not.toHaveBeenCalled()
  })

  it('leaves side surfaces empty when their fetch fails', async () => {
    vi.mocked(getDomain).mockResolvedValue(domainFixture)
    vi.mocked(getDomainHistory).mockRejectedValue(new Error('nope'))
    const { detail } = await setup({ fetchChangelog: () => Promise.reject(new Error('nope')) })
    expect(detail.domain.value).toEqual(domainFixture)
    expect(detail.changelogs.value).toEqual([])
    expect(detail.history.value).toEqual([])
    expect(detail.error.value).toBeNull()
  })

  it('refetches when the host changes (param-only navigation)', async () => {
    vi.mocked(getDomain).mockResolvedValue(domainFixture)
    vi.mocked(getDomainHistory).mockResolvedValue({
      host: 'bad.example',
      points: [],
      meta: { as_of: '2026-07-11T00:00:00Z', retention_days: 90 },
    })
    const host = ref('a.example')
    await setup(undefined, () => host.value)
    expect(vi.mocked(getDomain).mock.calls.at(-1)?.[0]).toBe('a.example')

    host.value = 'b.example'
    await flushPromises()
    expect(vi.mocked(getDomain).mock.calls.at(-1)?.[0]).toBe('b.example')
  })
})
