// @vitest-environment jsdom
// The keyboard path onto the charts' hover state (WCAG 2.1.1): focus and
// arrow keys must reach every per-point value the pointer can.
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ScatterChart from '@/components/charts/ScatterChart.vue'
import ChartFrame from '@/components/charts/ChartFrame.vue'
import Tracker from '@/components/Tracker.vue'
import { fmtCompact } from '@/components/charts/chart'
import type { HistoryPoint } from '@/api'

describe('ScatterChart keyboard access', () => {
  const wrapper = () =>
    mount(ScatterChart, {
      props: {
        points: [
          { key: 1, label: 'Alpha Net', sub: 'AS1', x: 1000, y: 40 },
          { key: 2, label: 'Beta Net', sub: 'AS2', x: 5000, y: 5 },
        ],
        label: 'test scatter',
      },
    })

  it('makes every dot a labelled focus stop', () => {
    const circles = wrapper().findAll('circle')
    expect(circles).toHaveLength(2)
    for (const c of circles) {
      expect(c.attributes('tabindex')).toBe('0')
      expect(c.attributes('role')).toBe('img')
    }
    expect(circles[0]!.attributes('aria-label')).toBe('Alpha Net, AS1: 1K domains, 40.0% IPv6')
  })

  it('drives the tooltip from focus and clears it on blur', async () => {
    const w = wrapper()
    const dot = w.findAll('circle')[1]!
    await dot.trigger('focus')
    expect(w.text()).toContain('Beta Net')
    expect(w.text()).toContain('5.0% IPv6')
    await dot.trigger('blur')
    expect(w.text()).not.toContain('5.0% IPv6')
  })
})

describe('ChartFrame keyboard access', () => {
  const wrapper = () =>
    mount(ChartFrame, {
      props: {
        labels: ['2026-08-01', '2026-08-02', '2026-08-03'],
        series: [{ key: 'a', label: 'Alpha', color: '#10b981' }],
        values: [[1, 2, 3]],
        yMax: 40,
        formatValue: fmtCompact,
        label: 'test chart',
      },
    })

  it('opens on the newest day and steps older with ArrowLeft', async () => {
    const w = wrapper()
    const root = w.find('[role="application"]')
    expect(root.attributes('tabindex')).toBe('0')

    await root.trigger('keydown.left')
    expect(w.text()).toContain('3 Aug') // first press lands on the newest day
    await root.trigger('keydown.left')
    expect(w.text()).toContain('2 Aug')
    await root.trigger('keydown.right')
    expect(w.text()).toContain('3 Aug')
  })

  it('dismisses the readout with Escape', async () => {
    const w = wrapper()
    const root = w.find('[role="application"]')
    await root.trigger('keydown.left')
    expect(w.find('[aria-live="polite"]').exists()).toBe(true)
    await root.trigger('keydown.esc')
    expect(w.find('[aria-live="polite"]').exists()).toBe(false)
  })
})

describe('Tracker keyboard access', () => {
  function point(day: string): HistoryPoint {
    return {
      day,
      base: 'supported',
      www: 'supported',
      ns: null,
      mx: null,
      conn: null,
      resources: null,
      classification: 'partial',
      latency_v4_ms: null,
      latency_v6_ms: null,
    }
  }

  const wrapper = (hoverEffect: boolean) =>
    mount(Tracker, {
      props: {
        points: [point('2026-08-01'), point('2026-08-02')],
        dimension: 'base' as const,
        days: 5,
        hoverEffect,
      },
    })

  it('is one focus stop whose arrows walk the real days, newest first', async () => {
    const w = wrapper(true)
    const row = w.find('[role="application"]')
    expect(row.attributes('tabindex')).toBe('0')

    await row.trigger('keydown.left')
    expect(w.text()).toContain('2 August 2026') // newest real day
    await row.trigger('keydown.left')
    expect(w.text()).toContain('1 August 2026')
    // The clamp stops at the oldest real day; padded no-data blocks are never reached.
    await row.trigger('keydown.left')
    expect(w.text()).toContain('1 August 2026')
    await row.trigger('keydown.esc')
    expect(w.find('[aria-live="polite"]').exists()).toBe(false)
  })

  it('stays a plain graphic without hoverEffect', () => {
    expect(wrapper(false).find('[role="application"]').exists()).toBe(false)
  })
})
