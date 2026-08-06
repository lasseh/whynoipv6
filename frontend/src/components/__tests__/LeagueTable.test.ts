// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import LeagueTable from '@/components/LeagueTable.vue'

describe('LeagueTable', () => {
  it('splits the bar by IPv6 share and derives the IPv4-only remainder', () => {
    const wrapper = mount(LeagueTable, {
      props: {
        rows: [{ key: 13335, name: 'Cloudflare, Inc.', sub: 'AS13335', total: 400, v6: 300 }],
      },
    })
    expect(wrapper.text()).toContain('75.0%')
    expect(wrapper.text()).toContain('300 dual-stack')
    expect(wrapper.text()).toContain('100 IPv4-only')
    // jsdom normalises the trailing zeros off the inline width.
    expect(wrapper.find('.bg-emerald-600').attributes('style')).toContain('width: 75%')
    expect(wrapper.find('.bg-violet-950').attributes('style')).toContain('width: 25%')
  })

  it('renders a zero-total row without dividing by zero', () => {
    const wrapper = mount(LeagueTable, {
      props: { rows: [{ key: 'x', name: 'Empty', total: 0, v6: 0 }] },
    })
    expect(wrapper.text()).toContain('0.0%')
  })

  it('shows the empty text when nothing matched', () => {
    const wrapper = mount(LeagueTable, {
      props: { rows: [], emptyText: 'No DNS providers to show yet.' },
    })
    expect(wrapper.text()).toContain('No DNS providers to show yet.')
  })
})
