import { createMemoryHistory, createRouter } from 'vue-router'
import type { Component } from 'vue'
import type { Meta, Page } from '@/api'

export const emptyPage: Page = { next_cursor: null, prev_cursor: null, has_more: false }

export const meta: Meta = {
  as_of: '2026-07-11T00:00:00Z',
  generation: 20260711,
  license: 'CC-BY-NC-4.0',
}

export const emptyCollection = { items: [], page: emptyPage, meta }

/** Memory router hosting the page under test plus the routes its links target. */
export async function makeRouter(path: string, component: Component) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path, name: 'UnderTest', component },
      { path: '/domains/:domain([^/]+)', name: 'DomainDetail', component: { template: '<div />' } },
      { path: '/countries/:id', name: 'CountryDetail', component: { template: '<div />' } },
      { path: '/campaigns/:uuid', name: 'CampaignDetail', component: { template: '<div />' } },
      { path: '/:catchAll(.*)', component: { template: '<div />' } },
    ],
  })
  await router.push(path)
  await router.isReady()
  return router
}

/** Chrome partials are stubbed in every page smoke test. */
export const chromeStubs = {
  Header: { template: '<div />' },
  Footer: { template: '<div />' },
  PageIllustration: { template: '<div />' },
}
