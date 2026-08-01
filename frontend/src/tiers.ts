// The tier collections (07 §2.3) as data: this one table drives the
// DomainList tabs, its fetch dispatch, and the per-tier redirect routes
// (/sinners /heroes /saints /almost-heroes). Adding a tier is one row
// here, nothing else.
import { listAlmostHeroes, listHeroes, listSaints, listSinners } from '@/api'
import type { DomainSummary, Meta, Page } from '@/api'

export interface Tier {
  /** URL slug: both the route path and the ?filter= value on /domains. */
  slug: string
  label: string
  /** URL-only tier: resolvable via ?filter= and its route, but no tab. */
  hidden?: boolean
  list: (
    query: { cursor?: string | undefined },
    signal?: AbortSignal,
  ) => Promise<{ items: DomainSummary[]; page: Page; meta: Meta }>
}

export const TIERS: Tier[] = [
  {
    slug: 'sinners',
    label: 'Sinners',
    list: listSinners,
  },
  {
    slug: 'heroes',
    label: 'Heroes',
    list: listHeroes,
  },
  {
    slug: 'saints',
    label: 'Saints',
    list: listSaints,
  },
  {
    slug: 'almost-heroes',
    label: 'Almost Heroes',
    hidden: true, // URL-only for now — no tab until the advocacy surface ships
    list: listAlmostHeroes,
  },
]

export function tierBySlug(slug: string | undefined): Tier {
  return TIERS.find((t) => t.slug === slug) ?? TIERS[0]!
}
