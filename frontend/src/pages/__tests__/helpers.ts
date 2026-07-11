import { createMemoryHistory, createRouter } from 'vue-router'
import type { Router } from 'vue-router'
import type { Meta, Page } from '@/api'

export const emptyPage: Page = { next_cursor: null, prev_cursor: null, has_more: false }

export const meta: Meta = {
  as_of: '2026-07-11T00:00:00Z',
  generation: 20260711,
  license: 'CC-BY-NC-4.0',
}

export const emptyCollection = { items: [], page: emptyPage, meta }

/** Minimal named-route table so page templates' router-links resolve. */
export function makeRouter(path: string, component: object): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'Home', component: { template: '<div />' } },
      { path, name: 'PageUnderTest', component },
      { path: '/domains/:domain([^/]+)', name: 'DomainDetail', component: { template: '<div />' } },
      {
        path: '/campaigns/:uuid/:domain([^/]+)',
        name: 'CampaignDomain',
        component: { template: '<div />' },
      },
      { path: '/:catchAll(.*)', name: 'CatchAll', component: { template: '<div />' } },
    ],
  })
}

export const layoutStubs = {
  Header: true,
  Footer: true,
  PageIllustration: true,
}
