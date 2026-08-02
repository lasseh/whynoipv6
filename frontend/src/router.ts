import { createWebHistory, createRouter } from 'vue-router'
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
        'Why No IPv6 scans the top million domains daily (www, nameservers, mail) and names the giants still IPv4-only. Sinners, Heroes, Saints. Shame on them.',
    },
  },
  {
    path: '/domains',
    name: 'DomainList',
    component: () => import('@/pages/DomainList.vue'),
    meta: {
      title: 'Domain Leaderboard - Why No IPv6',
      description:
        'Every domain we crawl, ranked by Tranco and checked for IPv6 (domain, www, nameservers, mail). Filter by tier: Sinners, Heroes, Saints.',
    },
  },
  {
    path: '/domains/:domain([^/]+)/not-found',
    name: 'DomainNotFound',
    component: () => import('@/pages/DomainNotFound.vue'),
    meta: {
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
        'The complete IPv6 report card for this domain: AAAA for domain and www, nameservers, MX, and whether it actually answers over IPv6.',
    },
  },
  {
    path: '/search',
    name: 'Search',
    component: () => import('@/pages/Search.vue'),
    meta: {
      title: 'Search Results - Why No IPv6',
      description: "Search the domains we crawl: who has IPv6 and who doesn't.",
    },
  },
  {
    path: '/check/:target?',
    name: 'LiveCheck',
    component: () => import('@/pages/LiveCheck.vue'),
    meta: {
      title: 'Live IPv6 Check - Why No IPv6',
      description:
        'Run a live IPv6 check on any domain: AAAA records, nameservers, MX, and a real connection attempt over IPv6. Answers come from DNS, not our cache.',
    },
  },
  {
    path: '/metrics',
    name: 'Metrics',
    component: () => import('@/pages/Metrics.vue'),
    meta: {
      title: 'IPv6 Adoption Metrics - Why No IPv6',
      description:
        "IPv6 adoption metrics for the top million domains, charted over time: how many publish AAAA records, how many don't, and how fast that's changing (slowly).",
    },
  },
  {
    path: '/countries',
    name: 'CountryList',
    component: () => import('@/pages/CountryList.vue'),
    meta: {
      title: 'IPv6 Adoption by Country - Why No IPv6',
      description:
        'IPv6 adoption ranked by country: who leads, who trails, and where the Sinners cluster. National pride, now measurable in AAAA records.',
    },
  },
  {
    path: '/countries/:id',
    name: 'CountryDetail',
    component: () => import('@/pages/CountryDetail.vue'),
    meta: {
      title: 'Country Details - Why No IPv6',
      description:
        "How this country's top domains score on IPv6: adoption rate, the local Heroes, and the Sinners dragging the national average down.",
    },
  },
  {
    path: '/campaigns',
    name: 'CampaignList',
    component: () => import('@/pages/CampaignList.vue'),
    meta: {
      title: 'Shame Campaigns - Why No IPv6',
      description:
        'Reader-submitted lists of big-name domains, tracked daily until the AAAA records show up. Shame as a Service.',
    },
  },
  {
    path: '/campaigns/:uuid',
    name: 'CampaignDetail',
    component: () => import('@/pages/CampaignDetail.vue'),
    meta: {
      title: 'Campaign Details - Why No IPv6',
      description:
        "Every domain in this campaign and its IPv6 status: who fixed it, who hasn't, and how the percentage is coming along.",
    },
  },
  {
    path: '/campaigns/:uuid/:domain([^/]+)/not-found',
    name: 'CampaignDomainNotFound',
    component: () => import('@/pages/DomainNotFound.vue'),
    meta: {
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
        "The full IPv6 checklist for this campaign domain: AAAA, nameservers, mail, and whether it's helping or hurting the campaign's numbers.",
    },
  },
  {
    path: '/changelog',
    name: 'Changelog',
    component: () => import('@/pages/Changelog.vue'),
    meta: {
      title: 'Changelog - Why No IPv6',
      description:
        "Who fixed their IPv6 and who broke it, day by day. Every AAAA record that appeared or quietly disappeared, pulled from the crawler's daily runs.",
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
        'How the crawler works, what the checks mean, and how to get your domain removed from the list. Short answer to that last one: start using IPv6.',
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
    if (savedPosition) {
      return savedPosition
    }
    if (to.hash) {
      return { el: to.hash, behavior: 'smooth' }
    }
    return { top: 0, behavior: 'smooth' }
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

installPageMeta(router)

export default router
