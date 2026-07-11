// The tier collections (07 §2.3) as data: this one table drives the
// DomainList tabs, its fetch dispatch, and the /heroes /sinners /gold
// /almost /mail routes. Adding a tier is one row here, nothing else.
import { listAlmost, listGold, listHeroes, listMail, listSinners } from '@/api'
import type { DomainSummary, Meta, Page } from '@/api'

export interface Tier {
  /** URL slug: both the route path and the ?filter= value on /domains. */
  slug: string
  label: string
  description: string
  list: (
    query: { cursor?: string | undefined },
    signal?: AbortSignal,
  ) => Promise<{ items: DomainSummary[]; page: Page; meta: Meta }>
}

export const TIERS: Tier[] = [
  {
    slug: 'sinners',
    label: 'Sinners',
    description: 'Ranked apex domains with no IPv6 at all.',
    list: listSinners,
  },
  {
    slug: 'heroes',
    label: 'Heroes',
    description: 'Domains fully reachable over IPv6.',
    list: listHeroes,
  },
  {
    slug: 'gold',
    label: 'Gold',
    description: 'Heroes that also serve their sub-resources over IPv6.',
    list: listGold,
  },
  {
    slug: 'almost',
    label: 'Almost There',
    description: 'Domains with IPv6 on the base host but missing the hero bar.',
    list: listAlmost,
  },
  {
    slug: 'mail',
    label: 'Mail',
    description: 'The mail (MX) track.',
    list: listMail,
  },
]

export function tierBySlug(slug: string | undefined): Tier {
  return TIERS.find((t) => t.slug === slug) ?? TIERS[0]!
}
