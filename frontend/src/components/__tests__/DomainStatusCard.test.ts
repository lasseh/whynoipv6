// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import DomainStatusCard from '@/components/DomainStatusCard.vue'
import type { DomainDetail, HistoryPoint, StatusValue } from '@/api'
import { domainDetail } from '@/pages/__tests__/test-utils'

const point: HistoryPoint = {
  day: '2026-07-10',
  base: 'supported',
  www: 'supported',
  ns: 'supported',
  mx: 'supported',
  conn: 'supported',
  resources: 'not_applicable',
  classification: 'hero',
  latency_v4_ms: null,
  latency_v6_ms: null,
}

function mountCard(conn: StatusValue, resources: StatusValue) {
  const domain: DomainDetail = {
    ...domainDetail,
    status: {
      ...domainDetail.status,
      conn: { value: conn, since: null },
      resources: { value: resources, since: null },
    },
  }
  return mount(DomainStatusCard, {
    props: { domain, history: [point] },
    global: { stubs: { RouterLink: true } },
  })
}

async function expandIPv6Only(wrapper: ReturnType<typeof mountCard>) {
  const row = wrapper.findAll('button').find((b) => b.text().includes('IPv6-only'))
  await row!.trigger('click')
}

function mountKind(kind: DomainDetail['kind'], parent: string | null) {
  const domain: DomainDetail = { ...domainDetail, host: 'psapi.nrk.no', kind, parent }
  return mount(DomainStatusCard, {
    props: { domain, history: [point] },
    global: { stubs: { RouterLink: true } },
  })
}

// www.<subdomain> is never looked up — the engine stores a fixed
// not_applicable — so showing the row would invent a hostname.
describe('DomainStatusCard www row for subdomains', () => {
  it('drops the www row and its star on a subdomain', () => {
    const wrapper = mountKind('subdomain', 'nrk.no')
    expect(wrapper.text()).not.toContain('www.psapi.nrk.no')
    expect(wrapper.findAll('svg[viewBox="0 0 22 20"]')).toHaveLength(3)
  })

  it('describes the host rather than "the apex domain"', () => {
    const wrapper = mountKind('subdomain', 'nrk.no')
    expect(wrapper.text()).toContain('This hostname publishes an AAAA record')
    expect(wrapper.text()).not.toContain('The apex domain publishes')
  })

  it('keeps the www row and four stars on an apex', () => {
    const wrapper = mountKind('apex', null)
    expect(wrapper.text()).toContain('www.psapi.nrk.no')
    expect(wrapper.findAll('svg[viewBox="0 0 22 20"]')).toHaveLength(4)
    expect(wrapper.text()).toContain('The apex domain publishes')
  })
})

// "Not applicable" on resources is ambiguous without the connection check:
// discovery only runs when the site loads over IPv6, so the same value means
// either "no external dependencies" or "couldn't evaluate".
describe('DomainStatusCard resources description', () => {
  it('explains not_applicable as no external deps when the site loads over IPv6', async () => {
    const wrapper = mountCard('supported', 'not_applicable')
    await expandIPv6Only(wrapper)
    expect(wrapper.text()).toContain('no resources from external hosts')
  })

  it('explains not_applicable as not evaluated when the site has no IPv6', async () => {
    const wrapper = mountCard('unsupported', 'not_applicable')
    await expandIPv6Only(wrapper)
    expect(wrapper.text()).toContain('page resources can’t be evaluated')
  })

  it('keeps the standard description otherwise', async () => {
    const wrapper = mountCard('supported', 'supported')
    await expandIPv6Only(wrapper)
    expect(wrapper.text()).toContain('Scripts, fonts, and images load from IPv6-capable hosts.')
  })
})
