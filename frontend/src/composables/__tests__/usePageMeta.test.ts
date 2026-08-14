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
})
