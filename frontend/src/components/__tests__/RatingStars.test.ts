// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import RatingStars from '@/components/RatingStars.vue'
import type { StatusBlock, StatusValue } from '@/api'

function block(base: StatusValue, www: StatusValue, ns: StatusValue, mx: StatusValue): StatusBlock {
  const status = (value: StatusValue) => ({ value, since: null })
  return {
    base: status(base),
    www: status(www),
    ns: status(ns),
    mx: status(mx),
    conn: status(null),
    resources: status(null),
  }
}

function starClasses(wrapper: ReturnType<typeof mount>): string[] {
  return wrapper.findAll('svg').map((svg) => svg.classes().find((c) => c.startsWith('text-')) ?? '')
}

// §7.3 (resolved OPEN-F1): supported → filled emerald, not_applicable → muted
// zinc (never penalized), unsupported/no_record/null → empty gray. Fixed
// 4-star layout, filled-first ordering.
describe('RatingStars', () => {
  it('counts supported dimensions as filled stars', () => {
    const wrapper = mount(RatingStars, {
      props: { status: block('supported', 'supported', 'unsupported', 'no_record') },
    })
    expect(starClasses(wrapper)).toEqual([
      'text-emerald-600',
      'text-emerald-600',
      'text-gray-600',
      'text-gray-600',
    ])
  })

  it('renders a muted star with tooltip for a no-MX domain', () => {
    const wrapper = mount(RatingStars, {
      props: { status: block('supported', 'supported', 'supported', 'not_applicable') },
    })
    expect(starClasses(wrapper)).toEqual([
      'text-emerald-600',
      'text-emerald-600',
      'text-emerald-600',
      'text-zinc-600',
    ])
    expect(wrapper.find('.tooltip').text()).toBe('Not applicable')
  })

  it('all-not_applicable renders four muted stars', () => {
    const wrapper = mount(RatingStars, {
      props: {
        status: block('not_applicable', 'not_applicable', 'not_applicable', 'not_applicable'),
      },
    })
    expect(starClasses(wrapper)).toEqual(Array(4).fill('text-zinc-600'))
  })

  it('never-confirmed (null) dimensions are empty stars', () => {
    const wrapper = mount(RatingStars, { props: { status: block(null, null, null, null) } })
    expect(starClasses(wrapper)).toEqual(Array(4).fill('text-gray-600'))
    expect(wrapper.find('.tooltip').exists()).toBe(false)
  })
})
