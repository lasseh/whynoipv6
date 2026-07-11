// Shared typed fixtures for page smoke tests (§11.7).
import type { CampaignDetail, DomainDetail, Meta, Page, Schemas } from '@/api'

export const page: Page = { next_cursor: null, prev_cursor: null, has_more: false }

export const meta: Meta = {
  as_of: '2026-07-11T00:00:00Z',
  generation: 20260711,
  license: 'CC-BY-NC-4.0',
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
  domains: { items: [], page },
  meta,
}

export const emptyChangelog = { items: [], page, meta }

export const emptyHistory = {
  host: 'example.com',
  points: [],
  meta: { retention_days: 730, as_of: '2026-07-11T00:00:00Z' },
}

export const layoutStubs = {
  Header: true,
  Footer: true,
  PageIllustration: true,
}
