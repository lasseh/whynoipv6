// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import LeagueTable from '@/components/LeagueTable.vue'
import { shareColor } from '@/components/charts/chart'

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
    // Asserted against shareColor itself, not against literal hexes: the
    // invariant is that the league and the scatter agree, and pinning the
    // palette here just means re-editing this test every time it is retuned.
    const rgb = (hex: string) => {
      const n = parseInt(hex.slice(1), 16)
      return `rgb(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255})`
    }
    const at = (total: number, v6: number) =>
      mount(LeagueTable, { props: { rows: [{ key: 'k', name: 'n', total, v6 }] } })
        .find('[role="progressbar"]')
        .attributes('style')

    for (const pct of [85, 45, 25, 4]) {
      expect(at(100, pct)).toContain(rgb(shareColor(pct)))
    }
    // And the ramp really does distinguish the bands, so the loop above is not
    // trivially satisfied by one colour.
    expect(new Set([85, 45, 25, 4].map(shareColor)).size).toBe(4)
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
