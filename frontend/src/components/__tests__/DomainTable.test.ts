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
describe('table empty state vs loading', () => {
  it('suppresses the empty state while loading', () => {
    const wrapper = mount(DomainTable, { props: { domains: [], loading: true } })
    expect(wrapper.text()).not.toContain('No domains found')
  })

  it('shows the empty state once loading settles empty', () => {
    const wrapper = mount(DomainTable, { props: { domains: [], loading: false } })
    expect(wrapper.text()).toContain('No domains found')
  })
})

// One table module, two surfaces: the leaderboard mode carries Rank and
// IPv6 Only; campaign mode drops both and highlights the server's
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

  it('campaign mode drops Rank / IPv6 Only and highlights v6_ready rows', () => {
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
    expect(wrapper.text()).not.toContain('IPv6 Only')
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0]?.classes()).toContain('bg-emerald-900/50')
    expect(rows[1]?.classes()).not.toContain('bg-emerald-900/50')
  })
})
