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

/**
 * Data-driven document title for entity pages (domain, country, campaign,
 * live check) — call once the entity has loaded; the static route meta title
 * stays as the pre-load fallback.
 */
export function setPageTitle(title: string): void {
  document.title = `${title} - Why No IPv6`
}

export function installPageMeta(router: Router): void {
  router.beforeEach((to) => {
    if (to.meta.title) {
      document.title = to.meta.title
      setMetaProperty('og:title', to.meta.title)
    }
    if (to.meta.description) {
      let tag = document.querySelector('meta[name="description"]')
      if (!tag) {
        tag = document.createElement('meta')
        tag.setAttribute('name', 'description')
        document.head.appendChild(tag)
      }
      tag.setAttribute('content', to.meta.description)
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
