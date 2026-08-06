// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import LeagueTable from '@/components/LeagueTable.vue'

describe('LeagueTable', () => {
  it('fills the bar to the IPv6 share and reports both counts', () => {
    const wrapper = mount(LeagueTable, {
      props: {
        rows: [{ key: 13335, name: 'Cloudflare, Inc.', sub: 'AS13335', total: 400, v6: 300 }],
      },
    })
    expect(wrapper.text()).toContain('75.0%')
    expect(wrapper.text()).toContain('300 of 400 domains answer over IPv6')

    const bar = wrapper.find('[role="progressbar"]')
    // jsdom normalises the trailing zeros off the inline width.
    expect(bar.attributes('style')).toContain('width: 75%')
    expect(bar.attributes('aria-valuenow')).toBe('75')
  })

  it('colours the bar on the same ramp the scatter uses', () => {
    const at = (total: number, v6: number) =>
      mount(LeagueTable, { props: { rows: [{ key: 'k', name: 'n', total, v6 }] } })
        .find('[role="progressbar"]')
        .attributes('style')

    expect(at(100, 85)).toContain('rgb(16, 185, 129)') // emerald, >= 60
    expect(at(100, 45)).toContain('rgb(245, 158, 11)') // amber, >= 40
    expect(at(100, 4)).toContain('rgb(225, 29, 72)') // rose, the rest
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
