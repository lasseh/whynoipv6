// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ListState from '@/components/ListState.vue'
import { ApiProblem } from '@/api/problem'

const slot = { default: '<ul><li>a row</li></ul>' }

// One render order, asserted once, for the six surfaces that each used to
// re-decide it. Three of those chains disagreed: one gated the table on a
// non-empty list so its empty branch was unreachable, another passed
// `loading` but rendered no spinner at all.
describe('ListState render order', () => {
  it('renders the error card and nothing else', () => {
    const wrapper = mount(ListState, {
      props: { error: ApiProblem.from(new Error('boom')), loading: true, count: 3 },
      slots: slot,
    })
    expect(wrapper.findComponent({ name: 'ApiError' }).exists()).toBe(true)
    expect(wrapper.text()).not.toContain('a row')
  })

  it('renders the spinner while the first page is in flight', () => {
    const wrapper = mount(ListState, { props: { loading: true, count: 0 }, slots: slot })
    expect(wrapper.findComponent({ name: 'LoadingSpinner' }).exists()).toBe(true)
    expect(wrapper.text()).not.toContain('a row')
  })

  // A pagination fetch must not blank the rows already on screen.
  it('keeps the rows during a later fetch', () => {
    const wrapper = mount(ListState, { props: { loading: true, count: 3 }, slots: slot })
    expect(wrapper.findComponent({ name: 'LoadingSpinner' }).exists()).toBe(false)
    expect(wrapper.text()).toContain('a row')
  })

  it('shows the empty message once the load settles empty', () => {
    const wrapper = mount(ListState, {
      props: { loading: false, count: 0, emptyText: 'No domains found' },
      slots: slot,
    })
    expect(wrapper.text()).toContain('No domains found')
    expect(wrapper.text()).not.toContain('a row')
  })

  it('renders the list when there is one', () => {
    const wrapper = mount(ListState, { props: { loading: false, count: 3 }, slots: slot })
    expect(wrapper.text()).toContain('a row')
    expect(wrapper.text()).not.toContain('No results found')
  })

  it('never shows the empty message while still loading', () => {
    const wrapper = mount(ListState, {
      props: { loading: true, count: 0, emptyText: 'No domains found' },
      slots: slot,
    })
    expect(wrapper.text()).not.toContain('No domains found')
  })
})
