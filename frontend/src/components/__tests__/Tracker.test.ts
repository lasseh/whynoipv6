// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import Tracker from '@/components/Tracker.vue'
import type { HistoryPoint, StatusValue } from '@/api'

function point(day: string, www: StatusValue): HistoryPoint {
  return {
    day,
    base: 'supported',
    www,
    ns: null,
    mx: null,
    conn: null,
    resources: null,
    classification: 'partial',
    latency_v4_ms: null,
    latency_v6_ms: null,
  }
}

function blockClasses(wrapper: ReturnType<typeof mount>): string[] {
  return wrapper
    .findAll('.group > div > div')
    .map((el) => el.classes().find((c) => c.startsWith('bg-')) ?? '')
}

describe('Tracker', () => {
  it('colors day blocks by the requested dimension, newest first', () => {
    const wrapper = mount(Tracker, {
      props: {
        points: [
          point('2026-07-09', 'supported'),
          point('2026-07-10', 'unsupported'),
          point('2026-07-11', 'no_record'),
        ],
        dimension: 'www',
        days: 3,
      },
    })
    // Row renders newest-first (index 0 = today, right edge via flex-row-reverse).
    expect(blockClasses(wrapper)).toEqual(['bg-amber-500', 'bg-pink-600', 'bg-emerald-600'])
  })

  it('reads a different dimension from the same points', () => {
    const wrapper = mount(Tracker, {
      props: { points: [point('2026-07-11', 'unsupported')], dimension: 'base', days: 1 },
    })
    expect(blockClasses(wrapper)).toEqual(['bg-emerald-600'])
  })

  it('pads short windows and renders null days as neutral blocks', () => {
    const wrapper = mount(Tracker, {
      props: {
        points: [point('2026-07-11', null)],
        dimension: 'www',
        days: 3,
      },
    })
    expect(blockClasses(wrapper)).toEqual(['bg-gray-800', 'bg-gray-800', 'bg-gray-800'])
  })

  it('empty history renders a full neutral window', () => {
    const wrapper = mount(Tracker, { props: { points: [], dimension: 'www', days: 30 } })
    const classes = blockClasses(wrapper)
    expect(classes).toHaveLength(30)
    expect(new Set(classes)).toEqual(new Set(['bg-gray-800']))
  })

  it('hover tooltip shows the day AND its status, colored by the block hue', async () => {
    const wrapper = mount(Tracker, {
      props: {
        points: [point('2026-07-24', 'unsupported')],
        dimension: 'www',
        days: 1,
        hoverEffect: true,
      },
    })
    await wrapper.find('.group > div').trigger('pointerenter', { pointerType: 'mouse' })
    const tooltip = wrapper.find('.absolute.z-10')
    expect(tooltip.exists()).toBe(true)
    expect(tooltip.text()).toContain('24 July 2026')
    expect(tooltip.text()).toContain('Missing')
    expect(tooltip.find('.rounded-full').classes()).toContain('bg-pink-600')

    await wrapper.find('.group > div').trigger('pointerleave', { pointerType: 'mouse' })
    expect(wrapper.find('.absolute.z-10').exists()).toBe(false)
  })

  it('tap toggles the tooltip; padded null days stay silent', async () => {
    const wrapper = mount(Tracker, {
      props: {
        points: [point('2026-07-24', 'supported')],
        dimension: 'www',
        days: 2,
        hoverEffect: true,
      },
    })
    const dayBlock = wrapper.findAll('.group > div')[0]!
    await dayBlock.trigger('click')
    expect(wrapper.find('.absolute.z-10').exists()).toBe(true)
    await dayBlock.trigger('click')
    expect(wrapper.find('.absolute.z-10').exists()).toBe(false)

    const padBlock = wrapper.findAll('.group > div')[1]!
    await padBlock.trigger('click')
    expect(wrapper.find('.absolute.z-10').exists()).toBe(false)
  })

  it('keeps only the newest `days` points', () => {
    const points = [
      point('2026-07-01', 'unsupported'),
      point('2026-07-02', 'supported'),
      point('2026-07-03', 'supported'),
    ]
    const wrapper = mount(Tracker, { props: { points, dimension: 'www', days: 2 } })
    expect(blockClasses(wrapper)).toEqual(['bg-emerald-600', 'bg-emerald-600'])
  })
})
