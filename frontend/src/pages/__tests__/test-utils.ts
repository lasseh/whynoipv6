// Shared router/stub helpers + typed fixtures for the page smoke tests (§11.7).
import { createMemoryHistory, createRouter } from 'vue-router'
import type { Component } from 'vue'
import type { Router } from 'vue-router'
import type { CampaignDetail, DomainDetail, Meta, Page, Schemas } from '@/api'

export const emptyPage: Page = { next_cursor: null, prev_cursor: null, has_more: false }

export const meta: Meta = {
  as_of: '2026-07-11T00:00:00Z',
  generation: 20260711,
  license: 'CC-BY-NC-4.0',
}

export const emptyCollection = { items: [], page: emptyPage, meta }

const stub = { template: '<div />' }

/**
 * Memory router hosting the page under test plus every named route page
 * templates link to. `path` is the route pattern; `initial` the concrete
 * URL to start on (defaults to the pattern for static paths).
 */
export async function makeRouter(
  path: string,
  component: Component,
  initial: string = path,
): Promise<Router> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      ...(path === '/' ? [] : [{ path: '/', name: 'Home', component: stub }]),
      { path, name: 'UnderTest', component },
      { path: '/domains/:domain([^/]+)', name: 'DomainDetail', component: stub },
      { path: '/countries/:id', name: 'CountryDetail', component: stub },
      { path: '/campaigns/:uuid', name: 'CampaignDetail', component: stub },
      { path: '/campaigns/:uuid/:domain([^/]+)', name: 'CampaignDomain', component: stub },
      { path: '/:catchAll(.*)', component: stub },
    ],
  })
  await router.push(initial)
  await router.isReady()
  return router
}

/** Chrome partials are stubbed in every page smoke test. */
export const layoutStubs = {
  Header: true,
  Footer: true,
  PageIllustration: true,
}

const status = (value: Schemas['IPv6Status'] | null) => ({ value, since: null })

export const domainDetail: DomainDetail = {
  host: 'example.com',
  rank: 1337,
  kind: 'apex',
  parent: null,
  classification: 'partial',
  class_flags: [],
  gold: false,
  ipv6_only: null,
  status: {
    base: status('supported'),
    www: status('unsupported'),
    ns: status('no_record'),
    mx: status('not_applicable'),
    conn: status(null),
    resources: status(null),
  },
  informational: {
    dnssec: null,
    ptr: null,
    smtp: null,
    parity: null,
    latency_v4_ms: null,
    latency_v6_ms: null,
  },
  tld: 'com',
  country: { code: 'NO', name: 'Norway' },
  asn: { number: 2119, name: 'Telenor Norge AS' },
  dns_provider: null,
  hosting_provider: null,
  subdomain_count: 0,
  disabled: false,
  last_checked_at: '2026-07-10T12:00:00Z',
  created_at: '2024-01-01T00:00:00Z',
  meta: { as_of: '2026-07-11T00:00:00Z', generation: 20260711 },
}

export const campaignDetail: CampaignDetail = {
  uuid: '3fa85f64-5717-4562-b3fc-2c963f66afa6',
  name: 'Test Campaign',
  description: 'A campaign',
  source_file: null,
  tags: [],
  disabled: false,
  adoption: null,
  domains: { items: [], page: emptyPage },
  meta,
}

export const emptyChangelog = { items: [], page: emptyPage, meta }

export const emptyHistory = {
  host: 'example.com',
  points: [],
  meta: { retention_days: 730, as_of: '2026-07-11T00:00:00Z' },
}
