// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import SegmentedTabs from '@/components/SegmentedTabs.vue'

const options = [
  { value: 'sinners', label: 'Sinners' },
  { value: 'heroes', label: 'Heroes' },
]

describe('SegmentedTabs', () => {
  it('renders one button per option and marks the active one', () => {
    const wrapper = mount(SegmentedTabs, { props: { options, modelValue: 'heroes' } })
    const buttons = wrapper.findAll('button')
    expect(buttons.map((b) => b.text())).toEqual(['Sinners', 'Heroes'])
    expect(buttons[0]!.attributes('aria-pressed')).toBe('false')
    expect(buttons[1]!.attributes('aria-pressed')).toBe('true')
    expect(buttons[1]!.classes()).toContain('text-fuchsia-600')
  })

  it('emits update:modelValue with the clicked value', async () => {
    const wrapper = mount(SegmentedTabs, { props: { options, modelValue: 'heroes' } })
    await wrapper.findAll('button')[0]!.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['sinners']])
  })
})
