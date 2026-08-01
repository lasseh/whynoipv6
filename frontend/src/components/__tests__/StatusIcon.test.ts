// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import StatusIcon from '@/components/StatusIcon.vue'
import type { StatusValue } from '@/api'

// All five §7.2 states render the right glyph, color class, and tooltip.
describe('StatusIcon', () => {
  const cases: Array<{
    value: StatusValue
    colorClass: string
    tooltip: string
    path: string
  }> = [
    {
      value: 'supported',
      colorClass: 'text-emerald-500',
      tooltip: 'Supported',
      path: 'M4.5 12.75l6 6 9-13.5',
    },
    {
      value: 'unsupported',
      colorClass: 'text-pink-500',
      tooltip: 'Missing',
      path: 'M6 18L18 6M6 6l12 12',
    },
    {
      value: 'no_record',
      colorClass: 'text-amber-500',
      tooltip: 'No record',
      path: 'M19.5 12h-15',
    },
    {
      value: 'not_applicable',
      colorClass: 'text-zinc-600',
      tooltip: 'Not applicable',
      path: 'M19.5 12h-15',
    },
    {
      value: null,
      colorClass: 'text-zinc-600',
      tooltip: 'Not yet checked',
      path: 'M19.5 12h-15',
    },
  ]

  it.each(cases)('$value', ({ value, colorClass, tooltip, path }) => {
    const wrapper = mount(StatusIcon, { props: { value } })
    expect(wrapper.classes()).toContain(colorClass)
    expect(wrapper.find('.tooltip').text()).toBe(tooltip)
    expect(wrapper.find('svg path').attributes('d')).toBe(path)
  })
})
