import type { Router } from 'vue-router'

// Applies per-route `meta: { title, description }` (§9.6) — replaces the old
// site's imperative onMounted document.title writes.

function headTag(selector: string, create: () => HTMLElement): HTMLElement {
  let tag = document.head.querySelector<HTMLElement>(selector)
  if (!tag) {
    tag = create()
    document.head.appendChild(tag)
  }
  return tag
}

function setMetaProperty(property: string, content: string): void {
  headTag(`meta[property="${property}"]`, () => {
    const el = document.createElement('meta')
    el.setAttribute('property', property)
    return el
  }).setAttribute('content', content)
}

function setMetaName(name: string, content: string): void {
  headTag(`meta[name="${name}"]`, () => {
    const el = document.createElement('meta')
    el.setAttribute('name', name)
    return el
  }).setAttribute('content', content)
}

/**
 * Data-driven document title for entity pages (domain, country, campaign,
 * live check) — call once the entity has loaded; the static route meta title
 * stays as the pre-load fallback.
 */
export function setPageTitle(title: string): void {
  document.title = `${title} - Why No IPv6`
}

/**
 * Title *and* the share tags, for content whose real meta is only known after
 * load. Blog posts are prerendered with correct per-post tags
 * (scripts/blog-plugin.ts), but the guard below then overwrites og:title and
 * description with the route's generic fallback the moment the app boots —
 * so a JS-executing crawler would read "Blog - Why No IPv6" off every post.
 * Restoring the whole set (not just the title) is what keeps the hydrated
 * head equal to the prerendered one.
 */
export function setPageMeta(title: string, description: string): void {
  const full = `${title} - Why No IPv6`
  document.title = full
  setMetaProperty('og:title', full)
  setMetaProperty('og:description', description)
  setMetaName('description', description)
  setMetaName('twitter:title', full)
  setMetaName('twitter:description', description)
}

export function installPageMeta(router: Router): void {
  router.beforeEach((to) => {
    if (to.meta.title) {
      document.title = to.meta.title
      setMetaProperty('og:title', to.meta.title)
    }
    if (to.meta.description) {
      setMetaName('description', to.meta.description)
    }
    // Per-route canonical + og:url — every path claims itself, not the homepage.
    const url = `${location.origin}${to.path}`
    headTag('link[rel="canonical"]', () => {
      const el = document.createElement('link')
      el.setAttribute('rel', 'canonical')
      return el
    }).setAttribute('href', url)
    setMetaProperty('og:url', url)
  })
}
