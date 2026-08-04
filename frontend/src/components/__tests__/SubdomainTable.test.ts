// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import SubdomainTable from '@/components/SubdomainTable.vue'
import type { DomainSummary } from '@/api'

function child(host: string, extra: Partial<DomainSummary> = {}): DomainSummary {
  const status = (value: string | null) => ({ value, since: null })
  return {
    host,
    rank: null,
    kind: 'subdomain',
    parent: 'example.com',
    classification: 'hero',
    class_flags: [],
    saint: false,
    ipv6_only: null,
    status: {
      base: status('supported'),
      www: status('not_applicable'),
      ns: status('supported'),
      mx: status('not_applicable'),
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

describe('SubdomainTable', () => {
  it('renders nothing when the domain has no children', () => {
    const wrapper = mount(SubdomainTable, { props: { subdomains: [] }, global: { stubs } })
    expect(wrapper.text()).toBe('')
  })

  it('lists each child and drops the Rank and WWW columns', () => {
    const wrapper = mount(SubdomainTable, {
      props: { subdomains: [child('api.example.com'), child('login.example.com')], total: 2 },
      global: { stubs },
    })
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.text()).toContain('api.example.com')
    expect(wrapper.text()).not.toContain('Rank')
    expect(wrapper.text()).not.toContain('WWW')
    expect(wrapper.text()).toContain('IPv6 Only')
  })

  // The column is a full height of dashes for most parents, but some children
  // do run their own mail and that verdict must stay visible.
  it('drops the Mail column when no child has a verdict', () => {
    const wrapper = mount(SubdomainTable, {
      props: { subdomains: [child('api.example.com'), child('login.example.com')], total: 2 },
      global: { stubs },
    })
    expect(wrapper.text()).not.toContain('Mail (MX)')
    expect(wrapper.findAll('thead th')).toHaveLength(4)
  })

  it('keeps the Mail column when a child runs mail', () => {
    const withMail = child('mail.example.com')
    withMail.status.mx = { value: 'unsupported', since: null }
    const wrapper = mount(SubdomainTable, {
      props: { subdomains: [child('api.example.com'), withMail], total: 2 },
      global: { stubs },
    })
    expect(wrapper.text()).toContain('Mail (MX)')
    expect(wrapper.findAll('thead th')).toHaveLength(5)
  })

  it('says the rating is unaffected, since coverage is uneven by design', () => {
    const wrapper = mount(SubdomainTable, {
      props: { subdomains: [child('api.example.com')], total: 1 },
      global: { stubs },
    })
    expect(wrapper.text()).toContain('do not affect its rating')
  })

  it('notes truncation only when more children exist than were fetched', () => {
    const one = mount(SubdomainTable, {
      props: { subdomains: [child('api.example.com')], total: 1 },
      global: { stubs },
    })
    expect(one.text()).not.toContain('Showing the first')

    const many = mount(SubdomainTable, {
      props: { subdomains: [child('api.example.com')], total: 5 },
      global: { stubs },
    })
    expect(many.text()).toContain('Showing the first 1 of 5')
  })
})
