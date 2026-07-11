// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import DomainTable from '@/components/DomainTable.vue'
import CampaignDomainTable from '@/components/CampaignDomainTable.vue'

// The empty state must not flash while the first page is still loading —
// it renders only once loading is false and the list is genuinely empty.
describe('table empty state vs loading', () => {
  it('DomainTable suppresses the empty state while loading', () => {
    const wrapper = mount(DomainTable, { props: { domains: [], loading: true } })
    expect(wrapper.text()).not.toContain('No domains found')
  })

  it('DomainTable shows the empty state once loading settles empty', () => {
    const wrapper = mount(DomainTable, { props: { domains: [], loading: false } })
    expect(wrapper.text()).toContain('No domains found')
  })

  it('CampaignDomainTable suppresses the empty state while loading', () => {
    const wrapper = mount(CampaignDomainTable, {
      props: { domains: [], uuid: 'u-1', loading: true },
    })
    expect(wrapper.text()).not.toContain('No domains found')
  })

  it('CampaignDomainTable shows the empty state once loading settles empty', () => {
    const wrapper = mount(CampaignDomainTable, {
      props: { domains: [], uuid: 'u-1', loading: false },
    })
    expect(wrapper.text()).toContain('No domains found')
  })
})
