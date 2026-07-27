import { createWebHistory, createRouter } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import { installPageMeta } from '@/composables/usePageMeta'
import { TIERS } from '@/tiers'

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
      title: 'Why No IPv6? - IPv6 Adoption Tracker',
      description:
        'Track IPv6 adoption across the web. See which domains support IPv6 and which still need to catch up.',
    },
  },
  {
    path: '/domains',
    name: 'DomainList',
    component: () => import('@/pages/DomainList.vue'),
    meta: {
      title: 'Domains - Why No IPv6?',
      description: 'Browse all domains and their IPv6 support status.',
    },
  },
  {
    path: '/domains/:domain([^/]+)/not-found',
    name: 'DomainNotFound',
    component: () => import('@/pages/DomainNotFound.vue'),
    meta: {
      title: 'Domain Not Found - Why No IPv6?',
      description: 'The requested domain could not be found in our database.',
    },
  },
  {
    path: '/domains/:domain([^/]+)',
    name: 'DomainDetail',
    component: () => import('@/pages/DomainDetail.vue'),
    meta: {
      title: 'Domain Details - Why No IPv6?',
      description: 'Detailed IPv6 support information for this domain.',
    },
  },
  {
    path: '/search',
    name: 'Search',
    component: () => import('@/pages/Search.vue'),
    meta: {
      title: 'Search Results - Why No IPv6?',
      description: 'Search results for domain IPv6 support.',
    },
  },
  {
    path: '/check',
    name: 'LiveCheck',
    component: () => import('@/pages/LiveCheck.vue'),
    meta: {
      title: 'Live IPv6 Check - Why No IPv6?',
      description: 'Run a live IPv6 check on any domain — DNS, mail, and real connectivity.',
    },
  },
  {
    path: '/metrics',
    name: 'Metrics',
    component: () => import('@/pages/Metrics.vue'),
    meta: {
      title: 'Metrics - Why No IPv6?',
      description: 'IPv6 adoption metrics and statistics.',
    },
  },
  {
    path: '/countries',
    name: 'CountryList',
    component: () => import('@/pages/CountryList.vue'),
    meta: {
      title: 'Countries - Why No IPv6?',
      description: 'IPv6 adoption by country.',
    },
  },
  {
    path: '/countries/:id',
    name: 'CountryDetail',
    component: () => import('@/pages/CountryDetail.vue'),
    meta: {
      title: 'Country Details - Why No IPv6?',
      description: 'IPv6 adoption details for this country.',
    },
  },
  {
    path: '/campaigns',
    name: 'CampaignList',
    component: () => import('@/pages/CampaignList.vue'),
    meta: {
      title: 'Campaigns - Why No IPv6?',
      description: 'IPv6 adoption campaigns and initiatives.',
    },
  },
  {
    path: '/campaigns/:uuid',
    name: 'CampaignDetail',
    component: () => import('@/pages/CampaignDetail.vue'),
    meta: {
      title: 'Campaign Details - Why No IPv6?',
      description: 'Detailed information about this IPv6 campaign.',
    },
  },
  {
    path: '/campaigns/:uuid/:domain([^/]+)/not-found',
    name: 'CampaignDomainNotFound',
    component: () => import('@/pages/DomainNotFound.vue'),
    meta: {
      title: 'Campaign Domain Not Found - Why No IPv6?',
      description: 'The requested domain could not be found in this campaign.',
    },
  },
  {
    path: '/campaigns/:uuid/:domain([^/]+)',
    name: 'CampaignDomain',
    component: () => import('@/pages/CampaignDomain.vue'),
    meta: {
      title: 'Campaign Domain - Why No IPv6?',
      description: 'Domain details within this campaign.',
    },
  },
  {
    path: '/changelog',
    name: 'Changelog',
    component: () => import('@/pages/Changelog.vue'),
    meta: {
      title: 'Changelog - Why No IPv6?',
      description: 'Recent changes and updates to IPv6 support tracking.',
    },
  },
  {
    path: '/faq',
    name: 'FAQ',
    component: () => import('@/pages/FAQ.vue'),
    meta: {
      title: 'FAQ - Why No IPv6?',
      description: 'Frequently asked questions about IPv6 adoption.',
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
      title: 'Page Not Found - Why No IPv6?',
      description: 'The page you are looking for could not be found.',
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
