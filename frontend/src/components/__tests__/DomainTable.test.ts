// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import DomainTable from '@/components/DomainTable.vue'
import type { DomainSummary } from '@/api'

function row(host: string, extra: Partial<DomainSummary> = {}): DomainSummary {
  const status = (value: string | null) => ({ value, since: null })
  return {
    host,
    rank: 42,
    kind: 'apex',
    parent: null,
    classification: 'hero',
    class_flags: [],
    saint: false,
    ipv6_only: null,
    status: {
      base: status('supported'),
      www: status('supported'),
      ns: status('supported'),
      mx: status('supported'),
      conn: status(null),
      resources: status(null),
    },
    tld: 'com',
    country: { code: 'NO', name: 'Norway' },
    asn: { number: 1, name: 'AS' },
    dns_provider: null,
    hosting_provider: null,
    last_checked_at: null,
    ...extra,
  } as DomainSummary
}

const stubs = { RouterLink: { template: '<a><slot /></a>' } }

// The empty state must not flash while the first page is still loading —
// it renders only once loading is false and the list is genuinely empty.
// The error / loading / empty states moved to ListState, which wraps this at
// every call site. What is left here is the table's own contract: no rows,
// no table.
describe('table with no rows', () => {
  it('renders nothing rather than an empty shell', () => {
    const wrapper = mount(DomainTable, { props: { domains: [] } })
    expect(wrapper.find('table').exists()).toBe(false)
  })
})

// One table module, two surfaces: both carry IPv6 Only; the leaderboard
// mode adds Rank; campaign mode drops Rank and highlights the server's
// v6_ready rows (never re-derived client-side).
describe('leaderboard vs campaign surface', () => {
  it('leaderboard mode renders Rank and IPv6 Only columns', () => {
    const wrapper = mount(DomainTable, {
      props: { domains: [row('a.example')] },
      global: { stubs },
    })
    expect(wrapper.text()).toContain('Rank')
    expect(wrapper.text()).toContain('IPv6 Only')
    expect(wrapper.text()).toContain('42')
  })

  it('campaign mode drops Rank, keeps IPv6 Only, highlights v6_ready rows', () => {
    const wrapper = mount(DomainTable, {
      props: {
        domains: [
          row('ready.example', { v6_ready: true }),
          row('not.example', { v6_ready: false }),
        ],
        campaignUuid: 'u-1',
      },
      global: { stubs },
    })
    expect(wrapper.text()).not.toContain('Rank')
    expect(wrapper.text()).toContain('IPv6 Only')
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0]?.classes()).toContain('bg-emerald-900/50')
    expect(rows[1]?.classes()).not.toContain('bg-emerald-900/50')
  })
})
