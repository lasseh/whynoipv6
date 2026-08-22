// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { installPageMeta, setPageMeta, setPageTitle } from '@/composables/usePageMeta'

const meta = (sel: string) => document.head.querySelector(sel)?.getAttribute('content')

beforeEach(() => {
  document.head.innerHTML = ''
  document.title = ''
})

describe('usePageMeta', () => {
  it('suffixes the site name onto data-driven titles', () => {
    setPageTitle('Does vg.no support IPv6?')
    expect(document.title).toBe('Does vg.no support IPv6? - Why No IPv6')
  })

  it('writes the full share-tag set and updates in place on the next call', () => {
    setPageMeta('First Post', 'One description')
    setPageMeta('Second Post', 'Another description')
    expect(document.title).toBe('Second Post - Why No IPv6')
    expect(meta('meta[property="og:title"]')).toBe('Second Post - Why No IPv6')
    expect(meta('meta[property="og:description"]')).toBe('Another description')
    expect(meta('meta[name="description"]')).toBe('Another description')
    expect(meta('meta[name="twitter:title"]')).toBe('Second Post - Why No IPv6')
    // Updated, not duplicated: the head must hold exactly one tag per slot.
    expect(document.head.querySelectorAll('meta[property="og:title"]')).toHaveLength(1)
  })

  it('applies route meta and a per-route canonical on navigation', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        {
          path: '/faq',
          component: { template: '<div />' },
          meta: { title: 'FAQ - Why No IPv6', description: 'Questions, answered.' },
        },
      ],
    })
    installPageMeta(router)
    await router.push('/faq')
    await router.isReady()

    expect(document.title).toBe('FAQ - Why No IPv6')
    expect(meta('meta[name="description"]')).toBe('Questions, answered.')
    const canonical = document.head.querySelector('link[rel="canonical"]')?.getAttribute('href')
    expect(canonical).toBe(`${location.origin}/faq`)
    expect(meta('meta[property="og:url"]')).toBe(`${location.origin}/faq`)
  })
  // The defect this replaces: the route guard wrote og:title but never
  // og:description or twitter:*, so on every non-blog route the unfurl
  // disagreed with itself — an updated og:title beside index.html's
  // homepage twitter:title and description.
  it('writes the complete share-tag set on navigation, not a subset', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        {
          path: '/domains',
          component: { template: '<div />' },
          meta: { title: 'Domain Leaderboard - Why No IPv6', description: 'Every ranked domain.' },
        },
      ],
    })
    installPageMeta(router)
    await router.push('/domains')
    await router.isReady()

    const full = 'Domain Leaderboard - Why No IPv6'
    expect(document.title).toBe(full)
    expect(meta('meta[property="og:title"]')).toBe(full)
    expect(meta('meta[name="twitter:title"]')).toBe(full)
    expect(meta('meta[name="description"]')).toBe('Every ranked domain.')
    expect(meta('meta[property="og:description"]')).toBe('Every ranked domain.')
    expect(meta('meta[name="twitter:description"]')).toBe('Every ranked domain.')
  })

  // A route title already carries the suffix; re-appending it would ship
  // "FAQ - Why No IPv6 - Why No IPv6".
  it('does not double the site name on an already-suffixed route title', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        {
          path: '/faq',
          component: { template: '<div />' },
          meta: { title: 'FAQ - Why No IPv6', description: 'Questions, answered.' },
        },
      ],
    })
    installPageMeta(router)
    await router.push('/faq')
    await router.isReady()
    expect(document.title).toBe('FAQ - Why No IPv6')
  })

  // A data-driven title refines every tag the title appears in, so the
  // entity name cannot show in one and not another.
  it('keeps the title tags consistent when an entity loads', () => {
    setPageMeta('Domain Details', 'A domain report.')
    setPageTitle('Does vg.no support IPv6?')

    const full = 'Does vg.no support IPv6? - Why No IPv6'
    expect(document.title).toBe(full)
    expect(meta('meta[property="og:title"]')).toBe(full)
    expect(meta('meta[name="twitter:title"]')).toBe(full)
    // The route's description still describes the page correctly.
    expect(meta('meta[name="description"]')).toBe('A domain report.')
  })
})
