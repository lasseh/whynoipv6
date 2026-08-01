// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import InformationalCard from '@/components/InformationalCard.vue'
import type { DomainDetail } from '@/api'
import { domainDetail } from '@/pages/__tests__/test-utils'

type Informational = DomainDetail['informational']

const empty: Informational = {
  dnssec: null,
  ptr: null,
  smtp: null,
  parity: null,
  latency_v4_ms: null,
  latency_v6_ms: null,
}

const mountCard = (informational: Informational) =>
  mount(InformationalCard, { props: { informational } })

describe('InformationalCard', () => {
  it('renders the four advisory dimensions with their verdicts', () => {
    const text = mountCard(domainDetail.informational).text()
    expect(text).toContain('DNSSEC')
    expect(text).toContain('Reverse DNS')
    expect(text).toContain('SMTP over IPv6')
    expect(text).toContain('Content parity')
    expect(text).toContain('Partial') // ptr
    expect(text).toContain('Missing') // parity
  })

  // A masked null means the observation was error/inconsistent — the domain
  // was scanned, so "Not checked" (the live-check wording) would be wrong.
  it('labels a masked null as no result rather than not checked', () => {
    const text = mountCard({ ...empty, ptr: 'supported' }).text()
    expect(text).toContain('No result')
    expect(text).not.toContain('Not checked')
  })

  it('renders nothing when every value is masked or absent', () => {
    expect(mountCard(empty).text()).toBe('')
  })

  it('reports the latency gap when both legs are measured', () => {
    expect(mountCard(domainDetail.informational).text()).toContain('IPv6 is 116 ms faster')
    expect(mountCard({ ...empty, latency_v4_ms: 20, latency_v6_ms: 130 }).text()).toContain(
      'IPv6 is 110 ms slower',
    )
  })

  // 3 averaged TTFB samples; a small delta is noise, not an IPv6 verdict.
  it('calls a within-noise difference on par', () => {
    expect(mountCard({ ...empty, latency_v4_ms: 100, latency_v6_ms: 96 }).text()).toContain(
      'on par',
    )
  })

  it('shows a one-sided measurement without a verdict', () => {
    const text = mountCard({ ...empty, latency_v4_ms: 283, latency_v6_ms: null }).text()
    expect(text).toContain('IPv4 283 ms · IPv6 —')
    expect(text).not.toContain('faster')
    expect(text).not.toContain('slower')
  })
})
