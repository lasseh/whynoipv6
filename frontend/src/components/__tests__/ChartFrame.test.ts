// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ChartFrame from '@/components/charts/ChartFrame.vue'
import { fmtCompact } from '@/components/charts/chart'

const series = [
  { key: 'a', label: 'Alpha', color: '#10b981' },
  { key: 'b', label: 'Beta', color: '#e11d48' },
]

const mountFrame = () =>
  mount(ChartFrame, {
    props: {
      labels: ['2026-08-01', '2026-08-02', '2026-08-03'],
      series,
      values: [
        [1, 2, 3],
        [10, 20, 30],
      ],
      yMax: 40,
      formatValue: fmtCompact,
      label: 'test chart',
    },
    slots: {
      // The scales the bodies draw with, surfaced so the contract is asserted
      // rather than assumed.
      default: `<template #default="s"><rect data-test="body"
        :x="s.xAt(0)" :y="s.yAt(40)" :width="s.bandWidth" :height="s.innerH" /></template>`,
    },
  })

describe('ChartFrame', () => {
  it('scales the value axis to yMax and pins the baseline at zero', () => {
    const wrapper = mountFrame()
    // Default height 260 with 10/26 top/bottom padding leaves 224 of plot.
    const body = wrapper.find('[data-test="body"]')
    expect(body.attributes('y')).toBe('10') // yMax sits at the top padding
    expect(body.attributes('height')).toBe('224')

    const labels = wrapper.findAll('text').map((t) => t.text())
    expect(labels).toContain('0')
    expect(labels).toContain('40')
  })

  it('labels the last column so the newest day is always readable', () => {
    const texts = mountFrame()
      .findAll('text')
      .map((t) => t.text())
    expect(texts).toContain('3 Aug')
  })

  it('exposes the chart to assistive tech as a labelled image', () => {
    const svg = mountFrame().find('svg')
    expect(svg.attributes('role')).toBe('img')
    expect(svg.attributes('aria-label')).toBe('test chart')
  })

  it('toggles a series off from the legend', async () => {
    const wrapper = mountFrame()
    const alpha = wrapper.findAll('button')[0]!
    expect(alpha.attributes('aria-pressed')).toBe('true')
    await alpha.trigger('click')
    expect(alpha.attributes('aria-pressed')).toBe('false')
  })

  it('refuses to hide the last visible series', async () => {
    const wrapper = mountFrame()
    const [alpha, beta] = wrapper.findAll('button')
    await alpha!.trigger('click')
    await beta!.trigger('click')
    // An empty plot reads as a broken panel, so the second toggle is a no-op.
    expect(beta!.attributes('aria-pressed')).toBe('true')
  })
})
