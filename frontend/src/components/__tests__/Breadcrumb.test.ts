// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import Breadcrumb from '@/components/Breadcrumb.vue'

const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
})

const mountTrail = (trail: Array<{ label: string; to: string }>) =>
  mount(Breadcrumb, { props: { trail }, global: { plugins: [router] } })

describe('Breadcrumb', () => {
  it('renders the implicit Home crumb plus the trail, in order', () => {
    const wrapper = mountTrail([
      { label: 'Campaigns', to: '/campaigns' },
      { label: 'NorwayGov', to: '/campaigns/abc' },
    ])
    const links = wrapper.findAll('a')
    expect(links.map((l) => l.text())).toEqual(['Home', 'Campaigns', 'NorwayGov'])
    expect(links.map((l) => l.attributes('href'))).toEqual(['/', '/campaigns', '/campaigns/abc'])
  })

  it('marks only the last crumb as the current page', () => {
    const wrapper = mountTrail([
      { label: 'Campaigns', to: '/campaigns' },
      { label: 'NorwayGov', to: '/campaigns/abc' },
    ])
    const items = wrapper.findAll('li')
    expect(items[items.length - 1]!.attributes('aria-current')).toBe('page')
    expect(items.slice(0, -1).every((li) => li.attributes('aria-current') === undefined)).toBe(true)
  })

  it('exposes the landmark for assistive tech', () => {
    const wrapper = mountTrail([{ label: 'Domains', to: '/domains' }])
    expect(wrapper.find('nav[aria-label="Breadcrumb"]').exists()).toBe(true)
  })
})
