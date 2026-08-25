import { nextTick } from 'vue'
import { createWebHistory, createRouter, START_LOCATION } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import { installPageMeta } from '@/composables/usePageMeta'
import { TIERS } from '@/tiers'
import { BLOG_LIST_META, BLOG_POST_META } from '../scripts/blog-shared'

// The tier collections (07 §2.3): /heroes /sinners /saints are
// presets over the /domains leaderboard — one redirect per TIERS row.
const tierRoutes: RouteRecordRaw[] = TIERS.map((t) => ({
  path: `/${t.slug}`,
  redirect: { path: '/domains', query: { filter: t.slug } },
}))

const routes: RouteRecordRaw[] = [
  ...tierRoutes,
  {
    path: '/',
    name: 'Home',
    component: () => import('@/pages/Home.vue'),
    meta: {
      title: 'Why No IPv6 - IPv6 Adoption Tracker',
      description:
        'Why No IPv6 checks the Tranco top million every day for IPv6 across DNS, web, and mail. Sinners, Heroes, Saints. Shame on them.',
    },
  },
  {
    path: '/domains',
    name: 'DomainList',
    component: () => import('@/pages/DomainList.vue'),
    meta: {
      title: 'Domain Leaderboard - Why No IPv6',
      description:
        'Every ranked domain we crawl, checked for IPv6 across its apex, www, nameserver hosts, mail hosts, and web connection.',
    },
  },
  {
    path: '/domains/:domain([^/]+)/not-found',
    name: 'DomainNotFound',
    component: () => import('@/pages/DomainNotFound.vue'),
    meta: {
      noindex: true,
      title: 'Domain Not Found - Why No IPv6',
      description:
        "This domain isn't in our database: not yet crawled, outside the Tranco top million, or a typo.",
    },
  },
  {
    path: '/domains/:domain([^/]+)',
    name: 'DomainDetail',
    component: () => import('@/pages/DomainDetail.vue'),
    meta: {
      title: 'Domain Details - Why No IPv6',
      description:
        'The IPv6 report for one domain: AAAA records on the apex, www, nameserver hosts, and mail hosts, plus a real connection over IPv6.',
    },
  },
  {
    path: '/search',
    name: 'Search',
    component: () => import('@/pages/Search.vue'),
    meta: {
      title: 'Search Results - Why No IPv6',
      description: 'Search the domains we crawl and see their IPv6 status.',
    },
  },
  {
    path: '/check/:target?',
    name: 'LiveCheck',
    component: () => import('@/pages/LiveCheck.vue'),
    meta: {
      title: 'Live IPv6 Check - Why No IPv6',
      description:
        'Run a live IPv6 check on any domain: AAAA records, nameserver hosts, mail hosts, and a real connection attempt over IPv6.',
    },
  },
  {
    path: '/metrics',
    name: 'Metrics',
    component: () => import('@/pages/Metrics.vue'),
    meta: {
      title: 'IPv6 Adoption Metrics - Why No IPv6',
      description:
        'IPv6 adoption across the Tranco top million over time: apex, www, nameserver hosts, mail hosts, reachability, and page resources.',
    },
  },
  {
    path: '/countries',
    name: 'CountryList',
    component: () => import('@/pages/CountryList.vue'),
    meta: {
      title: 'IPv6 Adoption by Country - Why No IPv6',
      description:
        'Ranked domains attributed by country, measured by confirmed apex IPv6 adoption.',
    },
  },
  {
    path: '/countries/:id',
    name: 'CountryDetail',
    component: () => import('@/pages/CountryDetail.vue'),
    meta: {
      title: 'Country Details - Why No IPv6',
      description:
        'Confirmed apex IPv6 adoption for the ranked domains attributed to this country, with its Heroes and Sinners.',
    },
  },
  {
    path: '/campaigns',
    name: 'CampaignList',
    component: () => import('@/pages/CampaignList.vue'),
    meta: {
      title: 'Shame Campaigns - Why No IPv6',
      description:
        'Reader-submitted domain lists, checked by the crawler and scored with the campaign IPv6-readiness rules.',
    },
  },
  {
    path: '/campaigns/:uuid',
    name: 'CampaignDetail',
    component: () => import('@/pages/CampaignDetail.vue'),
    meta: {
      title: 'Campaign Details - Why No IPv6',
      description:
        'Every domain in this campaign and whether it passes the campaign IPv6-readiness checks.',
    },
  },
  {
    path: '/campaigns/:uuid/:domain([^/]+)/not-found',
    name: 'CampaignDomainNotFound',
    component: () => import('@/pages/DomainNotFound.vue'),
    meta: {
      noindex: true,
      title: 'Campaign Domain Not Found - Why No IPv6',
      description:
        "This domain isn't tracked in this campaign. Either it was never on the list, or that's a typo.",
    },
  },
  {
    path: '/campaigns/:uuid/:domain([^/]+)',
    name: 'CampaignDomain',
    component: () => import('@/pages/CampaignDomain.vue'),
    meta: {
      title: 'Campaign Domain - Why No IPv6',
      description:
        'The IPv6 report for one campaign domain: AAAA records, nameserver hosts, mail hosts, and web reachability.',
    },
  },
  {
    path: '/changelog',
    name: 'Changelog',
    component: () => import('@/pages/Changelog.vue'),
    meta: {
      title: 'Changelog - Why No IPv6',
      description:
        'Confirmed IPv6 changes from the crawler: who fixed something, who broke it, and when.',
    },
  },
  {
    path: '/blog',
    name: 'BlogList',
    component: () => import('@/pages/BlogList.vue'),
    // Shared with the build-time prerender (scripts/blog-shared.ts) so the
    // crawler head and the runtime head can't drift.
    meta: BLOG_LIST_META,
  },
  {
    path: '/blog/:slug([a-z0-9-]+)',
    name: 'BlogPost',
    component: () => import('@/pages/BlogPost.vue'),
    meta: BLOG_POST_META,
  },
  {
    path: '/faq',
    name: 'FAQ',
    component: () => import('@/pages/FAQ.vue'),
    meta: {
      title: 'FAQ - Why No IPv6',
      description:
        'How the crawler works, what the checks mean, and how to get your domain removed from the list. Short answer: start using IPv6.',
    },
  },
  {
    path: '/ipv4-outage',
    name: 'Ipv4Outage',
    component: () => import('@/pages/Ipv4Outage.vue'),
    meta: {
      title: 'Planned IPv4 Outages - Why No IPv6',
      description:
        'On the 6th of every month this site stops answering over IPv4 for the day, and signals it with Retry-Over-IPv6. What that means and why we do it.',
    },
  },
  {
    // Published in the crawler's User-Agent string
    // (WhyNoIPv6Bot/1.0 (+https://whynoipv6.com/bot)) — keep this path stable.
    path: '/bot',
    name: 'Bot',
    component: () => import('@/pages/Bot.vue'),
    meta: {
      title: 'WhyNoIPv6Bot - Why No IPv6',
      description:
        'What the WhyNoIPv6 crawler does to your site, where it comes from, how to verify it, and how to reach us.',
    },
  },

  // Router-level backstop for the nginx 301 map (§5) — old singular URLs that
  // slip past nginx (e.g. client-side navigation, dev server) land here.
  { path: '/domain', redirect: (to) => ({ path: '/domains', query: to.query }) },
  {
    path: '/domain/:domain([^/]+)',
    redirect: (to) => ({ path: `/domains/${String(to.params.domain)}`, query: to.query }),
  },
  { path: '/country', redirect: (to) => ({ path: '/countries', query: to.query }) },
  {
    path: '/country/:id',
    redirect: (to) => ({ path: `/countries/${String(to.params.id)}`, query: to.query }),
  },
  { path: '/campaign', redirect: (to) => ({ path: '/campaigns', query: to.query }) },
  {
    path: '/campaign/:uuid',
    redirect: (to) => ({ path: `/campaigns/${String(to.params.uuid)}`, query: to.query }),
  },
  {
    path: '/campaign/:uuid/:domain([^/]+)',
    redirect: (to) => ({
      path: `/campaigns/${String(to.params.uuid)}/${String(to.params.domain)}`,
      query: to.query,
    }),
  },

  {
    path: '/:catchAll(.*)',
    name: 'PageNotFound',
    component: () => import('@/pages/PageNotFound.vue'),
    meta: {
      noindex: true,
      title: 'Page Not Found - Why No IPv6',
      description:
        "No route to this page. Unlike a missing AAAA record, this one probably isn't deliberate.",
    },
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior(to, _from, savedPosition) {
    // scrollTo with an explicit behavior option never consults the CSS
    // media query, so reduced-motion has to be checked here.
    const behavior = window.matchMedia('(prefers-reduced-motion: reduce)').matches
      ? 'auto'
      : ('smooth' as const)
    if (savedPosition) {
      return { ...savedPosition, behavior }
    }
    if (to.hash) {
      return { el: to.hash, behavior }
    }
    return { top: 0, behavior }
  },
})

// Strip residual trailing slashes before matching — the old site *forced* them
// onto detail URLs, so inbound links like /domain/example.com/ still exist.
router.beforeEach((to) => {
  if (to.path !== '/' && to.path.endsWith('/')) {
    return { path: to.path.replace(/\/+$/, ''), query: to.query, hash: to.hash, replace: true }
  }
  return true
})

// SPA route changes swap the view under a focus point that stays in the old
// header; move focus to the main landmark so keyboard users continue from the
// new content and screen readers announce the change (WCAG 2.4.3/4.1.3).
// Skipped on the initial navigation (the browser's document focus is right)
// and on same-path query/hash changes (filters, pagination).
router.afterEach((to, from) => {
  if (from !== START_LOCATION && to.path !== from.path) {
    void nextTick(() => {
      document.getElementById('main-content')?.focus({ preventScroll: true })
    })
  }
})

installPageMeta(router)

export default router
